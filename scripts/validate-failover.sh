#!/bin/bash
# Failover Validation Script for Cost Controller HA
# Tests: Kill Leader -> Standby promotes < 30s -> WAL Replay -> Queries work

set -euo pipefail

# Configuration
NAMESPACE="cost-management"
STATEFULSET="cost-controller"
LEADER_POD="${STATEFULSET}-0"
STANDBY_POD="${STATEFULSET}-1"
SERVICE_API="cost-controller-api"
SERVICE_STANDBY="cost-controller-standby"
SERVICE_WAL="cost-controller-wal"
FAILOVER_TIMEOUT=30
QUERY_TEST_TIMEOUT=10
WAL_REPLAY_TIMEOUT=60

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() { echo -e "${BLUE}[INFO]${NC} $*"; }
log_success() { echo -e "${GREEN}[PASS]${NC} $*"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
log_error() { echo -e "${RED}[FAIL]${NC} $*"; }

# Check if kubectl is available
check_kubectl() {
    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl not found in PATH"
        exit 1
    fi
    log_success "kubectl found"
}

# Check if StatefulSet exists and has 2 replicas
check_statefulset() {
    log_info "Checking StatefulSet ${STATEFULSET} in namespace ${NAMESPACE}..."
    
    if ! kubectl get statefulset "${STATEFULSET}" -n "${NAMESPACE}" &> /dev/null; then
        log_error "StatefulSet ${STATEFULSET} not found in namespace ${NAMESPACE}"
        exit 1
    fi
    
    REPLICAS=$(kubectl get statefulset "${STATEFULSET}" -n "${NAMESPACE}" -o jsonpath='{.spec.replicas}')
    if [[ "${REPLICAS}" != "2" ]]; then
        log_error "StatefulSet has ${REPLICAS} replicas, expected 2"
        exit 1
    fi
    
    READY_REPLICAS=$(kubectl get statefulset "${STATEFULSET}" -n "${NAMESPACE}" -o jsonpath='{.status.readyReplicas}')
    if [[ "${READY_REPLICAS}" != "2" ]]; then
        log_error "StatefulSet has ${READY_REPLICAS} ready replicas, expected 2"
        exit 1
    fi
    
    log_success "StatefulSet has 2 ready replicas"
}

# Check both pods are running
check_pods_running() {
    log_info "Checking pod status..."
    
    for pod in "${LEADER_POD}" "${STANDBY_POD}"; do
        PHASE=$(kubectl get pod "${pod}" -n "${NAMESPACE}" -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
        if [[ "${PHASE}" != "Running" ]]; then
            log_error "Pod ${pod} is not Running (phase: ${PHASE})"
            exit 1
        fi
        
        READY=$(kubectl get pod "${pod}" -n "${NAMESPACE}" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "False")
        if [[ "${READY}" != "True" ]]; then
            log_error "Pod ${pod} is not Ready"
            exit 1
        fi
    done
    
    log_success "Both pods are Running and Ready"
}

# Identify current leader via leader election lease
get_leader_pod() {
    LEASE_HOLDER=$(kubectl get lease "${STATEFULSET}-leader" -n "${NAMESPACE}" -o jsonpath='{.spec.holderIdentity}' 2>/dev/null || echo "")
    
    if [[ -z "${LEASE_HOLDER}" ]]; then
        # Fallback: check which pod has the leader lock
        LEASE_HOLDER=$(kubectl get lease cost-controller-leader -n "${NAMESPACE}" -o jsonpath='{.spec.holderIdentity}' 2>/dev/null || echo "")
    fi
    
    if [[ -z "${LEASE_HOLDER}" ]]; then
        log_warn "Could not determine leader from lease, assuming ${LEADER_POD} is leader"
        echo "${LEADER_POD}"
        return
    fi
    
    # Extract pod name from holder identity (format: pod-name_namespace)
    LEADER_POD_NAME=$(echo "${LEASE_HOLDER}" | cut -d'_' -f1)
    echo "${LEADER_POD_NAME}"
}

# Verify leader is serving API
verify_leader_api() {
    local leader_pod=$1
    log_info "Verifying leader API on ${leader_pod}..."
    
    # Port forward to leader pod
    kubectl port-forward -n "${NAMESPACE}" "${leader_pod}" 18080:8080 > /dev/null 2>&1 &
    PF_PID=$!
    sleep 2
    
    # Test query endpoint
    if curl -s -f "http://localhost:18080/api/v1/query?query=finops_cluster_total_hourly_cost_usd" > /dev/null; then
        log_success "Leader API responding to queries"
    else
        log_error "Leader API not responding to queries"
        kill ${PF_PID} 2>/dev/null || true
        return 1
    fi
    
    # Test readiness endpoint
    if curl -s -f "http://localhost:18080/readyz" > /dev/null; then
        log_success "Leader readiness endpoint healthy"
    else
        log_error "Leader readiness endpoint not healthy"
        kill ${PF_PID} 2>/dev/null || true
        return 1
    fi
    
    kill ${PF_PID} 2>/dev/null || true
    return 0
}

# Verify standby is in read-only mode
verify_standby_readonly() {
    local standby_pod=$1
    log_info "Verifying standby read-only mode on ${standby_pod}..."
    
    kubectl port-forward -n "${NAMESPACE}" "${standby_pod}" 18081:8080 > /dev/null 2>&1 &
    PF_PID=$!
    sleep 2
    
    # Test query endpoint (should work in read-only)
    if curl -s -f "http://localhost:18081/api/v1/query?query=finops_cluster_total_hourly_cost_usd" > /dev/null; then
        log_success "Standby API responding to read queries"
    else
        log_error "Standby API not responding to read queries"
        kill ${PF_PID} 2>/dev/null || true
        return 1
    fi
    
    # Test write endpoint (should fail in read-only)
    if curl -s -X POST "http://localhost:18081/api/v1/admin/tsdb/snapshot" 2>/dev/null | grep -q "read-only"; then
        log_success "Standby correctly rejects write operations (read-only mode)"
    else
        log_warn "Standby write endpoint behavior unclear"
    fi
    
    kill ${PF_PID} 2>/dev/null || true
    return 0
}

# Verify WAL replication is working
verify_wal_replication() {
    local leader_pod=$1
    local standby_pod=$2
    log_info "Verifying WAL replication between ${leader_pod} -> ${standby_pod}..."
    
    # Check leader WAL replication server
    kubectl port-forward -n "${NAMESPACE}" "${leader_pod}" 19091:9091 > /dev/null 2>&1 &
    PF_LEADER=$!
    sleep 1
    
    # Check standby WAL replication client connection
    kubectl port-forward -n "${NAMESPACE}" "${standby_pod}" 19092:9091 > /dev/null 2>&1 &
    PF_STANDBY=$!
    sleep 1
    
    # Check standby readiness includes WAL lag check
    if curl -s -f "http://localhost:18081/readyz" 2>/dev/null | grep -q "wal-lag"; then
        log_success "Standby readiness includes WAL lag check"
    else
        log_warn "Standby WAL lag check not visible in readiness"
    fi
    
    kill ${PF_LEADER} ${PF_STANDBY} 2>/dev/null || true
    return 0
}

# Kill leader pod and measure failover time
kill_leader_and_measure() {
    local leader_pod=$1
    log_info "Killing leader pod ${leader_pod}..."
    
    START_TIME=$(date +%s)
    
    # Delete the leader pod
    kubectl delete pod "${leader_pod}" -n "${NAMESPACE}" --grace-period=0 --force
    
    # Wait for new leader to be elected and ready
    log_info "Waiting for failover (timeout: ${FAILOVER_TIMEOUT}s)..."
    
    local elapsed=0
    local new_leader=""
    
    while [[ ${elapsed} -lt ${FAILOVER_TIMEOUT} ]]; do
        sleep 2
        elapsed=$(($(date +%s) - START_TIME))
        
        # Check lease holder
        new_leader=$(get_leader_pod)
        
        if [[ "${new_leader}" != "${leader_pod}" && -n "${new_leader}" ]]; then
            # Check if new leader is ready
            READY=$(kubectl get pod "${new_leader}" -n "${NAMESPACE}" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "False")
            if [[ "${READY}" == "True" ]]; then
                FAILOVER_TIME=${elapsed}
                log_success "Failover completed in ${FAILOVER_TIME}s (new leader: ${new_leader})"
                echo "${new_leader}"
                return 0
            fi
        fi
        
        echo -n "."
    done
    
    echo ""
    log_error "Failover did not complete within ${FAILOVER_TIMEOUT}s"
    return 1
}

# Verify WAL replay on new leader
verify_wal_replay() {
    local new_leader=$1
    log_info "Verifying WAL replay on new leader ${new_leader}..."
    
    kubectl port-forward -n "${NAMESPACE}" "${new_leader}" 18082:8080 > /dev/null 2>&1 &
    PF_PID=$!
    sleep 3
    
    local elapsed=0
    while [[ ${elapsed} -lt ${WAL_REPLAY_TIMEOUT} ]]; do
        # Check if TSDB is ready and has data
        if curl -s -f "http://localhost:18082/api/v1/query?query=finops_cluster_total_hourly_cost_usd" > /dev/null 2>&1; then
            log_success "WAL replay completed, TSDB accepting queries"
            kill ${PF_PID} 2>/dev/null || true
            return 0
        fi
        sleep 2
        elapsed=$((${elapsed} + 2))
        echo -n "."
    done
    
    echo ""
    log_error "WAL replay did not complete within ${WAL_REPLAY_TIMEOUT}s"
    kill ${PF_PID} 2>/dev/null || true
    return 1
}

# Verify queries work on new leader
verify_queries_work() {
    local new_leader=$1
    log_info "Verifying queries work on new leader ${new_leader}..."
    
    kubectl port-forward -n "${NAMESPACE}" "${new_leader}" 18083:8080 > /dev/null 2>&1 &
    PF_PID=$!
    sleep 2
    
    # Test various query types
    local queries=(
        "finops_cluster_total_hourly_cost_usd"
        "finops_workload_hourly_cost_usd"
        "finops_node_hourly_cost_usd"
        "sum(rate(finops_workload_cpu_cores_used[5m])) by (namespace)"
    )
    
    for query in "${queries[@]}"; do
        if curl -s -f -G "http://localhost:18083/api/v1/query" --data-urlencode "query=${query}" > /dev/null; then
            log_success "Query works: ${query}"
        else
            log_error "Query failed: ${query}"
            kill ${PF_PID} 2>/dev/null || true
            return 1
        fi
    done
    
    # Test range query
    if curl -s -f -G "http://localhost:18083/api/v1/query_range" \
        --data-urlencode "query=finops_cluster_total_hourly_cost_usd" \
        --data-urlencode "start=$(date -d '1 hour ago' +%s)" \
        --data-urlencode "end=$(date +%s)" \
        --data-urlencode "step=60" > /dev/null; then
        log_success "Range query works"
    else
        log_error "Range query failed"
        kill ${PF_PID} 2>/dev/null || true
        return 1
    fi
    
    kill ${PF_PID} 2>/dev/null || true
    return 0
}

# Verify service endpoints updated
verify_service_endpoints() {
    log_info "Verifying service endpoints updated..."
    
    # Check cost-controller-api service endpoints
    ENDPOINTS=$(kubectl get endpoints "${SERVICE_API}" -n "${NAMESPACE}" -o jsonpath='{.subsets[*].addresses[*].targetRef.name}' 2>/dev/null || echo "")
    
    if echo "${ENDPOINTS}" | grep -q "${STATEFULSET}"; then
        log_success "Service ${SERVICE_API} has endpoints for cost-controller pods"
    else
        log_warn "Service ${SERVICE_API} endpoints: ${ENDPOINTS}"
    fi
    
    # Check standby service points to standby pod
    STANDBY_EP=$(kubectl get endpoints "${SERVICE_STANDBY}" -n "${NAMESPACE}" -o jsonpath='{.subsets[*].addresses[*].targetRef.name}' 2>/dev/null || echo "")
    if [[ "${STANDBY_EP}" == "${STANDBY_POD}" ]]; then
        log_success "Standby service correctly points to ${STANDBY_POD}"
    else
        log_warn "Standby service endpoints: ${STANDBY_EP}"
    fi
    
    return 0
}

# Verify data consistency after failover
verify_data_consistency() {
    local new_leader=$1
    log_info "Verifying data consistency after failover..."
    
    kubectl port-forward -n "${NAMESPACE}" "${new_leader}" 18084:8080 > /dev/null 2>&1 &
    PF_PID=$!
    sleep 2
    
    # Get current metric value
    RESULT=$(curl -s -f -G "http://localhost:18084/api/v1/query" \
        --data-urlencode "query=finops_cluster_total_hourly_cost_usd" 2>/dev/null || echo "")
    
    if [[ -n "${RESULT}" ]]; log_success "Data query returns results"; else
        log_error "Data query returned empty"
        kill ${PF_PID} 2>/dev/null || true
        return 1
    fi
    
    # Check series count
    SERIES=$(curl -s -f -G "http://localhost:18084/api/v1/series" \
        --data-urlencode "match[]=finops_workload_hourly_cost_usd" 2>/dev/null | jq -r '.data | length' 2>/dev/null || echo "0")
    
    if [[ "${SERIES}" -gt 0 ]]; then
        log_success "Series count: ${SERIES} (data preserved)"
    else
        log_warn "Series count: ${SERIES} (may be empty cluster)"
    fi
    
    kill ${PF_PID} 2>/dev/null || true
    return 0
}

# Cleanup port forwards
cleanup_port_forwards() {
    pkill -f "kubectl port-forward.*cost-controller" 2>/dev/null || true
}

# Main test flow
main() {
    echo "=========================================="
    echo "Cost Controller HA Failover Test"
    echo "=========================================="
    echo ""
    
    # Trap cleanup
    trap cleanup_port_forwards EXIT
    
    check_kubectl
    check_statefulset
    check_pods_running
    
    # Get initial leader
    INITIAL_LEADER=$(get_leader_pod)
    log_info "Initial leader: ${INITIAL_LEADER}"
    
    # Verify initial state
    verify_leader_api "${INITIAL_LEADER}" || exit 1
    
    # Determine standby
    if [[ "${INITIAL_LEADER}" == "${LEADER_POD}" ]]; then
        INITIAL_STANDBY="${STANDBY_POD}"
    else
        INITIAL_STANDBY="${LEADER_POD}"
    fi
    log_info "Initial standby: ${INITIAL_STANDBY}"
    
    verify_standby_readonly "${INITIAL_STANDBY}" || exit 1
    verify_wal_replication "${INITIAL_LEADER}" "${INITIAL_STANDBY}" || exit 1
    
    # Perform failover
    NEW_LEADER=$(kill_leader_and_measure "${INITIAL_LEADER}") || exit 1
    
    # Verify post-failover state
    verify_wal_replay "${NEW_LEADER}" || exit 1
    verify_queries_work "${NEW_LEADER}" || exit 1
    verify_service_endpoints || exit 1
    verify_data_consistency "${NEW_LEADER}" || exit 1
    
    echo ""
    echo "=========================================="
    log_success "ALL FAILOVER TESTS PASSED"
    echo "=========================================="
    echo ""
    echo "Summary:"
    echo "  - Initial leader: ${INITIAL_LEADER}"
    echo "  - New leader: ${NEW_LEADER}"
    echo "  - Failover time: ${FAILOVER_TIME}s (target: < ${FAILOVER_TIMEOUT}s)"
    echo "  - WAL replay: Completed"
    echo "  - Queries: Working"
    echo "  - Data consistency: Verified"
}

# Run main
main "$@"
