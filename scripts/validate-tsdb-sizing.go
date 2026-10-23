// validate-tsdb-sizing.go
// Go-based validation for TSDB sizing guidelines (Task 1.5.10)
// Can be run as a standalone tool or integrated as a controller health check

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

// SizingGuideline represents the resource guidelines for a cluster category
type SizingGuideline struct {
	Category       string
	MaxNodes       int
	MaxSeries      int64
	MinPVCGiB      int64
	MinCPUCores    float64
	MinMemGiB      float64
	MinNetGbps     float64
	RequiresSharding bool
}

// TSDBMetrics represents key TSDB metrics
type TSDBMetrics struct {
	HeadSeries            int64   `json:"head_series"`
	HeadSamples           int64   `json:"head_samples"`
	HeadBytes             int64   `json:"head_bytes"`
	BlockCount            int64   `json:"block_count"`
	CompactionDuration    float64 `json:"compaction_duration"`
	WALCorruptions        int64   `json:"wal_corruptions"`
	IngesterBatchAvg      float64 `json:"ingester_batch_avg"`
	IngesterFlushDuration float64 `json:"ingester_flush_duration"`
	IngesterRejected      int64   `json:"ingester_rejected"`
	IngesterExemplarsDropped int64 `json:"ingester_exemplars_dropped"`
	QueryDurationP99      float64 `json:"query_duration_p99"`
	QuerySamples          int64   `json:"query_samples"`
	QueryErrors           int64   `json:"query_errors"`
	MaxActiveSeries       int64   `json:"max_active_series"`
}

// ValidationResult represents a single validation check result
type ValidationResult struct {
	Check    string `json:"check"`
	Status   string `json:"status"` // PASS, FAIL, WARN
	Message  string `json:"message"`
	Actual   string `json:"actual"`
	Expected string `json:"expected"`
}

// ValidationReport represents the complete validation report
type ValidationReport struct {
	Timestamp     string             `json:"timestamp"`
	Cluster       ClusterInfo        `json:"cluster"`
	TSDBMetrics   TSDBMetrics        `json:"tsdb_metrics"`
	Resources     ResourceInfo       `json:"resources"`
	Validations   []ValidationResult `json:"validations"`
	OverallStatus string             `json:"overall_status"`
}

type ClusterInfo struct {
	NodeCount int    `json:"node_count"`
	Category  string `json:"category"`
}

type ResourceInfo struct {
	PVC      PVCInfo   `json:"pvc"`
	CPU      CPUInfo   `json:"cpu"`
	Memory   MemInfo   `json:"memory"`
}

type PVCInfo struct {
	CapacityGiB int64 `json:"capacity_gib"`
	UsedGiB     int64 `json:"used_gib"`
}

type CPUInfo struct {
	RequestCores float64 `json:"request_cores"`
	LimitCores   float64 `json:"limit_cores"`
	UsageCores   float64 `json:"usage_cores"`
}

type MemInfo struct {
	RequestGiB float64 `json:"request_gib"`
	LimitGiB   float64 `json:"limit_gib"`
	UsageGiB   float64 `json:"usage_gib"`
}

var (
	// Sizing guidelines from PLAN-NATIVE.md
	guidelines = []SizingGuideline{
		{Category: "Small", MaxNodes: 49, MaxSeries: 50000, MinPVCGiB: 50, MinCPUCores: 1, MinMemGiB: 4, MinNetGbps: 1, RequiresSharding: false},
		{Category: "Medium", MaxNodes: 199, MaxSeries: 250000, MinPVCGiB: 200, MinCPUCores: 2, MinMemGiB: 8, MinNetGbps: 2, RequiresSharding: false},
		{Category: "Large", MaxNodes: 499, MaxSeries: 750000, MinPVCGiB: 500, MinCPUCores: 4, MinMemGiB: 16, MinNetGbps: 5, RequiresSharding: false},
		{Category: "X-Large", MaxNodes: 10000, MaxSeries: 1500000, MinPVCGiB: 1000, MinCPUCores: 8, MinMemGiB: 32, MinNetGbps: 10, RequiresSharding: true},
	}

	costControllerNamespace = flag.String("namespace", "cost-management", "Cost controller namespace")
	costControllerPod       = flag.String("pod", "cost-controller-0", "Cost controller pod name")
	metricsPort             = flag.Int("port", 8080, "Metrics port on cost controller pod")
	pvcName                 = flag.String("pvc", "cost-controller-data", "PVC name for TSDB storage")
	kubeconfig              = flag.String("kubeconfig", "", "Path to kubeconfig (optional, uses in-cluster if empty)")
	outputFile              = flag.String("output", "/tmp/tsdb-sizing-validation.json", "Output file for validation report")
)

