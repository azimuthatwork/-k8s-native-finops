# Kubernetes-Native FinOps Platform - Implementation Tasks

This document breaks down the **PLAN-NATIVE.md** into granular, trackable tasks for each phase. Each task is a checkbox item with clear acceptance criteria.

---

## Phase 0: Native Prerequisites & Baseline Validation
**Objective:** Validate KSM, Metrics Server, Cloud Identity, and CRD readiness.

### 0.1 Kube-State-Metrics (KSM) Hardening
- [x] **Task 0.1.1:** Deploy `kube-state-metrics` Deployment (2 replicas) in `monitoring` namespace
  - [x] Configure metric allowlist (only 14 critical metrics exposed)
  - [x] Configure label allowlist: `app`, `version`, `cost-center`, `team` for `kube_pod_labels`
  - [x] Ensure `namespace` labels (`cost-center`, `team`, `env`) propagate to `kube_namespace_labels`
  - [x] Set resource limits: Memory 512Mi+, CPU 200m
  - [x] Validate: `curl kube-state-metrics:8080/metrics | grep -c "kube_pod_container_resource_requests" > 0`

- [x] **Task 0.1.2:** Create KSM ConfigMap (`config/ksm/ksm-config.yaml`) with allowlist configuration
- [x] **Task 0.1.3:** Apply NetworkPolicy allowing Cost Controller -> KSM (port 8080)

### 0.2 Metrics Server & Resource Pipeline
- [x] **Task 0.2.1:** Deploy `metrics-server` with args: `--kubelet-insecure-tls`, `--metric-resolution=30s`
- [x] **Task 0.2.2:** Validate `kubectl top pods/nodes` returns data < 30s latency
- [x] **Task 0.2.3:** Configure `--kubelet-preferred-address-types=InternalIP,Hostname`

### 0.3 Node Exporter DaemonSet (Native Node Metrics)
- [x] **Task 0.3.1:** Deploy `prometheus-node-exporter` DaemonSet (hostNetwork, hostPID)
  - [x] Enable collectors: `cpu`, `mem`, `disk`, `net`, `netstat`, `filesystem`
  - [x] Create Service: `node-exporter.monitoring.svc.cluster.local:9100` (ClusterIP)
- [x] **Task 0.3.2:** Apply NetworkPolicy allowing Cost Controller -> Node Exporter (port 9100)
- [x] **Task 0.3.3:** Validate metrics endpoint: `curl node-exporter:9100/metrics | grep node_cpu_seconds_total`

### 0.4 Cloud Provider Authentication (IRSA / Workload Identity)
- [x] **Task 0.4.1:** Create ServiceAccount `cost-controller-sa` in `cost-management` namespace
- [x] **Task 0.4.2:** Configure Cloud IAM Roles/Policies (ReadOnly):
  - [x] **AWS:** `ec2:DescribeInstances`, `pricing:GetProducts`, `ce:GetCostAndUsage`, `cloudwatch:GetMetricStatistics`
  - [x] **GCP:** `compute.instances.list`, `cloudbilling.catalog.list`, `monitoring.timeSeries.list`
  - [x] **Azure:** `Microsoft.Compute/virtualMachines/read`, `Microsoft.Consumption/usageDetails/read`, `Microsoft.Insights/metrics/read`
- [x] **Task 0.4.3:** Annotate SA with IRSA/Workload Identity role ARN
- [x] **Task 0.4.4:** Test cloud SDK authentication from pod using `cost-controller-sa`

### 0.5 Core CRDs (The "Database Schema")
- [x] **Task 0.5.1:** Generate CRD manifests using `controller-gen` (`make manifests`)
- [x] **Task 0.5.2:** Apply all 7 CRDs (`kubectl apply -k config/crds`):
  - [x] `CostAllocation` (Workload-level cost & usage)
  - [x] `NodeCost` (Node-level cost & utilization)
  - [x] `PVCost` (PVC-level cost & utilization)
  - [x] `CostHistory` (Hourly snapshots for trends)
  - [x] `RightsizingRecommendation` (VPA replacement output)
  - [x] `NamespaceBudget` (Governance input)
  - [x] `LogEvent` (OOM/Eviction/Error events)
- [x] **Task 0.5.3:** Validate CRD status subresources enabled
- [x] **Task 0.5.4:** Create `NamespaceBudget` example CR for `finops-system` namespace

### 0.6 Validation & Sign-off
- [x] **Task 0.6.1:** Create validation script `scripts/validate-native-phase0.sh` orchestrating all checks
- [x] **Task 0.6.2:** Run validation script and generate report `docs/phase0-native-report.md`
- [x] **Task 0.6.3:** Obtain approvals:
  - [x] Platform Engineering (Infrastructure readiness)
  - [x] FinOps (Labeling strategy: `cost-center`, `team`, `environment`)
  - [x] Security (NetworkPolicies, RBAC, IRSA)

---

## Phase 1: Cost Controller (The Engine)
**Objective:** Single Go Controller calculating real-time cost, usage, efficiency. Replaces OpenCost + Prometheus + Alloy.

### 1.1 Controller Skeleton (`cmd/cost-controller`)
- [x] **Task 1.1.1:** Initialize Go module (`go mod init github.com/yourorg/kube-native-finops`)
- [x] **Task 1.1.2:** Scaffold `controller-runtime` project (kubebuilder v4+)
  - [x] `kubebuilder init --domain finops.io --repo github.com/yourorg/kube-native-finops`
  - [x] `kubebuilder create api --group finops --version v1alpha1 --kind CostAllocation`
  - [x] Repeat for: `NodeCost`, `PVCost`, `CostHistory`, `RightsizingRecommendation`, `NamespaceBudget`, `LogEvent`
- [x] **Task 1.1.3:** Implement `main.go` with Leader Election (`--leader-elect=true`)
- [x] **Task 1.1.4:** Create StatefulSet manifest (`config/default/statefulset.yaml`)
  - [x] 1 replica for MVP, PVC 500Gi SSD
  - [x] Mount PVC at `/data/tsdb`
  - [x] Resource limits: CPU 2, Memory 8Gi (Medium cluster)
- [x] **Task 1.1.5:** Implement three Reconcilers:
  - [x] `CostAllocationReconciler` (Watches Pods/Namespaces)
  - [x] `NodeCostReconciler` (Watches Nodes/Cloud)
  - [x] `PVCostReconciler` (Watches PVCs/PVs/StorageClasses)

