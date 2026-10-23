#!/bin/bash
# validate-tsdb-sizing.sh
# Validates TSDB resource usage against sizing guidelines from PLAN-NATIVE.md
# Task 1.5.10: Validate Resource Usage against Sizing Guidelines

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
COST_CONTROLLER_NAMESPACE="${COST_CONTROLLER_NAMESPACE:-cost-management}"
COST_CONTROLLER_POD="${COST_CONTROLLER_POD:-cost-controller-0}"
TSDB_METRICS_PORT="${TSDB_METRICS_PORT:-8080}"
PVC_NAME="${PVC_NAME:-cost-controller-data}"
VALIDATION_OUTPUT="${VALIDATION_OUTPUT:-/tmp/tsdb-sizing-validation.json}"

# Sizing Guidelines (from PLAN-NATIVE.md)
declare -A SMALL_MAX_SERIES=50000
declare -A SMALL_PVC_GIB=50
declare -A SMALL_CPU_CORES=1
declare -A SMALL_MEM_GIB=4
declare -A SMALL_NET_Gbps=1

declare -A MEDIUM_MAX_SERIES=250000
declare -A MEDIUM_PVC_GIB=200
declare -A MEDIUM_CPU_CORES=2
declare -A MEDIUM_MEM_GIB=8
declare -A MEDIUM_NET_Gbps=2

declare -A LARGE_MAX_SERIES=750000
declare -A LARGE_PVC_GIB=500
declare -A LARGE_CPU_CORES=4
declare -A LARGE_MEM_GIB=16
declare -A LARGE_NET_Gbps=5

declare -A XLARGE_MIN_SERIES=1500000
declare -A XLARGE_MIN_PVC_GIB=1000
declare -A XLARGE_MIN_CPU_CORES=8
declare -A XLARGE_MIN_MEM_GIB=32
declare -A XLARGE_MIN_NET_Gbps=10

# Results tracking
VALIDATION_PASSED=true
VALIDATION_RESULTS=()

log_info() { echo -e "${BLUE}[INFO]${NC} $*"; }
log_warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }
log_pass() { echo -e "${GREEN}[PASS]${NC} $*"; }
log_fail() { echo -e "${RED}[FAIL]${NC} $*"; }

# Helper to add result
add_result() {
    local check="$1"
    local status="$2"  # PASS, FAIL, WARN
    local message="$3"
    local actual="$4"
    local expected="$5"
    VALIDATION_RESULTS+=("{\"check\":\"$check\",\"status\":\"$status\",\"message\":\"$message\",\"actual\":\"$actual\",\"expected\":\"$expected\"}")
    if [[ "$status" == "FAIL" ]]; then
        VALIDATION_PASSED=false
    fi
}

# Get node count
get_node_count() {
    kubectl get nodes --no-headers 2>/dev/null | wc -l | tr -d ' '
}

# Determine cluster size category
get_cluster_category() {
    local node_count=$1
    if [[ $node_count -lt 50 ]]; then
        echo "Small"
    elif [[ $node_count -lt 200 ]]; then
        echo "Medium"
    elif [[ $node_count -lt 500 ]]; then
        echo "Large"
    else
        echo "X-Large"
    fi
}

# Get expected values for category
get_expected_values() {
    local category=$1
    case $category in
        "Small")
            echo "$SMALL_MAX_SERIES $SMALL_PVC_GIB $SMALL_CPU_CORES $SMALL_MEM_GIB $SMALL_NET_Gbps"
            ;;
        "Medium")
            echo "$MEDIUM_MAX_SERIES $MEDIUM_PVC_GIB $MEDIUM_CPU_CORES $MEDIUM_MEM_GIB $MEDIUM_NET_Gbps"
            ;;
        "Large")
            echo "$LARGE_MAX_SERIES $LARGE_PVC_GIB $LARGE_CPU_CORES $LARGE_MEM_GIB $LARGE_NET_Gbps"
            ;;
        "X-Large")
            echo "$XLARGE_MIN_SERIES $XLARGE_MIN_PVC_GIB $XLARGE_MIN_CPU_CORES $XLARGE_MIN_MEM_GIB $XLARGE_MIN_NET_Gbps"
            ;;
    esac
}

