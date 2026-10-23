package api

import (
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/parser"
)

// QueryRequest represents a query request
type QueryRequest struct {
	Query string `json:"query" form:"query"`
	Time  string `json:"time" form:"time"`
}

// QueryRangeRequest represents a range query request
type QueryRangeRequest struct {
	Query string `json:"query" form:"query"`
	Start string `json:"start" form:"start"`
	End   string `json:"end" form:"end"`
	Step  string `json:"step" form:"step"`
}

// SeriesRequest represents a series discovery request
type SeriesRequest struct {
	Match []string `json:"match[]" form:"match[]"`
	Start string   `json:"start" form:"start"`
	End   string   `json:"end" form:"end"`
}

// LabelsRequest represents a label names request
type LabelsRequest struct {
	Start string `json:"start" form:"start"`
	End   string `json:"end" form:"end"`
}

// LabelValuesRequest represents a label values request
type LabelValuesRequest struct {
	Name  string `json:"name" form:"name"`
	Start string `json:"start" form:"start"`
	End   string `json:"end" form:"end"`
}

// QueryResult represents a query result
type QueryResult struct {
	ResultType parser.ValueType `json:"resultType"`
	Result     parser.Value     `json:"result"`
}

// SeriesResult represents a series discovery result
type SeriesResult struct {
	Series []labels.Labels `json:"series"`
}

// LabelsResult represents a label names result
type LabelsResult struct {
	Names []string `json:"names"`
}

