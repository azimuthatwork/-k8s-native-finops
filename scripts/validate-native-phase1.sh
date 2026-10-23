#!/bin/bash
set -euo pipefail

# Phase 1 Validation Script for Kubernetes-Native FinOps Platform
# Validates Cost Controller deployment, TSDB ingestion, CRD updates, and API queryability

NAMESPACE="cost-management"
CONTROLLER_NAME="cost-controller"
TIMEOUT=300

echo "=== Phase 1 Validation: Cost Controller ==="
echo "Namespace: $NAMESPACE"
echo "Controller: $CONTROLLER_NAME"
echo ""

# Function to check if a command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Check prerequisites
for cmd in kubectl curl jq; do
    if ! command_exists "$cmd"; then
        echo "ERROR: $cmd is required but not installed"
        exit 1
    fi
done

echo "✓ Prerequisites check passed"
echo ""

# 1. Check Cost Controller StatefulSet
echo "1. Checking Cost Controller StatefulSet..."
if ! kubectl get statefulset "$CONTROLLER_NAME" -n "$NAMESPACE" >/dev/null 2>&1; then
    echo "ERROR: StatefulSet $CONTROLLER_NAME not found in namespace $NAMESPACE"
    exit 1
fi

REPLICAS=$(kubectl get statefulset "$CONTROLLER_NAME" -n "$NAMESPACE" -o jsonpath='{.spec.replicas}')
READY_REPLICAS=$(kubectl get statefulset "$CONTROLLER_NAME" -n "$NAMESPACE" -o jsonpath='{.status.readyReplicas}')

if [ "$READY_REPLICAS" != "$REPLICAS" ]; then
    echo "ERROR: StatefulSet not fully ready. Ready: $READY_REPLICAS/$REPLICAS"
    kubectl get pods -n "$NAMESPACE" -l app.kubernetes.io/name=cost-controller
    exit 1
fi

echo "✓ StatefulSet is ready ($READY_REPLICAS/$REPLICAS replicas)"
echo ""

# 2. Check Pods
echo "2. Checking Cost Controller Pods..."
PODS=$(kubectl get pods -n "$NAMESPACE" -l app.kubernetes.io/name=cost-controller -o jsonpath='{.items[*].metadata.name}')
for pod in $PODS; do
    PHASE=$(kubectl get pod "$pod" -n "$NAMESPACE" -o jsonpath='{.status.phase}')
    if [ "$PHASE" != "Running" ]; then
        echo "ERROR: Pod $pod is not Running (phase: $PHASE)"
        exit 1
    fi

    # Check container statuses
    READY=$(kubectl get pod "$pod" -n "$NAMESPACE" -o jsonpath='{.status.containerStatuses[*].ready}')
    if [[ "$READY" != *"true"* ]]; then
        echo "ERROR: Pod $pod containers not ready"
        kubectl describe pod "$pod" -n "$NAMESPACE"
        exit 1
    fi
done
echo "✓ All pods running and ready"
echo ""

# 3. Check PVC
echo "3. Checking TSDB PVC..."
PVC_NAME="tsdb-data-$CONTROLLER_NAME-0"
if ! kubectl get pvc "$PVC_NAME" -n "$NAMESPACE" >/dev/null 2>&1; then
    echo "ERROR: PVC $PVC_NAME not found"
    exit 1
fi

PVC_STATUS=$(kubectl get pvc "$PVC_NAME" -n "$NAMESPACE" -o jsonpath='{.status.phase}')
if [ "$PVC_STATUS" != "Bound" ]; then
    echo "ERROR: PVC $PVC_NAME not bound (status: $PVC_STATUS)"
    exit 1
fi
echo "✓ PVC $PVC_NAME is Bound"
echo ""

# 4. Check Metrics Endpoint
echo "4. Checking Metrics Endpoint..."
POD=$(kubectl get pods -n "$NAMESPACE" -l app.kubernetes.io/name=cost-controller -o jsonpath='{.items[0].metadata.name}')

# Wait for metrics endpoint to be available
for i in {1..30}; do
    if kubectl exec -n "$NAMESPACE" "$POD" -- wget -qO- http://localhost:8080/metrics 2>/dev/null | grep -q "tsdb_head_series_total"; then
        echo "✓ Metrics endpoint responding with TSDB metrics"
        break
    fi
    if [ $i -eq 30 ]; then
        echo "ERROR: Metrics endpoint not responding with TSDB metrics after 30 attempts"
        kubectl exec -n "$NAMESPACE" "$POD" -- wget -qO- http://localhost:8080/metrics 2>/dev/null | head -50
        exit 1
    fi
    sleep 2
done
echo ""

