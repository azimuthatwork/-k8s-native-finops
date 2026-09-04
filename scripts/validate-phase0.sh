#!/bin/bash
# Phase 0 Comprehensive Validation Script
# Validates all prerequisites for the Intelligent Kubernetes Observability & Cost Optimization Platform
# Usage: ./scripts/validate-phase0.sh [--fix] [--report-only] [--namespace=<ns>]

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
REPORT_FILE="docs/phase0-validation-report.md"
FIX_MODE=false
REPORT_ONLY=false
TARGET_NAMESPACE=""

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --fix)
            FIX_MODE=true
            shift
            ;;
        --report-only)
            REPORT_ONLY=true
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
0y
# Global counters
TOTAL_CHECKS=0
PASSED_CHECKS=0
FAILED_CHECKS=0
WARNING_CHECKS=0

# Results array for report
declare -A RESULTS

# Helper functions
log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_pass() { echo -e "${GREEN}[PASS]${NC} $1"; }
log_fail() { echo -e "${RED}[FAIL]${NC} $1"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }

record_result() {
    local category=$1
    local check=$2
    local status=$3
    local details=$4
    
    TOTAL_CHECKS=$((TOTAL_CHECKS + 1))
    case $status in
        PASS) PASSED_CHECKS=$((PASSED_CHECKS + 1)) ;;
        FAIL) FAILED_CHECKS=$((FAILED_CHECKS + 1)) ;;
        WARN) WARNING_CHECKS=$((WARNING_CHECKS + 1)) ;;
    esac
    
    RESULTS["${category}|${check}"]="${status}|${details}"
}

# Initialize report
init_report() {
    mkdir -p docs
    cat > "$REPORT_FILE" << EOF
# Phase 0 Validation Report
**Generated:** $(date -u +"%Y-%m-%d %H:%M:%S UTC")
**Cluster:** $(kubectl config current-context 2>/dev/null || echo "unknown")
**Validator:** $(whoami)

## Executive Summary
- **Total Checks:** $TOTAL_CHECKS
- **Passed:** $PASSED_CHECKS
- **Failed:** $FAILED_CHECKS
- **Warnings:** $WARNING_CHECKS

## Detailed Results

EOF
}

update_report() {
    cat > "$REPORT_FILE" << EOF
# Phase 0 Validation Report
**Generated:** $(date -u +"%Y-%m-%d %H:%M:%S UTC")
**Cluster:** $(kubectl config current-context 2>/dev/null || echo "unknown")
**Validator:** $(whoami)

## Executive Summary
- **Total Checks:** $TOTAL_CHECKS
- **Passed:** $PASSED_CHECKS
- **Failed:** $FAILED_CHECKS
- **Warnings:** $WARNING_CHECKS

## Detailed Results

EOF
    
    # Group by category
    local current_category=""
    for key in $(printf '%s\n' "${!RESULTS[@]}" | sort); do
        IFS='|' read -r category check <<< "$key"
        IFS='|' read -r status details <<< "${RESULTS[$key]}"
        
        if [[ "$category" != "$current_category" ]]; then
            echo "### $category" >> "$REPORT_FILE"
            current_category="$category"
        fi
        
        local status_icon=""
        case $status in
            PASS) status_icon="✅" ;;
            FAIL) status_icon="❌" ;;
            WARN) status_icon="⚠️" ;;
        esac
        
        echo "- **$check**: $status_icon $status" >> "$REPORT_FILE"
        if [[ -n "$details" ]]; then
            echo "  - Details: $details" >> "$REPORT_FILE"
        fi
        echo "" >> "$REPORT_FILE"
    done
    
    # Add recommendations
    cat >> "$REPORT_FILE" << EOF

## Recommendations
EOF
    
    if [[ $FAILED_CHECKS -gt 0 ]]; then
        echo "### Critical (Must Fix Before Phase 1)" >> "$REPORT_FILE"
        for key in $(printf '%s\n' "${!RESULTS[@]}" | sort); do
            IFS='|' read -r category check <<< "$key"
            IFS='|' read -r status details <<< "${RESULTS[$key]}"
            if [[ "$status" == "FAIL" ]]; then
                echo "- **$category / $check**: $details" >> "$REPORT_FILE"
            fi
        done
        echo "" >> "$REPORT_FILE"
    fi
    
    if [[ $WARNING_CHECKS -gt 0 ]]; then
        echo "### Warnings (Should Address)" >> "$REPORT_FILE"
        for key in $(printf '%s\n' "${!RESULTS[@]}" | sort); do
            IFS='|' read -r category check <<< "$key"
            IFS='|' read -r status details <<< "${RESULTS[$key]}"
            if [[ "$status" == "WARN" ]]; then
                echo "- **$category / $check**: $details" >> "$REPORT_FILE"
            fi
        done
        echo "" >> "$REPORT_FILE"
    fi
    
    cat >> "$REPORT_FILE" << EOF

