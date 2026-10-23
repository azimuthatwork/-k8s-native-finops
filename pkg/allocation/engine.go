package allocation

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/yourorg/kube-native-finops/pkg/clients"
	"github.com/yourorg/kube-native-finops/pkg/storage"
	finopsv1alpha1 "github.com/yourorg/kube-native-finops/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

// Engine orchestrates the cost allocation process
type Engine struct {
	clients       *clients.ClientFactory
	tsdb          *storage.TSDB
	namespaceCache *NamespaceCache
	nodeCache     *NodeCache
	pvcCache      *PVCCache
	pricingCache  *PricingCache
	metrics       *EngineMetrics
	mu            sync.RWMutex
}

// EngineMetrics holds metrics for the allocation engine
type EngineMetrics struct {
	ReconcileDuration    *prometheus.HistogramVec
	ReconcileTotal       prometheus.Counter
	ReconcileErrors      prometheus.Counter
	WorkloadsProcessed   prometheus.Counter
	NodesProcessed       prometheus.Counter
	PVCsProcessed        prometheus.Counter
	AllocationErrors     *prometheus.CounterVec
}

// NewEngineMetrics creates new engine metrics
func NewEngineMetrics(reg prometheus.Registerer) *EngineMetrics {
	m := &EngineMetrics{
		ReconcileDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "finops_allocation_reconcile_duration_seconds",
			Help:    "Duration of allocation reconciliation",
			Buckets: prometheus.ExponentialBuckets(0.1, 2, 10),
		}, []string{"type", "status"}),
		ReconcileTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "finops_allocation_reconcile_total",
			Help: "Total number of allocation reconciliations",
		}),
		ReconcileErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "finops_allocation_reconcile_errors_total",
			Help: "Total number of allocation reconciliation errors",
		}),
		WorkloadsProcessed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "finops_allocation_workloads_processed_total",
			Help: "Total number of workloads processed",
		}),
		NodesProcessed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "finops_allocation_nodes_processed_total",
			Help: "Total number of nodes processed",
		}),
		PVCsProcessed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "finops_allocation_pvcs_processed_total",
			Help: "Total number of PVCs processed",
		}),
		AllocationErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "finops_allocation_errors_total",
			Help: "Total number of allocation errors by type",
		}, []string{"type"}),
	}

	if reg != nil {
		reg.MustRegister(
			m.ReconcileDuration,
			m.ReconcileTotal,
			m.ReconcileErrors,
			m.WorkloadsProcessed,
			m.NodesProcessed,
			m.PVCsProcessed,
			m.AllocationErrors,
		)
	}

	return m
}

// NewEngine creates a new allocation engine
func NewEngine(clients *clients.ClientFactory, tsdb *storage.TSDB, reg prometheus.Registerer) *Engine {
	return &Engine{
		clients:        clients,
		tsdb:           tsdb,
		namespaceCache: NewNamespaceCache(),
		nodeCache:      NewNodeCache(),
		pvcCache:       NewPVCCache(),
		pricingCache:   NewPricingCache(),
		metrics:        NewEngineMetrics(reg),
	}
}

// ReconcileAll runs a full reconciliation cycle
func (e *Engine) ReconcileAll(ctx context.Context) error {
	start := time.Now()
	defer func() {
		e.metrics.ReconcileDuration.WithLabelValues("full", "success").Observe(time.Since(start).Seconds())
		e.metrics.ReconcileTotal.Inc()
	}()

	// Refresh caches
	if err := e.refreshCaches(ctx); err != nil {
		e.metrics.ReconcileErrors.Inc()
		e.metrics.ReconcileDuration.WithLabelValues("full", "error").Observe(time.Since(start).Seconds())
		return fmt.Errorf("failed to refresh caches: %w", err)
	}

	// Calculate node costs
	if err := e.reconcileNodeCosts(ctx); err != nil {
		klog.ErrorS(err, "Failed to reconcile node costs")
		e.metrics.AllocationErrors.WithLabelValues("node_cost").Inc()
	}

	// Calculate workload costs
	if err := e.reconcileWorkloadCosts(ctx); err != nil {
		klog.ErrorS(err, "Failed to reconcile workload costs")
		e.metrics.AllocationErrors.WithLabelValues("workload_cost").Inc()
	}

	// Calculate PVC costs
	if err := e.reconcilePVCCosts(ctx); err != nil {
		klog.ErrorS(err, "Failed to reconcile PVC costs")
		e.metrics.AllocationErrors.WithLabelValues("pvc_cost").Inc()
	}

	// Write metrics to TSDB
	if err := e.writeMetricsToTSDB(ctx); err != nil {
		klog.ErrorS(err, "Failed to write metrics to TSDB")
		e.metrics.AllocationErrors.WithLabelValues("tsdb_write").Inc()
	}

	return nil
}

