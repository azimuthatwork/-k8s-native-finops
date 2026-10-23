#!/bin/bash
# validate-query-performance.sh - Validate TSDB Query Performance
# Part of Phase 2 Task 2.1.2

set -euo pipefail

# Configuration
NAMESPACE="${NAMESPACE:-cost-management}"
POD_NAME="${POD_NAME:-cost-controller-0}"
CONTAINER_NAME="${CONTAINER_NAME:-cost-controller}"
API_PORT="${API_PORT:-8082}"
API_BASE="http://localhost:${API_PORT}/api/v1"

# Performance thresholds (from PLAN-NATIVE.md)
INSTANT_QUERY_THRESHOLD_MS=500
RANGE_QUERY_THRESHOLD_MS=2000
SERIES_DISCOVERY_THRESHOLD_MS=1000

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Results array
RESULTS=()

# Helper functions
log_info() { echo -e "${BLUE}[INFO]${NC} $*"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }
log_pass() { echo -e "${GREEN}[PASS]${NC} $*"; }

add_result() {
    local check_name="$1"
    local status="$2"  # PASS, WARN, FAIL
    local message="$3"
    local value_ms="$4"
    local threshold_ms="$5"
    RESULTS+=("{\"check\":\"$check_name\",\"status\":\"$status\",\"message\":\"$message\",\"value_ms\":$value_ms,\"threshold_ms\":$threshold_ms}")
}

# Get current timestamp in milliseconds
get_timestamp_ms() {
    date +%s%3N
}

# Execute query and measure latency
execute_query() {
    local url="$1"
    local start_time
    local end_time
    local response
    local http_code
    
    start_time=$(get_timestamp_ms)
    response=$(curl -s -w "\n%{http_code}" "$url" 2>/dev/null || echo -e "\n000")
    end_time=$(get_timestamp_ms)
    
    http_code=$(echo "$response" | tail -1)
    body=$(echo "$response" | head -n -1)
    
    latency_ms=$((end_time - start_time))
    
    echo "$latency_ms $http_code $body"
}

# Test instant query
test_instant_query() {
    local query="${1:-finops_cluster_total_hourly_cost_usd}"
    local url="${API_BASE}/query?query=$(echo "$query" | sed 's/ /%20/g')"
    
    log_info "Testing instant query: $query"
    
    local result
    result=$(execute_query "$url")
    read -r latency_ms http_code body <<< "$result"
    
    if [[ "$http_code" != "200" ]]; then
        add_result "instant_query" "FAIL" "HTTP $http_code: $body" "$latency_ms" "$INSTANT_QUERY_THRESHOLD_MS"
        return 1
    fi
    
    # Validate response format
    if ! echo "$body" | jq -e '.status == "success"' >/dev/null 2>&1; then
        add_result "instant_query" "FAIL" "Invalid response format: $body" "$latency_ms" "$INSTANT_QUERY_THRESHOLD_MS"
        return 1
    fi
    
    if [[ $latency_ms -gt $INSTANT_QUERY_THRESHOLD_MS ]]; then
        add_result "instant_query" "FAIL" "Instant query latency ${latency_ms}ms exceeds ${INSTANT_QUERY_THRESHOLD_MS}ms threshold" "$latency_ms" "$INSTANT_QUERY_THRESHOLD_MS"
        return 1
    else
        add_result "instant_query" "PASS" "Instant query latency ${latency_ms}ms within threshold" "$latency_ms" "$INSTANT_QUERY_THRESHOLD_MS"
        return 0
    fi
}

