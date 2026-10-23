package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/api/prometheus/v1"
	"go.uber.org/zap"

	"github.com/yourorg/kube-native-finops/pkg/cache"
	"github.com/yourorg/kube-native-finops/pkg/clients"
)

// DashboardHandler handles dashboard-related HTTP endpoints
type DashboardHandler struct {
	logger         *zap.Logger
	promClient     v1.API
	clientFactory  *clients.ClientFactory
	dashboardCache *cache.DashboardCache
	cacheTTL       time.Duration
}

// DashboardOverview represents the aggregated dashboard overview data
type DashboardOverview struct {
	ClusterSummary  ClusterSummary  `json:"clusterSummary"`
	CostSummary     CostSummary     `json:"costSummary"`
	WorkloadSummary WorkloadSummary `json:"workloadSummary"`
	StorageSummary  StorageSummary  `json:"storageSummary"`
	AlertSummary    AlertSummary    `json:"alertSummary"`
	LastUpdated     time.Time       `json:"lastUpdated"`
	CacheStatus     string          `json:"cacheStatus"` // "HIT", "MISS", "STALE"
}

// ClusterSummary contains cluster-level metrics
type ClusterSummary struct {
	TotalNodes        int     `json:"totalNodes"`
	ReadyNodes        int     `json:"readyNodes"`
	TotalCPU          float64 `json:"totalCPU"`
	TotalMemoryGiB    float64 `json:"totalMemoryGiB"`
	TotalGPU          int     `json:"totalGPU"`
	ClusterHourlyCost float64 `json:"clusterHourlyCostUSD"`
}

// CostSummary contains cost-related metrics
type CostSummary struct {
	TotalHourlyCostUSD      float64            `json:"totalHourlyCostUSD"`
	TotalMonthlyProjectedUSD float64           `json:"totalMonthlyProjectedUSD"`
	CostByNamespace         map[string]float64 `json:"costByNamespace"`
	CostByWorkload          map[string]float64 `json:"costByWorkload"`
	SpotSavingsUSD          float64            `json:"spotSavingsUSD"`
	SpotSavingsPercent      float64            `json:"spotSavingsPercent"`
}

// WorkloadSummary contains workload-related metrics
type WorkloadSummary struct {
	TotalWorkloads   int `json:"totalWorkloads"`
	RunningWorkloads int `json:"runningWorkloads"`
	FailedWorkloads  int `json:"failedWorkloads"`
	PendingWorkloads int `json:"pendingWorkloads"`
	TotalPods        int `json:"totalPods"`
	RunningPods      int `json:"runningPods"`
	RestartingPods   int `json:"restartingPods"`
}

// StorageSummary contains storage-related metrics
type StorageSummary struct {
	TotalPVCs           int     `json:"totalPVCs"`
	BoundPVCs           int     `json:"boundPVCs"`
	PendingPVCs         int     `json:"pendingPVCs"`
	TotalStorageGiB     float64 `json:"totalStorageGiB"`
	TotalMonthlyCostUSD float64 `json:"totalMonthlyCostUSD"`
	StorageByClass      map[string]float64 `json:"storageByClass"`
}

// AlertSummary contains alert/event-related metrics
type AlertSummary struct {
	TotalEvents       int            `json:"totalEvents"`
	CriticalEvents    int            `json:"criticalEvents"`
	WarningEvents     int            `json:"warningEvents"`
	ResolvedEvents    int            `json:"resolvedEvents"`
	EventsByType      map[string]int `json:"eventsByType"`
	EventsByNamespace map[string]int `json:"eventsByNamespace"`
}

// NewDashboardHandler creates a new dashboard handler
func NewDashboardHandler(
	logger *zap.Logger,
	promClient v1.API,
	clientFactory *clients.ClientFactory,
	dashboardCache *cache.DashboardCache,
	cacheTTL time.Duration,
) *DashboardHandler {
	return &DashboardHandler{
		logger:         logger,
		promClient:     promClient,
		clientFactory:  clientFactory,
		dashboardCache: dashboardCache,
		cacheTTL:       cacheTTL,
	}
}