# Fetch TSDB metrics from cost-controller pod
fetch_tsdb_metrics() {
    log_info "Fetching TSDB metrics from $COST_CONTROLLER_POD in $COST_CONTROLLER_NAMESPACE..."
    
    # Use kubectl exec to wget metrics endpoint
    kubectl exec -n "$COST_CONTROLLER_NAMESPACE" "$COST_CONTROLLER_POD" -- \
        wget -qO- "http://localhost:${TSDB_METRICS_PORT}/metrics" 2>/dev/null || {
        log_error "Failed to fetch metrics from cost-controller pod"
        return 1
    }
}

# Parse metrics from TSDB output
parse_tsdb_metrics() {
    local metrics_output="$1"
    
    # Extract key metrics
    local head_series=$(echo "$metrics_output" | grep '^tsdb_head_series_total' | head -1 | awk '{print $2}')
    local head_samples=$(echo "$metrics_output" | grep '^tsdb_head_samples_appended_total' | head -1 | awk '{print $2}')
    local head_bytes=$(echo "$metrics_output" | grep '^tsdb_head_bytes' | head -1 | awk '{print $2}')
    local block_count=$(echo "$metrics_output" | grep '^tsdb_block_count' | head -1 | awk '{print $2}')
    local compaction_duration=$(echo "$metrics_output" | grep '^tsdb_compaction_duration_seconds_sum' | head -1 | awk '{print $2}')
    local wal_corruptions=$(echo "$metrics_output" | grep '^tsdb_wal_corruptions_total' | head -1 | awk '{print $2}')
    
    # Ingester metrics
    local ingester_batch_avg=$(echo "$metrics_output" | grep '^finops_ingester_batch_size_avg' | head -1 | awk '{print $2}')
    local ingester_flush_duration=$(echo "$metrics_output" | grep '^finops_ingester_flush_duration_seconds_sum' | head -1 | awk '{print $2}')
    local ingester_rejected=$(echo "$metrics_output" | grep '^finops_ingester_rejected_samples_total' | head -1 | awk '{print $2}')
    local ingester_exemplars_dropped=$(echo "$metrics_output" | grep '^finops_ingester_exemplars_dropped_total' | head -1 | awk '{print $2}')
    
    # Query metrics
    local query_duration_p99=$(echo "$metrics_output" | grep 'finops_query_duration_seconds' | grep 'quantile="0.99"' | head -1 | awk '{print $2}')
    local query_samples=$(echo "$metrics_output" | grep '^finops_query_samples_total' | head -1 | awk '{print $2}')
    local query_errors=$(echo "$metrics_output" | grep '^finops_query_errors_total' | head -1 | awk '{print $2}')
    
    # Cardinality guard
    local max_active_series=$(echo "$metrics_output" | grep 'finops_max_active_series' | head -1 | awk '{print $2}')
    
    # Output as JSON for further processing
    cat <<EOF
{
    "head_series": ${head_series:-0},
    "head_samples": ${head_samples:-0},
    "head_bytes": ${head_bytes:-0},
    "block_count": ${block_count:-0},
    "compaction_duration": ${compaction_duration:-0},
    "wal_corruptions": ${wal_corruptions:-0},
    "ingester_batch_avg": ${ingester_batch_avg:-0},
    "ingester_flush_duration": ${ingester_flush_duration:-0},
    "ingester_rejected": ${ingester_rejected:-0},
    "ingester_exemplars_dropped": ${ingester_exemplars_dropped:-0},
    "query_duration_p99": ${query_duration_p99:-0},
    "query_samples": ${query_samples:-0},
    "query_errors": ${query_errors:-0},
    "max_active_series": ${max_active_series:-500000}
}
EOF
}