### 1.2 Data Ingestion Clients (`pkg/clients/`)
- [x] **Task 1.2.1:** Implement `KSMClient` (`pkg/clients/ksm.go`)
  - [x] Pull `/metrics` endpoint every 30s
  - [x] Parse Prometheus text format
  - [x] Cache in memory with TTL
  - [x] **Alternative:** Use `client-go` Informers for `Pod`, `Namespace`, `Node` (preferred for events)
- [x] **Task 1.2.2:** Implement `MetricsClient` (`pkg/clients/metrics.go`)
  - [x] Call `metrics.k8s.io/v1beta1` REST API
  - [x] Query `pods` and `nodes` metrics on reconcile
  - [x] Handle missing metrics gracefully (not ready)
- [x] **Task 1.2.3:** Implement `NodeExporterClient` (`pkg/clients/nodeexporter.go`)
  - [x] Pull `node-exporter:9100/metrics` every 60s
  - [x] Parse: `node_cpu_seconds_total`, `node_memory_*`, `node_network_*`, `node_filesystem_*`
- [x] **Task 1.2.4:** Implement `CloudPricingClient` (`pkg/clients/cloud/`)
  - [x] **AWS:** `pkg/clients/cloud/aws.go` - Pricing API + EC2 DescribeInstances + Cost Explorer
  - [x] **GCP:** `pkg/clients/cloud/gcp.go` - Cloud Billing Catalog + Compute API
  - [x] **Azure:** `pkg/clients/cloud/azure.go` - Retail Prices + Consumption API
  - [x] **Static Fallback:** `pkg/clients/cloud/static.go` - ConfigMap-based pricing
  - [x] Cache: 1h TTL (Spot: 5m TTL)
  - [x] Map instance type -> `Hourly_Price`, `CPU_Cores`, `Memory_GiB`, `GPU_Count`, `GPU_Type`
- [x] **Task 1.2.5:** Implement `K8sInformerCache` (`pkg/clients/informers.go`)
  - [x] SharedInformerFactory for: Pods, Nodes, Namespaces, PVCs, PVs, StorageClasses, Events
  - [x] Indexers: `namespace`, `nodeName`, `spec.nodeName`, `spec.volumeName`
- [x] **Task 1.2.6:** Implement `ClientFactory` (`pkg/clients/factory.go`) to wire all clients together
  - [x] Load kubeconfig (in-cluster + kubeconfig file)
  - [x] Create all clients with proper configuration
  - [x] Start/Stop lifecycle management

### 1.3 Allocation Algorithm (`pkg/allocation/engine.go`)
- [x] **Task 1.3.1:** Implement Node Cost Calculation
  - [x] `Hourly_Price * max(CPU_Request/Allocatable_CPU, Mem_Request/Allocatable_Mem)` (Weighted Share)
  - [x] Handle Spot/Preemptible pricing (separate price dimension)
  - [x] Handle GPU: `GPU_Request * GPU_Hourly_Price`
- [x] **Task 1.3.2:** Implement Shared Cost Distribution
  - [x] Control Plane cost (Master nodes) -> Distribute proportionally to workload CPU requests
  - [x] Unallocated Node Capacity -> Distribute proportionally to workload requests
  - [x] DaemonSet overhead -> Distribute to all pods on node
- [x] **Task 1.3.3:** Implement PV Cost Calculation
  - [x] `PVC_Request_GB * StorageClass_Price_GB_Month / 730`
  - [x] Handle dynamic provisioning vs static PVs
  - [x] Support `volumeBindingMode: WaitForFirstConsumer` (zone awareness)
- [x] **Task 1.3.4:** Implement Network Cost Allocation
  - [x] `Node_Egress_GB * Cloud_Egress_Price` -> Allocate by Pod Network Bytes (if CNI metrics)
  - [x] Fallback: Even split across pods on node
- [x] **Task 1.3.5:** Implement Allocation Engine Orchestration
  - [x] Run every 60s (Reconcile Loop)
  - [x] Input: KSM (Requests/Limits), Metrics Server (Usage), Node Exporter (Node Util), Cloud Pricing
  - [x] Output: `CostAllocation`, `NodeCost`, `PVCost` CRD Status updates
  - [x] Batch `Status().Patch()` per Namespace to avoid Etcd conflicts

### 1.4 Embedded TSDB (`pkg/storage/tsdb.go`)
- [x] **Task 1.4.1:** Initialize Prometheus TSDB (`github.com/prometheus/tsdb` v0.15+)
  - [x] Configure `RetentionDuration: 14d` (Hot)
  - [x] Configure `BlockRanges: [2h, 24h, 7d, 30d]`
  - [x] Enable WAL (`WALFlushInterval: 30s`)
  - [x] Set `maxActiveSeries: 500,000` (Cardinality Guard)
- [x] **Task 1.4.2:** Define Metric Schema (Export via `/metrics` endpoint)
  - [x] **Workload Level:**
    - [x] `finops_workload_hourly_cost_usd{namespace, workload, kind, cost_center, team}`
    - [x] `finops_workload_cpu_cores_requested`
    - [x] `finops_workload_cpu_cores_used`
    - [x] `finops_workload_memory_bytes_requested`
    - [x] `finops_workload_memory_bytes_used`
    - [x] `finops_workload_efficiency_ratio`
  - [x] **Node Level:**
    - [x] `finops_node_hourly_cost_usd{node, instance_type, zone, cost_center}`
    - [x] `finops_node_cpu_utilization_ratio`
    - [x] `finops_node_memory_utilization_ratio`
  - [x] **Cluster Level:**
    - [x] `finops_cluster_total_hourly_cost_usd`
    - [x] `finops_cluster_idle_cost_usd`
- [x] **Task 1.4.3:** Implement Ingester (`pkg/storage/ingester.go`)
  - [x] Batch writes: 5s flush interval or 10k samples
  - [x] Label sanitization (drop empty, enforce naming)
  - [x] Cost label enrichment (`cost_center`, `team` from namespace cache)
  - [x] Exemplar support (attach `trace_id`/`span_id`)
  - [x] Cardinality limit enforcement (reject with 429)
- [x] **Task 1.4.4:** Implement Compaction Configuration
  - [x] Vertical compaction: 1m -> 5m -> 1h downsampling for `finops_*` metrics
  - [x] Horizontal compaction: Merge blocks in same time range
- [x] **Task 1.4.5:** Implement HA Design (Single Writer + Fast Failover)
  - [x] StatefulSet with 1 replica (MVP)
  - [x] `terminationGracePeriodSeconds: 60`
  - [x] `podManagementPolicy: Parallel`
  - [x] WAL Replay < 30s for 14d data
  - [x] **Future:** Standby replica with WAL tailing (Phase 1.5)