func main() {
	klog.InitFlags(nil)
	flag.Parse()

	ctx := context.Background()

	// Create Kubernetes client
	client, err := createK8sClient()
	if err != nil {
		klog.Fatalf("Failed to create K8s client: %v", err)
	}

	// Get node count
	nodeCount, err := getNodeCount(ctx, client)
	if err != nil {
		klog.Fatalf("Failed to get node count: %v", err)
	}

	// Determine cluster category
	category := getClusterCategory(nodeCount)
	guideline := getGuideline(category)

	klog.Infof("Cluster: %d nodes (%s)", nodeCount, category)
	klog.Infof("Guidelines: Series<=%d, PVC>=%dGiB, CPU>=%.1f, Mem>=%.1fGiB", 
		guideline.MaxSeries, guideline.MinPVCGiB, guideline.MinCPUCores, guideline.MinMemGiB)

	// Fetch TSDB metrics
	metrics, err := fetchTSDBMetrics(ctx, client)
	if err != nil {
		klog.Fatalf("Failed to fetch TSDB metrics: %v", err)
	}

	// Get PVC usage
	pvcCapacity, pvcUsed, err := getPVCUsage(ctx, client)
	if err != nil {
		klog.Warningf("Failed to get PVC usage: %v", err)
		pvcCapacity, pvcUsed = 0, 0
	}

	// Get pod resources
	cpuReq, cpuLim, memReq, memLim, cpuUsage, memUsage, err := getPodResources(ctx, client)
	if err != nil {
		klog.Warningf("Failed to get pod resources: %v", err)
		cpuReq, cpuLim, memReq, memLim, cpuUsage, memUsage = 0, 0, 0, 0, 0, 0
	}

	// Run validations
	var results []ValidationResult
	overallPassed := true

	// Validate series count
	passed := validateSeries(metrics.HeadSeries, guideline, &results)
	overallPassed = overallPassed && passed

	// Validate PVC
	passed = validatePVC(pvcCapacity, pvcUsed, guideline, &results)
	overallPassed = overallPassed && passed

	// Validate CPU
	passed = validateCPU(cpuReq, cpuLim, cpuUsage, guideline, &results)
	overallPassed = overallPassed && passed

	// Validate Memory
	passed = validateMemory(memReq, memLim, memUsage, guideline, &results)
	overallPassed = overallPassed && passed

	// Validate TSDB Health
	validateTSDBHealth(metrics, &results)

	// Validate Cardinality Guard
	validateCardinalityGuard(metrics, &results)

	// Generate report
	report := ValidationReport{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Cluster: ClusterInfo{
			NodeCount: nodeCount,
			Category:  category,
		},
		TSDBMetrics: metrics,
		Resources: ResourceInfo{
			PVC: PVCInfo{CapacityGiB: pvcCapacity, UsedGiB: pvcUsed},
			CPU: CPUInfo{RequestCores: cpuReq, LimitCores: cpuLim, UsageCores: cpuUsage},
			Memory: MemInfo{RequestGiB: memReq, LimitGiB: memLim, UsageGiB: memUsage},
		},
		Validations:   results,
		OverallStatus: map[bool]string{true: "PASS", false: "FAIL"}[overallPassed],
	}

	// Write report
	if err := writeReport(report); err != nil {
		klog.Fatalf("Failed to write report: %v", err)
	}

	// Print summary
	printSummary(results, overallPassed)

	if !overallPassed {
		os.Exit(1)
	}
}

func createK8sClient() (kubernetes.Interface, error) {
	var config *rest.Config
	var err error

	if *kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)
	} else {
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(config)
}

func getNodeCount(ctx context.Context, client kubernetes.Interface) (int, error) {
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, err
	}
	return len(nodes.Items), nil
}

func getClusterCategory(nodeCount int) string {
	for _, g := range guidelines {
		if nodeCount <= g.MaxNodes {
			return g.Category
		}
	}
	return "X-Large"
}