# Test range query
test_range_query() {
    local query="${1:-finops_workload_hourly_cost_usd}"
    local start_time
    local end_time
    local step="${2:-3600}"  # 1 hour default
    local duration="${3:-2592000}"  # 30 days default
    
    end_time=$(date +%s)
    start_time=$((end_time - duration))
    
    local url="${API_BASE}/query_range?query=$(echo "$query" | sed 's/ /%20/g')&start=${start_time}&end=${end_time}&step=${step}"
    
    log_info "Testing range query: $query (${duration}s, step=${step}s)"
    
    local result
    result=$(execute_query "$url")
    read -r latency_ms http_code body <<< "$result"
    
    if [[ "$http_code" != "200" ]]; then
        add_result "range_query" "FAIL" "HTTP $http_code: $body" "$latency_ms" "$RANGE_QUERY_THRESHOLD_MS"
        return 1
    fi
    
    # Validate response format
    if ! echo "$body" | jq -e '.status == "success"' >/dev/null 2>&1; then
        add_result "range_query" "FAIL" "Invalid response format: $body" "$latency_ms" "$RANGE_QUERY_THRESHOLD_MS"
        return 1
    fi
    
    # Check data points count
    local data_points
    data_points=$(echo "$body" | jq '.data.result[0].values | length' 2>/dev/null || echo "0")
    
    if [[ $latency_ms -gt $RANGE_QUERY_THRESHOLD_MS ]]; then
        add_result "range_query" "FAIL" "Range query latency ${latency_ms}ms exceeds ${RANGE_QUERY_THRESHOLD_MS}ms threshold (data points: $data_points)" "$latency_ms" "$RANGE_QUERY_THRESHOLD_MS"
        return 1
    else
        add_result "range_query" "PASS" "Range query latency ${latency_ms}ms within threshold (data points: $data_points)" "$latency_ms" "$RANGE_QUERY_THRESHOLD_MS"
        return 0
    fi
}

# Test series discovery
test_series_discovery() {
    local match="${1:-finops_workload_hourly_cost_usd}"
    local url="${API_BASE}/series?match[]=$(echo "$match" | sed 's/ /%20/g')"
    
    log_info "Testing series discovery: $match"
    
    local result
    result=$(execute_query "$url")
    read -r latency_ms http_code body <<< "$result"
    
    if [[ "$http_code" != "200" ]]; then
        add_result "series_discovery" "FAIL" "HTTP $http_code: $body" "$latency_ms" "$SERIES_DISCOVERY_THRESHOLD_MS"
        return 1
    fi
    
    # Validate response format
    if ! echo "$body" | jq -e '.status == "success"' >/dev/null 2>&1; then
        add_result "series_discovery" "FAIL" "Invalid response format: $body" "$latency_ms" "$SERIES_DISCOVERY_THRESHOLD_MS"
        return 1
    fi
    
    # Check series count
    local series_count
    series_count=$(echo "$body" | jq '.data | length' 2>/dev/null || echo "0")
    
    if [[ $latency_ms -gt $SERIES_DISCOVERY_THRESHOLD_MS ]]; then
        add_result "series_discovery" "FAIL" "Series discovery latency ${latency_ms}ms exceeds ${SERIES_DISCOVERY_THRESHOLD_MS}ms threshold (series: $series_count)" "$latency_ms" "$SERIES_DISCOVERY_THRESHOLD_MS"
        return 1
    else
        add_result "series_discovery" "PASS" "Series discovery latency ${latency_ms}ms within threshold (series: $series_count)" "$latency_ms" "$SERIES_DISCOVERY_THRESHOLD_MS"
        return 0
    fi
}

# Test label discovery
test_label_discovery() {
    local url="${API_BASE}/labels"
    
    log_info "Testing label discovery"
    
    local result
    result=$(execute_query "$url")
    read -r latency_ms http_code body <<< "$result"
    
    if [[ "$http_code" != "200" ]]; then
        add_result "label_discovery" "FAIL" "HTTP $http_code: $body" "$latency_ms" "$SERIES_DISCOVERY_THRESHOLD_MS"
        return 1
    fi
    
    if ! echo "$body" | jq -e '.status == "success"' >/dev/null 2>&1; then
        add_result "label_discovery" "FAIL" "Invalid response format: $body" "$latency_ms" "$SERIES_DISCOVERY_THRESHOLD_MS"
        return 1
    fi
    
    local label_count
    label_count=$(echo "$body" | jq '.data | length' 2>/dev/null || echo "0")
    
    if [[ $latency_ms -gt $SERIES_DISCOVERY_THRESHOLD_MS ]]; then
        add_result "label_discovery" "FAIL" "Label discovery latency ${latency_ms}ms exceeds ${SERIES_DISCOVERY_THRESHOLD_MS}ms threshold (labels: $label_count)" "$latency_ms" "$SERIES_DISCOVERY_THRESHOLD_MS"
        return 1
    else
        add_result "label_discovery" "PASS" "Label discovery latency ${latency_ms}ms within threshold (labels: $label_count)" "$latency_ms" "$SERIES_DISCOVERY_THRESHOLD_MS"
        return 0
    fi
}