### 1.5 CRD Status Updates
- [x] **Task 1.5.1:** Implement `CostAllocation` Status update (every 60s)
  - [x] Fields: `hourlyCostUSD`, `cpuCoresRequested`, `cpuCoresUsed`, `memoryBytesRequested`, `memoryBytesUsed`, `efficiencyRatio`, `lastUpdate`
  - [x] Container breakdown with requests/limits/usage/cost
  - [x] PV allocation breakdown
  - [x] Network allocation
  - [x] Conditions with `Reconciled` status
- [x] **Task 1.5.2:** Implement `NodeCost` Status update
  - [x] Fields: `hourlyCostUSD`, `cpuUtilization`, `memoryUtilization`, `instanceType`, `zone`, `costCenter`
  - [x] Allocatable/allocated resources
  - [x] Unallocated/shared/daemonset cost breakdown
  - [x] Spot detection
  - [x] Conditions with `Reconciled` status
- [x] **Task 1.5.3:** Implement `PVCost` Status update
  - [x] Fields: `monthlyCostUSD`, `requestedGB`, `usedGB`, `storageClass`, `costCenter`
  - [x] Utilization ratio
  - [x] PVC phase tracking
  - [x] Conditions with `Reconciled` status
- [x] **Task 1.5.4:** Optimize patches using `client-go` `Patch` (Subresource Status)
  - [x] Controllers use `r.Status().Update()` for subresource updates
  - [x] Efficient change detection to avoid unnecessary updates
  - [x] Batch reconciliation per namespace (60s interval)
- [x] **Task 1.5.5:** Implement `tsdb-snapshotter` sidecar (`cmd/snapshotter/main.go`)
  - [x] Schedule: Every 6 hours (CronJob or in-process ticker)
  - [x] Process: `db.Snapshot(dir)` -> Tar + Zstd -> Upload to S3 (`s3://bucket/tsdb-snapshots/cluster/YYYYMMDD-HHMM.tar.zst`)
  - [x] Cleanup local snapshots > 24h

### 1.6 API Gateway (`pkg/api/`)
- [x] **Task 1.6.1:** Implement PromQL-compatible Query API
  - [x] `GET /api/v1/query` (Instant)
  - [x] `GET /api/v1/query_range` (Range)
  - [x] `GET /api/v1/series`, `/labels`, `/label/:name/values` (Discovery)
- [x] **Task 1.6.2:** Implement Dashboard Aggregation Endpoints
  - [x] `GET /api/v1/dashboard/overview` (Total spend, trend, top 10, efficiency)
  - [x] `GET /api/v1/dashboard/workloads` (Paginated table: cost, usage, efficiency, recs)
  - [x] `GET /api/v1/dashboard/nodes` (Heatmap data: node, util%, cost, instance-type)
  - [x] `GET /api/v1/dashboard/storage` (PVC table: cost, util%, orphaned, abandoned snaps)
  - [x] `GET /api/v1/dashboard/network` (Egress cost by NS, top talkers)
  - [x] `GET /api/v1/recommendations` (Rightsizing list with savings)
  - [x] `GET /api/v1/budgets` (Budget vs Actual per namespace)
- [x] **Task 1.6.3:** Implement Action Endpoint
  - [x] `POST /api/v1/recommendations/{name}/apply` (Triggers GitOps PR)
- [x] **Task 1.6.4:** Implement Auth Middleware
  - [x] Impersonation headers: `Impersonate-User`, `Impersonate-Groups`
  - [x] RBAC authorization via `SubjectAccessReview`

---

## Phase 1.5: Embedded TSDB - Technical Hardening
**Objective:** Production-grade TSDB with HA, Backup, Cardinality Control, Self-Observability.

### 1.5.1 HA & Failover
- [x] **Task 1.5.1:** Implement Leader Election with `controller-runtime` (Lease lock)
- [x] **Task 1.5.2:** Configure StatefulSet with 2 replicas for HA
  - [x] Leader: RW PVC, Active Writer
  - [x] Standby: RO PVC (if CSI supports `ReadOnlyMany`) OR emptyDir + WAL gRPC stream
  - [x] Health checks: `/ready` (Leader), `/ready` (Standby - WAL lag < 10s)
- [x] **Task 1.5.3:** Implement WAL Replication (Leader -> Standby gRPC stream)
  - [x] Create gRPC service definition (`pkg/storage/wal_replication.proto`)
  - [x] Implement `WALReplicationServer` on leader (streams WAL segments)
  - [x] Implement `WALReplicationClient` on standby (receives and applies WAL segments)
  - [x] Integrate with main.go leader/standby runnables
  - [x] Add WAL replication port (9091) to StatefulSet and Services
  - [x] Add readiness check for WAL replication lag (< 10 segments)
  - [x] Handle reconnection and resume from last segment
  - [x] Compress WAL segments with zstd for efficient transfer
  - [x] Verify segment integrity with CRC32 checksums
- [x] **Task 1.5.4:** Test Failover: Kill Leader -> Standby promotes < 30s -> WAL Replay -> Queries work
  - [x] Create failover validation script (`scripts/validate-failover.sh`)
  - [x] Test leader election lease transfer
  - [x] Verify standby promotes to leader within 30 seconds
  - [x] Verify WAL replay completes on new leader
  - [x] Verify API queries work on new leader
  - [x] Verify data consistency (no data loss)
  - [x] Verify service endpoints update correctly
  - [x] Document test results in `docs/failover-test-report.md`

### 1.5.2 Backup & Disaster Recovery
- [x] **Task 1.5.5:** Implement `tsdb-snapshotter` sidecar (`cmd/snapshotter/main.go`)
  - [x] Schedule: Every 6 hours (CronJob or in-process ticker)
  - [x] Process: `db.Snapshot(dir)` -> Tar + Zstd -> Upload to S3 (`s3://bucket/tsdb-snapshots/cluster/YYYYMMDD-HHMM.tar.zst`)
  - [x] Cleanup local snapshots > 24h
- [x] **Task 1.5.6:** Document Restore Procedure
  - [x] Download latest snapshot
  - [x] Extract to PVC data dir
  - [x] Start Controller -> TSDB opens blocks -> Replays WAL -> Ready

