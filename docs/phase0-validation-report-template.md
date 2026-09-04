# Phase 0 Validation Report Template
**Cluster:** {{CLUSTER_NAME}}
**Date:** {{DATE}}
**Validator:** {{VALIDATOR}}
**Kubernetes Version:** {{K8S_VERSION}}

---

## Executive Summary

| Metric | Count |
|--------|-------|
| Total Checks | {{TOTAL_CHECKS}} |
| ✅ Passed | {{PASSED_CHECKS}} |
| ❌ Failed | {{FAILED_CHECKS}} |
| ⚠️ Warnings | {{WARNING_CHECKS}} |

**Overall Status:** {{OVERALL_STATUS}}

---

## 0.1 Validate Existing Components

### 0.1.1 Namespace Strategy
| Check | Status | Details |
|-------|--------|---------|
| monitoring namespace exists | {{NS_MONITORING_EXISTS}} | {{NS_MONITORING_DETAILS}} |
| monitoring labels (cost-center, environment, team) | {{NS_MONITORING_LABELS}} | {{NS_MONITORING_LABELS_DETAILS}} |
| observability namespace exists | {{NS_OBSERVABILITY_EXISTS}} | {{NS_OBSERVABILITY_DETAILS}} |
| observability labels | {{NS_OBSERVABILITY_LABELS}} | {{NS_OBSERVABILITY_LABELS_DETAILS}} |
| cost-management namespace exists | {{NS_COST_MGMT_EXISTS}} | {{NS_COST_MGMT_DETAILS}} |
| cost-management labels | {{NS_COST_MGMT_LABELS}} | {{NS_COST_MGMT_LABELS_DETAILS}} |
| Application namespaces labeled | {{APP_NS_LABELS}} | {{APP_NS_LABELS_DETAILS}} |

### 0.1.2 Network Policies
| Check | Status | Details |
|-------|--------|---------|
| NetworkPolicy resources exist | {{NP_EXIST}} | {{NP_EXIST_DETAILS}} |
| Object storage egress (monitoring) | {{EGRESS_MONITORING}} | {{EGRESS_MONITORING_DETAILS}} |
| Object storage egress (observability) | {{EGRESS_OBSERVABILITY}} | {{EGRESS_OBSERVABILITY_DETAILS}} |
| Object storage egress (cost-management) | {{EGRESS_COST_MGMT}} | {{EGRESS_COST_MGMT_DETAILS}} |
| Alloy DaemonSet → Prometheus | {{ALLOY_DS_PROM}} | {{ALLOY_DS_PROM_DETAILS}} |
| Prometheus → kube-state-metrics | {{PROM_KSM}} | {{PROM_KSM_DETAILS}} |
| Prometheus → cAdvisor | {{PROM_CADVISOR}} | {{PROM_CADVISOR_DETAILS}} |
| Thanos Sidecar ↔ Store | {{THANOS_SIDE_STORE}} | {{THANOS_SIDE_STORE_DETAILS}} |
| Thanos Query → Grafana | {{THANOS_QUERY_GRAFANA}} | {{THANOS_QUERY_GRAFANA_DETAILS}} |
| Alloy → Loki Distributor | {{ALLOY_LOKI_DIST}} | {{ALLOY_LOKI_DIST_DETAILS}} |
| Loki Query → Grafana | {{LOKI_QUERY_GRAFANA}} | {{LOKI_QUERY_GRAFANA_DETAILS}} |

### 0.1.3 Alloy DaemonSet
| Check | Status | Details |
|-------|--------|---------|
| DaemonSet exists | {{ALLOY_DS_EXIST}} | {{ALLOY_DS_EXIST_DETAILS}} |
| All pods ready | {{ALLOY_DS_READY}} | {{ALLOY_DS_READY_DETAILS}} |
| node-exporter integration | {{ALLOY_NODE_EXPORTER}} | {{ALLOY_NODE_EXPORTER_DETAILS}} |
| cAdvisor integration | {{ALLOY_CADVISOR}} | {{ALLOY_CADVISOR_DETAILS}} |
| pod_logs labels (namespace, pod, container) | {{ALLOY_POD_LOGS_BASIC}} | {{ALLOY_POD_LOGS_BASIC_DETAILS}} |
| pod_logs labels (cost_center, team) | {{ALLOY_POD_LOGS_COST}} | {{ALLOY_POD_LOGS_COST_DETAILS}} |
| Log errors check | {{ALLOY_LOGS}} | {{ALLOY_LOGS_DETAILS}} |

### 0.1.4 Alloy Central
| Check | Status | Details |
|-------|--------|---------|
| Deployment exists | {{ALLOY_CENTRAL_EXIST}} | {{ALLOY_CENTRAL_EXIST_DETAILS}} |
| Replicas ready | {{ALLOY_CENTRAL_READY}} | {{ALLOY_CENTRAL_READY_DETAILS}} |
| prometheus.remote_write configured | {{ALLOY_CENTRAL_RW}} | {{ALLOY_CENTRAL_RW_DETAILS}} |
| Prometheus HA endpoints | {{ALLOY_CENTRAL_HA}} | {{ALLOY_CENTRAL_HA_DETAILS}} |
| loki.write configured | {{ALLOY_CENTRAL_LOKI}} | {{ALLOY_CENTRAL_LOKI_DETAILS}} |
| Remote write throughput (no 429/5xx) | {{ALLOY_CENTRAL_THROUGHPUT}} | {{ALLOY_CENTRAL_THROUGHPUT_DETAILS}} |