## Sign-off
- [ ] Platform Engineering Lead: _________________ Date: ___________
- [ ] FinOps Team Lead: _________________ Date: ___________
- [ ] Security Team Lead: _________________ Date: ___________

## Next Steps
EOF
    
    if [[ $FAILED_CHECKS -eq 0 ]]; then
        echo "✅ **All critical checks passed. Ready to proceed to Phase 1.**" >> "$REPORT_FILE"
    else
        echo "❌ **$FAILED_CHECKS critical failures must be resolved before proceeding to Phase 1.**" >> "$REPORT_FILE"
    fi
}

# =============================================================================
# 0.1.1 Namespace Strategy Validation
# =============================================================================
validate_namespaces() {
    log_info "=== 0.1.1 Namespace Strategy Validation ==="
    
    local required_namespaces=("monitoring" "observability" "cost-management")
    local required_labels=("cost-center" "environment" "team")
    
    for ns in "${required_namespaces[@]}"; do
        # Check namespace exists
        if kubectl get namespace "$ns" &>/dev/null; then
            record_result "Namespace Strategy" "Namespace $ns exists" "PASS" "Namespace found"
            
            # Check labels
            for label in "${required_labels[@]}"; do
                local value
                value=$(kubectl get namespace "$ns" -o jsonpath="{.metadata.labels.$label}" 2>/dev/null || echo "")
                if [[ -n "$value" ]]; then
                    record_result "Namespace Strategy" "Namespace $ns has label $label" "PASS" "Value: $value"
                else
                    record_result "Namespace Strategy" "Namespace $ns has label $label" "FAIL" "Missing required label: $label"
                    if [[ "$FIX_MODE" == "true" ]]; then
                        log_warn "Fix mode: Would add label $label to namespace $ns"
                        # kubectl label namespace "$ns" "$label"="<value>" --overwrite
                    fi
                fi
            done
        else
            record_result "Namespace Strategy" "Namespace $ns exists" "FAIL" "Namespace not found"
            if [[ "$FIX_MODE" == "true" ]]; then
                log_warn "Fix mode: Would create namespace $ns"
                # kubectl create namespace "$ns"
            fi
        fi
    done
    
    # Check all application namespaces have required labels
    log_info "Checking application namespaces for required labels..."
    local app_namespaces
    app_namespaces=$(kubectl get namespaces -o jsonpath='{.items[*].metadata.name}' | tr ' ' '\n' | grep -vE '^(kube-|monitoring|observability|cost-management|default|kube-node-lease)$' || true)
    
    if [[ -n "$app_namespaces" ]]; then
        while IFS= read -r ns; do
            [[ -z "$ns" ]] && continue
            local missing_labels=()
            for label in "${required_labels[@]}"; do
                local value
                value=$(kubectl get namespace "$ns" -o jsonpath="{.metadata.labels.$label}" 2>/dev/null || echo "")
                if [[ -z "$value" ]]; then
                    missing_labels+=("$label")
                fi
            done
            
            if [[ ${#missing_labels[@]} -eq 0 ]]; then
                record_result "Namespace Strategy" "App namespace $ns has all required labels" "PASS" ""
            else
                record_result "Namespace Strategy" "App namespace $ns has all required labels" "WARN" "Missing labels: ${missing_labels[*]}"
            fi
        done <<< "$app_namespaces"
    else
        record_result "Namespace Strategy" "Application namespaces check" "WARN" "No application namespaces found"
    fi
}

# =============================================================================
# 0.1.2 Network Policies Validation
# =============================================================================
validate_network_policies() {
    log_info "=== 0.1.2 Network Policies Validation ==="
    
    # Check NetworkPolicy resources exist
    local policies
    policies=$(kubectl get networkpolicy -A -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")
    
    if [[ -z "$policies" ]]; then
        record_result "Network Policies" "NetworkPolicy resources exist" "FAIL" "No NetworkPolicies found in cluster"
        return
    else
        record_result "Network Policies" "NetworkPolicy resources exist" "PASS" "Found policies: $policies"
    fi
    
    # Test critical connectivity paths
    test_connectivity "Object Storage Egress" "monitoring" "https://s3.amazonaws.com" "443"
    test_connectivity "Object Storage Egress" "observability" "https://s3.amazonaws.com" "443"
    test_connectivity "Object Storage Egress" "cost-management" "https://s3.amazonaws.com" "443"
    
    test_connectivity "Alloy DaemonSet -> Prometheus" "monitoring" "http://prometheus-operated.monitoring:9090/-/healthy" "9090"
    test_connectivity "Prometheus -> KSM" "monitoring" "http://kube-state-metrics.monitoring:8080/metrics" "8080"
    test_connectivity "Prometheus -> cAdvisor" "monitoring" "http://alloy-daemonset.monitoring:4194/metrics" "4190"
    
    # Thanos connectivity
    test_connectivity "Thanos Sidecar -> Store" "monitoring" "http://thanos-store.monitoring:10901" "10901"
    test_connectivity "Thanos Query -> Grafana" "observability" "http://thanos-query.monitoring:10902/-/healthy" "10902"
    
    # Loki connectivity
    test_connectivity "Alloy -> Loki Distributor" "monitoring" "http://loki-distributor.monitoring:3100/ready" "3100"
    test_connectivity "Loki Query -> Grafana" "observability" "http://loki-query.monitoring:3100/ready" "3100"
}

test_connectivity() {
    local name=$1
    local namespace=$2
    local url=$3
    local port=$4
    
    # Use a temporary pod to test connectivity
    local pod_name="test-connectivity-$(echo "$name" | tr ' ' '-' | tr '[:upper:]' '[:lower:]')-$(date +%s)"
    
    # Create a test pod with curl
    cat <<EOF | kubectl apply -f - >/dev/null 2>&1
apiVersion: v1
kind: Pod
metadata:
  name: $pod_name
  namespace: $namespace
  labels:
    test: connectivity
spec:
  restartPolicy: Never
  containers:
  - name: curl
    image: curlimages/curl:latest
    command: ["sleep", "30"]
EOF
    
    # Wait for pod to be ready
    kubectl wait --for=condition=Ready pod/"$pod_name" -n "$namespace" --timeout=30s >/dev/null 2>&1 || true
    
    # Test connectivity
    local result
    result=$(kubectl exec -n "$namespace" "$pod_name" -- curl -s -o /dev/null -w "%{http_code}" --max-time 10 "$url" 2>/dev/null || echo "000")
    
    # Cleanup
    kubectl delete pod "$pod_name" -n "$namespace" --grace-period=0 --force >/dev/null 2>&1 || true
    
    if [[ "$result" =~ ^2[0-9]{2}$ ]]; then
        record_result "Network Policies" "$name" "PASS" "HTTP $result"
        log_pass "$name: HTTP $result"
    else
        record_result "Network Policies" "$name" "FAIL" "HTTP $result (expected 2xx)"
        log_fail "$name: HTTP $result"
    fi
}

# =============================================================================
# 0.1.3 Alloy DaemonSet Validation
# =============================================================================
validate_alloy_daemonset() {
    log_info "=== 0.1.3 Alloy DaemonSet Validation ==="
    
    # Check DaemonSet exists and is healthy
    local ds_name="alloy-daemonset"
    local ds_namespace="monitoring"
    
    if kubectl get daemonset "$ds_name" -n "$ds_namespace" &>/dev/null; then
        record_result "Alloy DaemonSet" "DaemonSet exists" "PASS" ""
        
        # Check desired vs ready
        local desired ready
        desired=$(kubectl get daemonset "$ds_name" -n "$ds_namespace" -o jsonpath='{.status.desiredNumberScheduled}')
        ready=$(kubectl get daemonset "$ds_name" -n "$ds_namespace" -o jsonpath='{.status.numberReady}')
        
        if [[ "$desired" == "$ready" && "$desired" -gt 0 ]]; then
            record_result "Alloy DaemonSet" "All pods ready ($ready/$desired)" "PASS" ""
        else
            record_result "Alloy DaemonSet" "All pods ready ($ready/$desired)" "FAIL" "Desired: $desired, Ready: $ready"
        fi
    else
        record_result "Alloy DaemonSet" "DaemonSet exists" "FAIL" "DaemonSet not found in $ds_namespace"
        return
    fi
    
    # Check node-exporter metrics
    check_alloy_metrics "node-exporter" "node_cpu_seconds_total,node_memory_MemTotal_bytes,node_filesystem_size_bytes,node_network_receive_bytes_total"
    
    # Check cAdvisor metrics
    check_alloy_metrics "cadvisor" "container_spec_memory_limit_bytes,container_spec_cpu_quota,container_memory_working_set_bytes,container_cpu_usage_seconds_total,container_fs_usage_bytes,container_fs_limit_bytes"
    
    # Check pod_logs labels
    check_pod_logs_labels
    
    # Check logs for errors
    check_alloy_logs
}

check_alloy_metrics() {
    local component=$1
    local expected_metrics=$2
    
    # Get a sample pod
    local pod
    pod=$(kubectl get pods -n monitoring -l app=alloy-daemonset -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
    
    if [[ -z "$pod" ]]; then
        record_result "Alloy DaemonSet" "$component metrics check" "FAIL" "No Alloy DaemonSet pods found"
        return
    fi
    
    # Port forward and scrape metrics
    local port=12345
    kubectl port-forward -n monitoring "pod/$pod" "$port":12345 >/dev/null 2>&1 &
    local pf_pid=$!
    sleep 2
    
    local metrics
    metrics=$(curl -s "http://localhost:$port/metrics" 2>/dev/null || echo "")
    
    kill $pf_pid 2>/dev/null || true
    
    IFS=',' read -ra METRICS <<< "$expected_metrics"
    for metric in "${METRICS[@]}"; do
        if echo "$metrics" | grep -q "^$metric"; then
            record_result "Alloy DaemonSet" "$component metric: $metric" "PASS" "Found in metrics endpoint"
        else
            record_result "Alloy DaemonSet" "$component metric: $metric" "FAIL" "Missing from metrics endpoint"
        fi
    done
}

check_pod_logs_labels() {
    log_info "Checking pod_logs labels..."
    
    # Check Alloy config for label configuration
    local config_map
    config_map=$(kubectl get configmap -n monitoring alloy-config -o jsonpath='{.data.config\.alloy}' 2>/dev/null || echo "")
    
    if [[ -z "$config_map" ]]; then
        record_result "Alloy DaemonSet" "pod_logs labels configuration" "WARN" "Could not retrieve Alloy config"
        return
    fi
    
    local required_labels=("namespace" "pod" "container" "cost_center" "team")
    for label in "${required_labels[@]}"; do
        if echo "$config_map" | grep -q "$label"; then
            record_result "Alloy DaemonSet" "pod_logs label: $label" "PASS" "Configured in Alloy config"
        else
            record_result "Alloy DaemonSet" "pod_logs label: $label" "FAIL" "Missing from Alloy config"
        fi
    done
}

check_alloy_logs() {
    log_info "Checking Alloy DaemonSet logs for errors..."
    
    local pods
    pods=$(kubectl get pods -n monitoring -l app=alloy-daemonset -o jsonpath='{.items[*].metadata.name}')
    
    local error_count=0
    for pod in $pods; do
        local errors
        errors=$(kubectl logs -n monitoring "$pod" --tail=100 2>/dev/null | grep -iE "(error|fail|panic)" | wc -l)
        error_count=$((error_count + errors))
    done
    
    if [[ $error_count -eq 0 ]]; then
        record_result "Alloy DaemonSet" "Log error check" "PASS" "No errors in recent logs"
    else
        record_result "Alloy DaemonSet" "Log error check" "WARN" "Found $error_count error lines in recent logs"
    fi
}

# =============================================================================
# 0.1.4 Alloy Central Validation
# =============================================================================
validate_alloy_central() {
    log_info "=== 0.1.4 Alloy Central Validation ==="
    
    local deploy_name="alloy-central"
    local namespace="monitoring"
    
    if kubectl get deployment "$deploy_name" -n "$namespace" &>/dev/null; then
        record_result "Alloy Central" "Deployment exists" "PASS" ""
        
        local ready replicas
        ready=$(kubectl get deployment "$deploy_name" -n "$namespace" -o jsonpath='{.status.readyReplicas}')
        replicas=$(kubectl get deployment "$deploy_name" -n "$namespace" -o jsonpath='{.spec.replicas}')
        
        if [[ "$ready" == "$replicas" && "$ready" -gt 0 ]]; then
            record_result "Alloy Central" "Replicas ready ($ready/$replicas)" "PASS" ""
        else
            record_result "Alloy Central" "Replicas ready ($ready/$replicas)" "FAIL" "Desired: $replicas, Ready: $ready"
        fi
    else
        record_result "Alloy Central" "Deployment exists" "FAIL" "Deployment not found"
        return
    fi
    
    # Check config for remote_write endpoints
    local config_map
    config_map=$(kubectl get configmap -n monitoring alloy-central-config -o jsonpath='{.data.config\.alloy}' 2>/dev/null || echo "")
    
    if [[ -n "$config_map" ]]; then
        # Check Prometheus remote_write
        if echo "$config_map" | grep -q "prometheus.remote_write"; then
            record_result "Alloy Central" "prometheus.remote_write configured" "PASS" ""
            
            # Check for HA endpoints
            if echo "$config_map" | grep -q "prometheus-operated-0.monitoring:9090" && \
               echo "$config_map" | grep -q "prometheus-operated-1.monitoring:9090"; then
                record_result "Alloy Central" "Prometheus HA endpoints configured" "PASS" "Both endpoints found"
            else
                record_result "Alloy Central" "Prometheus HA endpoints configured" "WARN" "HA endpoints not both found in config"
            fi
        else
            record_result "Alloy Central" "prometheus.remote_write configured" "FAIL" "Not found in config"
        fi
        
        # Check Loki write
        if echo "$config_map" | grep -q "loki.write"; then
            record_result "Alloy Central" "loki.write configured" "PASS" ""
        else
            record_result "Alloy Central" "loki.write configured" "FAIL" "Not found in config"
        fi
    else
        record_result "Alloy Central" "Configuration retrieved" "FAIL" "Could not get alloy-central-config ConfigMap"
    fi
    
    # Check remote_write throughput (no 429/5xx)
    check_remote_write_health
}

check_remote_write_health() {
    log_info "Checking remote_write health..."
    
    local pod
    pod=$(kubectl get pods -n monitoring -l app=alloy-central -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
    
    if [[ -z "$pod" ]]; then
        record_result "Alloy Central" "Remote write health check" "WARN" "No Alloy Central pods found"
        return
    fi
    
    local logs
    logs=$(kubectl logs -n monitoring "$pod" --tail=500 2>/dev/null || echo "")
    
    local error_429 error_5xx
    error_429=$(echo "$logs" | grep -c "429" || echo "0")
    error_5xx=$(echo "$logs" | grep -cE "5[0-9]{2}" || echo "0")
    
    if [[ $error_429 -eq 0 && $error_5xx -eq 0 ]]; then
        record_result "Alloy Central" "Remote write throughput (no 429/5xx)" "PASS" "No rate limit or server errors"
    else
        record_result "Alloy Central" "Remote write throughput (no 429/5xx)" "WARN" "429 errors: $error_429, 5xx errors: $error_5xx"
    fi
}

# =============================================================================
# 0.1.5 Prometheus/Thanos Validation
# =============================================================================
validate_prometheus_thanos() {
    log_info "=== 0.1.5 Prometheus/Thanos Validation ==="
    
    # Check Thanos objstore secret
    if kubectl get secret thanos-objstore-config -n monitoring &>/dev/null; then
        record_result "Prometheus/Thanos" "Thanos objstore.yml secret exists" "PASS" "Secret found"
    else
        record_result "Prometheus/Thanos" "Thanos objstore.yml secret exists" "FAIL" "Secret not found in monitoring namespace"
    fi
    
    # Check Thanos components
    local components=("thanos-query:Deployment" "thanos-store:StatefulSet" "thanos-compact:Deployment")
    for comp in "${components[@]}"; do
        IFS=':' read -r name kind <<< "$comp"
        if kubectl get "$kind" "$name" -n monitoring &>/dev/null; then
            record_result "Prometheus/Thanos" "$name ($kind) deployed" "PASS" ""
        else
            record_result "Prometheus/Thanos" "$name ($kind) deployed" "FAIL" "Not found"
        fi
    done
    
    # Check Prometheus retention
    local prometheus_cr
    prometheus_cr=$(kubectl get prometheus -n monitoring -o jsonpath='{.items[0].spec.retention}' 2>/dev/null || echo "")
    if [[ "$prometheus_cr" == "2h" ]]; then
        record_result "Prometheus/Thanos" "Prometheus local retention (2h)" "PASS" "Configured as 2h"
    else
        record_result "Prometheus/Thanos" "Prometheus local retention (2h)" "WARN" "Current: $prometheus_cr (expected 2h)"
    fi
    
    # Check Thanos compactor retention
    local compact_args
    compact_args=$(kubectl get deployment thanos-compact -n monitoring -o jsonpath='{.spec.template.spec.containers[0].args}' 2>/dev/null || echo "")
    if echo "$compact_args" | grep -q "retention.resolution-raw=14d" && \
       echo "$compact_args" | grep -q "retention.resolution-5m=90d" && \
       echo "$compact_args" | grep -q "retention.resolution-1h=365d"; then
        record_result "Prometheus/Thanos" "Thanos compactor retention policies" "PASS" "14d/90d/365d configured"
    else
        record_result "Prometheus/Thanos" "Thanos compactor retention policies" "WARN" "Retention policies not matching expected values"
    fi
    
    # Check Prometheus HA pair
    local prom_pods
    prom_pods=$(kubectl get pods -n monitoring -l prometheus=k8s -o jsonpath='{.items[*].metadata.name}' 2>/dev/null | wc -w)
    if [[ $prom_pods -ge 2 ]]; then
        record_result "Prometheus/Thanos" "Prometheus HA pair running" "PASS" "$prom_pods pods found"
    else
        record_result "Prometheus/Thanos" "Prometheus HA pair running" "FAIL" "Only $prom_pods pods found (expected 2+)"
    fi
    
    # Test Thanos Query UI
    test_thanos_query_ui
}

test_thanos_query_ui() {
    log_info "Testing Thanos Query UI accessibility..."
    
    local pod_name="test-thanos-query-$(date +%s)"
    
    cat <<EOF | kubectl apply -f - >/dev/null 2>&1
apiVersion: v1
kind: Pod
metadata:
  name: $pod_name
  namespace: monitoring
spec:
  restartPolicy: Never
  containers:
  - name: curl
    image: curlimages/curl:latest
    command: ["sleep", "30"]
EOF
    
    kubectl wait --for=condition=Ready pod/"$pod_name" -n monitoring --timeout=30s >/dev/null 2>&1 || true
    
    local result
    result=$(kubectl exec -n monitoring "$pod_name" -- curl -s -o /dev/null -w "%{http_code}" --max-time 10 "http://thanos-query.monitoring:10902/-/healthy" 2>/dev/null || echo "000")
    
    kubectl delete pod "$pod_name" -n monitoring --grace-period=0 --force >/dev/null 2>&1 || true
    
    if [[ "$result" == "200" ]]; then
        record_result "Prometheus/Thanos" "Thanos Query UI accessible" "PASS" "HTTP 200"
    else
        record_result "Prometheus/Thanos" "Thanos Query UI accessible" "FAIL" "HTTP $result"
    fi
}

# =============================================================================
# 0.2 Required CRDs & Operators Validation
# =============================================================================
validate_crds_operators() {
    log_info "=== 0.2 Required CRDs & Operators Validation ==="
    
    # Prometheus Operator CRDs
    local prom_crds=("servicemonitors.monitoring.coreos.com" "podmonitors.monitoring.coreos.com" "probes.monitoring.coreos.com" "prometheusrules.monitoring.coreos.com")
    for crd in "${prom_crds[@]}"; do
        if kubectl get crd "$crd" &>/dev/null; then
            record_result "CRDs & Operators" "CRD: $crd" "PASS" "Installed"
        else
            record_result "CRDs & Operators" "CRD: $crd" "FAIL" "Not installed"
        fi
    done
    
    # Prometheus Operator version
    local po_image
    po_image=$(kubectl get deployment -n monitoring prometheus-operator -o jsonpath='{.spec.template.spec.containers[0].image}' 2>/dev/null || echo "")
    if [[ -n "$po_image" ]]; then
        local version
        version=$(echo "$po_image" | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1 || echo "unknown")
        record_result "CRDs & Operators" "Prometheus Operator version" "PASS" "Image: $po_image (version: $version)"
        
        # Check version >= 0.75
        if [[ "$version" =~ v([0-9]+)\.([0-9]+) ]]; then
            local major="${BASH_REMATCH[1]}"
            local minor="${BASH_REMATCH[2]}"
            if [[ $major -gt 0 ]] || [[ $major -eq 0 && $minor -ge 75 ]]; then
                record_result "CRDs & Operators" "Prometheus Operator >= v0.75" "PASS" "Version: $version"
            else
                record_result "CRDs & Operators" "Prometheus Operator >= v0.75" "FAIL" "Version: $version (need >= v0.75)"
            fi
        fi
    else
        record_result "CRDs & Operators" "Prometheus Operator version" "FAIL" "Deployment not found"
    fi
    
    # Grafana CRDs (optional - check if operator is used)
    local grafana_crds=("grafanadashboards.grafana.integreatly.org" "grafanadatasources.grafana.integreatly.org")
    for crd in "${grafana_crds[@]}"; do
        if kubectl get crd "$crd" &>/dev/null; then
            record_result "CRDs & Operators" "Grafana CRD: $crd" "PASS" "Installed (Grafana Operator detected)"
        else
            record_result "CRDs & Operators" "Grafana CRD: $crd" "WARN" "Not installed (may use Helm provisioning instead)"
        fi
    done
    
    # Check Grafana connectivity
    check_grafana_connectivity
    
    # OpenCost/Kubecost metrics adapter
    if kubectl get deployment -n cost-management opencost &>/dev/null || kubectl get deployment -n cost-management kubecost &>/dev/null; then
        record_result "CRDs & Operators" "OpenCost/Kubecost deployed" "PASS" "Found in cost-management namespace"
    else
        record_result "CRDs & Operators" "OpenCost/Kubecost deployed" "WARN" "Not deployed (planned for Phase 1.3)"
    fi
    
    # Custom metrics API
    if kubectl get apiservice v1beta1.custom.metrics.k8s.io &>/dev/null; then
        record_result "CRDs & Operators" "Custom metrics API registered" "PASS" "APIService found"
    else
        record_result "CRDs & Operators" "Custom metrics API registered" "WARN" "Not registered (needed for HPA custom metrics)"
    fi
    
    # VPA CRDs
    if kubectl get crd verticalpodautoscalers.autoscaling.k8s.io &>/dev/null; then
        record_result "CRDs & Operators" "VPA CRD installed" "PASS" ""
        
        # Check VPA components
        local vpa_components=("vpa-recommender:Deployment" "vpa-updater:Deployment" "vpa-admission-controller:Deployment")
        for comp in "${vpa_components[@]}"; do
            IFS=':' read -r name kind <<< "$comp"
            if kubectl get "$kind" "$name" -n kube-system &>/dev/null || kubectl get "$kind" "$name" -n monitoring &>/dev/null; then
                record_result "CRDs & Operators" "VPA component: $name" "PASS" "Deployed"
            else
                record_result "CRDs & Operators" "VPA component: $name" "WARN" "Not deployed (planned for Phase 5.1)"
            fi
        done
    else
        record_result "CRDs & Operators" "VPA CRD installed" "FAIL" "CRD not found"
    fi
    
    # PDB support for VPA
    local pdb_count
    pdb_count=$(kubectl get pdb -A --no-headers 2>/dev/null | wc -l)
    if [[ $pdb_count -gt 0 ]]; then
        record_result "CRDs & Operators" "PodDisruptionBudgets exist" "PASS" "$pdb_count PDBs found"
    else
        record_result "CRDs & Operators" "PodDisruptionBudgets exist" "WARN" "No PDBs found (needed for VPA eviction tolerance)"
    fi
}

check_grafana_connectivity() {
    log_info "Checking Grafana connectivity to Thanos and Loki..."
    
    local grafana_ns="observability"
    local grafana_pod
    grafana_pod=$(kubectl get pods -n "$grafana_ns" -l app=grafana -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
    
    if [[ -z "$grafana_pod" ]]; then
        record_result "CRDs & Operators" "Grafana connectivity check" "WARN" "Grafana pod not found in $grafana_ns"
        return
    fi
    
    # Test Thanos Query connectivity from Grafana pod
    local result
    result=$(kubectl exec -n "$grafana_ns" "$grafana_pod" -- curl -s -o /dev/null -w "%{http_code}" --max-time 5 "http://thanos-query.monitoring:10902/-/healthy" 2>/dev/null || echo "000")
    
    if [[ "$result" == "200" ]]; then
        record_result "CRDs & Operators" "Grafana -> Thanos Query connectivity" "PASS" "HTTP 200"
    else
        record_result "CRDs & Operators" "Grafana -> Thanos Query connectivity" "FAIL" "HTTP $result"
    fi
    
    # Test Loki connectivity
    result=$(kubectl exec -n "$grafana_ns" "$grafana_pod" -- curl -s -o /dev/null -w "%{http_code}" --max-time 5 "http://loki-query.monitoring:3100/ready" 2>/dev/null || echo "000")
    
    if [[ "$result" == "200" ]]; then
        record_result "CRDs & Operators" "Grafana -> Loki Query connectivity" "PASS" "HTTP 200"
    else
        record_result "CRDs & Operators" "Grafana -> Loki Query connectivity" "FAIL" "HTTP $result"
    fi
}

# =============================================================================
# 0.3 Documentation & Sign-off
# =============================================================================
generate_documentation() {
    log_info "=== 0.3 Documentation Generation ==="
    
    # Create namespace inventory
    create_namespace_inventory
    
    # Update validation report
    update_report
    
    log_info "Validation report generated: $REPORT_FILE"
    log_info "Namespace inventory: docs/namespace-inventory.md"
}

create_namespace_inventory() {
    cat > docs/namespace-inventory.md << EOF
# Namespace Label Inventory
**Generated:** $(date -u +"%Y-%m-%d %H:%M:%S UTC")

| Namespace | cost-center | environment | team | Status |
|-----------|-------------|-------------|------|--------|
EOF
    
    local namespaces
    namespaces=$(kubectl get namespaces -o jsonpath='{.items[*].metadata.name}' | tr ' ' '\n' | sort)
    
    while IFS= read -r ns; do
        [[ -z "$ns" ]] && continue
        
        local cc env team
        cc=$(kubectl get namespace "$ns" -o jsonpath='{.metadata.labels.cost-center}' 2>/dev/null || echo "MISSING")
        env=$(kubectl get namespace "$ns" -o jsonpath='{.metadata.labels.environment}' 2>/dev/null || echo "MISSING")
        team=$(kubectl get namespace "$ns" -o jsonpath='{.metadata.labels.team}' 2>/dev/null || echo "MISSING")
        
        local status="✅"
        if [[ "$cc" == "MISSING" || "$env" == "MISSING" || "$team" == "MISSING" ]]; then
            status="❌"
        fi
        
        echo "| $ns | $cc | $env | $team | $status |" >> docs/namespace-inventory.md
    done <<< "$namespaces"
    
    cat >> docs/namespace-inventory.md << EOF

## Summary
- **Total Namespaces:** $(kubectl get namespaces --no-headers | wc -l)
- **Fully Labeled:** $(grep -c "✅" docs/namespace-inventory.md || echo 0)
- **Missing Labels:** $(grep -c "❌" docs/namespace-inventory.md || echo 0)

## Required Actions
EOF
    
    # Add missing label actions
    while IFS= read -r ns; do
        [[ -z "$ns" ]] && continue
        local cc env team
        cc=$(kubectl get namespace "$ns" -o jsonpath='{.metadata.labels.cost-center}' 2>/dev/null || echo "MISSING")
        env=$(kubectl get namespace "$ns" -o jsonpath='{.metadata.labels.environment}' 2>/dev/null || echo "MISSING")
        team=$(kubectl get namespace "$ns" -o jsonpath='{.metadata.labels.team}' 2>/dev/null || echo "MISSING")
        
        if [[ "$cc" == "MISSING" || "$env" == "MISSING" || "$team" == "MISSING" ]]; then
            echo "- Namespace \`$ns\`: Add missing labels" >> docs/namespace-inventory.md
        fi
    done <<< "$namespaces"
}

# =============================================================================
# Main Execution
# =============================================================================
main() {
    log_info "Starting Phase 0 Validation..."
    log_info "Report will be saved to: $REPORT_FILE"
    
    init_report
    
    if [[ "$REPORT_ONLY" == "true" ]]; then
        log_info "Report-only mode: Generating report from existing data"
        update_report
        exit 0
    fi
    
    # Run all validations
    validate_namespaces
    validate_network_policies
    validate_alloy_daemonset
    validate_alloy_central
    validate_prometheus_thanos
    validate_crds_operators
    generate_documentation
    
    # Final summary
    echo ""
    log_info "=== Phase 0 Validation Complete ==="
    echo -e "Total Checks: ${BLUE}$TOTAL_CHECKS${NC}"
    echo -e "Passed: ${GREEN}$PASSED_CHECKS${NC}"
    echo -e "Failed: ${RED}$FAILED_CHECKS${NC}"
    echo -e "Warnings: ${YELLOW}$WARNING_CHECKS${NC}"
    echo ""
    log_info "Full report: $REPORT_FILE"
    
    if [[ $FAILED_CHECKS -gt 0 ]]; then
        log_fail "Phase 0 validation FAILED - $FAILED_CHECKS critical issues must be resolved"
        exit 1
    else
        log_pass "Phase 0 validation PASSED - Ready for Phase 1"
        exit 0
    fi
}

main "$@"