// refreshCaches refreshes all internal caches
func (e *Engine) refreshCaches(ctx context.Context) error {
	// Refresh namespace cache with labels
	namespaces, err := e.clients.K8sInformerCache().GetNamespaces()
	if err != nil {
		return err
	}
	e.namespaceCache.Update(namespaces)

	// Refresh node cache
	nodes, err := e.clients.K8sInformerCache().GetNodes()
	if err != nil {
		return err
	}
	e.nodeCache.Update(nodes)

	// Refresh PVC cache
	pvcs, err := e.clients.K8sInformerCache().GetPVCs()
	if err != nil {
		return err
	}
	e.pvcCache.Update(pvcs)

	// Refresh pricing
	if err := e.pricingCache.Refresh(ctx, e.clients.CloudPricingClient()); err != nil {
		klog.Warning("Failed to refresh pricing cache", "error", err)
	}

	// Update namespace cache in TSDB ingester for label enrichment
	if e.tsdb != nil && e.tsdb.Ingester() != nil {
		for _, ns := range namespaces {
			e.tsdb.Ingester().UpdateNamespaceCache(ns.Name, ns.Labels)
		}
	}

	return nil
}

// reconcileNodeCosts calculates and updates node costs
func (e *Engine) reconcileNodeCosts(ctx context.Context) error {
	nodes := e.nodeCache.GetAll()
	e.metrics.NodesProcessed.Add(float64(len(nodes)))

	for _, node := range nodes {
		cost, err := e.calculateNodeCost(node)
		if err != nil {
			return fmt.Errorf("failed to calculate cost for node %s: %w", node.Name, err)
		}

		// Update NodeCost CRD status
		if err := e.updateNodeCostStatus(ctx, node, cost); err != nil {
			klog.ErrorS(err, "Failed to update NodeCost status", "node", node.Name)
		}

		// Write to TSDB
		e.writeNodeMetrics(node, cost)
	}

	return nil
}

// calculateNodeCost calculates the hourly cost for a node
func (e *Engine) calculateNodeCost(node *NodeInfo) (*NodeCost, error) {
	pricing := e.pricingCache.Get(node.InstanceType, node.Zone)
	if pricing == nil {
		return nil, fmt.Errorf("no pricing found for instance type %s in zone %s", node.InstanceType, node.Zone)
	}

	hourlyPrice := pricing.HourlyPrice
	if node.IsSpot {
		hourlyPrice = pricing.SpotPrice
	}

	allocatableCPU := resource.NewMilliQuantity(node.AllocatableCPU, resource.DecimalSI)
	allocatableMem := resource.NewQuantity(node.AllocatableMemory, resource.BinarySI)

	allocatedCPU := resource.NewMilliQuantity(node.AllocatedCPU, resource.DecimalSI)
	allocatedMem := resource.NewQuantity(node.AllocatedMemory, resource.BinarySI)

	cpuRatio := float64(allocatedCPU.MilliValue()) / float64(allocatableCPU.MilliValue())
	memRatio := float64(allocatedMem.Value()) / float64(allocatableMem.Value())

	utilizationRatio := cpuRatio
	if memRatio > utilizationRatio {
		utilizationRatio = memRatio
	}

	// Weighted share: max of CPU and Memory ratios
	weightedShare := utilizationRatio
	if weightedShare > 1.0 {
		weightedShare = 1.0
	}

	hourlyCost := hourlyPrice * weightedShare

	return &NodeCost{
		NodeName:            node.Name,
		InstanceType:        node.InstanceType,
		Zone:                node.Zone,
		HourlyPrice:         hourlyPrice,
		HourlyCost:          hourlyCost,
		CPUUtilization:      cpuRatio,
		MemoryUtilization:   memRatio,
		WeightedUtilization: weightedShare,
		IsSpot:              node.IsSpot,
		AllocatableCPU:      node.AllocatableCPU,
		AllocatableMemory:   node.AllocatableMemory,
		AllocatedCPU:        node.AllocatedCPU,
		AllocatedMemory:     node.AllocatedMemory,
	}, nil
}