### 1.5.3 Cardinality Management
- [x] **Task 1.5.7:** Implement Cardinality Guard in Ingester
  - [x] Hard limit: `maxActiveSeries: 500,000` (configurable)
  - [x] Metric: `finops_ingester_rejected_samples_total` (Cardinality Limit)
  - [x] Alert: `tsdb_head_series_total > 0.8 * maxActiveSeries`
- [x] **Task 1.5.8:** Implement Label Value Normalization
  - [x] Truncate long label values (> 100 chars)
  - [x] Drop high-cardinality labels not in allowlist (e.g., `pod_uid`, `container_id`)
  - [x] Sanitize label names to Prometheus format (`[a-zA-Z_][a-zA-Z0-9_]*`)
  - [x] Sanitize label values (remove control characters)
  - [x] Configurable allowlist/denylist for label names
  - [x] Metrics for dropped/truncated labels: `finops_label_normalizer_dropped_total`, `finops_label_normalizer_truncated_total`
  - [x] Required label enrichment (cost_center, team from namespace cache)

### 1.5.4 Self-Observability (TSDB Metrics)
- [x] **Task 1.5.9:** Expose Internal TSDB Metrics on `/metrics`
  - [x] `tsdb_head_series_total`, `tsdb_head_samples_appended_total`, `tsdb_head_bytes`
  - [x] `tsdb_compaction_duration_seconds`, `tsdb_wal_corruptions_total`, `tsdb_block_count`
  - [x] `finops_ingester_batch_size_avg`, `finops_ingester_flush_duration_seconds`
  - [x] `finops_ingester_exemplars_dropped_total`
  - [x] `finops_query_duration_seconds{quantile="0.99"}`, `finops_query_samples_total`, `finops_query_errors_total`

### 1.5.5 Resource Sizing Validation
- [x] **Task 1.5.10:** Validate Resource Usage against Sizing Guidelines
  - [x] Create validation script `scripts/validate-tsdb-sizing.sh` (bash)
  - [x] Create validation tool `scripts/validate-tsdb-sizing.go` (Go)
  - [x] Validate against guidelines:
    - [x] Small (<50 nodes): 50k series, 50GiB PVC, 1 CPU, 4GiB Mem
    - [x] Medium (50-200): 250k series, 200GiB PVC, 2 CPU, 8GiB Mem
    - [x] Large (200-500): 750k series, 500GiB PVC, 4 CPU, 16GiB Mem
    - [x] X-Large (>500): Shard by `cost_center` (separate TSDB per team)
  - [x] Checks: Series count, PVC capacity/usage, CPU/Memory limits/usage, TSDB health metrics
  - [x] Output: JSON report with PASS/FAIL/WARN per check
  - [x] Integration: Can run as standalone or as Kubernetes Job/CronJob

---

## Phase 2: Historical Storage & Query Layer
**Objective:** Long-term trends, YoY comparison, Rightsizing history.

### 2.1 Embedded TSDB (Primary - Already in Phase 1)
- [x] **Task 2.1.1:** Verify TSDB Retention & Compaction working (14d Hot, 90d Warm on PVC)
  - [x] Create validation script `scripts/validate-tsdb-retention-compaction.sh`
  - [x] Verify retention duration is 14 days (336 hours)
  - [x] Verify BlockRanges: [2h, 24h, 7d, 30d] configured
  - [x] Verify WAL flush interval: 30s
  - [x] Verify compaction is running (tsdb_compaction_duration_seconds metric)
  - [x] Verify no WAL corruptions (tsdb_wal_corruptions_total = 0)
  - [x] Verify PVC storage usage within limits
  - [x] Verify query API responds correctly (instant, range, series)
  - [x] Verify snapshotter sidecar/CronJob is running
  - [x] Verify cardinality guard is active (finops_ingester_rejected_samples_total metric)
- [x] **Task 2.1.2:** Validate Query Performance
  - [x] Instant query (1hr range) < 500ms
  - [x] Range query (30d, 1h step) < 2s
  - [x] Series discovery (`/api/v1/series`) < 1s

### 2.2 Query API Hardening (`pkg/api/query.go`)
- [x] **Task 2.2.1:** Implement PromQL Engine (`prometheus/promql`)
  - [x] Support: Vector selectors, `rate()`, `sum()`, `topk()`, `histogram_quantile()`
  - [x] Support: `@` modifier (offset), `[range]` vectors
- [x] **Task 2.2.2:** Add Query Timeout & Cancellation (Context)
  - [x] Configurable timeout via `timeout` query parameter
  - [x] Min/max timeout bounds enforcement
  - [x] Context cancellation on client disconnect (HTTP/1.1 CloseNotifier + HTTP/2 Request.Context)
  - [x] Proper error responses: 504 Gateway Timeout for timeout, silent return for client cancellation
  - [x] Applied to all endpoints: `/query`, `/query_range`, `/series`, `/labels`, `/label/<name>/values`
- [x] **Task 2.2.3:** Implement Query Logging (Slow query > 5s)
  - [x] Added configurable slow query threshold (default 5 seconds)
  - [x] Added slow query logging to all endpoints: `/query`, `/query_range`, `/series`, `/labels`, `/label/<name>/values`
  - [x] Logs include: endpoint, duration, threshold, query (truncated), remote address, user agent
  - [x] Added `SetSlowQueryThreshold`, `GetSlowQueryThreshold`, `SetLogger` methods
  - [x] Added `SlowQueryConfig` and `ApplySlowQueryConfig` for structured configuration  
  - [x] **Task 2.2.4:** Add Response Caching (Redis or In-Memory) for Dashboard Overview

### 2.3 Long-Term Export (Parquet to S3)
- [x] **Task 2.3.1:** Create `cost-exporter` CronJob (`cmd/exporter/main.go`)
  - [x] Schedule: Daily 02:00 UTC
  - [x] Query TSDB for yesterday (1h step) via `/api/v1/query_range`
  - [x] Convert to Arrow/Parquet (Apache Arrow Go)
  - [x] Partition by `date/cost_center`
  - [x] Write to `s3://finops-bucket/cost/allocations/`
  - [x] Created `cmd/exporter/Dockerfile` for container image
  - [x] Created `config/exporter/cronjob.yaml` with CronJob, ServiceAccount, and Secret template
