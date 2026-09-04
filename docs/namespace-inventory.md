# Namespace Label Inventory
**Generated:** 2024-01-15 10:30:00 UTC

| Namespace | cost-center | environment | team | Status |
|-----------|-------------|-------------|------|--------|
| cost-management | finops | production | finops | ✅ |
| default | MISSING | MISSING | MISSING | ❌ |
| kube-node-lease | MISSING | MISSING | MISSING | ❌ |
| kube-public | MISSING | MISSING | MISSING | ❌ |
| kube-system | MISSING | MISSING | MISSING | ❌ |
| monitoring | platform | production | platform-engineering | ✅ |
| observability | platform | production | platform-engineering | ✅ |

## Summary
- **Total Namespaces:** 7
- **Fully Labeled:** 3
- **Missing Labels:** 4

## Required Actions
- Namespace `default`: Add missing labels
- Namespace `kube-node-lease`: Add missing labels (system namespace - may skip)
- Namespace `kube-public`: Add missing labels (system namespace - may skip)
- Namespace `kube-system`: Add missing labels (system namespace - may skip)

## Label Standards
```yaml
# Required labels for all namespaces
labels:
  cost-center: <finops|platform|engineering|marketing|sales|etc>
  environment: <production|staging|development|test>
  team: <team-name>

# Example for application namespace
metadata:
  name: payment-service
  labels:
    cost-center: engineering
    environment: production
    team: payments
```

## Automation
Apply labels to application namespaces:
```bash
# Example: Label a namespace
kubectl label namespace payment-service cost-center=engineering environment=production team=payments --overwrite

# Bulk label from CSV (if available)
# while IFS=',' read -r ns cc env team; do
#   kubectl label namespace "$ns" cost-center="$cc" environment="$env" team="$team" --overwrite
# done < namespace-labels.csv
```
