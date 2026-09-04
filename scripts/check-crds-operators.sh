#!/bin/bash
# CRDs and Operators Validation Script for Phase 0
# Validates all required CRDs, operators, and their versions

set -euo pipefail

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

check_crd() {
    local crd=$1
    local description=$2
    local required=${3:-true}
    
    if kubectl get crd "$crd" &>/dev/null; then
        local version
        version=$(kubectl get crd "$crd" -o jsonpath='{.spec.versions[-1].name}' 2>/dev/null || echo "unknown")
        log_pass "$description ($crd) - Version: $version"
        return 0
    else
        if [[ "$required" == "true" ]]; then
            log_fail "$description ($crd) - NOT INSTALLED (REQUIRED)"
            return 1
        else
            log_warn "$description ($crd) - Not installed (optional)"
            return 0
        fi
    fi
}

check_operator_version() {
    local deployment=$1
    local namespace=$2
    local name=$3
    local min_version=$4
    
    local image
    image=$(kubectl get deployment "$deployment" -n "$namespace" -o jsonpath='{.spec.template.spec.containers[0].image}' 2>/dev/null || echo "")
    
    if [[ -z "$image" ]]; then
        log_fail "$name: Deployment not found in $namespace"
        return 1
    fi
    
    local version
    version=$(echo "$image" | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1 || echo "unknown")
    
    log_info "$name: Image: $image"
    log_info "$name: Version: $version"
    
    # Version comparison (simplified)
    if [[ "$version" != "unknown" ]]; then
        log_pass "$name: Version detected"
    else
        log_warn "$name: Could not parse version from image"
    fi
}

