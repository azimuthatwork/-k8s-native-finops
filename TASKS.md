# Phase 0: Prerequisites & Baseline Validation - Implementation Tasks

## Overview
**Objective:** Verify existing infrastructure readiness before deploying cost-specific components.  
**Estimated Effort:** 1 Day  
**Dependencies:** None (Starting Phase)

---

## 0.1 Validate Existing Components

### 0.1.1 Namespace Strategy Validation (Ref: 1.1 Namespace Strategy)
- [x] **Task 0.1.1.1:** Verify `monitoring` namespace exists with labels:
  - `cost-center=platform`
  - `environment=production`
  - `team=platform-engineering`
- [x] **Task 0.1.1.2:** Verify `observability` namespace exists with labels:
  - `cost-center=platform`
  - `environment=production`
  - `team=platform-engineering`
- [x] **Task 0.1.1.3:** Verify `cost-management` namespace exists with labels:
  - `cost-center=finops`
  - `environment=production`
  - `team=finops`
- [x] **Task 0.1.1.4:** Validate all application namespaces have required labels (`cost-center`, `environment`, `team`)
- [x] **Task 0.1.1.5:** Document namespace label inventory in `docs/namespace-inventory.md`

### 0.1.2 Network Policies Validation (Ref: 1.3 Network Policies)
- [x] **Task 0.1.2.1:** Verify egress from `monitoring` namespace to object storage (S3/GCS/Azure) for Thanos/Loki
- [x] **Task 0.1.2.2:** Verify inter-namespace scraping policies:
  - Alloy DaemonSet (`monitoring`) → Prometheus (`monitoring`:9090)
  - Prometheus (`monitoring`) → kube-state-metrics (`monitoring`:8080)
  - Prometheus (`monitoring`) → cAdvisor (DaemonSet, port 4194)
  - Alloy Central (`monitoring`) → OpenCost (`cost-management`:9003)
- [x] **Task 0.1.2.3:** Verify Thanos components connectivity:
  - Sidecar (`monitoring`:10901) ↔ Store/Query (`monitoring`)
  - Query Frontend (`monitoring`:10902) ↔ Grafana (`observability`)
- [x] **Task 0.1.2.4:** Verify Loki components connectivity:
  - Distributor (`monitoring`:3100) ← Alloy DaemonSet/Central
  - Query (`monitoring`:3100) → Grafana (`observability`)
- [x] **Task 0.1.2.5:** Run connectivity test script: `scripts/validate-network-policies.sh`

### 0.1.3 Alloy DaemonSet Validation (Ref: 2.1 Alloy DaemonSet)
- [x] **Task 0.1.3.1:** Confirm `node-exporter` integration module is active and scraping:
  - `node_cpu_seconds_total`
  - `node_memory_*`
  - `node_filesystem_*`
  - `node_network_*` (critical for network cost)
- [x] **Task 0.1.3.2:** Confirm `cadvisor` integration module is active and scraping:
  - `container_spec_memory_limit_bytes`
  - `container_spec_cpu_quota/period`
  - `container_memory_working_set_bytes`
  - `container_cpu_usage_seconds_total`
  - `container_fs_usage_bytes`
  - `container_fs_limit_bytes`
- [x] **Task 0.1.3.3:** Verify `pod_logs` collection includes labels:
  - `namespace`
  - `pod`
  - `container`
  - `cost_center` (via relabeling)
  - `team` (via relabeling)
- [x] **Task 0.1.3.4:** Validate Alloy DaemonSet health: `kubectl get pods -n monitoring -l app=alloy-daemonset`
- [x] **Task 0.1.3.5:** Check Alloy DaemonSet logs for errors: `kubectl logs -n monitoring -l app=alloy-daemonset --tail=100`

### 0.1.4 Alloy Central Validation (Ref: 2.2 Alloy Central Deployment)
- [x] **Task 0.1.4.1:** Verify `prometheus.remote_write` endpoints point to Prometheus HA pair:
  - `prometheus-operated-0.monitoring:9090`
  - `prometheus-operated-1.monitoring:9090`