// reconcileWorkloadCosts calculates and updates workload costs
func (e *Engine) reconcileWorkloadCosts(ctx context.Context) error {
	workloads := e.getWorkloads()
	e.metrics.WorkloadsProcessed.Add(float64(len(workloads)))

	for _, workload := range workloads {
		cost, err := e.calculateWorkloadCost(workload)
		if err != nil {
			klog.ErrorS(err, "Failed to calculate workload cost", "workload", workload.Name)
			continue
		}

		if err := e.updateWorkloadCostStatus(ctx, workload, cost); err != nil {
			klog.ErrorS(err, "Failed to update CostAllocation status", "workload", workload.Name)
		}

		e.writeWorkloadMetrics(workload, cost)
	}

	return nil
}

// calculateWorkloadCost calculates the cost for a workload
func (e *Engine) calculateWorkloadCost(workload *WorkloadInfo) (*WorkloadCost, error) {
	node := e.nodeCache.Get(workload.NodeName)
	if node == nil {
		return nil, fmt.Errorf("node %s not found for workload %s", workload.NodeName, workload.Name)
	}

	nodeCost := e.calculateNodeCost(node)
	if nodeCost == nil {
		return nil, fmt.Errorf("failed to calculate node cost for %s", node.Name)
	}

	// Get container resource requests
	var totalCPURequest, totalMemRequest int64
	for _, container := range workload.Containers {
		totalCPURequest += container.CPURequest
		totalMemRequest += container.MemoryRequest
	}

	// Calculate share of node cost
	nodeAllocatableCPU := resource.NewMilliQuantity(node.AllocatableCPU, resource.DecimalSI)
	nodeAllocatableMem := resource.NewQuantity(node.AllocatableMemory, resource.BinarySI)

	cpuShare := float64(totalCPURequest) / float64(nodeAllocatableCPU.MilliValue())
	memShare := float64(totalMemRequest) / float64(nodeAllocatableMem.Value())

	share := cpuShare
	if memShare > share {
		share = memShare
	}

	workloadCost := nodeCost.HourlyCost * share

	// Add GPU cost if applicable
	gpuCost := float64(0)
	for _, container := range workload.Containers {
		if container.GPUCount > 0 {
			gpuPricing := e.pricingCache.GetGPU(container.GPUType)
			if gpuPricing != nil {
				gpuCost += float64(container.GPUCount) * gpuPricing.HourlyPrice
			}
		}
	}

	// Get usage metrics
	usage := e.clients.MetricsClient().GetPodUsage(workload.Namespace, workload.PodName)

	return &WorkloadCost{
		WorkloadName:    workload.Name,
		Namespace:       workload.Namespace,
		Kind:            workload.Kind,
		HourlyCost:      workloadCost + gpuCost,
		CPURequest:      totalCPURequest,
		MemoryRequest:   totalMemRequest,
		CPUUsage:        usage.CPUUsage,
		MemoryUsage:     usage.MemoryUsage,
		GPUCost:         gpuCost,
		EfficiencyRatio: e.calculateEfficiency(usage.CPUUsage, totalCPURequest, usage.MemoryUsage, totalMemRequest),
		CostCenter:      workload.Labels["cost-center"],
		Team:            workload.Labels["team"],
	}, nil
}

// calculateEfficiency calculates the efficiency ratio
func (e *Engine) calculateEfficiency(cpuUsed, cpuRequested, memUsed, memRequested int64) float64 {
	if cpuRequested == 0 && memRequested == 0 {
		return 0
	}

	cpuEff := float64(0)
	if cpuRequested > 0 {
		cpuEff = float64(cpuUsed) / float64(cpuRequested)
	}

	memEff := float64(0)
	if memRequested > 0 {
		memEff = float64(memUsed) / float64(memRequested)
	}

	// Return the lower of the two (bottleneck resource)
	if cpuEff > 0 && memEff > 0 {
		if cpuEff < memEff {
			return cpuEff
		}
		return memEff
	}
	if cpuEff > 0 {
		return cpuEff
	}
	return memEff
}

// reconcilePVCCosts calculates and updates PVC costs
func (e *Engine) reconcilePVCCosts(ctx context.Context) error {
	pvcs := e.pvcCache.GetAll()
	e.metrics.PVCsProcessed.Add(float64(len(pvcs)))

	for _, pvc := range pvcs {
		cost, err := e.calculatePVCCost(pvc)
		if err != nil {
			klog.ErrorS(err, "Failed to calculate PVC cost", "pvc", pvc.Name)
			continue
		}

		if err := e.updatePVCCostStatus(ctx, pvc, cost); err != nil {
			klog.ErrorS(err, "Failed to update PVCost status", "pvc", pvc.Name)
		}

		e.writePVCMetrics(pvc, cost)
	}

	return nil
}

