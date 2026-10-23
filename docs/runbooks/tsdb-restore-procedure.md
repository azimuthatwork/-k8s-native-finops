# TSDB Restore Procedure

## Overview
This document outlines the procedure to restore the embedded Prometheus TSDB from the latest backup snapshot in the event of data corruption, loss, or controller failure.

## Prerequisites
- Access to the S3 bucket containing TSDB snapshots: `s3://finops-bucket/tsdb-snapshots/`
- The `cost-controller` StatefulSet PVC: `tsdb-data` (500Gi SSD)
- `kubectl` access to the cluster
- `awscli` configured with appropriate IAM permissions

## Step-by-Step Restore Procedure

### 1. Identify the Latest Snapshot
List available snapshots sorted by date:
```
aws s3 ls s3://finops-bucket/tsdb-snapshots/ --recursive | sort
```
Identify the most recent snapshot file (e.g., `...-YYYYMMDD-HHMM.tar.zst`).

### 2. Download the Snapshot
Download using `awscli` from a recovery pod or local workstation:
```
kubectl exec -n cost-management <recovery-pod> -- aws s3 cp s3://finops-bucket/tsdb-snapshots/<snapshot-file> /tmp/
```
Alternatively, run `aws s3 cp` directly from your workstation if IAM permissions allow.

### 3. Extract the Snapshot to the Controller Pod
Copy the snapshot into the `cost-controller` pod:
```
kubectl cp ./<snapshot-file> cost-controller-0:cost-management:/tmp/ -n cost-management
```
Or exec into the pod:
```
kubectl exec -n cost-management cost-controller-0 -- bash
```
Inside the pod, navigate and extract:
```
cd /data/tsdb
rm -rf /data/tsdb/*
tar -I zstd -xf /tmp/<snapshot-file>
```
Ensure the Prometheus TSDB block and WAL directory structure is preserved.

### 4. Restart the Controller
Exit the pod shell and restart the StatefulSet to trigger TSDB recovery:
```
kubectl rollout restart statefulset/cost-controller -n cost-management
```
Wait for the pod to become Ready:
```
kubectl wait --for=condition=Ready pod/cost-controller-0 -n cost-management --timeout=300s
```
Check recovery logs:
```
kubectl logs -n cost-management cost-controller-0 | grep -i "tsdb|recovery|wal|replay"
```

### 5. Post-Restore Validation
Verify metrics endpoint:
```
kubectl port-forward -n cost-management cost-controller-0 8080:8080
```
Query TSDB health:
```
curl http://localhost:8080/metrics | grep tsdb_head_series_total
```
Confirm data integrity:
```
curl http://localhost:8080/api/v1/query_range?query=finops_cluster_total_hourly_cost_usd&start=...&end=...
```
Run validation script:
```
./scripts/validate-native-phase0.sh
```

## Rollback Procedure
If restore introduces data corruption or incorrect metrics:
1. Identify the previous snapshot (earlier timestamp).
2. Repeat the restore procedure with the older snapshot.
3. The TSDB does not support in-place rollback; fresh restore from backup is the only supported recovery path.

## Notes
- Snapshot Frequency: Every 6 hours (configurable via `tsdb-snapshotter` CronJob/scheduler).
- Retention: Hot data (14 days) on PVC; backups retained in S3 per organizational policy (e.g., 30 days).
- Data Loss Window: In a complete cluster failure, data newer than the last snapshot (up to 6 hours) may be lost. For zero-data-loss, consider multi-zone WAL replication (Phase 1.5.3).
- PVC Requirements: Ensure `tsdb-data` PVC is restored to a volume with equivalent or greater capacity and `storageClassName` `fast-ssd`.
