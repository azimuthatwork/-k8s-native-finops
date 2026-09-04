
                                                                                                                                                                          
# Intelligent Kubernetes Observability & Cost Optimization Platform - Implementation Plan                                                                                 
                                                                                                                                                                          
## Executive Summary                                                                                                                                                      
This plan defines the phased implementation of a full-stack observability platform (Grafana, Alloy, Prometheus, Loki, Thanos) focused on **cost attribution, resource     
optimization, utilization tracking, and waste reclamation**. The architecture leverages the existing namespace strategy, network policies, Alloy DaemonSet/Deployment     
topology, and Prometheus/Thanos configuration defined in the repository.                                                                                                  
                                                                                                                                                                          
---                                                                                                                                                                       
                                                                                                                                                                          
## Phase 0: Prerequisites & Baseline Validation                                                                                                                           
**Objective:** Verify existing infrastructure readiness before deploying cost-specific components.                                                                        
                                                                                                                                                                          
### 0.1 Validate Existing Components                                                                                                                                      
- [ ] **Namespace Strategy (1.1):** Confirm `monitoring`, `observability`, `cost-management` namespaces exist with correct labels (`cost-center`, `environment`, `team`). 
- [ ] **Network Policies (1.3):** Verify egress to object storage (Thanos/Loki) and inter-namespace scraping (Alloy -> Prometheus, Prometheus ->                          
kube-state-metrics/cadvisor) is permitted.                                                                                                                                
- [ ] **Alloy DaemonSet (2.1):** Confirm `node-exporter` and `cadvisor` integration modules are active; verify `pod_logs` collection includes `namespace`, `pod`,         
`container` labels.                                                                                                                                                       
- [ ] **Alloy Central (2.2):** Verify `prometheus.remote_write` endpoints point to Prometheus HA pair; `loki.write` endpoint configured.                                  
- [ ] **Prometheus/Thanos (3.1):** Confirm Thanos Sidecar `objstore.yml` (S3/GCS/Azure) configured; `query` and `store` components deployed; retention `2h` local, `90d`  
object storage.                                                                                                                                                           
                                                                                                                                                                          
### 0.2 Required CRDs & Operators                                                                                                                                         
- [ ] `PrometheusOperator` (v0.75+): `ServiceMonitor`, `PodMonitor`, `Probe`, `PrometheusRule`.                                                                           
- [ ] `GrafanaOperator` (or Helm): `GrafanaDashboard`, `GrafanaDataSource`.                                                                                               
- [ ] `KubeCost` / `OpenCost` integration (custom metrics adapter).                                                                                                       
- [ ] `VerticalPodAutoscaler` (VPA) CRDs for recommendation engine.                                                                                                       
                                                                                                                                                                          
---                                                                                                                                                                       
                                                                                                                                                                          
## Phase 1: Cost & Resource Metrics Enrichment                                                                                                                            
**Objective:** Ingest granular cost, usage, and capacity metrics into the TSDB.                                                                                           
                                                                                                                                                                          