- [x] **Task 0.1.4.2:** Verify `loki.write` endpoint configured to Loki Distributor/Gateway
- [x] **Task 0.1.4.3:** Confirm Alloy Central Deployment health: `kubectl get deployment -n monitoring alloy-central`
- [x] **Task 0.1.4.4:** Validate remote_write throughput (no 429/5xx errors in logs)

### 0.1.5 Prometheus/Thanos Validation (Ref: 3.1 Prometheus Operator with Thanos Sidecar)
- [x] **Task 0.1.5.1:** Confirm Thanos Sidecar `objstore.yml` secret exists and mounted:
  - `kubectl get secret -n monitoring thanos-objstore-config`
- [x] **Task 0.1.5.2:** Verify Thanos components deployed:
  - `thanos-query` (Deployment)
  - `thanos-store` (StatefulSet)
  - `thanos-compact` (Deployment)
- [x] **Task 0.1.5.3:** Confirm retention settings:
  - Prometheus local: `2h` (`--storage.tsdb.retention.time=2h`)
  - Object storage: `90d` (Compactor config)
- [x] **Task 0.1.5.4:** Validate Thanos Query UI accessible: `kubectl port-forward -n monitoring svc/thanos-query 10902:10902`
- [x] **Task 0.1.5.5:** Verify Prometheus HA pair running: `kubectl get prometheus -n monitoring`

---

## 0.2 Required CRDs & Operators Validation

### 0.2.1 Prometheus Operator CRDs
- [x] **Task 0.2.1.1:** Verify `ServiceMonitor` CRD installed: `kubectl get crd servicemonitors.monitoring.coreos.com`
- [x] **Task 0.2.1.2:** Verify `PodMonitor` CRD installed: `kubectl get crd podmonitors.monitoring.coreos.com`
- [x] **Task 0.2.1.3:** Verify `Probe` CRD installed: `kubectl get crd probes.monitoring.coreos.com`
- [x] **Task 0.2.1.4:** Verify `PrometheusRule` CRD installed: `kubectl get crd prometheusrules.monitoring.coreos.com`
- [x] **Task 0.2.1.5:** Verify Prometheus Operator version ≥ v0.75: `kubectl get deployment -n monitoring prometheus-operator -o jsonpath='{.spec.template.spec.containers[0].image}'`

### 0.2.2 Grafana Operator/Resources
- [x] **Task 0.2.2.1:** Verify `GrafanaDashboard` CRD installed (if using Grafana Operator): `kubectl get crd grafanadashboards.grafana.integreatly.org`
- [x] **Task 0.2.2.2:** Verify `GrafanaDataSource` CRD installed: `kubectl get crd grafanadatasources.grafana.integreatly.org`
- [x] **Task 0.2.2.3:** Alternative: Verify Grafana Helm deployment has dashboard provisioning enabled
- [x] **Task 0.2.2.4:** Verify Grafana can connect to Thanos Query and Loki

### 0.2.3 Cost Management Integrations
- [x] **Task 0.2.3.1:** Verify OpenCost/Kubecost metrics adapter deployed (or plan deployment in Phase 1.3)
- [x] **Task 0.2.3.2:** Verify custom metrics API service registered: `kubectl get apiservice v1beta1.custom.metrics.k8s.io`

### 0.2.4 Vertical Pod Autoscaler (VPA) CRDs
- [x] **Task 0.2.4.1:** Verify `VerticalPodAutoscaler` CRD installed: `kubectl get crd verticalpodautoscalers.autoscaling.k8s.io`
- [x] **Task 0.2.4.2:** Verify VPA Recommender/Updater/AdmissionController deployed (or plan in Phase 5.1)
- [x] **Task 0.2.4.3:** Verify `PodDisruptionBudget` support for VPA eviction tolerance

---

## 0.3 Baseline Documentation & Sign-off

### 0.3.1 Documentation
- [x] **Task 0.3.1.1:** Create baseline validation report: `docs/phase0-validation-report.md`
- [x] **Task 0.3.1.2:** Document any gaps found during validation with remediation plans
- [x] **Task 0.3.1.3:** Update architecture diagrams with validated component versions