// RegisterRoutes registers the dashboard routes
func (h *DashboardHandler) RegisterRoutes(r chi.Router) {
	r.Get("/overview", h.HandleOverview)
	r.Get("/overview/refresh", h.HandleOverviewRefresh)
}

// HandleOverview returns the dashboard overview with caching
func (h *DashboardHandler) HandleOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Try to get from cache first
	cachedData, cacheStatus, err := h.dashboardCache.GetOverview(ctx)
	if err == nil && cachedData != nil {
		h.logger.Debug("Dashboard overview cache hit")
		h.writeJSONResponse(w, http.StatusOK, cachedData, cacheStatus)
		return
	}

	h.logger.Debug("Dashboard overview cache miss, fetching fresh data")

	// Fetch fresh data
	overview, err := h.fetchOverviewData(ctx)
	if err != nil {
		h.logger.Error("Failed to fetch dashboard overview", zap.Error(err))
		// Try to return stale data if available
		if cachedData != nil {
			h.logger.Warn("Returning stale cached data due to fetch error")
			h.writeJSONResponse(w, http.StatusOK, cachedData, "STALE")
			return
		}
		http.Error(w, "Failed to fetch dashboard overview", http.StatusInternalServerError)
		return
	}

	// Store in cache
	if err := h.dashboardCache.SetOverview(ctx, overview, h.cacheTTL); err != nil {
		h.logger.Warn("Failed to cache dashboard overview", zap.Error(err))
	}

	h.writeJSONResponse(w, http.StatusOK, overview, "MISS")
}

// HandleOverviewRefresh forces a cache refresh and returns fresh data
func (h *DashboardHandler) HandleOverviewRefresh(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	overview, err := h.fetchOverviewData(ctx)
	if err != nil {
		h.logger.Error("Failed to fetch dashboard overview on refresh", zap.Error(err))
		http.Error(w, "Failed to fetch dashboard overview", http.StatusInternalServerError)
		return
	}

	// Update cache
	if err := h.dashboardCache.SetOverview(ctx, overview, h.cacheTTL); err != nil {
		h.logger.Warn("Failed to cache dashboard overview on refresh", zap.Error(err))
	}

	h.writeJSONResponse(w, http.StatusOK, overview, "REFRESHED")
}

// fetchOverviewData aggregates data from various sources
func (h *DashboardHandler) fetchOverviewData(ctx context.Context) (*DashboardOverview, error) {
	// Fetch all data in parallel
	type result struct {
		cluster   *ClusterSummary
		cost      *CostSummary
		workload  *WorkloadSummary
		storage   *StorageSummary
		alerts    *AlertSummary
		err       error
	}

	resultCh := make(chan result, 1)

	go func() {
		cluster, err := h.fetchClusterSummary(ctx)
		cost, costErr := h.fetchCostSummary(ctx)
		workload, workloadErr := h.fetchWorkloadSummary(ctx)
		storage, storageErr := h.fetchStorageSummary(ctx)
		alerts, alertsErr := h.fetchAlertSummary(ctx)

		// Return first error encountered
		var firstErr error
		for _, e := range []error{err, costErr, workloadErr, storageErr, alertsErr} {
			if e != nil {
				firstErr = e
				break
			}
		}

		resultCh <- result{
			cluster:  cluster,
			cost:     cost,
			workload: workload,
			storage:  storage,
			alerts:   alerts,
			err:      firstErr,
		}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-resultCh:
		if res.err != nil {
			return nil, res.err
		}
		return &DashboardOverview{
			ClusterSummary:  *res.cluster,
			CostSummary:     *res.cost,
			WorkloadSummary: *res.workload,
			StorageSummary:  *res.storage,
			AlertSummary:    *res.alerts,
			LastUpdated:     time.Now(),
			CacheStatus:     "MISS",
		}, nil
	}
}