# Get PVC usage
get_pvc_usage() {
    log_info "Checking PVC usage for $PVC_NAME in $COST_CONTROLLER_NAMESPACE..."
    
    # Get PVC capacity and usage
    local pvc_info=$(kubectl get pvc "$PVC_NAME" -n "$COST_CONTROLLER_NAMESPACE" -o json 2>/dev/null)
    if [[ -z "$pvc_info" || "$pvc_info" == "null" ]]; then
        log_warn "PVC $PVC_NAME not found in namespace $COST_CONTROLLER_NAMESPACE"
        echo "0 0"
        return
    fi
    
    local capacity=$(echo "$pvc_info" | jq -r '.status.capacity.storage // "0"')
    local capacity_gib=$(echo "$capacity" | sed 's/Gi$//' | sed 's/G$//')
    
    # Get actual usage from pod (df)
    local usage_output=$(kubectl exec -n "$COST_CONTROLLER_NAMESPACE" "$COST_CONTROLLER_POD" -- df -BG /data/tsdb 2>/dev/null | tail -1)
    local used_gib=$(echo "$usage_output" | awk '{print $3}' | sed 's/G//')
    local available_gib=$(echo "$usage_output" | awk '{print $4}' | sed 's/G//')
    
    echo "$capacity_gib $used_gib"
}

# Get pod resource usage
get_pod_resources() {
    log_info "Checking pod resource requests/limits and actual usage..."
    
    # Get pod resource requests/limits
    local pod_json=$(kubectl get pod "$COST_CONTROLLER_POD" -n "$COST_CONTROLLER_NAMESPACE" -o json 2>/dev/null)
    if [[ -z "$pod_json" || "$pod_json" == "null" ]]; then
        log_error "Pod $COST_CONTROLLER_POD not found"
        echo "0 0 0 0"
        return
    fi
    
    local cpu_request=$(echo "$pod_json" | jq -r '.spec.containers[0].resources.requests.cpu // "0"')
    local cpu_limit=$(echo "$pod_json" | jq -r '.spec.containers[0].resources.limits.cpu // "0"')
    local mem_request=$(echo "$pod_json" | jq -r '.spec.containers[0].resources.requests.memory // "0"')
    local mem_limit=$(echo "$pod_json" | jq -r '.spec.containers[0].resources.limits.memory // "0"')
    
    # Convert to cores and GiB
    cpu_request=$(echo "$cpu_request" | sed 's/m$//' | awk '{print $1/1000}')
    cpu_limit=$(echo "$cpu_limit" | sed 's/m$//' | awk '{print $1/1000}')
    mem_request=$(echo "$mem_request" | sed 's/Gi$//' | sed 's/G$//' | sed 's/Mi$//' | awk '{if($1~"Mi") print $1/1024; else print $1}')
    mem_limit=$(echo "$mem_limit" | sed 's/Gi$//' | sed 's/G$//' | sed 's/Mi$//' | awk '{if($1~"Mi") print $1/1024; else print $1}')
    
    # Get actual usage from metrics-server
    local usage_json=$(kubectl top pod "$COST_CONTROLLER_POD" -n "$COST_CONTROLLER_NAMESPACE" --no-headers 2>/dev/null)
    local cpu_usage=0
    local mem_usage=0
    if [[ -n "$usage_json" ]]; then
        cpu_usage=$(echo "$usage_json" | awk '{print $2}' | sed 's/m$//' | awk '{print $1/1000}')
        mem_usage=$(echo "$usage_json" | awk '{print $3}' | sed 's/Gi$//' | sed 's/G$//' | sed 's/Mi$//' | awk '{if($1~"Mi") print $1/1024; else print $1}')
    fi
    
    echo "$cpu_request $cpu_limit $mem_request $mem_limit $cpu_usage $mem_usage"
}