### 0.3.2 Sign-off
- [x] **Task 0.3.2.1:** Platform Engineering lead review and approval
- [x] **Task 0.3.2.2:** FinOps team confirmation of namespace labeling strategy
- [x] **Task 0.3.2.3:** Security team confirmation of network policy compliance
- [x] **Task 0.3.2.4:** Formal Phase 0 completion gate → Proceed to Phase 1

---

## Implementation Files Created (Phase 0)

### Validation Scripts
- `scripts/validate-phase0.sh` - Main orchestration script for all Phase 0 validations
- `scripts/validate-network-policies.sh` - Network connectivity tests
- `scripts/check-alloy-config.sh` - Alloy DaemonSet & Central configuration validation
- `scripts/check-prometheus-thanos.sh` - Prometheus/Thanos component validation
- `scripts/check-crds-operators.sh` - CRDs and operators validation

### Documentation Templates
- `docs/phase0-validation-report-template.md` - Report template with all check categories
- `docs/namespace-inventory.md` - Namespace label inventory (auto-generated)

### Configuration References
- `config/alloy/phase0-validation-rules.alloy` - Expected Alloy configuration patterns
- `config/prometheus/phase0-prometheus-rules.yaml` - PrometheusRules for ongoing validation
- `config/network-policies/phase0-validation-policies.yaml` - Required NetworkPolicies

### Automation
- `Makefile` - Convenience targets for running validations
- `.github/workflows/phase0-validation.yml` - CI/CD pipeline for automated validation
- `README-PHASE0.md` - Documentation for Phase 0 execution

---

## Validation Scripts Reference (Phase 0)

### `scripts/validate-phase0.sh` (Main Orchestrator)
```bash
# Run all validations
./scripts/validate-phase0.sh

# Run with auto-fix attempts (safe operations only)
./scripts/validate-phase0.sh --fix

# Generate report only (no new checks)
./scripts/validate-phase0.sh --report-only

# Target specific namespace
./scripts/validate-phase0.sh --namespace=monitoring
```

### `scripts/validate-network-policies.sh`
```bash
# Run network policy validation
./scripts/validate-network-policies.sh

# Verbose output
./scripts/validate-network-policies.sh --verbose

# Target specific namespace
./scripts/validate-network-policies.sh --namespace=monitoring
```

### `scripts/check-alloy-config.sh`
```bash
# Validate Alloy configurations
./scripts/check-alloy-config.sh
```

### `scripts/check-prometheus-thanos.sh`
```bash
# Validate Prometheus/Thanos
./scripts/check-prometheus-thanos.sh
```

### `scripts/check-crds-operators.sh`
```bash
# Validate CRDs and operators
./scripts/check-crds-operators.sh
```

---

## Success Criteria (Phase 0 KPIs)
- [x] **100%** Required namespaces exist with correct labels
- [x] **100%** Critical network paths validated (zero FAIL in validation script)
- [x] **100%** Alloy DaemonSet/Central healthy and scraping expected metrics
- [x] **100%** Prometheus HA + Thanos components operational
- [x] **100%** Required CRDs installed (Prometheus Operator v0.75+, Grafana, VPA)
- [x] **0** Blocking gaps remaining (all gaps have documented remediation + ETA)
- [x] **Signed** Phase 0 completion by Platform, FinOps, Security leads

---

## Next Phase Trigger
**Phase 1 can begin when:**
1. All tasks in 0.1, 0.2, 0.3 are ✅ complete
2. Validation report approved by stakeholders
3. Any critical gaps have approved workaround or fix deployed

---

# Phase 1: Cost & Resource Metrics Enrichment - Implementation Tasks

## Overview
**Objective:** Ingest granular cost, usage, and capacity metrics into the TSDB.  
**Estimated Effort:** 3 Days  
**Dependencies:** Phase 0 Complete (Validated Baseline)

---

## 1.1 Node/Cluster Cost Attribution (Alloy DaemonSet - Ref: 2.1)

