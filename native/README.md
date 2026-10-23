# Kubernetes-Native FinOps Platform - Phase 0 Implementation

This directory contains the Phase 0 (Native Prerequisites & Baseline Validation) implementation files.

## Directory Structure

```
native/
├── ksm/                    # Kube-State-Metrics hardening
│   ├── ksm-config.yaml     # ConfigMap with metric/label allowlists
│   ├── ksm-deployment.yaml # Deployment (2 replicas) + Service
│   └── ksm-networkpolicy.yaml
├── metrics-server/         # Metrics Server deployment
│   └── metrics-server-deployment.yaml
├── node-exporter/          # Node Exporter DaemonSet
│   ├── node-exporter-daemonset.yaml
│   └── node-exporter-networkpolicy.yaml
├── rbac/                   # RBAC for Cost Controller
│   └── cost-controller-sa.yaml
├── crds/                   # Core CRDs (7 total)
│   ├── costallocation-crd.yaml
│   ├── nodecost-crd.yaml
│   ├── pvcost-crd.yaml
│   ├── costhistory-crd.yaml
│   ├── rightsizingrecommendation-crd.yaml
│   ├── namespacebudget-crd.yaml
│   └── logevent-crd.yaml
├── examples/               # Example CRs
│   └── namespacebudget-example.yaml
├── scripts/                # Validation scripts
│   └── validate-native-phase0.sh
└── kustomization.yaml      # Kustomize entry point
```

## Deployment

### Prerequisites
- Kubernetes cluster v1.28+
- `kubectl` configured
- `kustomize` v5+ (or `kubectl kustomize`)

### Apply All Phase 0 Resources

```bash
# Apply all resources via kustomize
kubectl apply -k native/

# Or apply individually
kubectl apply -f native/ksm/
kubectl apply -f native/metrics-server/
kubectl apply -f native/node-exporter/
kubectl apply -f native/rbac/
kubectl apply -f native/crds/
kubectl apply -f native/examples/
```

### Verify Deployment

```bash
# Run validation script
chmod +x native/scripts/validate-native-phase0.sh
./native/scripts/validate-native-phase0.sh

# Check report
cat docs/phase0-native-report.md
```

## Component Details

### 0.1 Kube-State-Metrics (KSM)
- **Deployment**: 2 replicas in `monitoring` namespace
- **ConfigMap**: Allowlist of 14 critical metrics + label allowlist (`app`, `version`, `cost-center`, `team`)
- **NetworkPolicy**: Allows ingress from `cost-controller` (port 8080) and Prometheus
- **Resources**: CPU 200m/500m, Memory 256Mi/512Mi

### 0.2 Metrics Server
- **Deployment**: 2 replicas in `kube-system`
- **Args**: `--kubelet-insecure-tls`, `--metric-resolution=30s`, `--kubelet-preferred-address-types=InternalIP,Hostname`
- **Validation**: `kubectl top nodes/pods` < 30s latency

### 0.3 Node Exporter
- **DaemonSet**: HostNetwork + HostPID in `monitoring` namespace
- **Collectors**: cpu, mem, disk, net, netstat, filesystem
- **Service**: `node-exporter.monitoring.svc.cluster.local:9100` (ClusterIP)
- **NetworkPolicy**: Allows ingress from `cost-controller` (port 9100) and Prometheus

### 0.4 Cloud Provider Authentication
- **ServiceAccount**: `cost-controller-sa` in `cost-management` namespace
- **IRSA/Workload Identity**: Annotated with cloud role ARN
- **RBAC**: ClusterRole with read access to Pods, Nodes, PVCs, PVs, Namespaces, Events, Metrics, StorageClasses, and all FinOps CRDs