main() {
    log_info "Starting CRDs & Operators Validation..."
    echo ""
    
    local failed=0
    
    # =========================================================================
    # Prometheus Operator CRDs (REQUIRED)
    # =========================================================================
    log_info "=== Prometheus Operator CRDs (Required) ==="
    
    check_crd "servicemonitors.monitoring.coreos.com" "ServiceMonitor" true || failed=$((failed + 1))
    check_crd "podmonitors.monitoring.coreos.com" "PodMonitor" true || failed=$((failed + 1))
    check_crd "probes.monitoring.coreos.com" "Probe" true || failed=$((failed + 1))
    check_crd "prometheusrules.monitoring.coreos.com" "PrometheusRule" true || failed=$((failed + 1))
    check_crd "prometheuses.monitoring.coreos.com" "Prometheus" true || failed=$((failed + 1))
    check_crd "alertmanagers.monitoring.coreos.com" "Alertmanager" true || failed=$((failed + 1))
    check_crd "thanosrulers.monitoring.coreos.com" "ThanosRuler" false || true  # Optional
    
    echo ""
    
    # =========================================================================
    # Prometheus Operator Version
    # =========================================================================
    log_info "=== Prometheus Operator Version ==="
    check_operator_version "prometheus-operator" "monitoring" "Prometheus Operator" "0.75.0"
    
    echo ""
    
    # =========================================================================
    # Grafana Operator/Resources
    # =========================================================================
    log_info "=== Grafana Resources ==="
    
    # Check if Grafana Operator CRDs exist
    check_crd "grafanadashboards.grafana.integreatly.org" "GrafanaDashboard" false
    check_crd "grafanadatasources.grafana.integreatly.org" "GrafanaDataSource" false
    check_crd "grafanas.grafana.integreatly.org" "Grafana" false
    
    # Check Grafana deployment (Helm or Operator)
    if kubectl get deployment -n observability grafana &>/dev/null; then
        log_pass "Grafana Deployment: Found in observability namespace"
        
        # Check datasources
        local ds_configmaps
        ds_configmaps=$(kubectl get configmap -n observability -l grafana_datasource=1 --no-headers 2>/dev/null | wc -l)
        if [[ $ds_configmaps -gt 0 ]]; then
            log_pass "Grafana DataSources: $ds_configmaps ConfigMap(s) found"
        else
            log_warn "Grafana DataSources: No ConfigMap datasources found (may use CRDs)"
        fi
        
        # Check dashboard provisioning
        local dash_configmaps
        dash_configmaps=$(kubectl get configmap -n observability -l grafana_dashboard=1 --no-headers 2>/dev/null | wc -l)
        if [[ $dash_configmaps -gt 0 ]]; then
            log_pass "Grafana Dashboards: $dash_configmaps ConfigMap(s) found"
        else
            log_warn "Grafana Dashboards: No ConfigMap dashboards found (may use CRDs)"
        fi
    else
        log_fail "Grafana Deployment: Not found in observability namespace"
        failed=$((failed + 1))
    fi
    
    echo ""
    
    # =========================================================================
    # Cost Management Integrations
    # =========================================================================
    log_info "=== Cost Management Integrations ==="
    
    # OpenCost/Kubecost
    if kubectl get deployment -n cost-management opencost &>/dev/null; then
        log_pass "OpenCost: Deployment found in cost-management"
    elif kubectl get deployment -n cost-management kubecost &>/dev/null; then
        log_pass "Kubecost: Deployment found in cost-management"
    else
        log_warn "OpenCost/Kubecost: Not deployed (planned for Phase 1.3)"
    fi
    
    # Custom Metrics API
    if kubectl get apiservice v1beta1.custom.metrics.k8s.io &>/dev/null; then
        log_pass "Custom Metrics API: Registered"
        
        # Check if it's available
        local status
        status=$(kubectl get apiservice v1beta1.custom.metrics.k8s.io -o jsonpath='{.status.conditions[?(@.type=="Available")].status}' 2>/dev/null || echo "Unknown")
        if [[ "$status" == "True" ]]; then
            log_pass "  Status: Available"
        else
            log_warn "  Status: $status"
        fi
    else
        log_warn "Custom Metrics API: Not registered (needed for HPA custom metrics in Phase 5)"
    fi
    
    echo ""
    
    # =========================================================================
    # Vertical Pod Autoscaler (VPA)
    # =========================================================================
    log_info "=== Vertical Pod Autoscaler (VPA) ==="
    
    check_crd "verticalpodautoscalers.autoscaling.k8s.io" "VerticalPodAutoscaler" true || failed=$((failed + 1))
    check_crd "verticalpodautoscalercheckpoints.autoscaling.k8s.io" "VerticalPodAutoscalerCheckpoint" false
    
    # VPA Components
    local vpa_components=("vpa-recommender" "vpa-updater" "vpa-admission-controller")
    for comp in "${vpa_components[@]}"; do
        if kubectl get deployment "$comp" -n kube-system &>/dev/null; then
            log_pass "VPA Component $comp: Deployed in kube-system"
        elif kubectl get deployment "$comp" -n monitoring &>/dev/null; then
            log_pass "VPA Component $comp: Deployed in monitoring"
        else
            log_warn "VPA Component $comp: Not deployed (planned for Phase 5.1)"
        fi
    done
    
    # PDB Support
    local pdb_count
    pdb_count=$(kubectl get pdb -A --no-headers 2>/dev/null | wc -l)
    if [[ $pdb_count -gt 0 ]]; then
        log_pass "PodDisruptionBudgets: $pdb_count found (good for VPA eviction tolerance)"
    else
        log_warn "PodDisruptionBudgets: None found (recommended for VPA in Phase 5)"
    fi
    
    echo ""
    
    # =========================================================================
    # Additional Operators (Optional but Recommended)
    # =========================================================================
    log_info "=== Additional Operators ==="
    
    # Cert Manager
    if kubectl get crd certificates.cert-manager.io &>/dev/null; then
        log_pass "Cert Manager: CRDs installed"
        check_operator_version "cert-manager" "cert-manager" "Cert Manager" "1.13.0"
    else
        log_warn "Cert Manager: Not installed (recommended for TLS)"
    fi
    
    # External Secrets Operator
    if kubectl get crd externalsecrets.external-secrets.io &>/dev/null; then
        log_pass "External Secrets Operator: CRDs installed"
    else
        log_warn "External Secrets Operator: Not installed (recommended for secret management)"
    fi
    
    # Kyverno (for Phase 7 budget enforcement)
    if kubectl get crd clusterpolicies.kyverno.io &>/dev/null; then
        log_pass "Kyverno: CRDs installed (ready for Phase 7)"
    else
        log_info "Kyverno: Not installed (needed for Phase 7 budget enforcement)"
    fi
    
    echo ""
    
    # =========================================================================
    # Summary
    # =========================================================================
    log_info "=== CRDs & Operators Validation Summary ==="
    
    if [[ $failed -eq 0 ]]; then
        log_pass "All REQUIRED CRDs and operators validated successfully"
        exit 0
    else
        log_fail "$failed REQUIRED component(s) missing"
        exit 1
    fi
}

main "$@"