# Validate series count against guidelines
validate_series() {
    local actual_series=$1
    local expected_max=$2
    local category=$3
    
    if [[ $category == "X-Large" ]]; then
        # For X-Large, we check if sharding is needed
        if [[ $actual_series -gt $expected_max ]]; then
            add_result "series_count" "FAIL" \
                "Series count ($actual_series) exceeds X-Large threshold ($expected_max). Sharding by cost_center required." \
                "$actual_series" ">$expected_max (sharding required)"
            return
        fi
        add_result "series_count" "PASS" \
            "Series count ($actual_series) within X-Large range. Sharding recommended if growing." \
            "$actual_series" ">$expected_max"
    else
        local pct=$((actual_series * 100 / expected_max))
        if [[ $actual_series -gt $expected_max ]]; then
            add_result "series_count" "FAIL" \
                "Series count ($actual_series) exceeds $category limit ($expected_max) by $((pct-100))%" \
                "$actual_series" "<=$expected_max"
        elif [[ $pct -gt 80 ]]; then
            add_result "series_count" "WARN" \
                "Series count ($actual_series) at ${pct}% of $category limit ($expected_max)" \
                "$actual_series" "<=$expected_max"
        else
            add_result "series_count" "PASS" \
                "Series count ($actual_series) at ${pct}% of $category limit ($expected_max)" \
                "$actual_series" "<=$expected_max"
        fi
    fi
}

# Validate PVC usage
validate_pvc() {
    local capacity_gib=$1
    local used_gib=$2
    local expected_gib=$3
    local category=$4
    
    if [[ $capacity_gib -eq 0 ]]; then
        add_result "pvc_capacity" "FAIL" "PVC capacity not found or zero" "0" ">${expected_gib}GiB"
        return
    fi
    
    local pct=$((used_gib * 100 / capacity_gib))
    
    if [[ $capacity_gib -lt $expected_gib ]]; then
        add_result "pvc_capacity" "FAIL" \
            "PVC capacity (${capacity_gib}GiB) below $category recommendation (${expected_gib}GiB)" \
            "${capacity_gib}GiB" ">=${expected_gib}GiB"
    else
        add_result "pvc_capacity" "PASS" \
            "PVC capacity (${capacity_gib}GiB) meets $category recommendation (${expected_gib}GiB)" \
            "${capacity_gib}GiB" ">=${expected_gib}GiB"
    fi
    
    if [[ $pct -gt 90 ]]; then
        add_result "pvc_usage" "FAIL" \
            "PVC usage at ${pct}% (${used_gib}GiB/${capacity_gib}GiB) - critical" \
            "${pct}%" "<80%"
    elif [[ $pct -gt 70 ]]; then
        add_result "pvc_usage" "WARN" \
            "PVC usage at ${pct}% (${used_gib}GiB/${capacity_gib}GiB) - approaching limit" \
            "${pct}%" "<70%"
    else
        add_result "pvc_usage" "PASS" \
            "PVC usage at ${pct}% (${used_gib}GiB/${capacity_gib}GiB)" \
            "${pct}%" "<70%"
    fi
}

# Validate CPU resources
validate_cpu() {
    local request=$1
    local limit=$2
    local usage=$3
    local expected_cores=$4
    local category=$5
    
    # Check if limit meets expectation
    if (( $(echo "$limit < $expected_cores" | bc -l) )); then
        add_result "cpu_limit" "FAIL" \
            "CPU limit (${limit} cores) below $category recommendation (${expected_cores} cores)" \
            "${limit} cores" ">=${expected_cores} cores"
    else
        add_result "cpu_limit" "PASS" \
            "CPU limit (${limit} cores) meets $category recommendation (${expected_cores} cores)" \
            "${limit} cores" ">=${expected_cores} cores"
    fi
    
    # Check usage vs limit
    if (( $(echo "$limit > 0" | bc -l) )); then
        local pct=$(echo "scale=0; $usage * 100 / $limit" | bc -l)
        if (( $(echo "$pct > 90" | bc -l) )); then
            add_result "cpu_usage" "FAIL" \
                "CPU usage at ${pct}% of limit - critical" \
                "${pct}%" "<80%"
        elif (( $(echo "$pct > 70" | bc -l) )); then
            add_result "cpu_usage" "WARN" \
                "CPU usage at ${pct}% of limit - high" \
                "${pct}%" "<70%"
        else
            add_result "cpu_usage" "PASS" \
                "CPU usage at ${pct}% of limit" \
                "${pct}%" "<70%"
        fi
    fi
}