# Test label values discovery
test_label_values() {
    local label_name="${1:-namespace}"
    local url="${API_BASE}/label/$(echo "$label_name" | sed 's/ /%20/g')/values"
    
    log_info "Testing label values discovery: $label_name"
    
    local result
    result=$(execute_query "$url")
    read -r latency_ms http_code body <<< "$result"
    
    if [[ "$http_code" != "200" ]]; then
        add_result "label_values" "FAIL" "HTTP $http_code: $body" "$latency_ms" "$SERIES_DISCOVERY_THRESHOLD_MS"
        return 1
    fi
    
    if ! echo "$body" | jq -e '.status == "success"' >/dev/null 2>&1; then
        add_result "label_values" "FAIL" "Invalid response format: $body" "$latency_ms" "$SERIES_DISCOVERY_THRESHOLD_MS"
        return 1
    fi
    
    local values_count
    values_count=$(echo "$body" | jq '.data | length' 2>/dev/null || echo "0")
    
    if [[ $latency_ms -gt $SERIES_DISCOVERY_THRESHOLD_MS ]]; then
        add_result "label_values" "FAIL" "Label values latency ${latency_ms}ms exceeds ${SERIES_DISCOVERY_THRESHOLD_MS}ms threshold (values: $values_count)" "$latency_ms" "$SERIES_DISCOVERY_THRESHOLD_MS"
        return 1
    else
        add_result "label_values" "PASS" "Label values latency ${latency_ms}ms within threshold (values: $values_count)" "$latency_ms" "$SERIES_DISCOVERY_THRESHOLD_MS"
        return 0
    fi
}

# Test complex queries
test_complex_queries() {
    log_info "Testing complex queries..."
    
    # Test rate() query
    local rate_query="rate(finops_workload_cpu_cores_used[5m])"
    local url="${API_BASE}/query?query=$(echo "$rate_query" | sed 's/ /%20/g')"
    
    local result
    result=$(execute_query "$url")
    read -r latency_ms http_code body <<< "$result"
    
    if [[ "$http_code" == "200" ]] && echo "$body" | jq -e '.status == "success"' >/dev/null 2>&1; then
        if [[ $latency_ms -gt $INSTANT_QUERY_THRESHOLD_MS ]]; then
            add_result "complex_rate_query" "WARN" "Rate query latency ${latency_ms}ms exceeds ${INSTANT_QUERY_THRESHOLD_MS}ms" "$latency_ms" "$INSTANT_QUERY_THRESHOLD_MS"
        else
            add_result "complex_rate_query" "PASS" "Rate query latency ${latency_ms}ms within threshold" "$latency_ms" "$INSTANT_QUERY_THRESHOLD_MS"
        fi
    else
        add_result "complex_rate_query" "WARN" "Rate query failed or returned error: HTTP $http_code" "$latency_ms" "$INSTANT_QUERY_THRESHOLD_MS"
    fi
    
    # Test sum by query
    local sum_query="sum by (namespace) (finops_workload_hourly_cost_usd)"
    url="${API_BASE}/query?query=$(echo "$sum_query" | sed 's/ /%20/g')"
    
    result=$(execute_query "$url")
    read -r latency_ms http_code body <<< "$result"
    
    if [[ "$http_code" == "200" ]] && echo "$body" | jq -e '.status == "success"' >/dev/null 2>&1; then
        if [[ $latency_ms -gt $INSTANT_QUERY_THRESHOLD_MS ]]; then
            add_result "complex_sum_query" "WARN" "Sum by query latency ${latency_ms}ms exceeds ${INSTANT_QUERY_THRESHOLD_MS}ms" "$latency_ms" "$INSTANT_QUERY_THRESHOLD_MS"
        else
            add_result "complex_sum_query" "PASS" "Sum by query latency ${latency_ms}ms within threshold" "$latency_ms" "$INSTANT_QUERY_THRESHOLD_MS"
        fi
    else
        add_result "complex_sum_query" "WARN" "Sum by query failed or returned error: HTTP $http_code" "$latency_ms" "$INSTANT_QUERY_THRESHOLD_MS"
    fi
    
    # Test topk query
    local topk_query="topk(10, finops_workload_hourly_cost_usd)"
    url="${API_BASE}/query?query=$(echo "$topk_query" | sed 's/ /%20/g')"
    
    result=$(execute_query "$url")
    read -r latency_ms http_code body <<< "$result"
    
    if [[ "$http_code" == "200" ]] && echo "$body" | jq -e '.status == "success"' >/dev/null 2>&1; then
        if [[ $latency_ms -gt $INSTANT_QUERY_THRESHOLD_MS ]]; then
            add_result "complex_topk_query" "WARN" "Topk query latency ${latency_ms}ms exceeds ${INSTANT_QUERY_THRESHOLD_MS}ms" "$latency_ms" "$INSTANT_QUERY_THRESHOLD_MS"
        else
            add_result "complex_topk_query" "PASS" "Topk query latency ${latency_ms}ms within threshold" "$latency_ms" "$INSTANT_QUERY_THRESHOLD_MS"
        fi
    else
        add_result "complex_topk_query" "WARN" "Topk query failed or returned error: HTTP $http_code" "$latency_ms" "$INSTANT_QUERY_THRESHOLD_MS"
    fi
}

