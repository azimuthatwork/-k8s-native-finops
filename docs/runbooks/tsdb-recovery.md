# TSDB Disaster Recovery Runbook

## Overview
This runbook describes the procedure to restore the FinOps Cost Controller's embedded TSDB (Prometheus TSDB) from S3 snapshots in case of data corruption, PVC loss, or cluster migration.

## Architecture Context
- **TSDB Location**: `/data/tsdb` on PVC mounted to `cost-controller` StatefulSet
- **Snapshot Schedule**: Every 6 hours via `tsdb-snapshotter` sidecar
- **Snapshot Storage**: `s3://<bucket>/tsdb-snapshots/<cluster-name>/YYYYMMDD-HHMM.tar.zst`
- **Retention**: Local snapshots cleaned after 24h; S3 snapshots retained per lifecycle policy (default 90 days)

---

## Prerequisites
- `kubectl` access to target cluster
- `awscli` / `gsutil` / `azcli` configured with permissions to read from snapshot bucket
- `zstd` installed for decompression (`apt-get install zstd` or `brew install zstd`)
- Cost Controller StatefulSet scaled to **0 replicas** during restore

---

## Restore Procedure

### 1. Identify Target Snapshot
List available snapshots for the cluster:
```bash
# AWS S3
aws s3 ls s3://finops-bucket/tsdb-snapshots/prod-cluster/ --human-readable --summarize

# GCP GCS
gsutil ls -lh gs://finops-bucket/tsdb-snapshots/prod-cluster/

# Azure Blob
az storage blob list --container-name finops-bucket --prefix tsdb-snapshots/prod-cluster/ --output table
```

**Select the latest healthy snapshot** (verify timestamp and size > 100MB).

### 2. Scale Down Cost Controller
```bash
# Scale to 0 to release PVC lock
kubectl scale statefulset cost-controller -n cost-management --replicas=0

# Verify pod is terminated
kubectl wait --for=delete pod/cost-controller-0 -n cost-management --timeout=120s
```

### 3. Download & Extract Snapshot
```bash
# Set variables
SNAPSHOT_NAME="20240115-020000.tar.zst"  # Replace with actual snapshot name
CLUSTER_NAME="prod-cluster"
BUCKET="finops-bucket"
LOCAL_DIR="/tmp/tsdb-restore"

mkdir -p "$LOCAL_DIR"
cd "$LOCAL_DIR"

# Download (choose your cloud provider)
# AWS
aws s3 cp "s3://${BUCKET}/tsdb-snapshots/${CLUSTER_NAME}/${SNAPSHOT_NAME}" .

# GCP
gsutil cp "gs://${BUCKET}/tsdb-snapshots/${CLUSTER_NAME}/${SNAPSHOT_NAME}" .

# Azure
az storage blob download --container-name "${BUCKET}" --name "tsdb-snapshots/${CLUSTER_NAME}/${SNAPSHOT_NAME}" --file "${SNAPSHOT_NAME}"

# Decompress and extract
zstd -d "${SNAPSHOT_NAME}" -o snapshot.tar
tar -xf snapshot.tar

# Verify extracted structure
ls -la snapshot/
# Expected: snapshot/01FXXXXXXXX/  (TSDB block directories)
```

### 4. Prepare PVC Data Directory
**Option A: Fresh PVC (Recommended for DR)**
```bash
# Delete old PVC (if exists) and let StatefulSet recreate
kubectl delete pvc data-cost-controller-0 -n cost-management

# Wait for new PVC to bind
kubectl wait --for=condition=Bound pvc/data-cost-controller-0 -n cost-management --timeout=60s
```