### 0.1.5 Prometheus/Thanos
| Check | Status | Details |
|-------|--------|---------|
| Thanos objstore.yml secret | {{THANOS_OBJSTORE}} | {{THANOS_OBJSTORE_DETAILS}} |
| Thanos Query deployed | {{THANOS_QUERY_DEPLOY}} | {{THANOS_QUERY_DEPLOY_DETAILS}} |
| Thanos Store deployed | {{THANOS_STORE_DEPLOY}} | {{THANOS_STORE_DEPLOY_DETAILS}} |
| Thanos Compact deployed | {{THANOS_COMPACT_DEPLOY}} | {{THANOS_COMPACT_DEPLOY_DETAILS}} |
| Prometheus local retention (2h) | {{PROM_RETENTION}} | {{PROM_RETENTION_DETAILS}} |
| Thanos compactor retention (14d/90d/365d) | {{THANOS_RETENTION}} | {{THANOS_RETENTION_DETAILS}} |
| Thanos Query UI accessible | {{THANOS_UI}} | {{THANOS_UI_DETAILS}} |
| Prometheus HA pair running | {{PROM_HA}} | {{PROM_HA_DETAILS}} |

---

## 0.2 Required CRDs & Operators

### 0.2.1 Prometheus Operator CRDs
| CRD | Status | Version |
|-----|--------|---------|
| ServiceMonitor | {{CRD_SM}} | {{CRD_SM_VER}} |
| PodMonitor | {{CRD_PM}} | {{CRD_PM_VER}} |
| Probe | {{CRD_PROBE}} | {{CRD_PROBE_VER}} |
| PrometheusRule | {{CRD_PR}} | {{CRD_PR_VER}} |
| Prometheus | {{CRD_PROM}} | {{CRD_PROM_VER}} |
| Alertmanager | {{CRD_AM}} | {{CRD_AM_VER}} |

### 0.2.2 Grafana Operator/Resources
| Resource | Status | Details |
|----------|--------|---------|
| GrafanaDashboard CRD | {{GRAFANA_DASH_CRD}} | {{GRAFANA_DASH_CRD_DETAILS}} |
| GrafanaDataSource CRD | {{GRAFANA_DS_CRD}} | {{GRAFANA_DS_CRD_DETAILS}} |
| Grafana Deployment | {{GRAFANA_DEPLOY}} | {{GRAFANA_DEPLOY_DETAILS}} |
| Grafana → Thanos Query | {{GRAFANA_THANOS}} | {{GRAFANA_THANOS_DETAILS}} |
| Grafana → Loki Query | {{GRAFANA_LOKI}} | {{GRAFANA_LOKI_DETAILS}} |

### 0.2.3 Cost Management
| Component | Status | Details |
|-----------|--------|---------|
| OpenCost/Kubecost deployed | {{OPENCOST_DEPLOY}} | {{OPENCOST_DEPLOY_DETAILS}} |
| Custom Metrics API | {{CUSTOM_METRICS_API}} | {{CUSTOM_METRICS_API_DETAILS}} |

### 0.2.4 VPA
| Component | Status | Details |
|-----------|--------|---------|
| VPA CRD | {{VPA_CRD}} | {{VPA_CRD_DETAILS}} |
| VPA Recommender | {{VPA_RECOMMENDER}} | {{VPA_RECOMMENDER_DETAILS}} |
| VPA Updater | {{VPA_UPDATER}} | {{VPA_UPDATER_DETAILS}} |
| VPA Admission Controller | {{VPA_ADMISSION}} | {{VPA_ADMISSION_DETAILS}} |
| PodDisruptionBudgets | {{PDB_COUNT}} | {{PDB_COUNT_DETAILS}} |

---

## 0.3 Documentation & Sign-off

### Generated Artifacts
- [ ] `docs/phase0-validation-report.md` - This report
- [ ] `docs/namespace-inventory.md` - Namespace label inventory
- [ ] Architecture diagrams updated

### Sign-off
| Role | Name | Signature | Date |
|------|------|-----------|------|
| Platform Engineering Lead | | | |
| FinOps Team Lead | | | |
| Security Team Lead | | | |

---

## Gap Analysis & Remediation Plan

| Gap ID | Category | Description | Severity | Remediation | Owner | Target Date |
|--------|----------|-------------|----------|-------------|-------|-------------|
| GAP-001 | | | | | | |
| GAP-002 | | | | | | |

---

## Next Phase Readiness

**Phase 1 can begin when:**
- [ ] All critical checks (❌) resolved
- [ ] Validation report approved by all stakeholders
- [ ] Remediation plan for warnings (⚠️) documented
- [ ] Sign-off obtained from all three leads

**Target Phase 1 Start Date:** {{PHASE1_START_DATE}}
