#!/bin/bash
# Alloy Configuration Validation Script
# Validates Alloy DaemonSet and Central configurations for Phase 0 requirements

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

# Get Alloy DaemonSet config
get_daemonset_config() {
    kubectl get configmap -n monitoring alloy-config -o jsonpath='{.data.config\.alloy}' 2>/dev/null || echo ""
}

# Get Alloy Central config
get_central_config() {
    kubectl get configmap -n monitoring alloy-central-config -o jsonpath='{.data.config\.alloy}' 2>/dev/null || echo ""
}

validate_daemonset_config() {
    log_info "=== Validating Alloy DaemonSet Configuration ==="
    
    local config
    config=$(get_daemonset_config)
    
    if [[ -z "$config" ]]; then
        log_fail "Could not retrieve Alloy DaemonSet config (ConfigMap: alloy-config in monitoring)"
        return 1
    fi
    
    log_pass "Retrieved Alloy DaemonSet config"
    
    # Check node-exporter integration
    if echo "$config" | grep -q "prometheus.exporter.node"; then
        log_pass "node-exporter integration found"
        
        # Check for key metrics
        local metrics=("node_cpu_seconds_total" "node_memory_MemTotal_bytes" "node_filesystem_size_bytes" "node_network_receive_bytes_total")
        for metric in "${metrics[@]}"; do
            if echo "$config" | grep -q "$metric"; then
                log_pass "  Metric $metric: Configured"
            else
                log_warn "  Metric $metric: Not explicitly configured (may be default)"
            fi
        done
    else
        log_fail "node-exporter integration NOT found"
    fi
    
    # Check cAdvisor integration
    if echo "$config" | grep -q "prometheus.exporter.cadvisor"; then
        log_pass "cAdvisor integration found"
        
        local cadvisor_metrics=("container_spec_memory_limit_bytes" "container_spec_cpu_quota" "container_memory_working_set_bytes" "container_cpu_usage_seconds_total" "container_fs_usage_bytes" "container_fs_limit_bytes")
        for metric in "${cadvisor_metrics[@]}"; do
            if echo "$config" | grep -q "$metric"; then
                log_pass "  Metric $metric: Configured"
            else
                log_warn "  Metric $metric: Not explicitly configured (may be default)"
            fi
        done
    else
        log_fail "cAdvisor integration NOT found"
    fi
    
    # Check pod_logs with cost labels
    if echo "$config" | grep -q "loki.source.kubernetes"; then
        log_pass "loki.source.kubernetes (pod_logs) found"
        
        local labels=("namespace" "pod" "container" "cost_center" "team")
        for label in "${labels[@]}"; do
            if echo "$config" | grep -q "$label"; then
                log_pass "  Label $label: Configured"
            else
                log_fail "  Label $label: MISSING from pod_logs configuration"
            fi
        done
        
        # Check for relabeling from Kubernetes API
        if echo "$config" | grep -q "__meta_kubernetes_namespace_label_cost_center"; then
            log_pass "  cost_center relabeling from namespace label: Configured"
        else
            log_warn "  cost_center relabeling: Not found (may use different method)"
        fi
        
        if echo "$config" | grep -q "__meta_kubernetes_namespace_label_team"; then
            log_pass "  team relabeling from namespace label: Configured"
        else
            log_warn "  team relabeling: Not found (may use different method)"
        fi
    else
        log_fail "loki.source.kubernetes (pod_logs) NOT found"
    fi
    
    # Check prometheus.scrape for node-exporter (network metrics)
    if echo "$config" | grep -q "prometheus.scrape.*node-exporter"; then
        log_pass "prometheus.scrape for node-exporter found"
        if echo "$config" | grep -A 20 "prometheus.scrape.*node-exporter" | grep -q "node_network"; then
            log_pass "  node_network metrics: Included in scrape"
        else
            log_warn "  node_network metrics: Not explicitly included"
        fi
    else
        log_warn "prometheus.scrape for node-exporter: Not found (may use service discovery)"
    fi
}