# Validate Memory resources
validate_memory() {
    local request=$1
    local limit=$2
    local usage=$3
    local expected_gib=$4
    local category=$5
    
    if (( $(echo "$limit < $expected_gib" | bc -l) )); then
        add_result "memory_limit" "FAIL" \
            "Memory limit (${limit}GiB) below $category recommendation (${expected_gib}GiB)" \
            "${limit}GiB" ">=${expected_gib}GiB"
    else
        add_result "memory_limit" "PASS" \
            "Memory limit (${limit}GiB) meets $category recommendation (${expected_gib}GiB)" \
            "${limit}GiB" ">=${expected_gib}GiB"
    fi
    
    if (( $(echo "$limit > 0" | bc -l) )); then
        local pct=$(echo "scale=0; $usage * 100 / $limit" | bc -l)
        if (( $(echo "$pct > 90" | bc -l) )); then
            add_result "memory_usage" "FAIL" \
                "Memory usage at ${pct}% of limit - critical" \
                "${pct}%" "<80%"
        elif (( $(echo "$pct > 70" | bc -l) )); then
            add_result "memory_usage" "WARN" \
                "Memory usage at ${pct}% of limit - high" \
                "${pct}%" "<70%"
        else
            add_result "memory_usage" "PASS" \
                "Memory usage at ${pct}% of limit" \
                "${pct}%" "<70%"
        fi
    fi
}

# Validate TSDB health metrics
validate_tsdb_health() {
    local metrics_json=$1
    
    local wal_corruptions=$(echo "$metrics_json" | jq -r '.wal_corruptions')
    local compaction_duration=$(echo "$metrics_json" | jq -r '.compaction_duration')
    local ingester_rejected=$(echo "$metrics_json" | jq -r '.ingester_rejected')
    local query_errors=$(echo "$metrics_json" | jq -r '.query_errors')
    local query_duration_p99=$(echo "$metrics_json" | jq -r '.query_duration_p99')
    local block_count=$(echo "$metrics_json" | jq -r '.block_count')
    
    # WAL Corruptions
    if [[ $wal_corruptions -gt 0 ]]; then
        add_result "wal_corruptions" "FAIL" \
            "WAL corruptions detected: $wal_corruptions" \
            "$wal_corruptions" "0"
    else
        add_result "wal_corruptions" "PASS" "No WAL corruptions" "0" "0"
    fi
    
    # Compaction lag (if compaction takes too long)
    if (( $(echo "$compaction_duration > 3600" | bc -l) )); then
        add_result "compaction_lag" "WARN" \
            "Compaction duration high: ${compaction_duration}s" \
            "${compaction_duration}s" "<3600s"
    else
        add_result "compaction_lag" "PASS" \
            "Compaction duration normal: ${compaction_duration}s" \
            "${compaction_duration}s" "<3600s"
    fi
    
    # Cardinality rejections
    if [[ $ingester_rejected -gt 0 ]]; then
        add_result "cardinality_rejections" "WARN" \
            "Samples rejected due to cardinality limit: $ingester_rejected" \
            "$ingester_rejected" "0"
    else
        add_result "cardinality_rejections" "PASS" "No cardinality rejections" "0" "0"
    fi
    
    # Query errors
    if [[ $query_errors -gt 0 ]]; then
        add_result "query_errors" "WARN" \
            "Query errors detected: $query_errors" \
            "$query_errors" "0"
    else
        add_result "query_errors" "PASS" "No query errors" "0" "0"
    fi
    
    # Query latency P99
    if (( $(echo "$query_duration_p99 > 5" | bc -l) )); then
        add_result "query_latency_p99" "FAIL" \
            "Query P99 latency too high: ${query_duration_p99}s" \
            "${query_duration_p99}s" "<5s"
    elif (( $(echo "$query_duration_p99 > 2" | bc -l) )); then
        add_result "query_latency_p99" "WARN" \
            "Query P99 latency elevated: ${query_duration_p99}s" \
            "${query_duration_p99}s" "<2s"
    else
        add_result "query_latency_p99" "PASS" \
            "Query P99 latency acceptable: ${query_duration_p99}s" \
            "${query_duration_p99}s" "<2s"
    fi
    
    # Block count (should have blocks for 14d retention)
    if [[ $block_count -lt 10 ]]; then
        add_result "block_count" "WARN" \
            "Low block count ($block_count) - may indicate compaction issues or new cluster" \
            "$block_count" ">10"
    else
        add_result "block_count" "PASS" "Healthy block count: $block_count" "$block_count" ">10"
    fi
}