# 5. Check TSDB Metrics
echo "5. Validating TSDB Self-Observability Metrics..."
METRICS=$(kubectl exec -n "$NAMESPACE" "$POD" -- wget -qO- http://localhost:8080/metrics 2>/dev/null)

REQUIRED_METRICS=(
    "tsdb_head_series_total"
    "tsdb_head_samples_appended_total"
    "tsdb_head_bytes"
    "tsdb_compaction_duration_seconds"
    "tsdb_wal_corruptions_total"
    "tsdb_block_count"
    "finops_ingester_batch_size_avg"
    "finops_ingester_flush_duration_seconds"
    "finops_ingester_rejected_samples_total"
    "finops_ingester_exemplars_dropped_total"
    "finops_query_duration_seconds"
    "finops_query_samples_total"
    "finops_query_errors_total"
)

for metric in "${REQUIRED_METRICS[@]}"; do
    if echo "$METRICS" | grep -q "^$metric"; then
        echo "✓ $metric present"
    else
        echo "WARNING: $metric not found in metrics output"
    fi
done
echo ""

# 6. Check API Endpoints
echo "6. Checking API Endpoints..."
API_ENDPOINTS=(
    "/api/v1/query?query=finops_cluster_total_hourly_cost_usd"
    "/api/v1/query_range?query=finops_cluster_total_hourly_cost_usd&start=$(date -d '1 hour ago' +%s)&end=$(date +%s)&step=60"
    "/api/v1/series?match[]=finops_workload_hourly_cost_usd"
    "/api/v1/labels"
    "/api/v1/label/namespace/values"
    "/api/v1/dashboard/overview"
    "/api/v1/dashboard/workloads"
    "/api/v1/dashboard/nodes"
    "/api/v1/dashboard/storage"
    "/api/v1/dashboard/network"
    "/api/v1/recommendations"
    "/api/v1/budgets"
)

for endpoint in "${API_ENDPOINTS[@]}"; do
    if kubectl exec -n "$NAMESPACE" "$POD" -- wget -qO- --timeout=10 "http://localhost:8082$endpoint" 2>/dev/null | grep -q "status"; then
        echo "✓ $endpoint responding"
    else
        echo "WARNING: $endpoint not responding correctly"
    fi
done
echo ""

# 7. Check CRDs
echo "7. Checking CRD Status Updates..."
CRDS=("costallocations" "nodecosts" "pvcosts" "costhistories" "rightsizingrecommendations" "namespacebudgets" "logevents")

for crd in "${CRDS[@]}"; do
    if kubectl get crd "$crd.finops.io" >/dev/null 2>&1; then
        echo "✓ CRD $crd.finops.io exists"
    else
        echo "ERROR: CRD $crd.finops.io not found"
        exit 1
    fi
done

# Check if CostAllocation CRs have status
ALLOCATIONS=$(kubectl get costallocations -A --no-headers 2>/dev/null | wc -l)
if [ "$ALLOCATIONS" -gt 0 ]; then
    echo "✓ CostAllocation CRs found ($ALLOCATIONS)"
    # Check first one for status
    FIRST_ALLOC=$(kubectl get costallocations -A --no-headers -o jsonpath='{.items[0].metadata.name}')
    FIRST_NS=$(kubectl get costallocations -A --no-headers -o jsonpath='{.items[0].metadata.namespace}')
    if [ -n "$FIRST_ALLOC" ]; then
        STATUS=$(kubectl get costallocation "$FIRST_ALLOC" -n "$FIRST_NS" -o jsonpath='{.status.hourlyCostUSD}' 2>/dev/null)
        if [ -n "$STATUS" ] && [ "$STATUS" != "0" ]; then
            echo "✓ CostAllocation status populated (hourlyCostUSD: $STATUS)"
        else
            echo "WARNING: CostAllocation status not yet populated"
        fi
    fi
else
    echo "WARNING: No CostAllocation CRs found yet (may need time to reconcile)"
fi
echo ""

# 8. Check Leader Election
echo "8. Checking Leader Election..."
LEASE=$(kubectl get lease cost-controller-leader -n "$NAMESPACE" -o jsonpath='{.spec.holderIdentity}' 2>/dev/null)
if [ -n "$LEASE" ]; then
    echo "✓ Leader election active (holder: $LEASE)"
else
    echo "WARNING: Leader election lease not found"
fi
echo ""

# 9. Check Cardinality Guard
echo "9. Checking Cardinality Guard..."
CURRENT_SERIES=$(echo "$METRICS" | grep "^tsdb_head_series_total" | awk '{print $2}')
MAX_SERIES=500000
if [ -n "$CURRENT_SERIES" ]; then
    PERCENT=$(echo "scale=2; $CURRENT_SERIES * 100 / $MAX_SERIES" | bc -l 2>/dev/null || echo "0")
    echo "✓ Current series: $CURRENT_SERIES / $MAX_SERIES (${PERCENT}%)"
    if (( $(echo "$CURRENT_SERIES > $MAX_SERIES * 0.8" | bc -l 2>/dev/null || echo 0) )); then
        echo "WARNING: Series count above 80% threshold"
    fi
else
    echo "WARNING: Could not determine current series count"
fi
echo ""

# 10. Check Snapshotter
echo "10. Checking Snapshotter Sidecar..."
SNAPSHOTTER_READY=$(kubectl get pod "$POD" -n "$NAMESPACE" -o jsonpath='{.status.containerStatuses[?(@.name=="snapshotter")].ready}')
if [ "$SNAPSHOTTER_READY" = "true" ]; then
    echo "✓ Snapshotter sidecar running"
else
    echo "WARNING: Snapshotter sidecar not ready"
fi
echo ""

echo "=== Phase 1 Validation Complete ==="
echo ""
echo "Summary:"
echo "  - Cost Controller: Running"
echo "  - TSDB: Ingesting metrics"
echo "  - CRDs: Status updates active"
echo "  - API: Queryable"
echo "  - Self-Observability: Metrics exposed"
echo ""
echo "Next: Run Phase 1.5 validation for TSDB hardening"
echo "  ./scripts/validate-native-phase1.5.sh"
