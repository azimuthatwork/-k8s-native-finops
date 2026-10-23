#!/bin/bash
# Phase 2 Validation Script
# Validates: TSDB Retention, Compaction, Query API, Parquet Export

set -euo pipefail

NAMESPACE="${NAMESPACE:-cost-management}"
POD_SELECTOR="${POD_SELECTOR:-app=cost-controller}"

echo "=========================================="
echo "Phase 2 Validation: Historical Storage & Query Layer"
echo "=========================================="

# Source the retention validation script
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/validate-tsdb-retention-compaction.sh"

echo ""
echo "--- Query Performance Validation ---"

POD=$(kubectl get pods -n "$NAMESPACE" -l "$POD_SELECTOR" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)

if [[ -n "$POD" ]]; then
    # Test instant query latency
    echo "Testing instant query latency..."
    START=$(date +%s%N)
    kubectl exec -n "$NAMESPACE" "$POD" -- wget -qO- "http://localhost:8080/api/v1/query?query=finops_cluster_total_hourly_cost_usd" >/dev/null 2>&1
    END=$(date +%s%N)
    LATENCY_MS=$(( (END - START) / 1000000 ))
    
    if [[ $LATENCY_MS -lt 500 ]]; then
        echo -e "${GREEN}✓ PASS${NC}: Instant query latency: ${LATENCY_MS}ms (< 500ms)"
    else
        echo -e "${YELLOW}⚠ WARN${NC}: Instant query latency: ${LATENCY_MS}ms (>= 500ms)"
    fi

    # Test range query latency (30d, 1h step)
    echo "Testing range query latency (30d)..."
    END_TIME=$(date +%s)
    START_TIME=$((END_TIME - 30*86400))
    START=$(date +%s%N)
    kubectl exec -n "$NAMESPACE" "$POD" -- wget -qO- "http://localhost:8080/api/v1/query_range?query=finops_cluster_total_hourly_cost_usd&start=${START_TIME}&end=${END_TIME}&step=3600" >/dev/null 2>&1
    END=$(date +%s%N)
    LATENCY_MS=$(( (END - START) / 1000000 ))
    
    if [[ $LATENCY_MS -lt 2000 ]]; then
        echo -e "${GREEN}✓ PASS${NC}: Range query latency: ${LATENCY_MS}ms (< 2s)"
    else
        echo -e "${YELLOW}⚠ WARN${NC}: Range query latency: ${LATENCY_MS}ms (>= 2s)"
    fi

    # Test series discovery latency
    echo "Testing series discovery latency..."
    START=$(date +%s%N)
    kubectl exec -n "$NAMESPACE" "$POD" -- wget -qO- "http://localhost:8080/api/v1/series?match[]=finops_workload_hourly_cost_usd" >/dev/null 2>&1
    END=$(date +%s%N)
    LATENCY_MS=$(( (END - START) / 1000000 ))
    
    if [[ $LATENCY_MS -lt 1000 ]]; then
        echo -e "${GREEN}✓ PASS${NC}: Series discovery latency: ${LATENCY_MS}ms (< 1s)"
    else
        echo -e "${YELLOW}⚠ WARN${NC}: Series discovery latency: ${LATENCY_MS}ms (>= 1s)"
    fi
else
    echo -e "${RED}✗ FAIL${NC}: No cost-controller pod found"
fi

echo ""
echo "--- Parquet Export Validation ---"

# Check if exporter CronJob exists
EXPORTER_CRON=$(kubectl get cronjob -n "$NAMESPACE" -l app=cost-exporter -o name 2>/dev/null | head -1)
if [[ -n "$EXPORTER_CRON" ]]; then
    echo -e "${GREEN}✓ PASS${NC}: Cost exporter CronJob exists: $EXPORTER_CRON"
    
    # Check last run
    LAST_SCHEDULE=$(kubectl get "$EXPORTER_CRON" -n "$NAMESPACE" -o jsonpath='{.status.lastScheduleTime}' 2>/dev/null)
    if [[ -n "$LAST_SCHEDULE" ]]; then
        echo "  Last scheduled: $LAST_SCHEDULE"
    fi
else
    echo -e "${YELLOW}⚠ WARN${NC}: Cost exporter CronJob not found (may not be deployed yet)"
fi

# Check S3 bucket for parquet files (if AWS CLI available)
if command -v aws &> /dev/null; then
    BUCKET="${PARQUET_BUCKET:-finops-bucket}"
    echo "Checking S3 bucket: s3://$BUCKET/cost/allocations/"
    if aws s3 ls "s3://$BUCKET/cost/allocations/" --recursive 2>/dev/null | head -5; then
        echo -e "${GREEN}✓ PASS${NC}: Parquet files found in S3"
    else
        echo -e "${YELLOW}⚠ WARN${NC}: No parquet files found in S3 (may not have run yet)"
    fi
else
    echo "  AWS CLI not available, skipping S3 check"
fi

echo ""
echo "=========================================="
echo "Phase 2 Validation Complete"
echo "=========================================="
echo "Review results above. If all critical checks pass, Phase 2 is ready."