# Test concurrent queries
test_concurrent_queries() {
    local concurrent="${1:-5}"
    local query="finops_cluster_total_hourly_cost_usd"
    local url="${API_BASE}/query?query=$(echo "$query" | sed 's/ /%20/g')"
    
    log_info "Testing $concurrent concurrent queries..."
    
    local pids=()
    local latencies=()
    local temp_dir
    temp_dir=$(mktemp -d)
    
    for i in $(seq 1 "$concurrent"); do
        (
            start_time=$(get_timestamp_ms)
            response=$(curl -s -w "\n%{http_code}" "$url" 2>/dev/null || echo -e "\n000")
            end_time=$(get_timestamp_ms)
            latency_ms=$((end_time - start_time))
            http_code=$(echo "$response" | tail -1)
            echo "$latency_ms $http_code" > "$temp_dir/result_$i"
        ) &
        pids+=($!)
    done
    
    # Wait for all
    for pid in "${pids[@]}"; do
        wait "$pid"
    done
    
    # Collect results
    local total_latency=0
    local success_count=0
    local max_latency=0
    
    for i in $(seq 1 "$concurrent"); do
        if [[ -f "$temp_dir/result_$i" ]]; then
            read -r latency_ms http_code < "$temp_dir/result_$i"
            total_latency=$((total_latency + latency_ms))
            if [[ $latency_ms -gt $max_latency ]]; then
                max_latency=$latency_ms
            fi
            if [[ "$http_code" == "200" ]]; then
                ((success_count++))
            fi
        fi
    done
    
    rm -rf "$temp_dir"
    
    local avg_latency=0
    if [[ $success_count -gt 0 ]]; then
        avg_latency=$((total_latency / success_count))
    fi
    
    if [[ $success_count -eq $concurrent ]]; then
        if [[ $max_latency -gt $INSTANT_QUERY_THRESHOLD_MS ]]; then
            add_result "concurrent_queries" "WARN" "Max concurrent query latency ${max_latency}ms exceeds ${INSTANT_QUERY_THRESHOLD_MS}ms (avg: ${avg_latency}ms)" "$max_latency" "$INSTANT_QUERY_THRESHOLD_MS"
        else
            add_result "concurrent_queries" "PASS" "All $concurrent concurrent queries succeeded (max: ${max_latency}ms, avg: ${avg_latency}ms)" "$max_latency" "$INSTANT_QUERY_THRESHOLD_MS"
        fi
    else
        add_result "concurrent_queries" "FAIL" "Only $success_count/$concurrent concurrent queries succeeded" "$max_latency" "$INSTANT_QUERY_THRESHOLD_MS"
    fi
}