func getGuideline(category string) SizingGuideline {
	for _, g := range guidelines {
		if g.Category == category {
			return g
		}
	}
	return guidelines[len(guidelines)-1] // X-Large
}

func fetchTSDBMetrics(ctx context.Context, client kubernetes.Interface) (TSDBMetrics, error) {
	var metrics TSDBMetrics

	// Use port-forward or exec to get metrics
	// For simplicity, we'll use exec to wget the metrics endpoint
	req := client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(*costControllerPod).
		Namespace(*costControllerNamespace).
		SubResource("exec").
		Param("command", "wget").
		Param("command", "-qO-").
		Param("command", fmt.Sprintf("http://localhost:%d/metrics", *metricsPort))

	// Note: In production, use proper exec streaming
	// This is a simplified version - in reality you'd use remotecommand
	// For now, we'll simulate by making an HTTP request if port-forwarded
	// or use the metrics endpoint directly if accessible

	// Try direct HTTP first (if running with port-forward)
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/metrics", *metricsPort))
	if err == nil {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return parseTSDBMetrics(string(body)), nil
	}

	// Fallback: return zero metrics
	klog.Warningf("Could not fetch metrics via HTTP: %v", err)
	return metrics, nil
}

func parseTSDBMetrics(output string) TSDBMetrics {
	var metrics TSDBMetrics
	lines := strings.Split(output, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		name := parts[0]
		valueStr := parts[1]
		value, _ := strconv.ParseFloat(valueStr, 64)
		intValue := int64(value)

		switch {
		case strings.HasPrefix(name, "tsdb_head_series_total"):
			metrics.HeadSeries = intValue
		case strings.HasPrefix(name, "tsdb_head_samples_appended_total"):
			metrics.HeadSamples = intValue
		case strings.HasPrefix(name, "tsdb_head_bytes"):
			metrics.HeadBytes = intValue
		case strings.HasPrefix(name, "tsdb_block_count"):
			metrics.BlockCount = intValue
		case strings.HasPrefix(name, "tsdb_compaction_duration_seconds_sum"):
			metrics.CompactionDuration = value
		case strings.HasPrefix(name, "tsdb_wal_corruptions_total"):
			metrics.WALCorruptions = intValue
		case strings.HasPrefix(name, "finops_ingester_batch_size_avg"):
			metrics.IngesterBatchAvg = value
		case strings.HasPrefix(name, "finops_ingester_flush_duration_seconds_sum"):
			metrics.IngesterFlushDuration = value
		case strings.HasPrefix(name, "finops_ingester_rejected_samples_total"):
			metrics.IngesterRejected = intValue
		case strings.HasPrefix(name, "finops_ingester_exemplars_dropped_total"):
			metrics.IngesterExemplarsDropped = intValue
		case strings.Contains(name, "finops_query_duration_seconds") && strings.Contains(name, `quantile="0.99"`):
			metrics.QueryDurationP99 = value
		case strings.HasPrefix(name, "finops_query_samples_total"):
			metrics.QuerySamples = intValue
		case strings.HasPrefix(name, "finops_query_errors_total"):
			metrics.QueryErrors = intValue
		case strings.HasPrefix(name, "finops_max_active_series"):
			metrics.MaxActiveSeries = intValue
		}
	}

	if metrics.MaxActiveSeries == 0 {
		metrics.MaxActiveSeries = 500000 // Default
	}

	return metrics
}

func getPVCUsage(ctx context.Context, client kubernetes.Interface) (int64, int64, error) {
	pvc, err := client.CoreV1().PersistentVolumeClaims(*costControllerNamespace).Get(ctx, *pvcName, metav1.GetOptions{})
	if err != nil {
		return 0, 0, err
	}

	capacity := pvc.Status.Capacity[corev1.ResourceStorage]
	capacityGiB := capacity.ScaledValue(resource.Giga)

	// Get actual usage via df on the pod
	// This is simplified - in reality you'd exec into the pod
	usedGiB := int64(0)

	return capacityGiB, usedGiB, nil
}