### 0.5 Core CRDs (7 Total)
All CRDs have `status` subresource enabled for controller updates:
1. **CostAllocation** (Namespaced) - Workload-level cost & usage
2. **NodeCost** (Cluster) - Node-level cost & utilization
3. **PVCost** (Namespaced) - PVC-level cost & utilization
4. **CostHistory** (Cluster) - Hourly snapshots for trends
5. **RightsizingRecommendation** (Namespaced) - VPA replacement output
6. **NamespaceBudget** (Cluster) - Governance input
7. **LogEvent** (Cluster) - OOM/Eviction/Error events

### 0.6 Validation
- **Script**: `scripts/validate-native-phase0.sh` - Comprehensive validation
- **Report**: Generates `docs/phase0-native-report.md`
- **Approvals**: Platform Engineering, FinOps, Security

## Cloud Provider Configuration

### AWS (IRSA)
```yaml
# In cost-controller-sa.yaml annotations:
eks.amazonaws.com/role-arn: arn:aws:iam::ACCOUNT_ID:role/CostControllerIRSA
```
Required IAM Policy:
```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "ec2:DescribeInstances",
        "pricing:GetProducts",
        "ce:GetCostAndUsage",
        "cloudwatch:GetMetricStatistics"
      ],
      "Resource": "*"
    }
  ]
}
```

### GCP (Workload Identity)
```yaml
# In cost-controller-sa.yaml annotations:
iam.gke.io/gcp-service-account: cost-controller@my-project.iam.gserviceaccount.com
```
Required IAM Roles:
- `roles/compute.viewer`
- `roles/cloudbilling.catalogViewer`
- `roles/monitoring.viewer`

### Azure (Workload Identity)
```yaml
# In cost-controller-sa.yaml annotations:
azure.workload.identity/client-id: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
```
Required RBAC Roles:
- `Microsoft.Compute/virtualMachines/read`
- `Microsoft.Consumption/usageDetails/read`
- `Microsoft.Insights/metrics/read`

## Labeling Strategy (FinOps Requirement)

All namespaces MUST have these labels for cost allocation:
```yaml
metadata:
  labels:
    cost-center: "engineering"  # or marketing, sales, platform, etc.
    team: "platform-team"       # owning team
    env: "production"           # production, staging, development
```

Pods should inherit or have:
```yaml
metadata:
  labels:
    app: "my-app"
    version: "v1.2.3"
    cost-center: "engineering"
    team: "platform-team"
```

## Troubleshooting

### KSM Not Exposing Metrics
```bash
# Check KSM logs
kubectl logs -n monitoring -l app.kubernetes.io/name=kube-state-metrics

# Verify configmap mounted
kubectl exec -n monitoring <ksm-pod> -- cat /etc/kube-state-metrics/config.yaml

# Test metrics endpoint
kubectl exec -n monitoring <ksm-pod> -- wget -qO- localhost:8080/metrics | head -50
```

### Metrics Server Not Working
```bash
# Check metrics-server logs
kubectl logs -n kube-system -l app.kubernetes.io/name=metrics-server

# Verify API service
kubectl get apiservice v1beta1.metrics.k8s.io

# Test with verbose
kubectl top nodes -v=6
```

### Node Exporter Issues
```bash
# Check daemonset status
kubectl get daemonset node-exporter -n monitoring -o wide

# Check pod logs
kubectl logs -n monitoring -l app.kubernetes.io/name=node-exporter

# Test metrics
kubectl exec -n monitoring <node-exporter-pod> -- wget -qO- localhost:9100/metrics | grep node_cpu
```

### CRD Issues
```bash
# Check CRD status
kubectl get crd | grep finops.io

# Describe specific CRD
kubectl describe crd costallocations.finops.io

# Check status subresource
kubectl get crd costallocations.finops.io -o jsonpath='{.spec.versions[0].subresources.status}'
```

## Next Phase

After Phase 0 validation passes, proceed to **Phase 1: Cost Controller Implementation** which includes:
- Go controller scaffold with controller-runtime
- Data ingestion clients (KSM, Metrics Server, Node Exporter, Cloud Pricing)
- Allocation algorithm engine
- Embedded Prometheus TSDB
- CRD status updates
- API Gateway with PromQL-compatible query endpoints