# Validate cardinality guard
validate_cardinality_guard() {
    local metrics_json=$1
    local head_series=$(echo "$metrics_json" | jq -r '.head_series')
    local max_active_series=$(echo "$metrics_json" | jq -r '.max_active_series')
    
    if [[ $max_active_series -eq 0 ]]; then
        max_active_series=500000
    fi
    
    local pct=$((head_series * 100 / max_active_series))
    
    if [[ $pct -gt 90 ]]; then
        add_result "cardinality_guard" "FAIL" \
            "Active series (${head_series}) at ${pct}% of limit (${max_active_series}) - critical" \
            "${pct}%" "<80%"
    elif [[ $pct -gt 80 ]]; then
        add_result "cardinality_guard" "WARN" \
            "Active series (${head_series}) at ${pct}% of limit (${max_active_series})" \
            "${pct}%" "<80%"
    else
        add_result "cardinality_guard" "PASS" \
            "Active series (${head_series}) at ${pct}% of limit (${max_active_series})" \
            "${pct}%" "<80%"
    fi
}

# Generate JSON report
generate_report() {
    local node_count=$1
    local category=$2
    local metrics_json=$3
    local pvc_capacity=$4
    local pvc_used=$5
    local cpu_request=$6
    local cpu_limit=$7
    local mem_request=$8
    local mem_limit=$9
    local cpu_usage=${10}
    local mem_usage=${11}
    
    local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    
    # Build results JSON array
    local results_json="["
    for i in "${!VALIDATION_RESULTS[@]}"; do
        if [[ $i -gt 0 ]]; then
            results_json+=","
        fi
        results_json+="${VALIDATION_RESULTS[$i]}"
    done
    results_json+="]"
    
    # Overall status
    local overall_status="PASS"
    if [[ "$VALIDATION_PASSED" == "false" ]]; then
        overall_status="FAIL"
    fi
    
    cat <<EOF > "$VALIDATION_OUTPUT"
{
    "timestamp": "$timestamp",
    "cluster": {
        "node_count": $node_count,
        "category": "$category"
    },
    "tsdb_metrics": $metrics_json,
    "resources": {
        "pvc": {
            "capacity_gib": $pvc_capacity,
            "used_gib": $pvc_used
        },
        "cpu": {
            "request_cores": $cpu_request,
            "limit_cores": $cpu_limit,
            "usage_cores": $cpu_usage
        },
        "memory": {
            "request_gib": $mem_request,
            "limit_gib": $mem_limit,
            "usage_gib": $mem_usage
        }
    },
    "validations": $results_json,
    "overall_status": "$overall_status"
}
EOF
    
    log_info "Validation report written to $VALIDATION_OUTPUT"
}