# Main validation
main() {
    log_info "Starting Query Performance Validation"
    log_info "Target: $NAMESPACE/$POD_NAME:$API_PORT"
    log_info "Thresholds: Instant=${INSTANT_QUERY_THRESHOLD_MS}ms, Range=${RANGE_QUERY_THRESHOLD_MS}ms, Series=${SERIES_DISCOVERY_THRESHOLD_MS}ms"
    
    # Check if pod is accessible
    if ! kubectl get pod "$POD_NAME" -n "$NAMESPACE" >/dev/null 2>&1; then
        log_error "Pod $POD_NAME not found in namespace $NAMESPACE"
        exit 1
    fi
    
    # Check if port forward is needed or if we can access directly
    # For now, assume we're running with port-forward or inside cluster
    log_info "Testing API connectivity..."
    if ! curl -s -f "http://localhost:${API_PORT}/api/v1/query?query=up" >/dev/null 2>&1; then
        log_warn "API not accessible on localhost:$API_PORT. Attempting port-forward..."
        # Try to establish port-forward in background
        kubectl port-forward -n "$NAMESPACE" "pod/$POD_NAME" "$API_PORT:$API_PORT" >/dev/null 2>&1 &
        PF_PID=$!
        sleep 3
        if ! curl -s -f "http://localhost:${API_PORT}/api/v1/query?query=up" >/dev/null 2>&1; then
            log_error "Cannot connect to API. Please ensure port-forward is established:"
            log_error "  kubectl port-forward -n $NAMESPACE pod/$POD_NAME $API_PORT:$API_PORT"
            kill $PF_PID 2>/dev/null || true
            exit 1
        fi
        log_info "Port-forward established (PID: $PF_PID)"
    fi
    
    # Run tests
    log_info "=== Running Query Performance Tests ==="
    
    # Basic queries
    test_instant_query "finops_cluster_total_hourly_cost_usd"
    test_instant_query "finops_workload_hourly_cost_usd"
    test_instant_query "finops_node_hourly_cost_usd"
    
    # Range queries
    test_range_query "finops_workload_hourly_cost_usd" 3600 2592000  # 30 days, 1h step
    test_range_query "finops_workload_cpu_cores_used" 300 86400  # 1 day, 5m step
    test_range_query "finops_node_cpu_utilization_ratio" 3600 604800  # 7 days, 1h step
    
    # Series/Label discovery
    test_series_discovery "finops_workload_hourly_cost_usd"
    test_series_discovery "finops_node_hourly_cost_usd"
    test_label_discovery
    test_label_values "namespace"
    test_label_values "cost_center"
    test_label_values "team"
    
    # Complex queries
    test_complex_queries
    
    # Concurrent queries
    test_concurrent_queries 5
    test_concurrent_queries 10
    
    # Output JSON Report
    echo "{"
    echo "  \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\","
    echo "  \"thresholds\": {"
    echo "    \"instant_query_ms\": $INSTANT_QUERY_THRESHOLD_MS,"
    echo "    \"range_query_ms\": $RANGE_QUERY_THRESHOLD_MS,"
    echo "    \"series_discovery_ms\": $SERIES_DISCOVERY_THRESHOLD_MS"
    echo "  },"
    echo "  \"checks\": ["
    
    for i in "${!RESULTS[@]}"; do
        if [[ $i -gt 0 ]]; then echo "    ,"; fi
        echo "    ${RESULTS[$i]}"
    done
    
    echo "  ]"
    echo "}"
    
    # Summary
    PASS_COUNT=0
    WARN_COUNT=0
    FAIL_COUNT=0
    for result in "${RESULTS[@]}"; do
        status=$(echo "$result" | jq -r '.status')
        case "$status" in
            PASS) ((PASS_COUNT++)) ;;
            WARN) ((WARN_COUNT++)) ;;
            FAIL) ((FAIL_COUNT++)) ;;
        esac
    done
    
    echo ""
    log_info "Validation Summary: PASS=$PASS_COUNT, WARN=$WARN_COUNT, FAIL=$FAIL_COUNT"
    
    # Cleanup port-forward if we started it
    if [[ -n "${PF_PID:-}" ]]; then
        kill $PF_PID 2>/dev/null || true
    fi
    
    if [[ $FAIL_COUNT -gt 0 ]]; then
        log_error "Validation FAILED with $FAIL_COUNT critical issues"
        exit 1
    elif [[ $WARN_COUNT -gt 0 ]]; then
        log_warn "Validation PASSED with $WARN_COUNT warnings"
        exit 0
    else
        log_pass "Validation PASSED - all checks healthy"
        exit 0
    fi
}

# Check dependencies
for cmd in kubectl curl jq; do
    if ! command -v "$cmd" &> /dev/null; then
        log_error "Required command '$cmd' not found"
        exit 1
    fi
done

main "$@"