validate_central_config() {
    log_info "=== Validating Alloy Central Configuration ==="
    
    local config
    config=$(get_central_config)
    
    if [[ -z "$config" ]]; then
        log_fail "Could not retrieve Alloy Central config (ConfigMap: alloy-central-config in monitoring)"
        return 1
    fi
    
    log_pass "Retrieved Alloy Central config"
    
    # Check prometheus.remote_write to Prometheus HA
    if echo "$config" | grep -q "prometheus.remote_write"; then
        log_pass "prometheus.remote_write block found"
        
        # Check for HA endpoints
        local endpoints=("prometheus-operated-0.monitoring:9090" "prometheus-operated-1.monitoring:9090")
        for ep in "${endpoints[@]}"; do
            if echo "$config" | grep -q "$ep"; then
                log_pass "  Endpoint $ep: Configured"
            else
                log_warn "  Endpoint $ep: Not found (check HA configuration)"
            fi
        done
        
        # Check for fan-out to Thanos Receive (Phase 2)
        if echo "$config" | grep -q "thanos-receive"; then
            log_pass "  Thanos Receive fan-out: Configured (Phase 2 ready)"
        else
            log_info "  Thanos Receive fan-out: Not configured (Phase 2 feature)"
        fi
    else
        log_fail "prometheus.remote_write NOT found"
    fi
    
    # Check loki.write
    if echo "$config" | grep -q "loki.write"; then
        log_pass "loki.write block found"
        
        if echo "$config" | grep -A 10 "loki.write" | grep -q "endpoint"; then
            local endpoint
            endpoint=$(echo "$config" | grep -A 10 "loki.write" | grep "endpoint" | head -1 | sed 's/.*=//' | tr -d ' "')
            log_pass "  Endpoint: $endpoint"
        else
            log_warn "  Endpoint: Not explicitly found in config snippet"
        fi
    else
        log_fail "loki.write NOT found"
    fi
    
    # Check kube-state-metrics scrape
    if echo "$config" | grep -q "prometheus.scrape.*kube_state_metrics"; then
        log_pass "kube-state-metrics scrape configured"
        
        local ksm_metrics=("kube_pod_container_resource_requests" "kube_pod_container_resource_limits" "kube_node_status_capacity" "kube_node_status_allocatable" "kube_pod_owner" "kube_namespace_labels")
        for metric in "${ksm_metrics[@]}"; do
            if echo "$config" | grep -q "$metric"; then
                log_pass "  Metric $metric: Referenced"
            else
                log_warn "  Metric $metric: Not explicitly referenced (may be scraped by default)"
            fi
        done
    else
        log_fail "kube-state-metrics scrape NOT found"
    fi
    
    # Check OpenCost scrape (Phase 1.3)
    if echo "$config" | grep -q "prometheus.scrape.*opencost"; then
        log_pass "OpenCost scrape configured (Phase 1.3 ready)"
    else
        log_info "OpenCost scrape: Not configured (will be added in Phase 1.3)"
    fi
}

validate_running_config() {
    log_info "=== Validating Running Alloy Instances ==="
    
    # DaemonSet
    local ds_pods
    ds_pods=$(kubectl get pods -n monitoring -l app=alloy-daemonset -o jsonpath='{.items[*].metadata.name}' 2>/dev/null)
    
    if [[ -n "$ds_pods" ]]; then
        local count=0
        for pod in $ds_pods; do
            count=$((count + 1))
            local status
            status=$(kubectl get pod -n monitoring "$pod" -o jsonpath='{.status.phase}')
            if [[ "$status" == "Running" ]]; then
                log_pass "Alloy DaemonSet pod $pod: Running"
            else
                log_fail "Alloy DaemonSet pod $pod: $status"
            fi
        done
        log_info "Total Alloy DaemonSet pods: $count"
    else
        log_fail "No Alloy DaemonSet pods found"
    fi
    
    # Central
    local central_pods
    central_pods=$(kubectl get pods -n monitoring -l app=alloy-central -o jsonpath='{.items[*].metadata.name}' 2>/dev/null)
    
    if [[ -n "$central_pods" ]]; then
        local count=0
        for pod in $central_pods; do
            count=$((count + 1))
            local status
            status=$(kubectl get pod -n monitoring "$pod" -o jsonpath='{.status.phase}')
            if [[ "$status" == "Running" ]]; then
                log_pass "Alloy Central pod $pod: Running"
            else
                log_fail "Alloy Central pod $pod: $status"
            fi
        done
        log_info "Total Alloy Central pods: $count"
    else
        log_fail "No Alloy Central pods found"
    fi
}

main() {
    log_info "Starting Alloy Configuration Validation..."
    echo ""
    
    validate_daemonset_config
    echo ""
    validate_central_config
    echo ""
    validate_running_config
    echo ""
    
    log_info "Alloy configuration validation complete"
}

main "$@"