// fetchClusterSummary fetches cluster-level metrics from Prometheus
func (h *DashboardHandler) fetchClusterSummary(ctx context.Context) (*ClusterSummary, error) {
	// Query for node count and resources
	queries := map[string]string{
		"totalNodes":       `count(kube_node_info)`,
		"readyNodes":       `count(kube_node_status_condition{condition="Ready",status="true"})`,
		"totalCPU":         `sum(kube_node_status_capacity_cpu_cores)`,
		"totalMemoryGiB":   `sum(kube_node_status_capacity_memory_bytes) / 1024 / 1024 / 1024`,
		"totalGPU":         `sum(kube_node_status_capacity{resource=~"nvidia.com/gpu|amd.com/gpu"})`,
	}

	results := make(map[string]float64)
	for name, query := range queries {
		val, err := h.queryPrometheus(ctx, query)
		if err != nil {
			h.logger.Warn("Failed to query Prometheus", zap.String("query", name), zap.Error(err))
			results[name] = 0
		} else {
			results[name] = val
		}
	}

	// Get cluster hourly cost from CostHistory or cloud pricing
	hourlyCost := h.fetchClusterHourlyCost(ctx)

	return &ClusterSummary{
		TotalNodes:        int(results["totalNodes"]),
		ReadyNodes:        int(results["readyNodes"]),
		TotalCPU:          results["totalCPU"],
		TotalMemoryGiB:    results["totalMemoryGiB"],
		TotalGPU:          int(results["totalGPU"]),
		ClusterHourlyCost: hourlyCost,
	}, nil
}

// fetchCostSummary fetches cost-related metrics
func (h *DashboardHandler) fetchCostSummary(ctx context.Context) (*CostSummary, error) {
	// Get latest CostHistory for total cost
	// Query Prometheus for cost by namespace/workload
	costByNamespace := make(map[string]float64)
	costByWorkload := make(map[string]float64)
	
	// This would typically query cost allocation metrics
	// For now, return placeholder data
	totalHourlyCost := h.fetchClusterHourlyCost(ctx)

	return &CostSummary{
		TotalHourlyCostUSD:       totalHourlyCost,
		TotalMonthlyProjectedUSD: totalHourlyCost * 24 * 30,
		CostByNamespace:          costByNamespace,
		CostByWorkload:           costByWorkload,
		SpotSavingsUSD:           0,
		SpotSavingsPercent:       0,
	}, nil
}

// fetchWorkloadSummary fetches workload metrics
func (h *DashboardHandler) fetchWorkloadSummary(ctx context.Context) (*WorkloadSummary, error) {
	queries := map[string]string{
		"totalWorkloads":   `count(kube_deployment_status_replicas + kube_statefulset_status_replicas + kube_daemonset_status_desired_number_scheduled + kube_job_info + kube_cronjob_info)`,
		"runningWorkloads": `count(kube_deployment_status_replicas_ready > 0)`,
		"totalPods":        `count(kube_pod_info)`,
		"runningPods":      `count(kube_pod_status_phase{phase="Running"})`,
		"pendingPods":      `count(kube_pod_status_phase{phase="Pending"})`,
		"failedPods":       `count(kube_pod_status_phase{phase="Failed"})`,
	}

	results := make(map[string]float64)
	for name, query := range queries {
		val, err := h.queryPrometheus(ctx, query)
		if err != nil {
			h.logger.Warn("Failed to query Prometheus", zap.String("query", name), zap.Error(err))
			results[name] = 0
		} else {
			results[name] = val
		}
	}

	return &WorkloadSummary{
		TotalWorkloads:   int(results["totalWorkloads"]),
		RunningWorkloads: int(results["runningWorkloads"]),
		FailedWorkloads:  int(results["failedPods"]),
		PendingWorkloads: int(results["pendingPods"]),
		TotalPods:        int(results["totalPods"]),
		RunningPods:      int(results["runningPods"]),
		RestartingPods:   0, // Would need additional query
	}, nil
}