// calculatePVCCost calculates the monthly cost for a PVC
func (e *Engine) calculatePVCCost(pvc *PVCInfo) (*PVCCost, error) {
	sc := e.getStorageClass(pvc.StorageClass)
	if sc == nil {
		return nil, fmt.Errorf("storage class %s not found", pvc.StorageClass)
	}

	pricePerGBMonth := sc.PricePerGBMonth
	requestedGB := float64(pvc.RequestedBytes) / (1024 * 1024 * 1024)
	monthlyCost := requestedGB * pricePerGBMonth

	// Get usage if available
	usedGB := float64(0)
	if pvc.UsedBytes > 0 {
		usedGB = float64(pvc.UsedBytes) / (1024 * 1024 * 1024)
	}

	utilization := float64(0)
	if requestedGB > 0 {
		utilization = usedGB / requestedGB
	}

	namespace := e.namespaceCache.Get(pvc.Namespace)
	costCenter := ""
	team := ""
	if namespace != nil {
		costCenter = namespace.Labels["cost-center"]
		team = namespace.Labels["team"]
	}

	return &PVCCost{
		PVCName:      pvc.Name,
		Namespace:    pvc.Namespace,
		StorageClass: pvc.StorageClass,
		MonthlyCost:  monthlyCost,
		RequestedGB:  requestedGB,
		UsedGB:       usedGB,
		Utilization:  utilization,
		CostCenter:   costCenter,
		Team:         team,
	}, nil
}

// writeMetricsToTSDB writes all calculated metrics to the TSDB
func (e *Engine) writeMetricsToTSDB(ctx context.Context) error {
	// This is called after all reconciliations
	// The individual write methods (writeNodeMetrics, writeWorkloadMetrics, writePVCMetrics)
	// are called during reconciliation
	return nil
}

// writeNodeMetrics writes node metrics to TSDB
func (e *Engine) writeNodeMetrics(node *NodeInfo, cost *NodeCost) {
	labels := map[string]string{
		"node":           node.Name,
		"instance_type":  node.InstanceType,
		"zone":           node.Zone,
		"cost_center":    node.Labels["cost-center"],
	}

	samples := []prometheus.Metric{
		NewGaugeMetric("finops_node_hourly_cost_usd", labels, cost.HourlyCost),
		NewGaugeMetric("finops_node_cpu_utilization_ratio", labels, cost.CPUUtilization),
		NewGaugeMetric("finops_node_memory_utilization_ratio", labels, cost.MemoryUtilization),
	}

	if err := e.tsdb.Ingester().Write(samples); err != nil {
		klog.ErrorS(err, "Failed to write node metrics to TSDB", "node", node.Name)
	}
}

// writeWorkloadMetrics writes workload metrics to TSDB
func (e *Engine) writeWorkloadMetrics(workload *WorkloadInfo, cost *WorkloadCost) {
	labels := map[string]string{
		"namespace":   workload.Namespace,
		"workload":    workload.Name,
		"kind":        workload.Kind,
		"cost_center": cost.CostCenter,
		"team":        cost.Team,
	}

	samples := []prometheus.Metric{
		NewGaugeMetric("finops_workload_hourly_cost_usd", labels, cost.HourlyCost),
		NewGaugeMetric("finops_workload_cpu_cores_requested", labels, float64(cost.CPURequest)/1000),
		NewGaugeMetric("finops_workload_cpu_cores_used", labels, cost.CPUUsage),
		NewGaugeMetric("finops_workload_memory_bytes_requested", labels, float64(cost.MemoryRequest)),
		NewGaugeMetric("finops_workload_memory_bytes_used", labels, cost.MemoryUsage),
		NewGaugeMetric("finops_workload_efficiency_ratio", labels, cost.EfficiencyRatio),
	}

	if err := e.tsdb.Ingester().Write(samples); err != nil {
		if storage.IsCardinalityError(err) {
			klog.Warning("Cardinality limit reached, dropping workload metrics", "workload", workload.Name)
		} else {
			klog.ErrorS(err, "Failed to write workload metrics to TSDB", "workload", workload.Name)
		}
	}
}