### 1.1 Node/Cluster Cost Attribution (Alloy DaemonSet - 2.1)                                                                                                             
**File:** `config.alloy` (Node Level)                                                                                                                                     
```alloy                                                                                                                                                                  
// 1. Node Cost Metrics (Cloud Provider Integration)                                                                                                                      
discovery.relabel "cloud_cost" {                                                                                                                                          
  targets = discovery.digitalocean "nodes" {} // Or AWS/GCP/Azure SD                                                                                                      
  rule {                                                                                                                                                                  
    source_labels = ["__meta_digitalocean_droplet_id"]                                                                                                                    
    target_label = "instance_id"                                                                                                                                          
  }                                                                                                                                                                       
  // Add pricing metadata via relabeling or external lookup table                                                                                                         
}                                                                                                                                                                         
                                                                                                                                                                          
// 2. Enhanced cAdvisor Metrics (Container Resource Requests/Limits/Usage)                                                                                                
prometheus.exporter.cadvisor "enhanced" {                                                                                                                                 
  // Ensure these metrics are scraped:                                                                                                                                    
  // container_spec_memory_limit_bytes, container_spec_cpu_quota/period                                                                                                   
  // container_memory_working_set_bytes, container_cpu_usage_seconds_total                                                                                                
  // container_fs_usage_bytes, container_fs_limit_bytes                                                                                                                   
}                                                                                                                                                                         
                                                                                                                                                                          

1.2 Kubernetes State Metrics for Cost (Alloy Central - 2.2)                                                                                                               

File: config.alloy (Central Level)                                                                                                                                        

                                                                                                                                                                          
// Scrape kube-state-metrics (KSM) for Requests/Limits/Capacity                                                                                                           
prometheus.scrape "kube_state_metrics" {                                                                                                                                  
  targets = discovery.kubernetes "nodes" {                                                                                                                                
    role = "service"                                                                                                                                                      
    role = "service"                                                                                                                                                      
    namespaces = ["kube-system", "monitoring"]                                                                                                                            
    label_selector = "app=kube-state-metrics"                                                                                                                             
  }                                                                                                                                                                       
  forward_to = [prometheus.remote_write.default.receiver]                                                                                                                 
  // Critical Metrics:                                                                                                                                                    
  // kube_pod_container_resource_requests{resource="cpu|memory"}                                                                                                          
  // kube_pod_container_resource_limits{resource="cpu|memory"}                                                                                                            
  // kube_node_status_capacity{resource="cpu|memory|pods"}                                                                                                                
  // kube_node_status_allocatable{resource="cpu|memory|pods"}                                                                                                             
  // kube_pod_owner (for workload mapping)                                                                                                                                
  // kube_namespace_labels (for cost-center mapping)                                                                                                                      
}                                                                                                                                                                         
                                                                                                                                                                          

1.3 OpenCost / Kubecost Integration (Deployment)                                                                                                                          

Deployment: opencost Helm chart in cost-management namespace.                                                                                                             

 • Config: prometheus.fqdn=http://prometheus-operated.monitoring:9090                                                                                                     
 • Cloud Provider: Configure AWS/GCP/AZURE/CUSTOM pricing API keys.                                                                                                       
 • Output: Exposes opencost metrics (allocation_*, asset_*) scraped by Alloy Central.                                                                                     
 • NetworkPolicy: Allow Alloy Central -> OpenCost (port 9003).                                                                                                            

1.4 Persistent Volume Cost Tracking                                                                                                                                       

 • Alloy Central: Scrape kube_persistentvolume_capacity_bytes, kube_persistentvolumeclaim_resource_requests_storage_bytes.                                                
 • Loki Pipeline: Parse storage class provisioner logs (if available) for volume.beta.kubernetes.io/storage-provisioner to map PV -> Cloud Disk Type (gp3, pd-ssd) ->     
   Cost/GB.                                                                                                                                                               

--------------------------------------------------------------------------------------------------------------------------------------------------------------------------


Phase 2: Long-Term Storage & Query Federation (Thanos)                                                                                                                    

Objective: Enable historical cost trend analysis (YoY, MoM) without local Prometheus retention pressure.                                                                  

2.1 Thanos Receive (Optional - High Cardinality Cost Metrics)                                                                                                             

 • Deploy thanos-receive (StatefulSet) in monitoring for ingesting OpenCost/Allocation metrics directly if cardinality exceeds Prometheus limits.                         
 • Configure Alloy Central remote_write to fan-out: Primary -> Prometheus HA, Secondary -> Thanos Receive.                                                                

2.2 Thanos Query Frontend & Query                                                                                                                                         

 • Deploy thanos-query with --store=dnssrv+_grpc._tcp.thanos-store.monitoring (for block storage) + --store=prometheus-operated.monitoring:10901 (sidecars).              
 • Grafana Datasource: Single "Thanos" datasource pointing to thanos-query.monitoring:10902 (covers both real-time and historical).                                       

2.3 Compaction & Retention Policies                                                                                                                                       

 • Sidecar: --tsdb.retention=2h                                                                                                                                           
 • Compactor: --retention.resolution-raw=14d --retention.resolution-5m=90d --retention.resolution-1h=365d                                                                 
 • Bucket: Versioned, Lifecycle policy: Standard -> IA (30d) -> Glacier (90d).                                                                                            

--------------------------------------------------------------------------------------------------------------------------------------------------------------------------


Phase 3: Loki Log Correlation for Waste Detection                                                                                                                         

Objective: Correlate OOMKills, Throttling, Evictions with cost data.                                                                                                      


3.1 Alloy Log Pipeline Enhancements (config.alloy)                                                                                                                        

                                                                                                                                                                          
// DaemonSet: Tail container logs + Kubelet logs                                                                                                                          
loki.source.kubernetes "pod_logs" {                                                                                                                                       
  // ... existing config ...                                                                                                                                              
  // ADD: Structured metadata for cost correlation                                                                                                                        
  labels = {                                                                                                                                                              
    namespace = __meta_kubernetes_namespace                                                                                                                               
    pod       = __meta_kubernetes_pod_name                                                                                                                                
    container = __meta_kubernetes_container_name                                                                                                                          
    // Cost Labels injected via relabeling from K8s API                                                                                                                   
    cost_center = __meta_kubernetes_namespace_label_cost_center                                                                                                           
    team        = __meta_kubernetes_namespace_label_team                                                                                                                  
  }                                                                                                                                                                       
}                                                                                                                                                                         
                                                                                                                                                                          
// Central: Kube-Audit / Event Logs for Evictions/OOM                                                                                                                     
loki.source.kubernetes "kube_events" {                                                                                                                                    
  // Scrape kube-apiserver audit logs or event-exporter                                                                                                                   
  // Label: reason="Evicted|OOMKilling|FailedScheduling"                                                                                                                  
}                                                                                                                                                                         
                                                                                                                                                                          

3.2 Loki Index Optimization                                                                                                                                               

 • SchemaConfig: tsdb (boltdb-shipper deprecated).                                                                                                                        
 • Compactor: retention_period=30d (logs), 90d (structured metadata/events).                                                                                              
 • Structured Metadata: Enable for container, namespace, cost_center to accelerate LogQL filtering without full-text index.                                               

--------------------------------------------------------------------------------------------------------------------------------------------------------------------------


Phase 4: Intelligent Dashboard Suite (Grafana)                                                                                                                            

Objective: Build 5 core dashboards + Alerting Rules. Provisioned via GrafanaDashboard CRs / ConfigMaps.                                                                   

4.1 Dashboard 1: Cluster Cost Overview (Executive)                                                                                                                        

Panels:                                                                                                                                                                   

 1 Total Daily/Monthly Spend (Stacked: Compute, Storage, Network, Shared) -> sum(allocated_cost_per_day) by (category)                                                    
 2 Cost per Namespace/Team (Treemap) -> sum(namespace_monthly_cost) by (namespace, cost_center)                                                                           
 3 Efficiency Score (Gauge) -> sum(container_cpu_usage_seconds_total:rate5m) / sum(kube_pod_container_resource_requests_cpu_cores) * 100                                  
 4 Waste Trend (Time Series) -> sum(requests - usage) * unit_cost over 30d.                                                                                               
 5 Top 10 Most Expensive Workloads (Table) -> topk(10, sum by (workload, namespace) (allocation_cpu_cost + allocation_memory_cost))                                       

4.2 Dashboard 2: Resource Utilization & Rightsizing (Engineering)                                                                                                         

Panels:                                                                                                                                                                   

 1 CPU/Memory Request vs Limit vs Usage (Heatmap) per Workload.                                                                                                           
    • Query: histogram_quantile(0.5, rate(container_cpu_usage_seconds_total[5m])) / kube_pod_container_resource_requests_cpu_cores                                        
 2 Rightsizing Recommendations Table (VPA Integration):                                                                                                                   
    • Source: vpa_recommender_container_recommended_cpu_cores, vpa_recommender_container_recommended_memory_bytes.                                                        
    • Columns: Current Request | Recommended | Potential Savings ($/mo) | Action (Apply/Ignore).                                                                          
 3 Container Throttling % -> rate(container_cpu_cfs_throttled_periods_total[5m]) / rate(container_cpu_cfs_periods_total[5m]) > 0.25                                       
 4 OOMKills / Evictions Count (LogQL) -> sum by (namespace, pod) (count_over_time({reason=~"OOMKilling|Evicted"} [24h]))                                                  

4.3 Dashboard 3: Idle & Unused Resource Reclamation                                                                                                                       

Panels:                                                                                                                                                                   

 1 Idle Pods (CPU < 1% & Mem < 5% for 7d) -> avg_over_time(container_cpu_usage_seconds_total:rate5m[7d]) < 0.01                                                           
 2 Orphaned PVCs (Bound but no Pod mounting for 48h) -> kube_persistentvolumeclaim_info vs kube_pod_status_phase.                                                         
 3 Unused LoadBalancer IPs / Public IPs (Cloud Exporter metrics).                                                                                                         
 4 Over-provisioned Nodes (Allocatable vs Actual Usage < 30% for 14d) -> Candidate for Node Pool downsizing/Spot migration.                                               

4.4 Dashboard 4: Storage Cost Deep Dive                                                                                                                                   

Panels:                                                                                                                                                                   

 1 PV Cost by StorageClass (Pie) -> sum(kube_persistentvolume_capacity_bytes * storage_class_cost_per_gb) by (storageclass)                                               
 2 PVC Utilization % -> kubelet_volume_stats_used_bytes / kubelet_volume_stats_capacity_bytes                                                                             
 3 Snapshot/Backup Cost (if Velero/CSI Snapshots monitored).                                                                                                              
 4 Abandoned Snapshots (Age > Retention Policy).                                                                                                                          

4.5 Dashboard 5: Network & Data Transfer Costs                                                                                                                            

Panels:                                                                                                                                                                   

 1 Cross-AZ / Cross-Region Traffic (Node Exporter node_network_trans_bytes + Topology labels).                                                                            
 2 Ingress/Egress Cost by Namespace (Cilium/Calico Hubble metrics or Cloud VPC Flow Logs parsed in Loki).                                                                 
 3 Public Egress Top Talkers.                                                                                                                                             

--------------------------------------------------------------------------------------------------------------------------------------------------------------------------


Phase 5: Automation & Optimization Engine                                                                                                                                 

Objective: Move from visibility to action.                                                                                                                                

5.1 Vertical Pod Autoscaler (VPA) Deployment                                                                                                                              

 • Mode: Auto (for dev/staging), RecommendationOnly (for prod).                                                                                                           
 • ResourcePolicy: Exclude kube-system, monitoring; Set minAllowed/maxAllowed per container.                                                                              
 • Updater: EvictionTolerance configured to respect PDBs.                                                                                                                 

5.2 Horizontal Pod Autoscaler (HPA) Custom Metrics                                                                                                                        

 • Prometheus Adapter: Expose cost_per_request, cpu_utilization_per_replica.                                                                                              
 • HPA Config: Scale on cpu_utilization_per_replica target 70% + cost_per_request anomaly detection.                                                                      

5.3 Cluster Autoscaler (CA) / Karpenter Tuning                                                                                                                            

 • Node Templates: Define cost-center labels on NodePools.                                                                                                                
 • Consolidation: Enable --scale-down-utilization-threshold=0.5 (aggressive) for batch workloads.                                                                         
 • Spot/Preemptible: Prioritize Spot for spot-eligible=true workloads (savings 60-90%).                                                                                   

5.4 Automated Cleanup Jobs (CronJobs)                                                                                                                                     

 1 PVC Reaper: Delete Released PVs + Pending PVCs > 7d.                                                                                                                   
 2 Image Cleanup: node-image-gc high threshold 80%, low 70%.                                                                                                              
 3 Completed Job/Pod GC: TTL 24h for Completed, 1h for Failed.                                                                                                            

--------------------------------------------------------------------------------------------------------------------------------------------------------------------------


Phase 6: Alerting & Governance (PrometheusRules)                                                                                                                          

File: cost-optimization-rules.yaml (Applied via PrometheusOperator)                                                                                                       

                                                                                                                                                                          
groups:                                                                                                                                                                   
- name: cost-optimization.alerts                                                                                                                                          
  interval: 10m                                                                                                                                                           
  rules:                                                                                                                                                                  
  # 1. Efficiency Alerts                                                                                                                                                  
  - alert: WorkloadLowCPUUtilization                                                                                                                                      
    expr: |                                                                                                                                                               
      (sum(rate(container_cpu_usage_seconds_total{container!="",container!="POD"}[24h])) by (namespace, workload, pod)                                                    
       / sum(kube_pod_container_resource_requests_cpu_cores) by (namespace, workload, pod)) < 0.1                                                                         
    for: 7d                                                                                                                                                               
    labels: { severity: warning, category: "cost-optimization" }                                                                                                          
    annotations:                                                                                                                                                          
      summary: "{{$labels.workload}} in {{$labels.namespace}} avg CPU util < 10% for 7 days"                                                                              
      runbook: "https://wiki.internal/rightsizing-guide"                                                                                                                  
                                                                                                                                                                          
  - alert: WorkloadMemoryOverprovisioned                                                                                                                                  
    expr: |                                                                                                                                                               
      (sum(container_memory_working_set_bytes) by (namespace, workload)                                                                                                   
       / sum(kube_pod_container_resource_requests_memory_bytes) by (namespace, workload)) < 0.2                                                                           
    for: 7d                                                                                                                                                               
    labels: { severity: warning, category: "cost-optimization" }                                                                                                          
                                                                                                                                                                          
  # 2. Waste Alerts                                                                                                                                                       
  - alert: NamespaceMonthlyCostAnomaly                                                                                                                                    
    expr: |                                                                                                                                                               
      (sum(increase(allocated_cost_total[1d])) by (namespace)                                                                                                             
       / avg_over_time(sum(increase(allocated_cost_total[1d])) by (namespace)[30d:1d])) > 1.5                                                                             
    for: 1h                                                                                                                                                               
    labels: { severity: critical, category: "finops" }                                                                                                                    
                                                                                                                                                                          
  # 3. Capacity Alerts                                                                                                                                                    
  - alert: ClusterUnusedCapacityHigh                                                                                                                                      
    expr: |                                                                                                                                                               
      (sum(kube_node_status_allocatable_cpu_cores) - sum(kube_pod_container_resource_requests_cpu_cores))                                                                 
      / sum(kube_node_status_allocatable_cpu_cores) > 0.4                                                                                                                 
    for: 24h                                                                                                                                                              
    labels: { severity: info, category: "capacity-planning" }                                                                                                             
                                                                                                                                                                          
  # 4. Storage Alerts                                                                                                                                                     
  - alert: PVCUnusedLargeVolume                                                                                                                                           
    expr: |                                                                                                                                                               
      kube_persistentvolumeclaim_resource_requests_storage_bytes > 100Gi                                                                                                  
      and kubelet_volume_stats_used_bytes / kubelet_volume_stats_capacity_bytes < 0.1                                                                                     
    for: 14d                                                                                                                                                              
    labels: { severity: warning, category: "storage-optimization" }                                                                                                       
                                                                                                                                                                          

--------------------------------------------------------------------------------------------------------------------------------------------------------------------------


Phase 7: FinOps Integration & Reporting                                                                                                                                   

Objective: Close the loop with Finance/Business.                                                                                                                          

7.1 Showback/Chargeback API                                                                                                                                               

 • Tool: kubecost API or custom PromQL -> Python/Go Exporter -> Grafana Infinity Datasource / CSV Export.                                                                 
 • Dimensions: Namespace, Label:cost_center, Label:project, Label:environment.                                                                                            
 • Frequency: Daily snapshot stored in Postgres/ClickHouse for BI tools (Superset/Metabase).                                                                              

7.2 Budget Enforcement (Kyverno / Gatekeeper)                                                                                                                             

 • Policy: NamespaceBudget CRD.                                                                                                                                           
 • Admission Control: Reject Pod creation if sum(requests) * unit_cost > namespace_budget_limit (soft warn / hard block).                                                 
 • Mutation: Auto-inject resources.requests based on VPA history if missing.                                                                                              

7.3 Monthly Optimization Report (Automated)                                                                                                                               

 • Grafana Reporting: Scheduled PDF/CSV export of Dashboards 1, 2, 3.                                                                                                     
 • Content: Total Spend, Top 5 Savings Opportunities, Rightsizing Actions Taken, Waste Eliminated.                                                                        

--------------------------------------------------------------------------------------------------------------------------------------------------------------------------


Implementation Sequence & Dependencies                                                                                                                                    

                                                                                         
 Phase  Dependency  Est. Effort  Key Deliverable                                         
 ─────────────────────────────────────────────────────────────────────────────────────── 
 0      None        1 Day        Validated Baseline Checklist                            
 1      0           3 Days       Enriched Metrics (Cost, Requests, Limits, PV) in Thanos 
 2      1           2 Days       Historical Query Capability (1yr+)                      
 3      0           2 Days       Log Correlation (OOM/Eviction/Throttle)                 
 4      1, 2, 3     5 Days       5 Production Dashboards + Alerting Rules                
 5      4           5 Days       VPA/HPA/CA Tuning + Cleanup Automation                  
 6      4           1 Day        PrometheusRule Deployment                               
 7      4, 5        3 Days       Chargeback API + Budget Policies + Reports              
                                                                                         

--------------------------------------------------------------------------------------------------------------------------------------------------------------------------


Configuration Checklist (Ready for Apply)                                                                                                                                 

Alloy (config.alloy - DaemonSet)                                                                                                                                          

 • [ ] prometheus.exporter.cadvisor enabled with machines/containers filters.                                                                                             
 • [ ] loki.source.kubernetes adds cost_center, team labels via discovery.kubernetes relabeling.                                                                          
 • [ ] prometheus.scrape for node-exporter includes node_network_* for network cost.                                                                                      

Alloy (config.alloy - Central)                                                                                                                                            

 • [ ] prometheus.scrape for kube-state-metrics (Core metrics list in 1.2).                                                                                               
 • [ ] prometheus.scrape for opencost (port 9003).                                                                                                                        
 • [ ] prometheus.remote_write -> Prometheus HA + Thanos Receive (fanout).                                                                                                
 • [ ] loki.write -> Loki Distributor/Gateway.                                                                                                                            

Prometheus/Thanos (3.1)                                                                                                                                                   

 • [ ] Prometheus CR: retention: 2h, retentionSize: 50GB, enableFeatures: ["ExemplarStorage"].                                                                            
 • [ ] Thanos Sidecar: objstore.yml secret mounted.                                                                                                                       
 • [ ] ServiceMonitor for Alloy Central, OpenCost, KSM, Node Exporter.                                                                                                    

Grafana                                                                                                                                                                   

 • [ ] Datasource: Thanos (Primary), Loki (Logs), Prometheus (Local fallback).                                                                                            
 • [ ] Dashboards: Provisioned via sidecar or ConfigMap (JSON model).                                                                                                     
 • [ ] Alerting: Unified Alerting -> Contact Points (Slack/PagerDuty/Email) -> Notification Policies (Route by category: cost-optimization).                              

Network Policies (1.3 - Updates)                                                                                                                                          

 • [ ] cost-management -> monitoring (Prometheus API 9090).                                                                                                               
 • [ ] monitoring (Alloy Central) -> cost-management (OpenCost 9003).                                                                                                     
 • [ ] monitoring (Thanos Query) -> monitoring (Prometheus Sidecar 10901, Thanos Store gRPC).                                                                             
 • [ ] observability (Grafana) -> monitoring (Thanos Query 10902, Loki Query 3100).                                                                                       

--------------------------------------------------------------------------------------------------------------------------------------------------------------------------


Success Criteria (KPIs)                                                                                                                                                   

 1 Coverage: 100% Namespaces have cost_center label; 100% Pods have Requests/Limits.                                                                                      
 2 Accuracy: Monthly Cloud Bill vs Platform Showback variance < 5%.                                                                                                       
 3 Actionability: > 80% of "Rightsizing Recommendations" reviewed/acted upon monthly.                                                                                     
 4 Waste Reduction: 20% reduction in Idle Pod count & Over-provisioned CPU cores within 90 days.                                                                          
 5 MTTR: < 15 min to identify cost spike root cause (Namespace/Workload/Label) via Dashboard 1