**Option B: Reuse Existing PVC (Faster, if PVC intact)**
```bash
# Create a helper pod to clean data directory
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: tsdb-cleaner
  namespace: cost-management
spec:
  restartPolicy: Never
  volumes:
  - name: tsdb-data
    persistentVolumeClaim:
      claimName: data-cost-controller-0
  containers:
  - name: cleaner
    image: alpine:3.19
    command: ["sh", "-c", "rm -rf /data/tsdb/* && ls -la /data/tsdb/"]
    volumeMounts:
    - name: tsdb-data
      mountPath: /data/tsdb
EOF

kubectl wait --for=condition=Ready pod/tsdb-cleaner -n cost-management --timeout=30s
kubectl logs tsdb-cleaner -n cost-management
kubectl delete pod tsdb-cleaner -n cost-management
```

### 5. Copy Snapshot Data to PVC
```bash
# Create a helper pod with PVC mounted
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: tsdb-restorer
  namespace: cost-management
spec:
  restartPolicy: Never
  volumes:
  - name: tsdb-data
    persistentVolumeClaim:
      claimName: data-cost-controller-0
  - name: snapshot-data
    hostPath:
      path: ${LOCAL_DIR}/snapshot
      type: Directory
  containers:
  - name: restorer
    image: alpine:3.19
    command: ["sh", "-c", "cp -r /snapshot/* /data/tsdb/ && ls -la /data/tsdb/"]
    volumeMounts:
    - name: tsdb-data
      mountPath: /data/tsdb
    - name: snapshot-data
      mountPath: /snapshot
EOF

kubectl wait --for=condition=Ready pod/tsdb-restorer -n cost-management --timeout=30s
kubectl logs tsdb-restorer -n cost-management
kubectl delete pod tsdb-restorer -n cost-management
```

### 6. Verify Data Integrity
```bash
# Create verification pod
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata:
  name: tsdb-verify
  namespace: cost-management
spec:
  restartPolicy: Never
  volumes:
  - name: tsdb-data
    persistentVolumeClaim:
      claimName: data-cost-controller-0
  containers:
  - name: verify
    image: alpine:3.19
    command: ["sh", "-c", "
      echo '=== Block Count ==='
      find /data/tsdb -maxdepth 1 -type d -name '01F*' | wc -l
      echo '=== WAL Directory ==='
      ls -la /data/tsdb/wal/ 2>/dev/null || echo 'No WAL (expected for cold restore)'
      echo '=== Disk Usage ==='
      du -sh /data/tsdb
      echo '=== Sample Block Meta ==='
      head -20 /data/tsdb/01F*/meta.json 2>/dev/null | head -20
    "]
    volumeMounts:
    - name: tsdb-data
      mountPath: /data/tsdb
EOF

kubectl wait --for=condition=Ready pod/tsdb-verify -n cost-management --timeout=30s
kubectl logs tsdb-verify -n cost-management
kubectl delete pod tsdb-verify -n cost-management
```

### 7. Start Cost Controller
```bash
# Scale up to 1 replica
kubectl scale statefulset cost-controller -n cost-management --replicas=1

# Monitor startup logs
kubectl logs -f cost-controller-0 -n cost-management -c cost-controller
```

**Expected Startup Sequence:**
1. `tsdb` opens existing blocks from `/data/tsdb`
2. `WAL replay` - Replays any WAL segments (should be minimal/none for snapshot restore)
3. `compaction` - May trigger vertical compaction for downsampling
4. `head` - Opens new head block for incoming writes
5. `HTTP server` - Starts on `:8080` (metrics, API)
6. `Leader election` - Acquires lease lock

### 8. Verify Restore Success
```bash
# Wait for pod ready
kubectl wait --for=condition=Ready pod/cost-controller-0 -n cost-management --timeout=180s

# Check TSDB metrics
kubectl exec -n cost-management cost-controller-0 -- wget -qO- localhost:8080/metrics | grep -E 'tsdb_head_series_total|tsdb_block_count|tsdb_wal_segments'

# Verify API query works
kubectl exec -n cost-management cost-controller-0 -- wget -qO- 'localhost:8080/api/v1/query?query=finops_cluster_total_hourly_cost_usd'

# Check CRD status updates resume
kubectl get costallocation -A --watch  # Should show recent timestamps
```