- [x] **Task 2.3.2:** Define Parquet Schema (PyArrow compatible)
  - [x] Fields: `timestamp`, `cluster`, `namespace`, `workload`, `kind`, `cost_center`, `team`, `cpu_cores_requested`, `cpu_cores_used`, `memory_gib_requested`, `memory_gib_used`, `storage_gib_requested`, `storage_gib_used`, `compute_cost_usd`, `storage_cost_usd`, `network_cost_usd`, `shared_cost_usd`, `total_cost_usd`, `efficiency_pct`
  - [x] Schema defined in `cmd/exporter/main.go` as `ParquetSchema` variable
  - [x] Uses Apache Arrow Go types matching PyArrow schema
  - [x] All fields properly typed (timestamp_us, string, float64)
  - [x] Nullable fields: cost_center, team (others required)
- [ ] **Task 2.3.3:** Validate Parquet Read (Athena/Trino/BigQuery external table)

---

## Phase 3: Event & Log Intelligence (Replaces Loki)
**Objective:** Detect OOM, Evictions, Throttling, Crashes without Loki.

### 3.1 Kubernetes Event Controller (`cmd/event-controller`)
- [x] **Task 3.1.1:** Scaffold Controller (`kubebuilder create api --group finops --version v1alpha1 --kind LogEvent`)
- [x] **Task 3.1.2:** Implement Event Watcher (`pkg/controllers/event_controller.go`)
  - [x] Watch `events.k8s.io/v1` (Cluster-scoped)
  - [x] Filter: `Reason IN (OOMKilling, Evicted, FailedScheduling, Killing, FailedMount, CrashLoopBackOff, BackOff)`
- [x] **Task 3.1.3:** Implement Enrichment Pipeline
  - [x] Join `involvedObject` -> Pod -> Owner (Workload) -> Namespace Labels (`cost-center`)
  - [x] Join Node -> `NodeCost` CRD (Node Price)
- [x] **Task 3.1.4:** Output to CRD & TSDB
  - [x] Create/Update `LogEvent` CRD (Immediate visibility)
  - [x] Increment TSDB Counter: `finops_events_total{reason, namespace, workload, cost_center}`

### 3.2 Container Log Tailer (Optional - Fluent Bit DaemonSet)
- [x] **Task 3.2.1:** Deploy Fluent Bit DaemonSet (if DaemonSets allowed)
  - [x] Config: Tail `/var/log/containers/*.log`
  - [x] Parser: Regex for `OOM`, `Error`, `Exception`, `OutOfMemory`, `Killed`
  - [x] Output: HTTP -> `cost-controller:8081/api/v1/logs`
- [x] **Task 3.2.2:** Implement Log Ingestion Endpoint (`pkg/api/logs.go`)
  - [x] Parse structured fields (JSON or Regex)
  - [x] Map to `LogEvent` CRD + TSDB `finops_log_errors_total{namespace, workload, container, level}`

### 3.3 Audit Log Webhook (Advanced)
- [ ] **Task 3.3.1:** Configure Kube-Apiserver Audit Sink (Webhook to `cost-controller:8081/api/v1/audit`)
- [ ] **Task 3.3.2:** Define Audit Policy (Metadata level for: `pods/create`, `pods/delete`, `pvcs/create/delete`)
- [ ] **Task 3.3.3:** Implement Audit Handler
  - [ ] Track "Who created expensive PVC", "Pod churn rate"
  - [ ] Emit `finops_audit_events_total{user, verb, resource, namespace}`

---

## Phase 4: API Gateway & Dashboard (Replaces Grafana)
**Objective:** Visualize Cost, Utilization, Waste, Rightsizing.

### 4.1 API Gateway (Integrated in Cost Controller)
- [ ] **Task 4.1.1:** Ensure all Phase 1.6 endpoints implemented and tested
- [ ] **Task 4.1.2:** Add OpenAPI/Swagger Spec (`docs/api/openapi.yaml`)
- [ ] **Task 1.6.3:** Implement Rate Limiting (Token Bucket per User/SA)
- [ ] **Task 1.6.4:** Add Request/Response Logging (Structured JSON)

### 4.2 React Dashboard (`ui/dashboard/`)
- [ ] **Task 4.2.1:** Scaffold React 18 + TypeScript + Vite + TailwindCSS
  - [ ] `npm create vite@latest ui/dashboard -- --template react-ts`
  - [ ] Install: `recharts`, `@tanstack/react-table`, `axios`, `react-router-dom`, `date-fns`
- [ ] **Task 4.2.2:** Implement Authentication
  - [ ] OAuth2-Proxy sidecar pattern (Ingress level)
  - [ ] OR K8s Impersonation Headers (Pass `Impersonate-User` from Dashboard SA)
- [ ] **Task 4.2.3:** Build Core Layout & Navigation
  - [ ] Sidebar: Executive, Workloads, Nodes, Storage, Network, Waste, Governance
  - [ ] Header: Cluster Selector, Time Range Picker, User Menu
- [ ] **Task 4.2.4:** Page 1 - Executive Dashboard
  - [ ] Total Spend (Current Month, MTD, Projected)
  - [ ] Spend Trend (Last 30 days, `finops_cluster_total_hourly_cost_usd`)
  - [ ] Top 10 Workloads by Cost (Table + Bar Chart)
  - [ ] Cluster Efficiency Gauge (Avg `finops_workload_efficiency_ratio`)
- [ ] **Task 4.2.5:** Page 2 - Workloads Explorer
  - [ ] Searchable/Filterable Table (Namespace, Workload, Kind, Cost, CPU%, Mem%, Efficiency)
  - [ ] Drill-down: Click Workload -> Detail Modal (Cost Trend, Usage Trend, Recommendations)
  - [ ] Column: "Rightsizing Status" (None, Pending, Approved, Applied)
- [ ] **Task 4.2.6:** Page 3 - Node Utilization Heatmap
  - [ ] Grid: Node (Y) vs Hour (X) -> Color = CPU Util% / Mem Util%
  - [ ] Tooltip: Node Cost, Instance Type, Zone, Pods Running
  - [ ] Filter: Spot vs On-Demand, Cost Center
- [ ] **Task 4.2.7:** Page 4 - Storage Analytics
  - [ ] PVC Table: Namespace, PVC, SC, Requested GB, Used GB, Util%, Monthly Cost, Status
  - [ ] Tabs: Orphaned PVCs (Released > 7d), Abandoned Snapshots, Overprovisioned PVCs
- [ ] **Task 4.2.8:** Page 5 - Network Egress
  - [ ] Sankey Diagram: Namespace -> AZ -> Cost (if AZ labels available)
  - [ ] Top Talkers Table: Workload, Egress GB, Cost