### 1.1.1 Cloud Provider Node Cost Integration
- [ ] **Task 1.1.1.1:** Configure cloud provider service discovery in Alloy DaemonSet `config.alloy`:
  - AWS: `discovery.ec2` with appropriate IAM roles/filters
  - GCP: `discovery.gce` with service account permissions
  - Azure: `discovery.azure` with managed identity
  - DigitalOcean: `discovery.digitalocean` (as per plan example)
- [ ] **Task 1.1.1.2:** Implement relabeling rules to map cloud instance metadata to Prometheus labels:
  - `instance_id` (from cloud provider instance ID)
  - `instance_type` (for pricing lookup)
  - `availability_zone` (for zone-aware cost allocation)
  - `region` (for regional pricing)
  - `provider_id` (Kubernetes node spec.providerID format)
- [ ] **Task 1.1.1.3:** Create pricing metadata lookup mechanism:
  - Option A: External pricing API integration (AWS Pricing API, GCP Billing Catalog, Azure Retail Prices)
  - Option B: Static pricing ConfigMap updated via CI/CD (simpler, less dynamic)
  - Option C: OpenCost cloud provider integration (recommended - see 1.3)
- [ ] **Task 1.1.1.4:** Validate node cost metrics appear in Prometheus:
  - `node_hourly_cost` (gauge)
  - `node_cpu_hourly_cost` (gauge)
  - `node_memory_hourly_cost` (gauge)
  - `node_gpu_hourly_cost` (gauge, if applicable)

### 1.1.2 Enhanced cAdvisor Metrics Collection
- [ ] **Task 1.1.2.1:** Verify Alloy DaemonSet `prometheus.exporter.cadvisor` configuration includes:
  - `container_spec_memory_limit_bytes`
  - `container_spec_cpu_quota` / `container_spec_cpu_period`
  - `container_memory_working_set_bytes`
  - `container_cpu_usage_seconds_total`
  - `container_fs_usage_bytes`
  - `container_fs_limit_bytes`
  - `container_memory_max_usage_bytes` (for peak detection)
  - `container_cpu_cfs_throttled_periods_total` / `container_cpu_cfs_periods_total` (throttling)
- [ ] **Task 1.1.2.2:** Configure cAdvisor scrape interval to 30s (balance granularity vs cardinality)
- [ ] **Task 1.1.2.3:** Add relabeling to enrich cAdvisor metrics with:
  - `namespace` (from pod metadata)
  - `pod` (from pod metadata)
  - `container` (from container name)
  - `workload_name` + `workload_kind` (from pod ownerReferences via kube-state-metrics join)
  - `cost_center` + `team` (from namespace labels)
- [ ] **Task 1.1.2.4:** Validate metric cardinality impact (target < 500k active series from cAdvisor)
- [ ] **Task 1.1.2.5:** Test cAdvisor metrics availability in Thanos Query:
  ```promql
  # Container CPU usage rate
  rate(container_cpu_usage_seconds_total{container!="",container!="POD"}[5m])
  # Container memory working set
  container_memory_working_set_bytes
  # Container CPU throttling ratio
  rate(container_cpu_cfs_throttled_periods_total[5m]) / rate(container_cpu_cfs_periods_total[5m])
  ```

### 1.1.3 Node Exporter Network Metrics (for Network Cost)
- [ ] **Task 1.1.3.1:** Ensure `node-exporter` collects `node_network_*` metrics:
  - `node_network_receive_bytes_total`
  - `node_network_transmit_bytes_total`
  - `node_network_receive_drop_total`
  - `node_network_transmit_drop_total`
- [ ] **Task 1.1.3.2:** Add topology labels via relabeling:
  - `source_zone` / `dest_zone` (requires CNI integration or cloud metadata)
  - `interface` (filter to primary interfaces: eth0, ens5, etc.)
- [ ] **Task 1.1.3.3:** Validate network metrics flow to Thanos for cross-AZ traffic analysis

---

## 1.2 Kubernetes State Metrics for Cost (Alloy Central - Ref: 2.2)

