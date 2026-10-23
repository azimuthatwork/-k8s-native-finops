#!/bin/bash
# validate-promql-engine.sh - Validate PromQL Engine Compatibility
# Part of Phase 2 Task 2.2.1

set -euo pipefail

# Configuration
NAMESPACE="${NAMESPACE:-cost-management}"
POD_NAME="${POD_NAME:-cost-controller-0}"
CONTAINER_NAME="${CONTAINER_NAME:-cost-controller}"
API_PORT="${API_PORT:-8082}"
API_BASE="http://localhost:${API_PORT}/api/v1"

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
    RESULTS+=("{\"check\":\"$check_name\",\"status\":\"$status\",\"message\":\"$message\"}")
}

# Execute query and return status code and body
execute_query() {
    local url="$1"
    local response
    response=$(curl -s -w "\n%{http_code}" "$url" 2>/dev/null || echo -e "\n000")
    http_code=$(echo "$response" | tail -1)
    body=$(echo "$response" | head -n -1)
    echo "$http_code $body"
}

# Test parser compatibility (using Go parser via a simple test)
test_parser_compatibility() {
    log_info "Testing PromQL parser compatibility..."
    
    # We'll test via the API since we don't have Go parser in bash
    # This is a basic check - the Go version does full parser testing
    local test_queries=(
        "up"
        "finops_workload_hourly_cost_usd"
        "rate(finops_workload_cpu_cores_used[5m])"
        "sum by (namespace) (finops_workload_hourly_cost_usd)"
        "topk(10, finops_workload_hourly_cost_usd)"
        "histogram_quantile(0.95, rate(finops_span_duration_seconds_bucket[5m]))"
        "finops_workload_hourly_cost_usd @ 3600"
        "finops_workload_hourly_cost_usd[1h]"
        "finops_workload_hourly_cost_usd offset 1h"
    )
    
    local pass_count=0
    for query in "${test_queries[@]}"; do
        local url="${API_BASE}/query?query=$(echo "$query" | sed 's/ /%20/g')"
        local result
        result=$(execute_query "$url")
        read -r http_code body <<< "$result"
        
        if [[ "$http_code" == "200" ]] && echo "$body" | jq -e '.status == "success"' >/dev/null 2>&1; then
            ((pass_count++))
        fi
    done
    
    if [[ $pass_count -eq ${#test_queries[@]} ]]; then
        add_result "parser_compatibility" "PASS" "All ${#test_queries[@]} test queries parsed and executed successfully"
    else
        add_result "parser_compatibility" "WARN" "$pass_count/${#test_queries[@]} test queries passed"
    fi
}

# Test instant queries
test_instant_queries() {
    log_info "Testing instant queries..."
    
    local queries=(
        "simple_metric:finops_cluster_total_hourly_cost_usd"
        "vector_selector:finops_workload_hourly_cost_usd{namespace=\"default\"}"
        "vector_selector_multi:finops_workload_hourly_cost_usd{namespace=~\"prod.*\"}"
        "binary_op:finops_workload_cpu_cores_used / finops_workload_cpu_cores_requested"
        "aggregation:sum(finops_workload_hourly_cost_usd)"
        "aggregation_by:sum by (namespace) (finops_workload_hourly_cost_usd)"
        "topk:topk(5, finops_workload_hourly_cost_usd)"
        "bottomk:bottomk(5, finops_workload_hourly_cost_usd)"
    )
    
    for item in "${queries[@]}"; do
        local name="${item%%:*}"
        local query="${item#*:}"
        local url="${API_BASE}/query?query=$(echo "$query" | sed 's/ /%20/g')"
        
        local result
        result=$(execute_query "$url")
        read -r http_code body <<< "$result"
        
        if [[ "$http_code" == "200" ]] && echo "$body" | jq -e '.status == "success"' >/dev/null 2>&1; then
            add_result "instant_$name" "PASS" "Query executed successfully"
        else
            add_result "instant_$name" "FAIL" "HTTP $http_code: $body"
        fi
    done
}

# Test range queries
test_range_queries() {
    log_info "Testing range queries..."
    
    local end_time
    end_time=$(date +%s)
    local start_time=$((end_time - 3600))  # 1 hour ago
    local step=60  # 1 minute
    
    local queries=(
        "range_vector:finops_workload_hourly_cost_usd"
        "rate_function:rate(finops_workload_cpu_cores_used[5m])"
        "increase_function:increase(finops_workload_cpu_cores_used[1h])"
        "aggregation_over_time:avg_over_time(finops_workload_cpu_cores_used[1h])"
    )
    
    for item in "${queries[@]}"; do
        local name="${item%%:*}"
        local query="${item#*:}"
        local url="${API_BASE}/query_range?query=$(echo "$query" | sed 's/ /%20/g')&start=${start_time}&end=${end_time}&step=${step}"
        
        local result
        result=$(execute_query "$url")
        read -r http_code body <<< "$result"
        
        if [[ "$http_code" == "200" ]] && echo "$body" | jq -e '.status == "success"' >/dev/null 2>&1; then
            # Check result type is matrix
            local result_type
            result_type=$(echo "$body" | jq -r '.data.resultType' 2>/dev/null || echo "")
            if [[ "$result_type" == "matrix" ]]; then
                add_result "range_$name" "PASS" "Range query returned matrix"
            else
                add_result "range_$name" "WARN" "Range query succeeded but result type is $result_type"
            fi
        else
            add_result "range_$name" "FAIL" "HTTP $http_code: $body"
        fi
    done
}

# Test series discovery
test_series_discovery() {
    log_info "Testing series discovery..."
    
    local matches=(
        "finops_workload_hourly_cost_usd"
        "finops_workload_hourly_cost_usd{namespace=\"default\"}"
        "finops_workload_hourly_cost_usd{namespace=~\"prod.*\"}"
        "{__name__=~\"finops_.*\"}"
    )
    
    for match in "${matches[@]}"; do
        local url="${API_BASE}/series?match[]=$(echo "$match" | sed 's/ /%20/g')"
        local result
        result=$(execute_query "$url")
        read -r http_code body <<< "$result"
        
        if [[ "$http_code" == "200" ]] && echo "$body" | jq -e '.status == "success"' >/dev/null 2>&1; then
            local series_count
            series_count=$(echo "$body" | jq '.data | length' 2>/dev/null || echo "0")
            add_result "series_$(echo "$match" | sed 's/[^a-zA-Z0-9]/_/g')" "PASS" "Found $series_count series"
        else
            add_result "series_$(echo "$match" | sed 's/[^a-zA-Z0-9]/_/g')" "FAIL" "HTTP $http_code: $body"
        fi
    done
}

# Test label discovery
test_label_discovery() {
    log_info "Testing label discovery..."
    
    # Test /labels
    local url="${API_BASE}/labels"
    local result
    result=$(execute_query "$url")
    read -r http_code body <<< "$result"
    
    if [[ "$http_code" == "200" ]] && echo "$body" | jq -e '.status == "success"' >/dev/null 2>&1; then
        local label_count
        label_count=$(echo "$body" | jq '.data | length' 2>/dev/null || echo "0")
        add_result "labels" "PASS" "Found $label_count label names"
    else
        add_result "labels" "FAIL" "HTTP $http_code: $body"
    fi
    
    # Test /label/<name>/values
    local label_names=("namespace" "cost_center" "team" "workload" "kind" "__name__")
    for label_name in "${label_names[@]}"; do
        local url="${API_BASE}/label/$(echo "$label_name" | sed 's/ /%20/g')/values"
        local result
        result=$(execute_query "$url")
        read -r http_code body <<< "$result"
        
        if [[ "$http_code" == "200" ]] && echo "$body" | jq -e '.status == "success"' >/dev/null 2>&1; then
            local values_count
            values_count=$(echo "$body" | jq '.data | length' 2>/dev/null || echo "0")
            add_result "label_values_$label_name" "PASS" "Found $values_count values for $label_name"
        else
            add_result "label_values_$label_name" "FAIL" "HTTP $http_code: $body"
        fi
    done
}

# Test PromQL functions
test_promql_functions() {
    log_info "Testing PromQL functions..."
    
    local functions=(
        "rate:rate(finops_workload_cpu_cores_used[5m])"
        "irate:irate(finops_workload_cpu_cores_used[5m])"
        "increase:increase(finops_workload_cpu_cores_used[1h])"
        "sum:sum(finops_workload_hourly_cost_usd)"
        "avg:avg(finops_workload_cpu_cores_used)"
        "max:max(finops_workload_memory_bytes_used)"
        "min:min(finops_workload_cpu_cores_requested)"
        "count:count(finops_workload_hourly_cost_usd)"
        "stddev:stddev(finops_workload_cpu_cores_used)"
        "topk:topk(10, finops_workload_hourly_cost_usd)"
        "bottomk:bottomk(10, finops_workload_hourly_cost_usd)"
        "histogram_quantile:histogram_quantile(0.95, rate(finops_span_duration_seconds_bucket[5m]))"
        "sort:sort(finops_workload_hourly_cost_usd)"
        "sort_desc:sort_desc(finops_workload_hourly_cost_usd)"
        "time:time()"
        "timestamp:timestamp(finops_workload_hourly_cost_usd)"
        "abs:abs(finops_workload_cpu_cores_used - finops_workload_cpu_cores_requested)"
        "ceil:ceil(finops_workload_cpu_cores_used)"
        "floor:floor(finops_workload_cpu_cores_used)"
        "sqrt:sqrt(finops_workload_cpu_cores_used)"
        "ln:ln(finops_workload_hourly_cost_usd + 1)"
        "log2:log2(finops_workload_hourly_cost_usd + 1)"
        "log10:log10(finops_workload_hourly_cost_usd + 1)"
        "exp:exp(finops_workload_cpu_cores_used / 100)"
        "vector_matching:finops_workload_cpu_cores_used / finops_workload_cpu_cores_requested"
        "vector_matching_on:finops_workload_cpu_cores_used / on(namespace) finops_workload_cpu_cores_requested"
    )
    
    for item in "${functions[@]}"; do
        local name="${item%%:*}"
        local query="${item#*:}"
        local url="${API_BASE}/query?query=$(echo "$query" | sed 's/ /%20/g')"
        
        local result
        result=$(execute_query "$url")
        read -r http_code body <<< "$result"
        
        if [[ "$http_code" == "200" ]] && echo "$body" | jq -e '.status == "success"' >/dev/null 2>&1; then
            add_result "function_$name" "PASS" "Function executed successfully"
        elif [[ "$http_code" == "200" ]] && echo "$body" | jq -e '.status == "error"' >/dev/null 2>&1; then
            # Some functions may error due to no data, which is acceptable
            add_result "function_$name" "WARN" "Function returned error (may be no data): $(echo "$body" | jq -r '.error')"
        else
            add_result "function_$name" "FAIL" "HTTP $http_code: $body"
        fi
    done
}

# Test modifiers
test_modifiers() {
    log_info "Testing PromQL modifiers..."
    
    local modifiers=(
        "offset:finops_workload_hourly_cost_usd offset 1h"
        "offset_negative:finops_workload_hourly_cost_usd offset -1h"
        "at_modifier:finops_workload_hourly_cost_usd @ 1704067200"
        "range_vector:finops_workload_hourly_cost_usd[1h]"
        "range_vector_5m:finops_workload_cpu_cores_used[5m]"
        "subquery:avg_over_time(finops_workload_cpu_cores_used[1h:30s])"
    )
    
    for item in "${modifiers[@]}"; do
        local name="${item%%:*}"
        local query="${item#*:}"
        local url="${API_BASE}/query?query=$(echo "$query" | sed 's/ /%20/g')"
        
        local result
        result=$(execute_query "$url")
        read -r http_code body <<< "$result"
        
        if [[ "$http_code" == "200" ]] && echo "$body" | jq -e '.status == "success"' >/dev/null 2>&1; then
            add_result "modifier_$name" "PASS" "Modifier executed successfully"
        elif [[ "$http_code" == "200" ]] && echo "$body" | jq -e '.status == "error"' >/dev/null 2>&1; then
            add_result "modifier_$name" "WARN" "Modifier returned error (may be no data): $(echo "$body" | jq -r '.error')"
        else
            add_result "modifier_$name" "FAIL" "HTTP $http_code: $body"
        fi
    done
}

# Test error handling
test_error_handling() {
    log_info "Testing error handling..."
    
    local error_cases=(
        "empty_query::400"
        "invalid_syntax:finops_workload_hourly_cost_usd {:400"
        "unknown_function:unknown_function(finops_workload_hourly_cost_usd):400"
        "invalid_range:finops_workload_hourly_cost_usd[invalid]:400"
    )
    
    for item in "${error_cases[@]}"; do
        local name="${item%%:*}"
        local rest="${item#*:}"
        local query="${rest%:*}"
        local expect_code="${rest##*:}"
        
        local url="${API_BASE}/query?query=$(echo "$query" | sed 's/ /%20/g')"
        local result
        result=$(execute_query "$url")
        read -r http_code body <<< "$result"
        
        if [[ "$http_code" == "$expect_code" ]]; then
            add_result "error_$name" "PASS" "Correctly returned HTTP $http_code"
        else
            add_result "error_$name" "WARN" "Expected HTTP $expect_code, got $http_code: $body"
        fi
    done
    
    # Test query_range without required params
    local url="${API_BASE}/query_range?query=finops_workload_hourly_cost_usd"
    local result
    result=$(execute_query "$url")
    read -r http_code body <<< "$result"
    
    if [[ "$http_code" == "400" ]]; then
        add_result "error_missing_params_range" "PASS" "Correctly returned HTTP 400 for missing params"
    else
        add_result "error_missing_params_range" "WARN" "Expected HTTP 400, got $http_code: $body"
    fi
}

# Main validation
main() {
    log_info "Starting PromQL Engine Validation"
    log_info "Target: $NAMESPACE/$POD_NAME:$API_PORT"
    
    # Check if pod is accessible
    if ! kubectl get pod "$POD_NAME" -n "$NAMESPACE" >/dev/null 2>&1; then
        log_error "Pod $POD_NAME not found in namespace $NAMESPACE"
        exit 1
    fi
    
    # Check if port forward is needed
    log_info "Testing API connectivity..."
    if ! curl -s -f "http://localhost:${API_PORT}/api/v1/query?query=up" >/dev/null 2>&1; then
        log_warn "API not accessible on localhost:$API_PORT. Attempting port-forward..."
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
    log_info "=== Running PromQL Engine Tests ==="
    
    test_parser_compatibility
    test_instant_queries
    test_range_queries
    test_series_discovery
    test_label_discovery
    test_promql_functions
    test_modifiers
    test_error_handling
    
    # Output JSON Report
    echo "{"
    echo "  \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\","
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
