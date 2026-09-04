#!/bin/bash
# Prometheus/Thanos Validation Script for Phase 0
# Validates Prometheus HA, Thanos components, and retention settings

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

validate_prometheus() {
    log_info "=== Validating Prometheus ==="
    
    # Check Prometheus CR
    local prom_crs
    prom_crs=$(kubectl get prometheus -n monitoring --no-headers 2>/dev/null | wc -l)
    
    if [[ $prom_crs -gt 0 ]]; then
        log_pass "Prometheus CR(s) found: $prom_crs"
        
        # Check each Prometheus
        kubectl get prometheus -n monitoring -o jsonpath='{.items[*].metadata.name}' | tr ' ' '\n' | while read -r name; do
            [[ -z "$name" ]] && continue
            
            log_info "Checking Prometheus: $name"
            
            # Retention
            local retention
            retention=$(kubectl get prometheus "$name" -n monitoring -o jsonpath='{.spec.retention}' 2>/dev/null || echo "not set")
            if [[ "$retention" == "2h" ]]; then
                log_pass "  Retention: $retention (correct)"
            else
                log_warn "  Retention: $retention (expected 2h)"
            fi
            
            # Retention size
            local retention_size
            retention_size=$(kubectl get prometheus "$name" -n monitoring -o jsonpath='{.spec.retentionSize}' 2>/dev/null || echo "not set")
            log_info "  Retention Size: $retention_size"
            
            # Enable features
            local features
            features=$(kubectl get prometheus "$name" -n monitoring -o jsonpath='{.spec.enableFeatures}' 2>/dev/null || echo "[]")
            if echo "$features" | grep -q "ExemplarStorage"; then
                log_pass "  ExemplarStorage: Enabled"
            else
                log_warn "  ExemplarStorage: Not enabled"
            fi
            
            # Thanos sidecar
            local sidecar_image
            sidecar_image=$(kubectl get prometheus "$name" -n monitoring -o jsonpath='{.spec.thanos.sidecarImage}' 2>/dev/null || echo "not set")
            if [[ "$sidecar_image" != "not set" ]]; then
                log_pass "  Thanos Sidecar Image: $sidecar_image"
            else
                log_warn "  Thanos Sidecar Image: Not configured"
            fi
            
            # Object storage config
            local objstore_secret
            objstore_secret=$(kubectl get prometheus "$name" -n monitoring -o jsonpath='{.spec.thanos.objectStorageConfig.name}' 2>/dev/null || echo "")
            if [[ -n "$objstore_secret" ]]; then
                log_pass "  Object Storage Config Secret: $objstore_secret"
                
                # Verify secret exists
                if kubectl get secret "$objstore_secret" -n monitoring &>/dev/null; then
                    log_pass "  Secret exists and accessible"
                else
                    log_fail "  Secret $objstore_secret NOT found"
                fi
            else
                log_fail "  Object Storage Config: Not configured"
            fi
        done
    else
        log_fail "No Prometheus CR found in monitoring namespace"
    fi
    
    # Check Prometheus pods (HA)
    local prom_pods
    prom_pods=$(kubectl get pods -n monitoring -l prometheus=k8s --no-headers 2>/dev/null | wc -l)
    if [[ $prom_pods -ge 2 ]]; then
        log_pass "Prometheus HA: $prom_pods pods running"
    else
        log_fail "Prometheus HA: Only $prom_pods pod(s) running (expected 2+)"
    fi
    
    # Check ServiceMonitors for Alloy
    local sm_count
    sm_count=$(kubectl get servicemonitor -n monitoring --no-headers 2>/dev/null | wc -l)
    log_info "ServiceMonitors in monitoring: $sm_count"
    
    # Check specific ServiceMonitors
    for sm in alloy-daemonset alloy-central kube-state-metrics node-exporter; do
        if kubectl get servicemonitor "$sm" -n monitoring &>/dev/null; then
            log_pass "  ServiceMonitor: $sm"
        else
            log_warn "  ServiceMonitor: $sm (not found)"
        fi
    done
}