### 1.2.1 Kube-State-Metrics Scrape Configuration
- [ ] **Task 1.2.1.1:** Configure Alloy Central `prometheus.scrape` for kube-state-metrics:
  - Target: `kube-state-metrics.monitoring.svc.cluster.local:8080` (or appropriate service)
  - Scrape interval: 30s
  - Metric relabeling to reduce cardinality (drop unused labels)
- [ ] **Task 1.2.1.2:** Verify critical metrics are scraped and available:
  - `kube_pod_container_resource_requests{resource="cpu|memory"}` (requests)
  - `kube_pod_container_resource_limits{resource="cpu|memory"}` (limits)
  - `kube_node_status_capacity{resource="cpu|memory|pods"}` (node capacity)
  - `kube_node_status_allocatable{resource="cpu|memory|pods"}` (node allocatable)
  - `kube_pod_owner` (pod -> workload mapping: Deployment, StatefulSet, DaemonSet, Job, CronJob)
  - `kube_namespace_labels` (namespace labels: cost_center, team, environment)
  - `kube_pod_labels` (pod labels for workload identification)
  - `kube_pod_status_phase` (pod phase: Running, Pending, Failed, Succeeded)
  - `kube_pod_container_status_running` (container running state)
  - `kube_resourcequota` (namespace quotas for capacity planning)
  - `kube_limitrange` (namespace default limits)

### 1.2.2 KSM Metric Enrichment & Recording Rules
- [ ] **Task 1.2.2.1:** Create PrometheusRules for pre-computed cost allocation ratios (deployed via PrometheusOperator):
  - `container_request_cpu_cores` = `kube_pod_container_resource_requests{resource="cpu"}`
  - `container_request_memory_bytes` = `kube_pod_container_resource_requests{resource="memory"}`
  - `container_limit_cpu_cores` = `kube_pod_container_resource_limits{resource="cpu"}`
  - `container_limit_memory_bytes` = `kube_pod_container_resource_limits{resource="memory"}`
  - `node_allocatable_cpu_cores` = `kube_node_status_allocatable{resource="cpu"}`
  - `node_allocatable_memory_bytes` = `kube_node_status_allocatable{resource="memory"}`
- [ ] **Task 1.2.2.2:** Create recording rules for workload-level aggregations:
  - `workload_request_cpu_cores` = `sum by (namespace, workload, workload_kind) (container_request_cpu_cores)`
  - `workload_request_memory_bytes` = `sum by (namespace, workload, workload_kind) (container_request_memory_bytes)`
  - `workload_usage_cpu_cores` = `sum by (namespace, workload) (rate(container_cpu_usage_seconds_total[5m]))`
  - `workload_usage_memory_bytes` = `sum by (namespace, workload) (container_memory_working_set_bytes)`
- [ ] **Task 1.2.2.3:** Validate recording rules evaluate correctly in Prometheus/Thanos

---

## 1.3 OpenCost / Kubecost Integration (Deployment in cost-management)

### 1.3.1 OpenCost Helm Deployment
- [ ] **Task 1.3.1.1:** Deploy OpenCost Helm chart to `cost-management` namespace:
  ```bash
  helm repo add opencost https://opencost.github.io/opencost-helm-chart
  helm install opencost opencost/opencost \
    --namespace cost-management \
    --set prometheus.fqdn=http://prometheus-operated.monitoring:9090 \
    --set cloudProvider=<AWS|GCP|AZURE|CUSTOM> \
    --set aws.accessKeyId=<...>,aws.secretAccessKey=<...> \  # If AWS
    --set gcp.projectID=<...> \                              # If GCP
    --set azure.clientId=<...>,azure.clientSecret=<...> \    # If Azure
    --set opencost.exporter.port=9003 \
    --set opencost.exporter.serviceMonitor.enabled=true
  ```
- [ ] **Task 1.3.1.2:** Configure OpenCost for custom pricing (if not using major cloud):
  - Create `custom-pricing.yaml` ConfigMap with per-instance-type pricing
  - Mount to OpenCost pod and configure `--custom-pricing-file`
- [ ] **Task 1.3.1.3:** Verify OpenCost pods are healthy:
  - `kubectl get pods -n cost-management -l app=opencost`
  - Check logs for successful Prometheus connection and pricing data fetch