- [ ] **Task 4.2.9:** Page 6 - Waste Dashboard
  - [ ] Idle Workloads (CPU < 5% for 7d, Cost > $10/mo)
  - [ ] Orphaned Resources (PVCs, PVs, LBs, IPs)
  - [ ] Overprovisioned Nodes (Allocatable > 80% Requested, Util < 20%)
- [ ] **Task 4.2.10:** Page 7 - Governance
  - [ ] Budget Tracking: Namespace, Budget, Actual, % Used, Status (OK/Warn/Critical)
  - [ ] Recommendation Approval Queue: List `RightsizingRecommendation` CRDs with `status.approved: false`
  - [ ] "Approve" Button -> Patches CRD `status.approved: true` + `status.approvedBy`
- [ ] **Task 4.2.11:** Deploy Dashboard
  - [ ] Build Docker Image (`ui/dashboard/Dockerfile` - Nginx static serve)
  - [ ] Deploy: `Deployment` + `Service` + `Ingress` (TLS) in `observability` ns
  - [ ] Configure Ingress: Auth (OAuth2-Proxy), Rate Limit, CORS

---

## Phase 5: Automation & Optimization Engine
**Objective:** Actionable Rightsizing, Cleanup, Scaling.

### 5.1 Rightsizing Controller (`cmd/rightsizing-controller`)
- [ ] **Task 5.1.1:** Scaffold Controller (Watches `CostAllocation` / Queries TSDB)
- [ ] **Task 5.1.2:** Implement Recommendation Algorithm
  - [ ] `Rec_CPU = max(P95(Usage_1h) * 1.2, Min_Allowed)` (Min: 10m or Namespace default)
  - [ ] `Rec_Mem = max(Max(Usage_24h) * 1.15, Min_Allowed)` (Memory needs headroom)
  - [ ] `Savings = (Current_Req - Rec) * Node_Unit_Price * 730`
  - [ ] Window: 30 days (Configurable per Namespace Label `finops/rightsizing-window`)
- [ ] **Task 5.1.3:** Create `RightsizingRecommendation` CRD Instances
  - [ ] Fields: `targetRef` (Workload), `currentRequests`, `recommendedRequests`, `estimatedMonthlySavingsUSD`, `confidence` (High/Med/Low), `status` (Pending/Approved/Applied/Rejected)
- [ ] **Task 5.1.4:** Implement Policy Enforcement
  - [ ] Namespace Label `finops/rightsizing-mode: Auto | RecommendationOnly | Off`
  - [ ] If `Auto`: Auto-approve after 24h (Configurable)

### 5.2 GitOps Integration (ArgoCD / Flux)
- [ ] **Task 5.2.1:** Design GitOps Workflow
  - [ ] Option A: ArgoCD ApplicationSet Generator (CRD -> Application -> Kustomize Patch)
  - [ ] Option B: Controller creates GitHub/GitLab PR via API (Complex, more flexible)
- [ ] **Task 5.2.2:** Implement Option A (ArgoCD ApplicationSet)
  - [ ] Create `ApplicationSet` manifest generator for `RightsizingRecommendation`
  - [ ] Template: Patch `Deployment` resources `spec.template.spec.containers[].resources.requests`
  - [ ] ArgoCD Sync Policy: Automated + Prune + Self-Heal
- [ ] **Task 5.2.3:** Implement "Apply" Button in Dashboard
  - [ ] PATCH `RightsizingRecommendation` `status.approved: true`
  - [ ] ArgoCD detects change -> Generates Application -> Syncs to Cluster

### 5.3 Cleanup Controllers (CronJobs / Controllers)
- [ ] **Task 5.3.1:** `pvc-reaper` CronJob (Daily 03:00)
  - [ ] Delete `Released` PVs > 7d
  - [ ] Delete `Pending` PVCs > 7d (no PV bound)
  - [ ] Dry-run mode first (Log only)
- [ ] **Task 5.3.2:** `image-gc` Configuration (Kubelet)
  - [ ] Set `image-gc-high-threshold=80`, `image-gc-low-threshold=70` via KubeletConfig
- [ ] **Task 5.3.3:** `completed-job-gc` CronJob (Hourly)
  - [ ] TTL: `Completed` 24h, `Failed` 1h (`.spec.ttlSecondsAfterFinished`)
- [ ] **Task 5.3.4:** `idle-ns-detector` CronJob (Daily)
  - [ ] Find Namespaces: `cost > $0` AND `Pods == 0` for 30d
  - [ ] Create `LogEvent` CRD (Type: `IdleNamespace`) + Alert

### 5.4 Cluster Autoscaler / Karpenter Integration
- [ ] **Task 5.4.1:** Ensure Cost Controller Labels Nodes
  - [ ] `finops/cost-center`, `finops/instance-type`, `finops/zone`
- [ ] **Task 5.4.2:** Karpenter NodePool Optimization
  - [ ] Controller reads `NodeCost` utilization trends
  - [ ] Update `NodePool` `limits.cpu` based on projected demand
  - [ ] Consolidation: Trigger NodePool `disruption.consolidationPolicy: WhenEmptyOrUnderutilized`

---

## Phase 6: Alerting & Governance (Native)
**Objective:** Real-time alerts, Hard Budget Enforcement.

### 6.1 Alerting Controller (`cmd/alert-controller`)
- [ ] **Task 6.1.1:** Scaffold Controller (Deployment, not StatefulSet)
- [ ] **Task 6.1.2:** Implement ConfigMap-based Rule Loader (`config/alerting/rules.yaml`)
  - [ ] Hot Reload on ConfigMap change (Inotify/fsnotify)
  - [ ] Rule Format:
    ```yaml
    - name: WorkloadLowCPUUtilization
      type: Threshold
      query: "finops_workload_efficiency_ratio{namespace=~\"prod.*\"} < 0.1"
      window: "7d"
      severity: Warning
      annotations:
        runbook: "https://wiki/rightsizing"
    - name: NamespaceCostAnomaly
      type: Anomaly
      query: "finops_namespace_daily_cost_usd"
      baseline: "30d"
      threshold: 1.5
      severity: Critical
    ```
- [ ] **Task 6.1.3:** Implement Evaluation Loop (Every 60s)
  - [ ] Query TSDB `/api/v1/query` for each rule
  - [ ] Track State: `Firing`/`Resolved` in Memory + `AlertState` CRD
  - [ ] Deduplication: Group by Labels (Namespace, Workload)
- [ ] **Task 6.1.4:** Implement Notifiers
  - [ ] Slack (Webhook + Blocks formatting)
  - [ ] PagerDuty (Events API v2)
  - [ ] Email (SMTP + Template)
  - [ ] Generic Webhook (JSON Payload)