// LabelValuesResult represents a label values result
type LabelValuesResult struct {
	Values []string `json:"values"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error     string `json:"error"`
	ErrorType string `json:"errorType"`
}

// HealthResponse represents a health check response
type HealthResponse struct {
	Status string `json:"status"`
}

// ApplyRecommendationRequest represents an apply recommendation request
type ApplyRecommendationRequest struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// ApplyRecommendationResponse represents an apply recommendation response
type ApplyRecommendationResponse struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// DashboardOverview represents the dashboard overview data
type DashboardOverview struct {
	TotalHourlyCostUSD    float64       `json:"totalHourlyCostUSD"`
	TotalMonthlyCostUSD   float64       `json:"totalMonthlyCostUSD"`
	IdleCostUSD           float64       `json:"idleCostUSD"`
	EfficiencyRatio       float64       `json:"efficiencyRatio"`
	TopWorkloads          []WorkloadCost `json:"topWorkloads"`
	CostTrend             []CostPoint    `json:"costTrend"`
	NamespaceCount        int           `json:"namespaceCount"`
	WorkloadCount         int           `json:"workloadCount"`
	NodeCount             int           `json:"nodeCount"`
	LastUpdate            time.Time     `json:"lastUpdate"`
}

type WorkloadCost struct {
	Namespace  string  `json:"namespace"`
	Workload   string  `json:"workload"`
	Kind       string  `json:"kind"`
	CostCenter string  `json:"costCenter"`
	Team       string  `json:"team"`
	HourlyCost float64 `json:"hourlyCostUSD"`
	Efficiency float64 `json:"efficiencyRatio"`
}

type CostPoint struct {
	Timestamp time.Time `json:"timestamp"`
	CostUSD   float64   `json:"costUSD"`
}

// WorkloadsResponse represents the workloads list response
type WorkloadsResponse struct {
	Workloads []WorkloadDetail `json:"workloads"`
	Total     int              `json:"total"`
	Page      int              `json:"page"`
	PageSize  int              `json:"pageSize"`
}

type WorkloadDetail struct {
	Namespace       string             `json:"namespace"`
	Workload        string             `json:"workload"`
	Kind            string             `json:"kind"`
	CostCenter      string             `json:"costCenter"`
	Team            string             `json:"team"`
	HourlyCostUSD   float64            `json:"hourlyCostUSD"`
	MonthlyCostUSD  float64            `json:"monthlyCostUSD"`
	CPURequested    float64            `json:"cpuCoresRequested"`
	CPUUsed         float64            `json:"cpuCoresUsed"`
	MemoryRequested int64              `json:"memoryBytesRequested"`
	MemoryUsed      int64              `json:"memoryBytesUsed"`
	EfficiencyRatio float64            `json:"efficiencyRatio"`
	Recommendation  *RecommendationInfo `json:"recommendation,omitempty"`
	LastUpdate      time.Time          `json:"lastUpdate"`
}

type RecommendationInfo struct {
	RecommendedCPU    float64 `json:"recommendedCPU"`
	RecommendedMemory int64   `json:"recommendedMemory"`
	EstimatedSavings  float64 `json:"estimatedMonthlySavingsUSD"`
	Status            string  `json:"status"`
}

// NodesResponse represents the nodes list response
type NodesResponse struct {
	Nodes []NodeDetail `json:"nodes"`
}

type NodeDetail struct {
	NodeName            string  `json:"nodeName"`
	InstanceType        string  `json:"instanceType"`
	Zone                string  `json:"zone"`
	CostCenter          string  `json:"costCenter"`
	HourlyCostUSD       float64 `json:"hourlyCostUSD"`
	CPUUtilization      float64 `json:"cpuUtilizationRatio"`
	MemoryUtilization   float64 `json:"memoryUtilizationRatio"`
	CPUAllocatable      float64 `json:"cpuAllocatable"`
	MemoryAllocatable   int64   `json:"memoryAllocatable"`
	PodCount            int     `json:"podCount"`
	IsSpot              bool    `json:"isSpot"`
	LastUpdate          time.Time `json:"lastUpdate"`
}

// StorageResponse represents the storage list response
type StorageResponse struct {
	PVCs []PVCDetail `json:"pvcs"`
}

type PVCDetail struct {
	Namespace      string  `json:"namespace"`
	PVCName        string  `json:"pvcName"`
	StorageClass   string  `json:"storageClass"`
	CostCenter     string  `json:"costCenter"`
	RequestedGB    float64 `json:"requestedGB"`
	UsedGB         float64 `json:"usedGB"`
	UtilizationPct float64 `json:"utilizationPct"`
	MonthlyCostUSD float64 `json:"monthlyCostUSD"`
	Phase          string  `json:"phase"`
	IsOrphaned     bool    `json:"isOrphaned"`
	LastUpdate     time.Time `json:"lastUpdate"`
}

// NetworkResponse represents the network response
type NetworkResponse struct {
	ByNamespace []NamespaceNetwork `json:"byNamespace"`
	TopTalkers  []NetworkTalker    `json:"topTalkers"`
}

type NamespaceNetwork struct {
	Namespace  string  `json:"namespace"`
	CostCenter string  `json:"costCenter"`
	EgressGB   float64 `json:"egressGB"`
	CostUSD    float64 `json:"costUSD"`
}

type NetworkTalker struct {
	Namespace string  `json:"namespace"`
	Workload  string  `json:"workload"`
	EgressGB  float64 `json:"egressGB"`
	CostUSD   float64 `json:"costUSD"`
}

// RecommendationsResponse represents the recommendations response
type RecommendationsResponse struct {
	Recommendations []RecommendationDetail `json:"recommendations"`
	Total           int                    `json:"total"`
}

type RecommendationDetail struct {
	Name              string    `json:"name"`
	Namespace         string    `json:"namespace"`
	Workload          string    `json:"workload"`
	Kind              string    `json:"kind"`
	CurrentCPU        float64   `json:"currentCPU"`
	CurrentMemory     int64     `json:"currentMemory"`
	RecommendedCPU    float64   `json:"recommendedCPU"`
	RecommendedMemory int64     `json:"recommendedMemory"`
	EstimatedSavings  float64   `json:"estimatedMonthlySavingsUSD"`
	Confidence        string    `json:"confidence"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"createdAt"`
	ApprovedAt        *time.Time `json:"approvedAt,omitempty"`
}

// BudgetsResponse represents the budgets response
type BudgetsResponse struct {
	Budgets []BudgetDetail `json:"budgets"`
}

type BudgetDetail struct {
	Namespace         string  `json:"namespace"`
	CostCenter        string  `json:"costCenter"`
	Team              string  `json:"team"`
	BudgetLimitUSD    float64 `json:"budgetLimitUSD"`
	CurrentSpendUSD   float64 `json:"currentSpendUSD"`
	ProjectedSpendUSD float64 `json:"projectedSpendUSD"`
	UtilizationPct    float64 `json:"utilizationPct"`
	Status            string  `json:"status"`
	Period            string  `json:"period"`
}