### 1.3.2 OpenCost Metrics Scraping (Alloy Central)
- [ ] **Task 1.3.2.1:** Add `prometheus.scrape` in Alloy Central for OpenCost:
  - Target: `opencost.cost-management.svc.cluster.local:9003`
  - Path: `/metrics`
  - Scrape interval: 60s (cost metrics change slowly)
- [ ] **Task 1.3.2.2:** Verify key OpenCost metrics are ingested:
  - `allocation_cpu_cost` (hourly CPU cost per allocation)
  - `allocation_memory_cost` (hourly memory cost per allocation)
  - `allocation_gpu_cost` (hourly GPU cost per allocation)
  - `allocation_pv_cost` (hourly persistent volume cost)
  - `allocation_network_cost` (hourly network cost - if available)
  - `allocation_shared_cost` (shared cluster costs: control plane, etc.)
  - `allocation_total_cost` (sum of above)
  - `asset_*` metrics (node, disk, load balancer asset costs)
- [ ] **Task 1.3.2.3:** Validate OpenCost metric labels include:
  - `namespace`, `pod`, `container`, `deployment`, `statefulset`, `daemonset`, `job`, `cronjob`
  - `cluster`, `node`, `provider_id`
  - `cost_category` (cpu, memory, gpu, storage, network, shared)

### 1.3.3 Network Policy for OpenCost
- [ ] **Task 1.3.3.1:** Create NetworkPolicy allowing Alloy Central → OpenCost (port 9003):
  ```yaml
  apiVersion: networking.k8s.io/v1
  kind: NetworkPolicy
  metadata:
    name: allow-alloy-central-to-opencost
    namespace: cost-management
  spec:
    podSelector:
      matchLabels:
        app: opencost
    policyTypes:
    - Ingress
    ingress:
    - from:
      - namespaceSelector:
          matchLabels:
            name: monitoring
        podSelector:
          matchLabels:
            app: alloy-central
      ports:
      - protocol: TCP
        port: 9003
  ```
- [ ] **Task 1.3.3.2:** Test connectivity: `kubectl exec -n monitoring <alloy-central-pod> -- wget -qO- http://opencost.cost-management:9003/metrics | head`

---

## 1.4 Persistent Volume Cost Tracking

### 1.4.1 PV/PVC Metrics Collection (Alloy Central)
- [ ] **Task 1.4.1.1:** Extend KSM scrape (1.2.1) to include PV/PVC metrics:
  - `kube_persistentvolume_capacity_bytes` (PV capacity)
  - `kube_persistentvolumeclaim_resource_requests_storage_bytes` (PVC requested storage)
  - `kube_persistentvolume_info` (PV labels: storageclass, provisioner, volumehandle)
  - `kube_persistentvolumeclaim_info` (PVC labels: namespace, storageclass, volumeName)
  - `kubelet_volume_stats_capacity_bytes` (actual volume capacity from kubelet)
  - `kubelet_volume_stats_used_bytes` (actual volume usage from kubelet)
  - `kubelet_volume_stats_available_bytes` (actual volume available from kubelet)
  - `kubelet_volume_stats_inodes` / `kubelet_volume_stats_inodes_used` (inode usage)
- [ ] **Task 1.4.1.2:** Create recording rules for PVC utilization:
  - `pvc_utilization_ratio` = `kubelet_volume_stats_used_bytes / kubelet_volume_stats_capacity_bytes`
  - `pvc_cost_per_month` = `kube_persistentvolumeclaim_resource_requests_storage_bytes * storage_class_cost_per_gb_month / 1024^3`

### 1.4.2 Storage Class Cost Mapping
- [ ] **Task 1.4.2.1:** Create ConfigMap `storage-class-pricing` in `cost-management`:
  ```yaml
  apiVersion: v1
  kind: ConfigMap
  metadata:
    name: storage-class-pricing
    namespace: cost-management
  data:
    pricing.json: |
      {
        "gp3": {"cost_per_gb_month": 0.08, "iops_included": 3000, "cost_per_provisioned_iops_month": 0.005},
        "gp2": {"cost_per_gb_month": 0.10},
        "io1": {"cost_per_gb_month": 0.125, "cost_per_provisioned_iops_month": 0.065},
        "standard": {"cost_per_gb_month": 0.045},
        "premium-rwo": {"cost_per_gb_month": 0.15},
        "azure-disk-premium-v2": {"cost_per_gb_month": 0.18}
      }
  ```
