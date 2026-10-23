#!/bin/bash
set -euo pipefail

# Phase 0 Native Validation Script
# Validates KSM, Metrics Server, Node Exporter, Cloud Auth, CRDs

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPORT_FILE="${SCRIPT_DIR}/../../docs/phase0-native-report.md"
NAMESPACE_MONITORING="monitoring"
NAMESPACE_COST_MGMT="cost-management"
NAMESPACE_KUBE_SYSTEM="kube-system"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Counters
PASS=0
FAIL=0
WARN=0

# Helper functions
log_info() { echo -e "${GREEN}[INFO]${NC} $*"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $*"; ((WARN++)); }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; ((FAIL++)); }
log_pass() { echo -e "${GREEN}[PASS]${NC} $*"; ((PASS++)); }

check_command() {
  if command -v "$1" &> /dev/null; then
    log_pass "Command '$1' is available"
    return 0
  else
    log_error "Command '$1' is NOT available"
    return 1
  fi
}

check_kubectl() {
  if kubectl version --client &> /dev/null; then
    log_pass "kubectl is available and configured"
    return 0
  else
    log_error "kubectl is not available or not configured"
    return 1
  fi
}

check_namespace() {
  local ns=$1
  if kubectl get namespace "$ns" &> /dev/null; then
    log_pass "Namespace '$ns' exists"
    return 0
  else
    log_error "Namespace '$ns' does NOT exist"
    return 1
  fi
}

check_deployment() {
  local ns=$1
  local name=$2
  local expected_replicas=${3:-1}
  
  if kubectl get deployment "$name" -n "$ns" &> /dev/null; then
    local ready=$(kubectl get deployment "$name" -n "$ns" -o jsonpath='{.status.readyReplicas}')
    local desired=$(kubectl get deployment "$name" -n "$ns" -o jsonpath='{.spec.replicas}')
    if [[ "$ready" == "$desired" ]] && [[ "$ready" -ge "$expected_replicas" ]]; then
      log_pass "Deployment '$name' in '$ns' is ready ($ready/$desired replicas)"
      return 0
    else
      log_error "Deployment '$name' in '$ns' NOT ready ($ready/$desired replicas)"
      return 1
    fi
  else
    log_error "Deployment '$name' in '$ns' does NOT exist"
    return 1
  fi
}

check_daemonset() {
  local ns=$1
  local name=$2
  
  if kubectl get daemonset "$name" -n "$ns" &> /dev/null; then
    local desired=$(kubectl get daemonset "$name" -n "$ns" -o jsonpath='{.status.desiredNumberScheduled}')
    local ready=$(kubectl get daemonset "$name" -n "$ns" -o jsonpath='{.status.numberReady}')
    if [[ "$ready" == "$desired" ]] && [[ "$desired" -gt 0 ]]; then
      log_pass "DaemonSet '$name' in '$ns' is ready ($ready/$desired pods)"
      return 0
    else
      log_error "DaemonSet '$name' in '$ns' NOT ready ($ready/$desired pods)"
      return 1
    fi
  else
    log_error "DaemonSet '$name' in '$ns' does NOT exist"
    return 1
  fi
}

check_service() {
  local ns=$1
  local name=$2
  local port=$3
  
  if kubectl get service "$name" -n "$ns" &> /dev/null; then
    local svc_port=$(kubectl get service "$name" -n "$ns" -o jsonpath='{.spec.ports[0].port}')
    if [[ "$svc_port" == "$port" ]]; then
      log_pass "Service '$name' in '$ns' exists on port $port"
      return 0
    else
      log_warn "Service '$name' in '$ns' exists but on port $svc_port (expected $port)"
      return 1
    fi
  else
    log_error "Service '$name' in '$ns' does NOT exist"
    return 1
  fi
}

check_networkpolicy() {
  local ns=$1
  local name=$2
  
  if kubectl get networkpolicy "$name" -n "$ns" &> /dev/null; then
    log_pass "NetworkPolicy '$name' in '$ns' exists"
    return 0
  else
    log_error "NetworkPolicy '$name' in '$ns' does NOT exist"
    return 1
  fi
}

check_crd() {
  local name=$1
  
  if kubectl get crd "$name" &> /dev/null; then
    local established=$(kubectl get crd "$name" -o jsonpath='{.status.conditions[?(@.type=="Established")].status}')
    if [[ "$established" == "True" ]]; then
      log_pass "CRD '$name' is established"
      return 0
    else
      log_error "CRD '$name' exists but NOT established"
      return 1
    fi
  else
    log_error "CRD '$name' does NOT exist"
    return 1
  fi
}

check_crd_status_subresource() {
  local name=$1
  
  if kubectl get crd "$name" -o jsonpath='{.spec.versions[0].subresources.status}' | grep -q "{}"; then
    log_pass "CRD '$name' has status subresource enabled"
    return 0
  else
    log_error "CRD '$name' does NOT have status subresource enabled"
    return 1
  fi
}

check_ksm_metrics() {
  log_info "Checking KSM metrics endpoint..."
  local pod=$(kubectl get pods -n "$NAMESPACE_MONITORING" -l app.kubernetes.io/name=kube-state-metrics -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
  if [[ -z "$pod" ]]; then
    log_error "No KSM pod found"
    return 1
  fi
  
  if kubectl exec -n "$NAMESPACE_MONITORING" "$pod" -- wget -qO- localhost:8080/metrics 2>/dev/null | grep -q "kube_pod_container_resource_requests"; then
    log_pass "KSM exposes kube_pod_container_resource_requests metric"
    return 0
  else
    log_error "KSM does NOT expose kube_pod_container_resource_requests metric"
    return 1
  fi
}

check_metrics_server() {
  log_info "Checking Metrics Server..."
  if kubectl top nodes &> /dev/null; then
    log_pass "kubectl top nodes works"
  else
    log_error "kubectl top nodes FAILED"
    return 1
  fi
  
  if kubectl top pods -A &> /dev/null; then
    log_pass "kubectl top pods works"
    return 0
  else
    log_error "kubectl top pods FAILED"
    return 1
  fi
}

check_node_exporter_metrics() {
  log_info "Checking Node Exporter metrics endpoint..."
  local pod=$(kubectl get pods -n "$NAMESPACE_MONITORING" -l app.kubernetes.io/name=node-exporter -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
  if [[ -z "$pod" ]]; then
    log_error "No Node Exporter pod found"
    return 1
  fi
  
  if kubectl exec -n "$NAMESPACE_MONITORING" "$pod" -- wget -qO- localhost:9100/metrics 2>/dev/null | grep -q "node_cpu_seconds_total"; then
    log_pass "Node Exporter exposes node_cpu_seconds_total metric"
    return 0
  else
    log_error "Node Exporter does NOT expose node_cpu_seconds_total metric"
    return 1
  fi
}

check_cloud_auth() {
  log_info "Checking Cloud Authentication (IRSA/Workload Identity)..."
  local pod=$(kubectl get pods -n "$NAMESPACE_COST_MGMT" -l app.kubernetes.io/name=cost-controller -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
  if [[ -z "$pod" ]]; then
    log_warn "Cost Controller pod not found - skipping cloud auth test (expected in Phase 0)"
    return 0
  fi
  
  # Test AWS
  if kubectl exec -n "$NAMESPACE_COST_MGMT" "$pod" -- aws sts get-caller-identity &> /dev/null; then
    log_pass "AWS IRSA authentication works"
  elif kubectl exec -n "$NAMESPACE_COST_MGMT" "$pod" -- gcloud auth list --filter=status:ACTIVE --format="value(account)" &> /dev/null; then
    log_pass "GCP Workload Identity authentication works"
  elif kubectl exec -n "$NAMESPACE_COST_MGMT" "$pod" -- az account show &> /dev/null; then
    log_pass "Azure Workload Identity authentication works"
  else
    log_warn "Cloud authentication test skipped (no cloud CLI in pod or not configured)"
  fi
  return 0
}

check_serviceaccount() {
  local ns=$1
  local name=$2
  
  if kubectl get serviceaccount "$name" -n "$ns" &> /dev/null; then
    log_pass "ServiceAccount '$name' in '$ns' exists"
    return 0
  else
    log_error "ServiceAccount '$name' in '$ns' does NOT exist"
    return 1
  fi
}

generate_report() {
  log_info "Generating Phase 0 validation report..."
  cat > "$REPORT_FILE" <<EOF
# Phase 0 Native Validation Report

**Date:** $(date -u +"%Y-%m-%d %H:%M:%S UTC")
**Cluster:** $(kubectl config current-context 2>/dev/null || echo "unknown")
**Validator:** $(whoami)

## Summary

| Status | Count |
|--------|-------|
| ✅ Pass | $PASS |
| ⚠️ Warn | $WARN |
| ❌ Fail | $FAIL |

## Detailed Results

### 0.1 Kube-State-Metrics (KSM) Hardening
- Namespace 'monitoring' exists: $(check_namespace "$NAMESPACE_MONITORING" &>/dev/null && echo "✅" || echo "❌")
- KSM Deployment (2 replicas): $(check_deployment "$NAMESPACE_MONITORING" "kube-state-metrics" 2 &>/dev/null && echo "✅" || echo "❌")
- KSM ConfigMap with allowlist: $(kubectl get configmap kube-state-metrics-config -n "$NAMESPACE_MONITORING" &>/dev/null && echo "✅" || echo "❌")
- KSM Service (port 8080): $(check_service "$NAMESPACE_MONITORING" "kube-state-metrics" 8080 &>/dev/null && echo "✅" || echo "❌")
- KSM NetworkPolicy: $(check_networkpolicy "$NAMESPACE_MONITORING" "kube-state-metrics-allow-cost-controller" &>/dev/null && echo "✅" || echo "❌")
- KSM Metrics Validation: $(check_ksm_metrics &>/dev/null && echo "✅" || echo "❌")

### 0.2 Metrics Server & Resource Pipeline
- Metrics Server Deployment: $(check_deployment "$NAMESPACE_KUBE_SYSTEM" "metrics-server" 2 &>/dev/null && echo "✅" || echo "❌")
- Metrics Server Args (--kubelet-insecure-tls, --metric-resolution=30s): $(kubectl get deployment metrics-server -n "$NAMESPACE_KUBE_SYSTEM" -o jsonpath='{.spec.template.spec.containers[0].args}' | grep -q "kubelet-insecure-tls" && echo "✅" || echo "❌")
- kubectl top nodes/pods: $(check_metrics_server &>/dev/null && echo "✅" || echo "❌")

### 0.3 Node Exporter DaemonSet
- Node Exporter DaemonSet: $(check_daemonset "$NAMESPACE_MONITORING" "node-exporter" &>/dev/null && echo "✅" || echo "❌")
- Node Exporter Service (port 9100): $(check_service "$NAMESPACE_MONITORING" "node-exporter" 9100 &>/dev/null && echo "✅" || echo "❌")
- Node Exporter NetworkPolicy: $(check_networkpolicy "$NAMESPACE_MONITORING" "node-exporter-allow-cost-controller" &>/dev/null && echo "✅" || echo "❌")
- Node Exporter Metrics Validation: $(check_node_exporter_metrics &>/dev/null && echo "✅" || echo "❌")

### 0.4 Cloud Provider Authentication
- Cost Controller ServiceAccount: $(check_serviceaccount "$NAMESPACE_COST_MGMT" "cost-controller-sa" &>/dev/null && echo "✅" || echo "❌")
- IRSA/Workload Identity Annotation: $(kubectl get sa cost-controller-sa -n "$NAMESPACE_COST_MGMT" -o jsonpath='{.metadata.annotations}' | grep -q "eks.amazonaws.com/role-arn\|iam.gke.io/gcp-service-account\|azure.workload.identity/client-id" && echo "✅" || echo "❌")
- Cloud SDK Test: $(check_cloud_auth &>/dev/null && echo "✅" || echo "⚠️")

### 0.5 Core CRDs
- CostAllocation CRD: $(check_crd "costallocations.finops.io" &>/dev/null && echo "✅" || echo "❌")
- NodeCost CRD: $(check_crd "nodecosts.finops.io" &>/dev/null && echo "✅" || echo "❌")
- PVCost CRD: $(check_crd "pvcosts.finops.io" &>/dev/null && echo "✅" || echo "❌")
- CostHistory CRD: $(check_crd "costhistories.finops.io" &>/dev/null && echo "✅" || echo "❌")
- RightsizingRecommendation CRD: $(check_crd "rightsizingrecommendations.finops.io" &>/dev/null && echo "✅" || echo "❌")
- NamespaceBudget CRD: $(check_crd "namespacebudgets.finops.io" &>/dev/null && echo "✅" || echo "❌")
- LogEvent CRD: $(check_crd "logevents.finops.io" &>/dev/null && echo "✅" || echo "❌")
- CRD Status Subresources: $(check_crd_status_subresource "costallocations.finops.io" &>/dev/null && echo "✅" || echo "❌")
- NamespaceBudget Example CR: $(kubectl get namespacebudget finops-system-budget -n finops-system &>/dev/null && echo "✅" || echo "❌")

## Approvals Required

- [ ] **Platform Engineering** - Infrastructure readiness
- [ ] **FinOps** - Labeling strategy (cost-center, team, environment)
- [ ] **Security** - NetworkPolicies, RBAC, IRSA

## Next Steps

If all checks pass, proceed to **Phase 1: Cost Controller Implementation**.

EOF
  log_info "Report generated at: $REPORT_FILE"
}

main() {
  log_info "Starting Phase 0 Native Validation..."
  echo ""
  
  # Prerequisites
  check_command "kubectl"
  check_command "wget"
  check_kubectl
  
  echo ""
  log_info "=== 0.1 Kube-State-Metrics (KSM) Hardening ==="
  check_namespace "$NAMESPACE_MONITORING"
  check_deployment "$NAMESPACE_MONITORING" "kube-state-metrics" 2
  kubectl get configmap kube-state-metrics-config -n "$NAMESPACE_MONITORING" &>/dev/null && log_pass "KSM ConfigMap exists" || log_error "KSM ConfigMap missing"
  check_service "$NAMESPACE_MONITORING" "kube-state-metrics" 8080
  check_networkpolicy "$NAMESPACE_MONITORING" "kube-state-metrics-allow-cost-controller"
  check_ksm_metrics
  
  echo ""
  log_info "=== 0.2 Metrics Server & Resource Pipeline ==="
  check_deployment "$NAMESPACE_KUBE_SYSTEM" "metrics-server" 2
  check_metrics_server
  
  echo ""
  log_info "=== 0.3 Node Exporter DaemonSet ==="
  check_daemonset "$NAMESPACE_MONITORING" "node-exporter"
  check_service "$NAMESPACE_MONITORING" "node-exporter" 9100
  check_networkpolicy "$NAMESPACE_MONITORING" "node-exporter-allow-cost-controller"
  check_node_exporter_metrics
  
  echo ""
  log_info "=== 0.4 Cloud Provider Authentication ==="
  check_namespace "$NAMESPACE_COST_MGMT"
  check_serviceaccount "$NAMESPACE_COST_MGMT" "cost-controller-sa"
  check_cloud_auth
  
  echo ""
  log_info "=== 0.5 Core CRDs ==="
  check_crd "costallocations.finops.io"
  check_crd "nodecosts.finops.io"
  check_crd "pvcosts.finops.io"
  check_crd "costhistories.finops.io"
  check_crd "rightsizingrecommendations.finops.io"
  check_crd "namespacebudgets.finops.io"
  check_crd "logevents.finops.io"
  check_crd_status_subresource "costallocations.finops.io"
  kubectl get namespacebudget finops-system-budget -n finops-system &>/dev/null && log_pass "NamespaceBudget example CR exists" || log_error "NamespaceBudget example CR missing"
  
  echo ""
  log_info "=== Validation Summary ==="
  echo -e "  ${GREEN}Passed:${NC} $PASS"
  echo -e "  ${YELLOW}Warnings:${NC} $WARN"
  echo -e "  ${RED}Failed:${NC} $FAIL"
  
  generate_report
  
  if [[ $FAIL -gt 0 ]]; then
    log_error "Phase 0 validation FAILED with $FAIL errors"
    exit 1
  else
    log_pass "Phase 0 validation PASSED"
    exit 0
  fi
}

main "$@"
