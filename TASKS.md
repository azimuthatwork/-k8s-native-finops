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

## Implementation Files Created

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

## Validation Scripts Reference

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