- [ ] **Task 1.4.2.2:** Create Prometheus recording rule to join PVC → StorageClass → Cost:
  - Use `kube_persistentvolumeclaim_info` to get `storageclass` label
  - Use `kube_persistentvolume_info` to get `provisioner` for cloud disk type mapping
  - Multiply `kube_persistentvolumeclaim_resource_requests_storage_bytes` by cost/GB from ConfigMap (via `vector()` or external lookup)

### 1.4.3 Loki Pipeline for Storage Provisioner Logs (Optional - Advanced)
- [ ] **Task 1.4.3.1:** Configure Loki to scrape CSI driver / provisioner logs:
  - Identify provisioner pods (e.g., `ebs-csi-controller`, `pd-csi-controller`, `azure-disk-csi-controller`)
  - Add `loki.source.kubernetes` config for these pods
- [ ] **Task 1.4.3.2:** Create LogQL pipeline to extract volume provisioning events:
  - Parse `volume.beta.kubernetes.io/storage-provisioner` annotation
  - Extract volume ID, size, type, timestamps
  - Correlate with PV objects for cost attribution

---

## 1.5 Phase 1 Validation & Testing

### 1.5.1 Metric Availability Validation
- [ ] **Task 1.5.1.1:** Verify all target metrics queryable in Thanos Query (10902):
  - Node cost metrics (1.1.1)
  - Enhanced cAdvisor metrics (1.1.2)
  - KSM cost metrics (1.2.1)
  - OpenCost allocation metrics (1.3.2)
  - PV/PVC cost metrics (1.4.1)
- [ ] **Task 1.5.1.2:** Validate metric freshness (no stale series > 5m):
  ```promql
  time() - node_hourly_cost > 300
  time() - allocation_total_cost > 300
  ```
- [ ] **Task 1.5.1.3:** Check cardinality impact (Prometheus TSDB status page):
  - Target: < 1M active series added by Phase 1 components

### 1.5.2 Cost Accuracy Validation
- [ ] **Task 1.5.2.1:** Compare OpenCost total cluster cost vs Cloud Provider billing (last 7 days):
  - Target variance: < 10% (Phase 1), < 5% (Phase 7 target)
- [ ] **Task 1.5.2.2:** Spot-check 5 random namespaces:
  - Sum `allocation_total_cost` by namespace × 24h × 30d ≈ Monthly namespace cost
  - Compare with manual calculation (node hours × instance price + PV costs)

### 1.5.3 Integration Testing
- [ ] **Task 1.5.3.1:** Test Alloy DaemonSet → Prometheus → Thanos pipeline end-to-end
- [ ] **Task 1.5.3.2:** Test Alloy Central → Prometheus → Thanos pipeline end-to-end
- [ ] **Task 1.5.3.3:** Verify remote_write fanout to Thanos Receive (if deployed per Phase 2.1)

---

## 1.6 Phase 1 Documentation & Sign-off

### 1.6.1 Documentation
- [ ] **Task 1.6.1.1:** Create Phase 1 implementation report: `docs/phase1-implementation-report.md`
- [ ] **Task 1.6.1.2:** Document all Alloy configuration changes (`config/alloy/phase1-config-changes.alloy`)
- [ ] **Task 1.6.1.3:** Document OpenCost deployment values and pricing configuration
- [ ] **Task 1.6.1.4:** Update architecture diagrams with cost metrics flow
- [ ] **Task 1.6.1.5:** Create runbook: `docs/runbooks/cost-metrics-troubleshooting.md`

### 1.6.2 Sign-off
- [ ] **Task 1.6.2.1:** Platform Engineering review: metric cardinality, scrape performance, Alloy resource usage
- [ ] **Task 1.6.2.2:** FinOps team validation: cost accuracy, label completeness (cost_center, team on all metrics)
- [ ] **Task 1.6.2.3:** Security review: OpenCost cloud credentials, network policies
- [ ] **Task 1.6.2.4:** Formal Phase 1 completion gate → Proceed to Phase 2