- [ ] **Task 6.1.5:** Implement Alert Silencing/Inhibition (Match Alertmanager behavior)

### 6.2 Budget Admission Webhook (`cmd/budget-webhook`)
- [ ] **Task 6.2.1:** Scaffold ValidatingWebhookConfiguration
  - [ ] `failurePolicy: Fail` (Hard Block)
  - [ ] `rules`: `CREATE`, `UPDATE` on `pods`, `deployments`, `statefulsets`, `daemonsets`, `jobs`, `cronjobs`
  - [ ] `namespaceSelector`: Match namespaces with `NamespaceBudget` CRD
- [ ] **Task 6.2.2:** Implement Admission Logic (`pkg/webhook/budget.go`)
  - [ ] Calculate `Additional_Cost = Sum(New_Pod_Requests) * Node_Unit_Price * 730`
  - [ ] Fetch `NamespaceBudget` CRD for Namespace
  - [ ] Fetch Current `Namespace_Commitment` (Sum of all Pod Requests * Price from `CostAllocation`)
  - [ ] Decision:
    - [ ] `Hard` Block: `Commitment + Additional > Budget.Limit` -> Deny (Response: "Budget Exceeded: $X/$Y")
    - [ ] `Soft` Warn: Allow + Annotate Pod `finops/budget-warning: "true"` + Fire Alert
- [ ] **Task 6.2.3:** (Optional) MutatingWebhook for Default Requests
  - [ ] If Pod has no requests/limits -> Inject from `RightsizingRecommendation` CRD (if exists)
  - [ ] Or inject Namespace Default (from `LimitRange`)

---

## Phase 7: FinOps Reporting & Showback
**Objective:** Finance-grade data export.

### 7.1 Daily Snapshot Job (`cmd/exporter`)
- [ ] **Task 7.1.1:** Implement Parquet Exporter (Daily CronJob 02:00 UTC)
  - [ ] Query TSDB for yesterday (1h granularity)
  - [ ] Enrich with Namespace Labels (`cost-center`, `team`)
  - [ ] Write Parquet to S3 (Partitioned: `year=2024/month=01/day=15/cost_center=engineering/`)
- [ ] **Task 7.1.2:** Validate Schema Compliance (PyArrow)
- [ ] **Task 7.1.3:** Register Table in Data Catalog (AWS Glue / GCP Data Catalog / Azure Purview)

### 7.2 Showback API (`/api/v1/showback`)
- [ ] **Task 7.2.1:** Implement Query Endpoint
  - [ ] Filters: `cost_center`, `namespace`, `workload`, `start`, `end`, `granularity` (hourly/daily/monthly)
  - [ ] Formats: JSON, CSV, Parquet (Streaming)
  - [ ] Aggregation: Sum, Avg, Max by Group By labels
- [ ] **Task 7.2.2:** Add Authentication & Authorization (RBAC on `cost_center` label)

### 7.3 Automated Monthly Report
- [ ] **Task 7.3.1:** Create Report Generator (`cmd/report-generator/main.go`)
  - [ ] Schedule: 1st of month 06:00 UTC
  - [ ] Template: Go `text/template` -> HTML -> PDF (Chromium Headless / `go-wkhtmltopdf`)
- [ ] **Task 7.3.2:** Define Report Sections
  - [ ] 1. Bill vs Showback Variance (< 5% Target)
  - [ ] 2. Top 5 Savings Opportunities (Rightsizing, Idle, Storage)
  - [ ] 3. Actions Taken (Merged Recommendations count, $ saved)
  - [ ] 4. Waste Eliminated (GB/CPU freed)
  - [ ] 5. Forecast (Linear Regression on TSDB `finops_cluster_total_hourly_cost_usd`)
- [ ] **Task 7.3.3:** Delivery: Email (HTML + PDF Attachment) + Slack Summary + S3 Archive

---

## Cross-Cutting Concerns

### Testing
- [ ] **Task T.1:** Unit Tests for Allocation Engine (`pkg/allocation/engine_test.go`)
  - [ ] Test Node Cost Calculation (On-Demand, Spot, GPU)
  - [ ] Test Shared Cost Distribution
  - [ ] Test PV Cost Calculation
  - [ ] Test Network Allocation
- [ ] **Task T.2:** Unit Tests for TSDB Ingester (`pkg/storage/ingester_test.go`)
  - [ ] Batch Flush Logic
  - [ ] Label Enrichment
  - [ ] Cardinality Rejection
  - [ ] Exemplar Attachment
  - [ ] Label Normalization (allowlist, denylist, truncation, sanitization)
- [ ] **Task T.3:** Integration Tests (Kind Cluster)
  - [ ] Deploy Cost Controller + KSM + Metrics Server + Node Exporter
  - [ ] Create Test Pods with Requests/Limits
  - [ ] Verify `CostAllocation` CRD Created with Correct Cost
  - [ ] Verify TSDB Metrics Exposed
  - [ ] Verify API `/api/v1/query` Returns Data
- [ ] **Task T.4:** E2E Tests (Staging Cluster)
  - [ ] Full Flow: Pod Create -> Cost Allocation -> Dashboard -> Recommendation -> GitOps Apply
  - [ ] Budget Webhook: Create Pod Over Budget -> Denied
  - [ ] Alerting: Simulate Low Util -> Alert Fires -> Slack Received

### Documentation & Runbooks
- [ ] **Task D.1:** Architecture Decision Records (ADRs) in `docs/adr/`
  - [ ] ADR-001: Why Embedded TSDB vs Thanos/VictoriaMetrics
  - [ ] ADR-002: Cost Allocation Algorithm Choice (Weighted Share vs Proportional)
  - [ ] ADR-003: HA Strategy (Single Writer + WAL vs Multi-Writer)
  - [ ] ADR-004: Log Handling (Structured Metrics vs Full Text)
- [ ] **Task D.2:** Runbooks in `docs/runbooks/`
  - [ ] `cost-controller-debugging.md` (High Cardinality, OOM, Slow Queries)
  - [ ] `tsdb-recovery.md` (WAL Corruption, Snapshot Restore)
  - [ ] `budget-webhook-troubleshooting.md` (False Positives, Performance)
  - [ ] `rightsizing-rollback.md` (Bad Recommendation Applied)
- [ ] **Task D.3:** API Documentation (OpenAPI Spec + Examples)
- [ ] **Task D.4:** Dashboard User Guide (FinOps Team, Engineering Teams)