---

## Validation Checklist
- [ ] Pod `cost-controller-0` is `Running` and `Ready`
- [ ] `tsdb_head_series_total` > 0 (metrics ingesting)
- [ ] `tsdb_block_count` matches expected block count from snapshot
- [ ] `/api/v1/query` returns data for last 14 days
- [ ] `/api/v1/series?match[]=finops_workload_hourly_cost_usd` returns series
- [ ] `CostAllocation` CRDs updating with recent `lastUpdate` timestamps
- [ ] No `tsdb_wal_corruptions_total` increase in metrics
- [ ] Dashboard loads historical data (30d range query < 5s)

---

## Rollback Procedure (If Restore Fails)
```bash
# 1. Scale down
kubectl scale statefulset cost-controller -n cost-management --replicas=0

# 2. Delete PVC to force fresh start
kubectl delete pvc data-cost-controller-0 -n cost-management

# 3. Scale up (starts with empty TSDB, begins fresh ingestion)
kubectl scale statefulset cost-controller -n cost-management --replicas=1

# Note: Historical data lost, but real-time cost allocation resumes immediately
```

---

## Troubleshooting

### Issue: `tsdb_wal_corruptions_total` Increases
**Cause**: WAL segments corrupted during copy or snapshot.
**Fix**: 
1. Delete WAL directory: `rm -rf /data/tsdb/wal` (via helper pod)
2. Restart controller - TSDB will recover from persisted blocks only (last 2h may be lost)

### Issue: `tsdb_head_series_total` Stays at 0
**Cause**: No blocks found or permission issue.
**Fix**: 
1. Verify PVC mount: `kubectl exec cost-controller-0 -- ls -la /data/tsdb/`
2. Check block directories exist: `01F*` directories with `meta.json`, `chunks/`, `index/`
3. Verify UID/GID: TSDB runs as non-root (UID 65534), PVC must allow write

### Issue: Query Returns No Data for Restored Period
**Cause**: Snapshot too old or blocks not loaded.
**Fix**: 
1. Check block time ranges: `cat /data/tsdb/01F*/meta.json | jq '.minTime, .maxTime'`
2. Ensure snapshot covers desired time range
3. Restore from earlier snapshot if needed

### Issue: Controller OOMKilled During Startup
**Cause**: Large block count causing high memory during block loading.
**Fix**: 
1. Increase memory limit: `resources.limits.memory: 16Gi` (for large clusters)
2. Add `--query.lookback-delta=5m` to reduce initial query load
3. Consider restoring only last 30 days (delete older blocks before copy)

---

## Automation Script
```bash
#!/bin/bash
# scripts/restore-tsdb.sh
# Usage: ./restore-tsdb.sh <cluster-name> <snapshot-timestamp> [namespace]

set -euo pipefail

CLUSTER="${1:-prod-cluster}"
SNAPSHOT="${2:-latest}"
NAMESPACE="${3:-cost-management}"
BUCKET="finops-bucket"

echo "=== TSDB Restore for ${CLUSTER} ==="
echo "Snapshot: ${SNAPSHOT}"
echo "Namespace: ${NAMESPACE}"

# ... (implement steps 1-8 from above with error handling)
# This script should be tested in staging before production use
```

---

## Related Documents
- [Cost Controller Debugging](../runbooks/cost-controller-debugging.md)
- [Phase 1.5 TSDB Hardening Plan](../../PLAN-NATIVE.md#phase-15-embedded-time-series-database---technical-implementation-plan)
- [Snapshotter Implementation](../../cmd/snapshotter/main.go)

---

## Contact
- **Owner**: Platform Engineering / FinOps Team
- **Slack**: #finops-platform
- **Escalation**: On-call Platform Engineer