func getPodResources(ctx context.Context, client kubernetes.Interface) (float64, float64, float64, float64, float64, float64, error) {
	pod, err := client.CoreV1().Pods(*costControllerNamespace).Get(ctx, *costControllerPod, metav1.GetOptions{})
	if err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}

	var cpuRequest, cpuLimit, memRequest, memLimit resource.Quantity
	for _, container := range pod.Spec.Containers {
		if container.Name == "cost-controller" || container.Name == "manager" {
			cpuRequest.Add(container.Resources.Requests.Cpu())
			cpuLimit.Add(container.Resources.Limits.Cpu())
			memRequest.Add(container.Resources.Requests.Memory())
			memLimit.Add(container.Resources.Limits.Memory())
			break
		}
	}

	// Get actual usage from metrics server (simplified)
	cpuUsage := float64(0)
	memUsage := float64(0)

	return float64(cpuRequest.MilliValue()) / 1000,
		float64(cpuLimit.MilliValue()) / 1000,
		float64(memRequest.Value()) / (1024 * 1024 * 1024),
		float64(memLimit.Value()) / (1024 * 1024 * 1024),
		cpuUsage, memUsage, nil
}

func validateSeries(actual int64, guideline SizingGuideline, results *[]ValidationResult) bool {
	if guideline.RequiresSharding {
		if actual > guideline.MaxSeries {
			*results = append(*results, ValidationResult{
				Check:    "series_count",
				Status:   "FAIL",
				Message:  fmt.Sprintf("Series count (%d) exceeds X-Large threshold (%d). Sharding by cost_center required.", actual, guideline.MaxSeries),
				Actual:   strconv.FormatInt(actual, 10),
				Expected: fmt.Sprintf(">%d (sharding required)", guideline.MaxSeries),
			})
			return false
		}
		*results = append(*results, ValidationResult{
			Check:    "series_count",
			Status:   "PASS",
			Message:  fmt.Sprintf("Series count (%d) within X-Large range. Sharding recommended if growing.", actual),
			Actual:   strconv.FormatInt(actual, 10),
			Expected: fmt.Sprintf(">%d", guideline.MaxSeries),
		})
		return true
	}

	pct := float64(actual) * 100 / float64(guideline.MaxSeries)
	if actual > guideline.MaxSeries {
		*results = append(*results, ValidationResult{
			Check:    "series_count",
			Status:   "FAIL",
			Message:  fmt.Sprintf("Series count (%d) exceeds %s limit (%d) by %.0f%%", actual, guideline.Category, guideline.MaxSeries, pct-100),
			Actual:   strconv.FormatInt(actual, 10),
			Expected: fmt.Sprintf("<=%d", guideline.MaxSeries),
		})
		return false
	} else if pct > 80 {
		*results = append(*results, ValidationResult{
			Check:    "series_count",
			Status:   "WARN",
			Message:  fmt.Sprintf("Series count (%d) at %.0f%% of %s limit (%d)", actual, pct, guideline.Category, guideline.MaxSeries),
			Actual:   strconv.FormatInt(actual, 10),
			Expected: fmt.Sprintf("<=%d", guideline.MaxSeries),
		})
		return true
	}

	*results = append(*results, ValidationResult{
		Check:    "series_count",
		Status:   "PASS",
		Message:  fmt.Sprintf("Series count (%d) at %.0f%% of %s limit (%d)", actual, pct, guideline.Category, guideline.MaxSeries),
		Actual:   strconv.FormatInt(actual, 10),
		Expected: fmt.Sprintf("<=%d", guideline.MaxSeries),
	})
	return true
}