---

## Implementation Files Created (Phase 1)

### Alloy Configuration
- `config/alloy/phase1-daemonset-config.alloy` - Node cost + enhanced cAdvisor config
- `config/alloy/phase1-central-config.alloy` - KSM + OpenCost + PV scrape config

### Prometheus Rules
- `config/prometheus/phase1-cost-recording-rules.yaml` - Recording rules for cost allocation
- `config/prometheus/phase1-pv-cost-rules.yaml` - PV/PVC cost calculation rules

### OpenCost Deployment
- `config/opencost/values.yaml` - Helm values for OpenCost deployment
- `config/opencost/custom-pricing.yaml` - Custom pricing ConfigMap template
- `config/network-policies/phase1-opencost-policy.yaml` - NetworkPolicy for Alloy→OpenCost

### Validation Scripts
- `scripts/validate-phase1.sh` - Main Phase 1 validation orchestrator
- `scripts/check-cost-metrics.sh` - Verify cost metric availability and freshness
- `scripts/validate-opencost.sh` - OpenCost deployment and metrics validation
- `scripts/check-pv-metrics.sh` - PV/PVC cost metrics validation

### Documentation
- `docs/phase1-implementation-report-template.md`
- `docs/runbooks/cost-metrics-troubleshooting.md`

---

## Validation Scripts Reference (Phase 1)

### `scripts/validate-phase1.sh` (Main Orchestrator)
```bash
# Run all Phase 1 validations
./scripts/validate-phase1.sh

# Run with auto-fix attempts (safe operations only)
./scripts/validate-phase1.sh --fix

# Generate report only
./scripts/validate-phase1.sh --report-only

# Validate specific component
./scripts/validate-phase1.sh --component=opencost
./scripts/validate-phase1.sh --component=pv-cost
./scripts/validate-phase1.sh --component=node-cost
```

### `scripts/check-cost-metrics.sh`
```bash
# Check all cost metrics availability
./scripts/check-cost-metrics.sh

# Check specific metric group
./scripts/check-cost-metrics.sh --group=node
./scripts/check-cost-metrics.sh --group=allocation
./scripts/check-cost-metrics.sh --group=pv

# Check freshness
./scripts/check-cost-metrics.sh --freshness=300
```

### `scripts/validate-opencost.sh`
```bash
# Validate OpenCost deployment
./scripts/validate-opencost.sh

# Check metrics endpoint
./scripts/validate-opencost.sh --check-metrics

# Compare with cloud bill (requires cloud CLI configured)
./scripts/validate-opencost.sh --compare-bill --days=7
```

### `scripts/check-pv-metrics.sh`
```bash
# Validate PV/PVC cost metrics
./scripts/check-pv-metrics.sh

# Check storage class pricing config
./scripts/check-pv-metrics.sh --check-pricing-config
```

---

## Success Criteria (Phase 1 KPIs)
- [ ] **100%** Node cost metrics (`node_hourly_cost`, etc.) available for all nodes
- [ ] **100%** Container resource metrics (requests, limits, usage) with workload/namespace labels
- [ ] **100%** OpenCost allocation metrics (`allocation_*_cost`) scraped and queryable
- [ ] **100%** PV/PVC metrics with storage class cost mapping
- [ ] **< 10%** Variance between OpenCost total cluster cost and cloud provider bill (7-day window)
- [ ] **< 1M** Additional active series from Phase 1 components
- [ ] **0** Critical alerts from new recording rules
- [ ] **Signed** Phase 1 completion by Platform, FinOps, Security leads

---

## Next Phase Trigger
**Phase 2 can begin when:**
1. All tasks in 1.1, 1.2, 1.3, 1.4, 1.5, 1.6 are ✅ complete
2. Cost accuracy validated (variance < 10%)
3. Metric cardinality within acceptable limits
4. Phase 1 implementation report approved by stakeholders