// writePVCMetrics writes PVC metrics to TSDB
func (e *Engine) writePVCMetrics(pvc *PVCInfo, cost *PVCCost) {
	labels := map[string]string{
		"namespace":       pvc.Namespace,
		"pvc":             pvc.Name,
		"storageclass":    pvc.StorageClass,
		"cost_center":     cost.CostCenter,
		"team":            cost.Team,
	}

	samples := []prometheus.Metric{
		NewGaugeMetric("finops_pvc_monthly_cost_usd", labels, cost.MonthlyCost),
		NewGaugeMetric("finops_pvc_requested_gib", labels, cost.RequestedGB),
		NewGaugeMetric("finops_pvc_used_gib", labels, cost.UsedGB),
		NewGaugeMetric("finops_pvc_utilization_ratio", labels, cost.Utilization),
	}

	if err := e.tsdb.Ingester().Write(samples); err != nil {
		if storage.IsCardinalityError(err) {
			klog.Warning("Cardinality limit reached, dropping PVC metrics", "pvc", pvc.Name)
		} else {
			klog.ErrorS(err, "Failed to write PVC metrics to TSDB", "pvc", pvc.Name)
		}
	}
}

// updateNodeCostStatus updates the NodeCost CRD status
func (e *Engine) updateNodeCostStatus(ctx context.Context, node *NodeInfo, cost *NodeCost) error {
	// Implementation would update the NodeCost CRD
	return nil
}

// updateWorkloadCostStatus updates the CostAllocation CRD status
func (e *Engine) updateWorkloadCostStatus(ctx context.Context, workload *WorkloadInfo, cost *WorkloadCost) error {
	// Implementation would update the CostAllocation CRD
	return nil
}

// updatePVCCostStatus updates the PVCost CRD status
func (e *Engine) updatePVCCostStatus(ctx context.Context, pvc *PVCInfo, cost *PVCCost) error {
	// Implementation would update the PVCost CRD
	return nil
}

// getWorkloads returns all workloads from the cluster
func (e *Engine) getWorkloads() []*WorkloadInfo {
	// Implementation would fetch workloads from informers
	return []*WorkloadInfo{}
}

// getStorageClass returns storage class info
func (e *Engine) getStorageClass(name string) *StorageClassInfo {
	return e.pricingCache.GetStorageClass(name)
}

// NodeInfo represents node information
type NodeInfo struct {
	Name              string
	InstanceType      string
	Zone              string
	IsSpot            bool
	AllocatableCPU    int64
	AllocatableMemory int64
	AllocatedCPU      int64
	AllocatedMemory   int64
	Labels            map[string]string
}

// NodeCost represents calculated node cost
type NodeCost struct {
	NodeName            string
	InstanceType        string
	Zone                string
	HourlyPrice         float64
	HourlyCost          float64
	CPUUtilization      float64
	MemoryUtilization   float64
	WeightedUtilization float64
	IsSpot              bool
	AllocatableCPU      int64
	AllocatableMemory   int64
	AllocatedCPU        int64
	AllocatedMemory     int64
}

// WorkloadInfo represents workload information
type WorkloadInfo struct {
	Name        string
	Namespace   string
	Kind        string
	NodeName    string
	PodName     string
	Containers  []ContainerInfo
	Labels      map[string]string
}

// ContainerInfo represents container resource info
type ContainerInfo struct {
	Name         string
	CPURequest   int64
	MemoryRequest int64
	GPUCount     int
	GPUType      string
}

// WorkloadCost represents calculated workload cost
type WorkloadCost struct {
	WorkloadName    string
	Namespace       string
	Kind            string
	HourlyCost      float64
	CPURequest      int64
	MemoryRequest   int64
	CPUUsage        float64
	MemoryUsage     float64
	GPUCost         float64
	EfficiencyRatio float64
	CostCenter      string
	Team            string
}

// PVCInfo represents PVC information
type PVCInfo struct {
	Name           string
	Namespace      string
	StorageClass   string
	RequestedBytes int64
	UsedBytes      int64
	Phase          string
}

// PVCCost represents calculated PVC cost
type PVCCost struct {
	PVCName      string
	Namespace    string
	StorageClass string
	MonthlyCost  float64
	RequestedGB  float64
	UsedGB       float64
	Utilization  float64
	CostCenter   string
	Team         string
}

// StorageClassInfo represents storage class pricing
type StorageClassInfo struct {
	Name             string
	PricePerGBMonth  float64
}

// PricingInfo represents instance pricing
type PricingInfo struct {
	InstanceType string
	Zone         string
	HourlyPrice  float64
	SpotPrice    float64
	CPU          int
	MemoryGB     float64
	GPUCount     int
	GPUType      string
}