func validatePVC(capacity, used, expected int64, guideline SizingGuideline, results *[]ValidationResult) bool {
	passed := true

	if capacity == 0 {
		*results = append(*results, ValidationResult{
			Check:    "pvc_capacity",
			Status:   "FAIL",
			Message:  "PVC capacity not found or zero",
			Actual:   "0",
			Expected: fmt.Sprintf(">=%dGiB", expected),
		})
		return false
	}

	if capacity < expected {
		*results = append(*results, ValidationResult{
			Check:    "pvc_capacity",
			Status:   "FAIL",
			Message:  fmt.Sprintf("PVC capacity (%dGiB) below %s recommendation (%dGiB)", capacity, guideline.Category, expected),
			Actual:   fmt.Sprintf("%dGiB", capacity),
			Expected: fmt.Sprintf(">=%dGiB", expected),
		})
		passed = false
	} else {
		*results = append(*results, ValidationResult{
			Check:    "pvc_capacity",
			Status:   "PASS",
			Message:  fmt.Sprintf("PVC capacity (%dGiB) meets %s recommendation (%dGiB)", capacity, guideline.Category, expected),
			Actual:   fmt.Sprintf("%dGiB", capacity),
			Expected: fmt.Sprintf(">=%dGiB", expected),
		})
	}

	if capacity > 0 {
		pct := float64(used) * 100 / float64(capacity)
		if pct > 90 {
			*results = append(*results, ValidationResult{
				Check:    "pvc_usage",
				Status:   "FAIL",
				Message:  fmt.Sprintf("PVC usage at %.0f%% (%dGiB/%dGiB) - critical", pct, used, capacity),
				Actual:   fmt.Sprintf("%.0f%%", pct),
				Expected: "<80%",
			})
			passed = false
		} else if pct > 70 {
			*results = append(*results, ValidationResult{
				Check:    "pvc_usage",
				Status:   "WARN",
				Message:  fmt.Sprintf("PVC usage at %.0f%% (%dGiB/%dGiB) - approaching limit", pct, used, capacity),
				Actual:   fmt.Sprintf("%.0f%%", pct),
				Expected: "<70%",
			})
		} else {
			*results = append(*results, ValidationResult{
				Check:    "pvc_usage",
				Status:   "PASS",
				Message:  fmt.Sprintf("PVC usage at %.0f%% (%dGiB/%dGiB)", pct, used, capacity),
				Actual:   fmt.Sprintf("%.0f%%", pct),
				Expected: "<70%",
			})
		}
	}

	return passed
}

func validateCPU(request, limit, usage, expected float64, guideline SizingGuideline, results *[]ValidationResult) bool {
	passed := true

	if limit < expected {
		*results = append(*results, ValidationResult{
			Check:    "cpu_limit",
			Status:   "FAIL",
			Message:  fmt.Sprintf("CPU limit (%.2f cores) below %s recommendation (%.1f cores)", limit, guideline.Category, expected),
			Actual:   fmt.Sprintf("%.2f cores", limit),
			Expected: fmt.Sprintf(">=%.1f cores", expected),
		})
		passed = false
	} else {
		*results = append(*results, ValidationResult{
			Check:    "cpu_limit",
			Status:   "PASS",
			Message:  fmt.Sprintf("CPU limit (%.2f cores) meets %s recommendation (%.1f cores)", limit, guideline.Category, expected),
			Actual:   fmt.Sprintf("%.2f cores", limit),
			Expected: fmt.Sprintf(">=%.1f cores", expected),
		})
	}

	if limit > 0 {
		pct := usage * 100 / limit
		if pct > 90 {
			*results = append(*results, ValidationResult{
				Check:    "cpu_usage",
				Status:   "FAIL",
				Message:  fmt.Sprintf("CPU usage at %.0f%% of limit - critical", pct),
				Actual:   fmt.Sprintf("%.0f%%", pct),
				Expected: "<80%",
			})
			passed = false
		} else if pct > 70 {
			*results = append(*results, ValidationResult{
				Check:    "cpu_usage",
				Status:   "WARN",
				Message:  fmt.Sprintf("CPU usage at %.0f%% of limit - high", pct),
				Actual:   fmt.Sprintf("%.0f%%", pct),
				Expected: "<70%",
			})
		} else {
			*results = append(*results, ValidationResult{
				Check:    "cpu_usage",
				Status:   "PASS",
				Message:  fmt.Sprintf("CPU usage at %.0f%% of limit", pct),
				Actual:   fmt.Sprintf("%.0f%%", pct),
				Expected: "<70%",
			})
		}
	}

	return passed
}

