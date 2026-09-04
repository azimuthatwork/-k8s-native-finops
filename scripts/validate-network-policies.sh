#!/bin/bash
# Network Policy Validation for Phase 0
# Tests critical network paths for the observability platform
# Usage: ./scripts/validate-network-policies.sh [--verbose] [--namespace=<ns>]

set -euo pipefail

VERBOSE=false
TARGET_NAMESPACE=""

while [[ $# -gt 0 ]]; do
    case $1 in
        --verbose)
            VERBOSE=true
            shift
            ;;
        --namespace=*)
            TARGET_NAMESPACE="${1#*=}"
            shift
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_pass() { echo -e "${GREEN}[PASS]${NC} $1"; }
log_fail() { echo -e "${RED}[FAIL]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }

# Test a single connection
test_connection() {
    local name=$1
    local namespace=$2
    local url=$3
    local expected_code=${4:-200}
    local timeout=${5:-10}
    
    local pod_name="net-test-$(echo "$name" | tr ' ' '-' | tr '[:upper:]' '[:lower:]' | tr -cd 'a-z0-9-')-$(date +%s%N)"
    
    # Create test pod
    cat <<EOF | kubectl apply -f - >/dev/null 2>&1
apiVersion: v1
kind: Pod
metadata:
  name: $pod_name
  namespace: $namespace
  labels:
    test: network-policy-validation
spec:
  restartPolicy: Never
  serviceAccountName: default
  containers:
  - name: curl
    image: curlimages/curl:latest
    command: ["sleep", "60"]
    resources:
      requests:
        memory: "10Mi"
        cpu: "10m"
      limits:
        memory: "20Mi"
        cpu: "50m"
EOF
    
    # Wait for pod ready
    if ! kubectl wait --for=condition=Ready "pod/$pod_name" -n "$namespace" --timeout=30s >/dev/null 2>&1; then
        log_fail "$name: Test pod failed to start"
        kubectl delete pod "$pod_name" -n "$namespace" --grace-period=0 --force >/dev/null 2>&1 || true
        return 1
    fi
    
    # Execute curl test
    local result
    result=$(kubectl exec -n "$namespace" "$pod_name" -- \
        curl -s -o /dev/null -w "%{http_code}" --max-time "$timeout" "$url" 2>/dev/null || echo "000")
    
    # Cleanup
    kubectl delete pod "$pod_name" -n "$namespace" --grace-period=0 --force >/dev/null 2>&1 || true
    
    if [[ "$result" == "$expected_code" ]] || [[ "$result" =~ ^2[0-9]{2}$ ]]; then
        log_pass "$name: HTTP $result"
        return 0
    else
        log_fail "$name: HTTP $result (expected $expected_code)"
        return 1
    fi
}

# Test DNS resolution
test_dns() {
    local name=$1
    local namespace=$2
    local hostname=$3
    
    local pod_name="dns-test-$(echo "$name" | tr ' ' '-' | tr '[:upper:]' '[:lower:]' | tr -cd 'a-z0-9-')-$(date +%s%N)"
    
    cat <<EOF | kubectl apply -f - >/dev/null 2>&1
apiVersion: v1
kind: Pod
metadata:
  name: $pod_name
  namespace: $namespace
  labels:
    test: network-policy-validation
spec:
  restartPolicy: Never
  containers:
  - name: dnsutils
    image: registry.k8s.io/e2e-test-images/dnsutils:1.3
    command: ["sleep", "60"]
EOF
    
    kubectl wait --for=condition=Ready "pod/$pod_name" -n "$namespace" --timeout=30s >/dev/null 2>&1 || true
    
    local result
    result=$(kubectl exec -n "$namespace" "$pod_name" -- nslookup "$hostname" 2>/dev/null | grep -c "Name:" || echo "0")
    
    kubectl delete pod "$pod_name" -n "$namespace" --grace-period=0 --force >/dev/null 2>&1 || true
    
    if [[ $result -gt 0 ]]; then
        log_pass "$name: DNS resolution successful"
        return 0
    else
        log_fail "$name: DNS resolution failed"
        return 1
    fi
}

main() {
    log_info "Starting Network Policy Validation..."
    echo ""
    
    local failed=0
    
    # =========================================================================
    # Object Storage Egress
    # =========================================================================
    log_info "=== Testing Object Storage Egress ==="
    
    local storage_endpoints=()
    # Detect cloud provider
    if kubectl get nodes -o jsonpath='{.items[0].spec.providerID}' 2>/dev/null | grep -q "aws"; then
        storage_endpoints+=("s3.amazonaws.com:443" "s3.us-east-1.amazonaws.com:443")
    elif kubectl get nodes -o jsonpath='{.items[0].spec.providerID}' 2>/dev/null | grep -q "gce"; then
        storage_endpoints+=("storage.googleapis.com:443")
    elif kubectl get nodes -o jsonpath='{.items[0].spec.providerID}' 2>/dev/null | grep -q "azure"; then
        storage_endpoints+=("blob.core.windows.net:443")
    else
        storage_endpoints+=("s3.amazonaws.com:443")  # Default to S3
    fi
    
    for ns in monitoring observability cost-management; do
        for endpoint in "${storage_endpoints[@]}"; do
            IFS=':' read -r host port <<< "$endpoint"
            if ! test_connection "Egress: $ns -> $host" "$ns" "https://$host" 200 15; then
                failed=$((failed + 1))
            fi
        done
    done
    
    echo ""
    
    # =========================================================================
    # Inter-Namespace Scraping
    # =========================================================================
    log_info "=== Testing Inter-Namespace Scraping ==="
    
    # Alloy DaemonSet -> Prometheus
    if ! test_connection "Alloy DaemonSet -> Prometheus" "monitoring" "http://prometheus-operated.monitoring:9090/-/healthy"; then
        failed=$((failed + 1))
    fi
    
    # Prometheus -> kube-state-metrics
    if ! test_connection "Prometheus -> kube-state-metrics" "monitoring" "http://kube-state-metrics.monitoring:8080/metrics"; then
        failed=$((failed + 1))
    fi
    
    # Prometheus -> cAdvisor (via Alloy DaemonSet)
    if ! test_connection "Prometheus -> cAdvisor (Alloy DS)" "monitoring" "http://alloy-daemonset.monitoring:4194/metrics"; then
        failed=$((failed + 1))
    fi
    
    # Alloy Central -> OpenCost (will be deployed in Phase 1.3)
    log_warn "Alloy Central -> OpenCost: Deferred to Phase 1.3 (OpenCost not yet deployed)"
    
    echo ""
    
    # =========================================================================
    # Thanos Components Connectivity
    # =========================================================================
    log_info "=== Testing Thanos Components Connectivity ==="
    
    # Sidecar -> Store
    if ! test_connection "Thanos Sidecar -> Store" "monitoring" "http://thanos-store.monitoring:10901" 200; then
        failed=$((failed + 1))
    fi
    
    # Query Frontend -> Grafana (reverse: Grafana -> Query)
    if ! test_connection "Grafana -> Thanos Query" "observability" "http://thanos-query.monitoring:10902/-/healthy"; then
        failed=$((failed + 1))
    fi
    
    echo ""
    
    # =========================================================================
    # Loki Components Connectivity
    # =========================================================================
    log_info "=== Testing Loki Components Connectivity ==="
    
    # Alloy -> Loki Distributor
    if ! test_connection "Alloy -> Loki Distributor" "monitoring" "http://loki-distributor.monitoring:3100/ready"; then
        failed=$((failed + 1))
    fi
    
    # Loki Query -> Grafana (reverse: Grafana -> Loki Query)
    if ! test_connection "Grafana -> Loki Query" "observability" "http://loki-query.monitoring:3100/ready"; then
        failed=$((failed + 1))
    fi
    
    echo ""
    
    # =========================================================================
    # DNS Resolution Tests
    # =========================================================================
    log_info "=== Testing DNS Resolution ==="
    
    local dns_targets=(
        "prometheus-operated.monitoring"
        "kube-state-metrics.monitoring"
        "thanos-query.monitoring"
        "thanos-store.monitoring"
        "loki-distributor.monitoring"
        "loki-query.monitoring"
        "alloy-daemonset.monitoring"
        "alloy-central.monitoring"
    )
    
    for target in "${dns_targets[@]}"; do
        if ! test_dns "DNS: $target" "monitoring" "$target"; then
            failed=$((failed + 1))
        fi
    done
    
    echo ""
    
    # =========================================================================
    # Summary
    # =========================================================================
    log_info "=== Network Policy Validation Summary ==="
    
    if [[ $failed -eq 0 ]]; then
        log_pass "All network connectivity tests PASSED"
        exit 0
    else
        log_fail "$failed network connectivity test(s) FAILED"
        exit 1
    fi
}

main "$@"
