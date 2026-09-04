# Phase 0: Prerequisites & Baseline Validation

This directory contains all scripts, templates, and documentation needed to execute Phase 0 of the Intelligent Kubernetes Observability & Cost Optimization Platform implementation.

## Overview

Phase 0 validates that the existing infrastructure is ready before deploying cost-specific components. It verifies:

1. **Namespace Strategy** - Required namespaces exist with proper labels
2. **Network Policies** - Critical connectivity paths are permitted
3. **Alloy DaemonSet** - Node-level collection is healthy and configured
4. **Alloy Central** - Cluster-level collection is configured correctly
5. **Prometheus/Thanos** - Long-term storage and query federation ready
6. **CRDs & Operators** - All required custom resources and operators installed

## Quick Start

```bash
# Run all validations
make validate

# Run specific validations
make validate-network      # Network policies only
make validate-alloy        # Alloy configuration only
make validate-prometheus   # Prometheus/Thanos only
make validate-crds         # CRDs and operators only

# Generate report from existing data
make report

# Clean up test resources
make clean
```

## Scripts

| Script | Purpose |
|--------|---------|
| `scripts/validate-phase0.sh` | Main validation orchestrator - runs all checks |
| `scripts/validate-network-policies.sh` | Network connectivity tests |
| `scripts/check-alloy-config.sh` | Alloy DaemonSet & Central configuration validation |
| `scripts/check-prometheus-thanos.sh` | Prometheus/Thanos component validation |
| `scripts/check-crds-operators.sh` | CRDs and operators validation |

## Output

All validations generate a comprehensive report at:
- `docs/phase0-validation-report.md` - Detailed validation results
- `docs/namespace-inventory.md` - Namespace label inventory

## CI/CD Integration

GitHub Actions workflow (`.github/workflows/phase0-validation.yml`) runs:
- On demand via `workflow_dispatch`
- Daily on schedule
- On PR changes to validation scripts

## Prerequisites

- `kubectl` configured with cluster access
- `curl` for connectivity tests
- `base64` for secret decoding
- Appropriate RBAC permissions to:
  - Read namespaces, network policies, CRDs
  - Create temporary pods in monitoring, observability, cost-management namespaces
  - Read ConfigMaps and Secrets in monitoring namespace

## Required RBAC for Validation Pods

The validation scripts create temporary pods to test connectivity. These pods need:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: phase0-validation
  namespace: monitoring
rules:
- apiGroups: [""]
  resources: ["pods", "pods/log"]
  verbs: ["create", "get", "delete", "exec"]
- apiGroups: [""]
  resources: ["services", "endpoints"]
  verbs: ["get", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: phase0-validation
  namespace: monitoring
subjects:
- kind: ServiceAccount
  name: default
  namespace: monitoring
roleRef:
  kind: Role
  name: phase0-validation
  apiGroup: rbac.authorization.k8s.io
```

Repeat for `observability` and `cost-management` namespaces.

## Success Criteria

Phase 0 is complete when:

- [ ] All 100% required namespaces exist with correct labels
- [ ] 100% critical network paths validated (zero failures)
- [ ] 100% Alloy DaemonSet/Central healthy and scraping expected metrics
- [ ] 100% Prometheus HA + Thanos components operational
- [ ] 100% Required CRDs installed (Prometheus Operator v0.75+, Grafana, VPA)
- [ ] 0 blocking gaps remaining
- [ ] Signed off by Platform, FinOps, and Security leads

## Next Steps

After Phase 0 completion, proceed to **Phase 1: Cost & Resource Metrics Enrichment** which includes:

1. Node/Cluster cost attribution (Alloy DaemonSet enhancements)
2. Kubernetes state metrics for cost (Alloy Central)
3. OpenCost/Kubecost integration
4. Persistent volume cost tracking

See `PLAN.md` for full implementation details.