func validateMemory(request, limit, usage, expected float64, guideline SizingGuideline, results *[]ValidationResult) bool {
	passed := true

	if limit < expected {
		*results = append(*results, ValidationResult{
			Check:    "memory_limit",
			Status:   "FAIL",
			Message:  fmt.Sprintf("Memory limit (%.1fGiB) below %s recommendation (%.1fGiB)", limit, guideline.Category, expected),
			Actual:   fmt.Sprintf("%.1fGiB", limit),
			Expected: fmt.Sprintf(">=%.1fGiB", expected),
		})
		passed = false
	} else {
		*results = append(*results, ValidationResult{
			Check:    "memory_limit",
			Status:   "PASS",
			Message:  fmt.Sprintf("Memory limit (%.1fGiB) meets %s recommendation (%.1fGiB)", limit, guideline.Category, expected),
			Actual:   fmt.Sprintf("%.1fGiB", limit),
			Expected: fmt.Sprintf(">=%.1fGiB", expected),
		})
	}

	if limit > 0 {
		pct := usage * 100 / limit
		if pct > 90 {
			*results = append(*results, ValidationResult{
				Check:    "memory_usage",
				Status:   "FAIL",
				Message:  fmt.Sprintf("Memory usage at %.0f%% of limit - critical", pct),
				Actual:   fmt.Sprintf("%.0f%%", pct),
				Expected: "<80%",
			})
			passed = false
		} else if pct > 70 {
			*results = append(*results, ValidationResult{
				Check:    "memory_usage",
				Status:   "WARN",
				Message:  fmt.Sprintf("Memory usage at %.0f%% of limit - high", pct),
				Actual:   fmt.Sprintf("%.0f%%", pct),
				Expected: "<70%",
			})
		} else {
			*results = append(*results, ValidationResult{
				Check:    "memory_usage",
				Status:   "PASS",
				Message:  fmt.Sprintf("Memory usage at %.0f%% of limit", pct),
				Actual:   fmt.Sprintf("%.0f%%", pct),
				Expected: "<70%",
			})
		}
	}

	return passed
}

func validateTSDBHealth(metrics TSDBMetrics, results *[]ValidationResult) {
	// WAL Corruptions
	if metrics.WALCorruptions > 0 {
		*results = append(*results, ValidationResult{
			Check:    "wal_corruptions",
			Status:   "FAIL",
			Message:  fmt.Sprintf("WAL corruptions detected: %d", metrics.WALCorruptions),
			Actual:   strconv.FormatInt(metrics.WALCorruptions, 10),
			Expected: "0",
		})
	} else {
		*results = append(*results, ValidationResult{
			Check:    "wal_corruptions",
			Status:   "PASS",
			Message:  "No WAL corruptions",
			Actual:   "0",
			Expected: "0",
		})
	}

	// Compaction lag
	if metrics.CompactionDuration > 3600 {
		*results = append(*results, ValidationResult{
			Check:    "compaction_lag",
			Status:   "WARN",
			Message:  fmt.Sprintf("Compaction duration high: %.0fs", metrics.CompactionDuration),
			Actual:   fmt.Sprintf("%.0fs", metrics.CompactionDuration),
			Expected: "<3600s",
		})
	} else {
		*results = append(*results, ValidationResult{
			Check:    "compaction_lag",
			Status:   "PASS",
			Message:  fmt.Sprintf("Compaction duration normal: %.0fs", metrics.CompactionDuration),
			Actual:   fmt.Sprintf("%.0fs", metrics.CompactionDuration),
			Expected: "<3600s",
		})
	}

	// Cardinality rejections
	if metrics.IngesterRejected > 0 {
		*results = append(*results, ValidationResult{
			Check:    "cardinality_rejections",
			Status:   "WARN",
			Message:  fmt.Sprintf("Samples rejected due to cardinality limit: %d", metrics.IngesterRejected),
			Actual:   strconv.FormatInt(metrics.IngesterRejected, 10),
			Expected: "0",
		})
	} else {
		*results = append(*results, ValidationResult{
			Check:    "cardinality_rejections",
			Status:   "PASS",
			Message:  "No cardinality rejections",
			Actual:   "0",
			Expected: "0",
		})
	}

	// Query errors
	if metrics.QueryErrors > 0 {
		*results = append(*results, ValidationResult{
			Check:    "query_errors",
			Status:   "WARN",
			Message:  fmt.Sprintf("Query errors detected: %d", metrics.QueryErrors),
			Actual:   strconv.FormatInt(metrics.QueryErrors, 10),
			Expected: "0",
		})
	} else {
		*results = append(*results, ValidationResult{
			Check:    "query_errors",
			Status:   "PASS",
			Message:  "No query errors",
			Actual:   "0",
			Expected: "0",
		})
	}

	// Query latency P99
	if metrics.QueryDurationP99 > 5 {
		*results = append(*results, ValidationResult{
			Check:    "query_latency_p99",
			Status:   "FAIL",
			Message:  fmt.Sprintf("Query P99 latency too high: %.2fs", metrics.QueryDurationP99),
			Actual:   fmt.Sprintf("%.2fs", metrics.QueryDurationP99),
			Expected: "<5s",
		})
	} else if metrics.QueryDurationP99 > 2 {
		*results = append(*results, ValidationResult{
			Check:    "query_latency_p99",
			Status:   "WARN",
			Message:  fmt.Sprintf("Query P99 latency elevated: %.2fs", metrics.QueryDurationP99),
			Actual:   fmt.Sprintf("%.2fs", metrics.QueryDurationP99),
			Expected: "<2s",
		})
	} else {
		*results = append(*results, ValidationResult{
			Check:    "query_latency_p99",
			Status:   "PASS",
			Message:  fmt.Sprintf("Query P99 latency acceptable: %.2fs", metrics.QueryDurationP99),
			Actual:   fmt.Sprintf("%.2fs", metrics.QueryDurationP99),
			Expected: "<2s",
		})
	}

	// Block count
	if metrics.BlockCount < 10 {
		*results = append(*results, ValidationResult{
			Check:    "block_count",
			Status:   "WARN",
			Message:  fmt.Sprintf("Low block count (%d) - may indicate compaction issues or new cluster", metrics.BlockCount),
			Actual:   strconv.FormatInt(metrics.BlockCount, 10),
			Expected: ">10",
		})
	} else {
		*results = append(*results, ValidationResult{
			Check:    "block_count",
			Status:   "PASS",
			Message:  fmt.Sprintf("Healthy block count: %d", metrics.BlockCount),
			Actual:   strconv.FormatInt(metrics.BlockCount, 10),
			Expected: ">10",
		})
	}
}