validate_thanos() {
    log_info "=== Validating Thanos Components ==="
    
    # Thanos Query
    if kubectl get deployment thanos-query -n monitoring &>/dev/null; then
        log_pass "Thanos Query: Deployment exists"
        
        local ready replicas
        ready=$(kubectl get deployment thanos-query -n monitoring -o jsonpath='{.status.readyReplicas}')
        replicas=$(kubectl get deployment thanos-query -n monitoring -o jsonpath='{.spec.replicas}')
        if [[ "$ready" == "$replicas" && "$ready" -gt 0 ]]; then
            log_pass "  Replicas: $ready/$replicas ready"
        else
            log_fail "  Replicas: $ready/$replicas ready"
        fi
        
        # Check stores configuration
        local query_args
        query_args=$(kubectl get deployment thanos-query -n monitoring -o jsonpath='{.spec.template.spec.containers[0].args}' 2>/dev/null || echo "")
        if echo "$query_args" | grep -q "store"; then
            log_pass "  Store configuration: Found"
        else
            log_warn "  Store configuration: Not visible in args"
        fi
    else
        log_fail "Thanos Query: Deployment not found"
    fi
    
    # Thanos Store
    if kubectl get statefulset thanos-store -n monitoring &>/dev/null; then
        log_pass "Thanos Store: StatefulSet exists"
        
        local ready replicas
        ready=$(kubectl get statefulset thanos-store -n monitoring -o jsonpath='{.status.readyReplicas}')
        replicas=$(kubectl get statefulset thanos-store -n monitoring -o jsonpath='{.spec.replicas}')
        if [[ "$ready" == "$replicas" && "$ready" -gt 0 ]]; then
            log_pass "  Replicas: $ready/$replicas ready"
        else
            log_fail "  Replicas: $ready/$replicas ready"
        fi
    else
        log_fail "Thanos Store: StatefulSet not found"
    fi
    
    # Thanos Compact
    if kubectl get deployment thanos-compact -n monitoring &>/dev/null; then
        log_pass "Thanos Compact: Deployment exists"
        
        local ready replicas
        ready=$(kubectl get deployment thanos-compact -n monitoring -o jsonpath='{.status.readyReplicas}')
        replicas=$(kubectl get deployment thanos-compact -n monitoring -o jsonpath='{.spec.replicas}')
        if [[ "$ready" == "$replicas" && "$ready" -gt 0 ]]; then
            log_pass "  Replicas: $ready/$replicas ready"
        else
            log_fail "  Replicas: $ready/$replicas ready"
        fi
        
        # Check retention policies
        local compact_args
        compact_args=$(kubectl get deployment thanos-compact -n monitoring -o jsonpath='{.spec.template.spec.containers[0].args}' 2>/dev/null || echo "")
        
        local retentions=("retention.resolution-raw=14d" "retention.resolution-5m=90d" "retention.resolution-1h=365d")
        for ret in "${retentions[@]}"; do
            if echo "$compact_args" | grep -q "$ret"; then
                log_pass "  Retention policy: $ret"
            else
                log_warn "  Retention policy: $ret (not found)"
            fi
        done
    else
        log_fail "Thanos Compact: Deployment not found"
    fi
    
    # Thanos Receive (Phase 2 - optional)
    if kubectl get statefulset thanos-receive -n monitoring &>/dev/null; then
        log_pass "Thanos Receive: StatefulSet exists (Phase 2 deployed)"
    else
        log_info "Thanos Receive: Not deployed (Phase 2 optional component)"
    fi
    
    # Object storage bucket verification
    log_info "=== Object Storage Verification ==="
    if kubectl get secret thanos-objstore-config -n monitoring &>/dev/null; then
        log_pass "Thanos object storage secret exists"
        
        # Try to decode and validate (without exposing credentials)
        local secret_data
        secret_data=$(kubectl get secret thanos-objstore-config -n monitoring -o jsonpath='{.data.objstore\.yml}' | base64 -d 2>/dev/null || echo "")
        
        if [[ -n "$secret_data" ]]; then
            if echo "$secret_data" | grep -q "type:"; then
                local bucket_type
                bucket_type=$(echo "$secret_data" | grep "type:" | head -1 | awk '{print $2}')
                log_pass "  Bucket type: $bucket_type"
            fi
            
            if echo "$secret_data" | grep -q "bucket:"; then
                log_pass "  Bucket name: Configured"
            else
                log_warn "  Bucket name: Not found in config"
            fi
        fi
    else
        log_fail "Thanos object storage secret NOT found"
    fi
}

validate_thanos_query_api() {
    log_info "=== Testing Thanos Query API ==="
    
    # Test via port-forward to avoid network policy issues
    local port=10902
    kubectl port-forward -n monitoring svc/thanos-query "$port":10902 >/dev/null 2>&1 &
    local pf_pid=$!
    sleep 3
    
    # Test health
    local health
    health=$(curl -s -o /dev/null -w "%{http_code}" --max-time 5 "http://localhost:$port/-/healthy" 2>/dev/null || echo "000")
    
    kill $pf_pid 2>/dev/null || true
    
    if [[ "$health" == "200" ]]; then
        log_pass "Thanos Query API: Healthy (HTTP 200)"
    else
        log_fail "Thanos Query API: Unhealthy (HTTP $health)"
    fi
    
    # Test query API
    local query_result
    query_result=$(curl -s -o /dev/null -w "%{http_code}" --max-time 10 "http://localhost:$port/api/v1/query?query=up" 2>/dev/null || echo "000")
    
    if [[ "$query_result" == "200" ]]; then
        log_pass "Thanos Query API: Query endpoint working"
    else
        log_warn "Thanos Query API: Query endpoint returned HTTP $query_result"
    fi
}

main() {
    log_info "Starting Prometheus/Thanos Validation..."
    echo ""
    
    validate_prometheus
    echo ""
    validate_thanos
    echo ""
    validate_thanos_query_api
    echo ""
    
    log_info "Prometheus/Thanos validation complete"
}

main "$@"