// fetchStorageSummary fetches storage metrics
func (h *DashboardHandler) fetchStorageSummary(ctx context.Context) (*StorageSummary, error) {
	queries := map[string]string{
		"totalPVCs":       `count(kube_persistentvolumeclaim_info)`,
		"boundPVCs":       `count(kube_persistentvolumeclaim_status_phase{phase="Bound"})`,
		"pendingPVCs":     `count(kube_persistentvolumeclaim_status_phase{phase="Pending"})`,
		"totalStorageGiB": `sum(kube_persistentvolumeclaim_resource_requests_storage_bytes) / 1024 / 1024 / 1024`,
	}

	results := make(map[string]float64)
	for name, query := range queries {
		val, err := h.queryPrometheus(ctx, query)
		if err != nil {
			h.logger.Warn("Failed to query Prometheus", zap.String("query", name), zap.Error(err))
			results[name] = 0
		} else {
			results[name] = val
		}
	}

	// Get PVC costs from PVCost CRDs
	monthlyCost := h.fetchPVCMonthlyCost(ctx)

	return &StorageSummary{
		TotalPVCs:           int(results["totalPVCs"]),
		BoundPVCs:           int(results["boundPVCs"]),
		PendingPVCs:         int(results["pendingPVCs"]),
		TotalStorageGiB:     results["totalStorageGiB"],
		TotalMonthlyCostUSD: monthlyCost,
		StorageByClass:      make(map[string]float64),
	}, nil
}

// fetchAlertSummary fetches alert/event metrics from LogEvent CRDs
func (h *DashboardHandler) fetchAlertSummary(ctx context.Context) (*AlertSummary, error) {
	// Query LogEvent CRDs for events
	// This would use the KSM client to list LogEvents
	eventsByType := make(map[string]int)
	eventsByNamespace := make(map[string]int)

	// Placeholder - would query actual LogEvent resources
	return &AlertSummary{
		TotalEvents:       0,
		CriticalEvents:    0,
		WarningEvents:     0,
		ResolvedEvents:    0,
		EventsByType:      eventsByType,
		EventsByNamespace: eventsByNamespace,
	}, nil
}

// fetchClusterHourlyCost gets the current cluster hourly cost
func (h *DashboardHandler) fetchClusterHourlyCost(ctx context.Context) float64 {
	// Try to get from CostHistory CRD first
	// Fallback to cloud pricing client
	cloudClient := h.clientFactory.CloudPricingClient()
	if cloudClient != nil {
		// This would aggregate node prices
		// For now return 0
		return 0
	}
	return 0
}

// fetchPVCMonthlyCost gets the total monthly PVC cost
func (h *DashboardHandler) fetchPVCMonthlyCost(ctx context.Context) float64 {
	// Query PVCost CRDs
	return 0
}

// queryPrometheus executes a PromQL query and returns a float64 value
func (h *DashboardHandler) queryPrometheus(ctx context.Context, query string) (float64, error) {
	result, warnings, err := h.promClient.Query(ctx, query, time.Now())
	if err != nil {
		return 0, err
	}
	if len(warnings) > 0 {
		h.logger.Debug("Prometheus query warnings", zap.Strings("warnings", warnings))
	}

	// Parse vector result
	vector, ok := result.(v1.Vector)
	if !ok || len(vector) == 0 {
		return 0, nil
	}

	return float64(vector[0].Value), nil
}

// writeJSONResponse writes a JSON response with cache status header
func (h *DashboardHandler) writeJSONResponse(w http.ResponseWriter, statusCode int, data interface{}, cacheStatus string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Cache-Status", cacheStatus)
	w.WriteHeader(statusCode)
	
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("Failed to encode JSON response", zap.Error(err))
	}
}