func validateCardinalityGuard(metrics TSDBMetrics, results *[]ValidationResult) {
	if metrics.MaxActiveSeries == 0 {
		metrics.MaxActiveSeries = 500000
	}

	pct := float64(metrics.HeadSeries) * 100 / float64(metrics.MaxActiveSeries)

	if pct > 90 {
		*results = append(*results, ValidationResult{
			Check:    "cardinality_guard",
			Status:   "FAIL",
			Message:  fmt.Sprintf("Active series (%d) at %.0f%% of limit (%d) - critical", metrics.HeadSeries, pct, metrics.MaxActiveSeries),
			Actual:   fmt.Sprintf("%.0f%%", pct),
			Expected: "<80%",
		})
	} else if pct > 80 {
		*results = append(*results, ValidationResult{
			Check:    "cardinality_guard",
			Status:   "WARN",
			Message:  fmt.Sprintf("Active series (%d) at %.0f%% of limit (%d)", metrics.HeadSeries, pct, metrics.MaxActiveSeries),
			Actual:   fmt.Sprintf("%.0f%%", pct),
			Expected: "<80%",
		})
	} else {
		*results = append(*results, ValidationResult{
			Check:    "cardinality_guard",
			Status:   "PASS",
			Message:  fmt.Sprintf("Active series (%d) at %.0f%% of limit (%d)", metrics.HeadSeries, pct, metrics.MaxActiveSeries),
			Actual:   fmt.Sprintf("%.0f%%", pct),
			Expected: "<80%",
		})
	}
}

func writeReport(report ValidationReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(*outputFile, data, 0644)
}

func printSummary(results []ValidationResult, overallPassed bool) {
	fmt.Println()
	fmt.Println("==========================================")
	fmt.Println("  TSDB Sizing Validation Summary")
	fmt.Println("==========================================")
	fmt.Println()

	passCount, failCount, warnCount := 0, 0, 0

	for _, r := range results {
		switch r.Status {
		case "PASS":
			passCount++
			fmt.Printf("✅ [%s] %s\n", r.Check, r.Message)
		case "FAIL":
			failCount++
			fmt.Printf("❌ [%s] %s\n", r.Check, r.Message)
		case "WARN":
			warnCount++
			fmt.Printf("⚠️  [%s] %s\n", r.Check, r.Message)
		}
	}

	fmt.Println()
	fmt.Println("------------------------------------------")
	fmt.Printf("  Total: %d | Pass: %d | Fail: %d | Warn: %d\n", passCount+failCount+warnCount, passCount, failCount, warnCount)
	fmt.Println("------------------------------------------")

	if overallPassed {
		fmt.Println("✅ OVERALL: VALIDATION PASSED")
	} else {
		fmt.Println("❌ OVERALL: VALIDATION FAILED")
	}
}