// GPUInfo represents GPU pricing
type GPUInfo struct {
	Type         string
	HourlyPrice  float64
}

// NamespaceCache caches namespace information
type NamespaceCache struct {
	namespaces map[string]*NamespaceInfo
	mu         sync.RWMutex
}

type NamespaceInfo struct {
	Name   string
	Labels map[string]string
}

func NewNamespaceCache() *NamespaceCache {
	return &NamespaceCache{
		namespaces: make(map[string]*NamespaceInfo),
	}
}

func (c *NamespaceCache) Update(namespaces []*NamespaceInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ns := range namespaces {
		c.namespaces[ns.Name] = ns
	}
}

func (c *NamespaceCache) Get(name string) *NamespaceInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.namespaces[name]
}

// NodeCache caches node information
type NodeCache struct {
	nodes map[string]*NodeInfo
	mu    sync.RWMutex
}

func NewNodeCache() *NodeCache {
	return &NodeCache{
		nodes: make(map[string]*NodeInfo),
	}
}

func (c *NodeCache) Update(nodes []*NodeInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, node := range nodes {
		c.nodes[node.Name] = node
	}
}

func (c *NodeCache) Get(name string) *NodeInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.nodes[name]
}

func (c *NodeCache) GetAll() []*NodeInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]*NodeInfo, 0, len(c.nodes))
	for _, node := range c.nodes {
		result = append(result, node)
	}
	return result
}

// PVCCache caches PVC information
type PVCCache struct {
	pvcs map[string]*PVCInfo
	mu   sync.RWMutex
}

func NewPVCCache() *PVCCache {
	return &PVCCache{
		pvcs: make(map[string]*PVCInfo),
	}
}

func (c *PVCCache) Update(pvcs []*PVCInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, pvc := range pvcs {
		key := pvc.Namespace + "/" + pvc.Name
		c.pvcs[key] = pvc
	}
}

func (c *PVCCache) Get(namespace, name string) *PVCInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.pvcs[namespace+"/"+name]
}

func (c *PVCCache) GetAll() []*PVCInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]*PVCInfo, 0, len(c.pvcs))
	for _, pvc := range c.pvcs {
		result = append(result, pvc)
	}
	return result
}

// PricingCache caches pricing information
type PricingCache struct {
	instancePricing map[string]*PricingInfo
	storagePricing  map[string]*StorageClassInfo
	gpuPricing      map[string]*GPUInfo
	mu              sync.RWMutex
}

func NewPricingCache() *PricingCache {
	return &PricingCache{
		instancePricing: make(map[string]*PricingInfo),
		storagePricing:  make(map[string]*StorageClassInfo),
		gpuPricing:      make(map[string]*GPUInfo),
	}
}

func (c *PricingCache) Get(instanceType, zone string) *PricingInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	key := instanceType + ":" + zone
	return c.instancePricing[key]
}

func (c *PricingCache) GetStorageClass(name string) *StorageClassInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.storagePricing[name]
}

func (c *PricingCache) GetGPU(gpuType string) *GPUInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.gpuPricing[gpuType]
}

func (c *PricingCache) Refresh(ctx context.Context, client clients.CloudPricingClient) error {
	// Implementation would fetch pricing from cloud provider
	return nil
}

// GaugeMetric implements prometheus.Metric for gauge values
type GaugeMetric struct {
	name   string
	labels map[string]string
	value  float64
}

func NewGaugeMetric(name string, labels map[string]string, value float64) *GaugeMetric {
	return &GaugeMetric{
		name:   name,
		labels: labels,
		value:  value,
	}
}

func (m *GaugeMetric) Desc() *prometheus.Desc {
	return prometheus.NewDesc(m.name, "", nil, nil)
}

func (m *GaugeMetric) Write(out *prometheus.Metric) error {
	lbls := make([]*prometheus.LabelPair, 0, len(m.labels))
	for k, v := range m.labels {
		lbls = append(lbls, &prometheus.LabelPair{Name: k, Value: v})
	}
	return out.Encode(prometheus.GaugeValue, m.value, lbls...)
}

func (m *GaugeMetric) Labels() map[string]string {
	return m.labels
}

func (m *GaugeMetric) Name() string {
	return m.name
}

func (m *GaugeMetric) Value() float64 {
	return m.value
}

func (m *GaugeMetric) Timestamp() int64 {
	return time.Now().UnixMilli()
}

func (m *GaugeMetric) Exemplar() prometheus.Exemplar {
	return prometheus.Exemplar{}
}