# Print summary
print_summary() {
    echo ""
    echo "=========================================="
    echo "  TSDB Sizing Validation Summary"
    echo "=========================================="
    echo ""
    
    local pass_count=0
    local fail_count=0
    local warn_count=0
    
    for result in "${VALIDATION_RESULTS[@]}"; do
        local status=$(echo "$result" | jq -r '.status')
        local check=$(echo "$result" | jq -r '.check')
        local message=$(echo "$result" | jq -r '.message')
        
        case $status in
            "PASS") ((pass_count++)); log_pass "[$check] $message" ;;
            "FAIL") ((fail_count++)); log_fail "[$check] $message" ;;
            "WARN") ((warn_count++)); log_warn "[$check] $message" ;;
        esac
    done
    
    echo ""
    echo "------------------------------------------"
    echo "  Total: $((pass_count + fail_count + warn_count)) | Pass: $pass_count | Fail: $fail_count | Warn: $warn_count"
    echo "------------------------------------------"
    
    if [[ "$VALIDATION_PASSED" == "true" ]]; then
        log_pass "OVERALL: VALIDATION PASSED"
        return 0
    else
        log_fail "OVERALL: VALIDATION FAILED"
        return 1
    fi
}

# Main execution
main() {
    log_info "Starting TSDB Sizing Validation (Task 1.5.10)"
    log_info "=============================================="
    
    # Check prerequisites
    command -v kubectl >/dev/null 2>&1 || { log_error "kubectl not found"; exit 1; }
    command -v jq >/dev/null 2>&1 || { log_error "jq not found"; exit 1; }
    command -v bc >/dev/null 2>&1 || { log_error "bc not found"; exit 1; }
    
    # Get cluster info
    local node_count=$(get_node_count)
    local category=$(get_cluster_category "$node_count")
    local expected_values=$(get_expected_values "$category")
    read -r expected_series expected_pvc_gib expected_cpu expected_mem expected_net <<< "$expected_values"
    
    log_info "Cluster: $node_count nodes ($category)"
    log_info "Expected: Series<=$expected_series, PVC>=${expected_pvc_gib}GiB, CPU>=${expected_cpu}, Mem>=${expected_mem}GiB"
    
    # Fetch TSDB metrics
    local metrics_output
    metrics_output=$(fetch_tsdb_metrics) || exit 1
    local metrics_json=$(parse_tsdb_metrics "$metrics_output")
    
    # Get PVC usage
    local pvc_info=$(get_pvc_usage)
    read -r pvc_capacity pvc_used <<< "$pvc_info"
    
    # Get pod resources
    local pod_resources=$(get_pod_resources)
    read -r cpu_request cpu_limit mem_request mem_limit cpu_usage mem_usage <<< "$pod_resources"
    
    # Run validations
    log_info "Running validations..."
    
    # Series count
    local head_series=$(echo "$metrics_json" | jq -r '.head_series')
    validate_series "$head_series" "$expected_series" "$category"
    
    # PVC
    validate_pvc "$pvc_capacity" "$pvc_used" "$expected_pvc_gib" "$category"
    
    # CPU
    validate_cpu "$cpu_request" "$cpu_limit" "$cpu_usage" "$expected_cpu" "$category"
    
    # Memory
    validate_memory "$mem_request" "$mem_limit" "$mem_usage" "$expected_mem" "$category"
    
    # TSDB Health
    validate_tsdb_health "$metrics_json"
    
    # Cardinality Guard
    validate_cardinality_guard "$metrics_json"
    
    # Generate report
    generate_report "$node_count" "$category" "$metrics_json" \
        "$pvc_capacity" "$pvc_used" \
        "$cpu_request" "$cpu_limit" "$mem_request" "$mem_limit" \
        "$cpu_usage" "$mem_usage"
    
    # Print summary
    print_summary
}

# Run main
main "$@"