### Security Hardening
- [ ] **Task S.1:** NetworkPolicies for All Components
  - [ ] Cost Controller: Ingress from Dashboard, Prometheus (scrape), KSM, Node Exporter, K8s API
  - [ ] Cost Controller: Egress to Cloud APIs, S3, K8s API, Slack/PagerDuty
  - [ ] Dashboard: Ingress from Ingress Controller, Egress to Cost Controller API
  - [ ] Event Controller: Egress to K8s API, Cost Controller API
- [ ] **Task S.2:** RBAC Least Privilege
  - [ ] Cost Controller SA: `get/list/watch` Pods, Nodes, PVCs, PVs, Namespaces, Events, Nodes/Metrics
  - [ ] Dashboard SA: `get` CostAllocation/NodeCost/PVCost CRDs, `create` RightsizingRecommendation Patch
  - [ ] Budget Webhook SA: `get` NamespaceBudget, `get/list` CostAllocation
- [ ] **Task S.3:** Secrets Management
  - [ ] Cloud Credentials: IRSA/Workload Identity (No Secrets in Cluster)
  - [ ] S3 Backup Credentials: IRSA + Bucket Policy (Write Only)
  - [ ] Alerting Webhooks: SealedSecrets / External Secrets Operator
- [ ] **Task S.4:** Image Security
  - [ ] Base Image: `gcr.io/distroless/static:nonroot` or `alpine:3.19`
  - [ ] Scan: `trivy` / `grype` in CI Pipeline
  - [ ] Sign: `cosign` / `notation`
  - [ ] Verify: `admission-webhook` for Signed Images

### CI/CD Pipeline (`.github/workflows/`)
- [ ] **Task C.1:** `ci.yaml` - Go Build, Test, Vet, Lint (`golangci-lint`), Security Scan (`gosec`)
- [ ] **Task C.2:** `docker.yaml` - Multi-arch Build (amd64/arm64), Push to GHCR, Sign
- [ ] **Task C.3:** `deploy-staging.yaml` - Deploy to Staging on Merge to Main
- [ ] **Task C.4:** `deploy-prod.yaml` - Manual Approval -> Deploy to Prod (ArgoCD App of Apps)
- [ ] **Task C.5:** `validate-phase0.yaml` - Run Phase 0 Validation on Schedule
- [ ] **Task C.6:** `validate-phase1.yaml` - Run Phase 1 Validation on Staging Deploy

---

## Milestone Tracking

| Milestone | Target Date | Status | Key Deliverables |
| :--- | :--- | :--- | :--- |
| **M0: Foundation Ready** | Week 1 | ✅ | Phase 0 Validated, CRDs Installed, Cloud Auth Working |
| **M1: Core Engine** | Week 3 | ✅ | Cost Controller Running, TSDB Ingesting, CRDs Updated, API Queryable |
| **M1.5: TSDB Hardened** | Week 4 | ✅ | HA Failover Tested, Backup/Restore Verified, Cardinality Guards Active |
| **M2: Historical Data** | Week 5 | ✅ | Parquet Export Working, Query API Performant, Query Performance Validated |
| **M3: Observability** | Week 5 | ⬜ | Event Controller Detecting OOM/Evictions, Log Tailer Optional |
| **M4: Dashboard MVP** | Week 7 | ⬜ | 7 Pages Functional, Auth Working, Real-time Data |
| **M5: Automation** | Week 9 | ⬜ | Rightsizing Recs Generated, GitOps Loop Working, Cleanup Jobs Running |
| **M6: Governance** | Week 10 | ⬜ | Alerting Firing, Budget Webhook Blocking, Reports Generating |
| **M7: Production Ready** | Week 11 | ⬜ | All KPIs Met, Security Hardened, Docs Complete, Runbooks Tested |

---

## Quick Reference: Key Commands

```bash
# Phase 0 Validation
make validate-phase0

# Build & Deploy Cost Controller (Phase 1)
make generate manifests
make docker-build IMG=ghcr.io/yourorg/kube-native-finops/cost-controller:v0.1.0
make deploy IMG=ghcr.io/yourorg/kube-native-finops/cost-controller:v0.1.0

# Validate Phase 1
./scripts/validate-native-phase1.sh

# Build & Deploy Dashboard (Phase 4)
cd ui/dashboard && npm run build
docker build -t ghcr.io/yourorg/kube-native-finops/dashboard:v0.1.0 .
docker push ghcr.io/yourorg/kube-native-finops/dashboard:v0.1.0
kubectl apply -k config/dashboard

# Local Development
make run-controller  # Run controller locally against cluster
make test            # Run unit tests

# TSDB Snapshot (Manual)
kubectl exec -n cost-management cost-controller-0 -c snapshotter -- /snapshotter

# Check TSDB Health
kubectl exec -n cost-management cost-controller-0 -- wget -qO- localhost:8080/metrics | grep tsdb_

# Failover Test
chmod +x scripts/validate-failover.sh
./scripts/validate-failover.sh

# TSDB Sizing Validation (Task 1.5.10)
chmod +x scripts/validate-tsdb-sizing.sh
./scripts/validate-tsdb-sizing.sh

# Or use Go version
go run scripts/validate-tsdb-sizing.go -namespace cost-management -pod cost-controller-0

# Phase 2 Validation (Task 2.1.1)
chmod +x scripts/validate-tsdb-retention-compaction.sh
./scripts/validate-tsdb-retention-compaction.sh

# Query Performance Validation (Task 2.1.2)
chmod +x scripts/validate-query-performance.sh
./scripts/validate-query-performance.sh

# Or use Go version
go run scripts/validate-query-performance.go -namespace cost-management -pod cost-controller-0

# PromQL Engine Validation (Task 2.2.1)
go run scripts/validate-promql-engine.go -namespace cost-management -pod cost-controller-0

# Full Phase 2 Validation
chmod +x scripts/validate-native-phase2.sh
./scripts/validate-native-phase2.sh
```

---

## Definition of Done (Per Task)
- [ ] Code Written & Formatted (`go fmt` / `prettier`)
- [ ] Unit Tests Pass (`go test ./... -cover`)
- [ ] Integration Tests Pass (Kind Cluster)
- [ ] Documentation Updated (Godoc, README, Runbooks)
- [ ] Security Reviewed (RBAC, NetworkPolicy, Secrets)
- [ ] Deployed to Staging & Validated
- [ ] Peer Reviewed (PR Approved)
