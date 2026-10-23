// validate-query-performance.go - TSDB Query Performance Validation Tool
// Part of Phase 2 Task 2.1.2
// Validates query performance against thresholds from PLAN-NATIVE.md

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultNamespace     = "cost-management"
	DefaultPodName       = "cost-controller-0"
	DefaultAPIPort       = 8082
	DefaultInstantThresholdMS  = 500
	DefaultRangeThresholdMS    = 2000
	DefaultSeriesThresholdMS   = 1000
)

var (
	namespace         = flag.String("namespace", DefaultNamespace, "Kubernetes namespace")
	podName           = flag.String("pod", DefaultPodName, "Pod name")
	apiPort           = flag.Int("port", DefaultAPIPort, "API port")
	instantThreshold  = flag.Int("instant-threshold", DefaultInstantThresholdMS, "Instant query threshold (ms)")
	rangeThreshold    = flag.Int("range-threshold", DefaultRangeThresholdMS, "Range query threshold (ms)")
	seriesThreshold   = flag.Int("series-threshold", DefaultSeriesThresholdMS, "Series discovery threshold (ms)")
	concurrent        = flag.Int("concurrent", 5, "Number of concurrent queries to test")
	outputFormat      = flag.String("output", "json", "Output format: json, text")
	help              = flag.Bool("help", false, "Show help")
)

type CheckResult struct {
	Check       string  `json:"check"`
	Status      string  `json:"status"` // PASS, WARN, FAIL
	Message     string  `json:"message"`
	ValueMS     int64   `json:"value_ms"`
	ThresholdMS int64   `json:"threshold_ms"`
}

type ValidationReport struct {
	Timestamp  string        `json:"timestamp"`
	Thresholds Thresholds    `json:"thresholds"`
	Checks     []CheckResult `json:"checks"`
	Summary    Summary       `json:"summary"`
}

type Thresholds struct {
	InstantQueryMS     int `json:"instant_query_ms"`
	RangeQueryMS       int `json:"range_query_ms"`
	SeriesDiscoveryMS  int `json:"series_discovery_ms"`
}

type Summary struct {
	Pass int `json:"pass"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
}

type QueryResult struct {
	Status string      `json:"status"`
	Data   interface{} `json:"data"`
}

func main() {
	flag.Parse()
	
	if *help {
		flag.Usage()
		os.Exit(0)
	}
	
	ctx := context.Background()
	
	// Build base URL
	baseURL := fmt.Sprintf("http://localhost:%d/api/v1", *apiPort)
	
	// Test connectivity
	if !testConnectivity(ctx, baseURL) {
		fmt.Fprintf(os.Stderr, "Error: Cannot connect to API at %s\n", baseURL)
		fmt.Fprintf(os.Stderr, "Please ensure port-forward is established:\n")
		fmt.Fprintf(os.Stderr, "  kubectl port-forward -n %s pod/%s %d:%d\n", *namespace, *podName, *apiPort, *apiPort)
		os.Exit(1)
	}
	
	report := &ValidationReport{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Thresholds: Thresholds{
			InstantQueryMS:    *instantThreshold,
			RangeQueryMS:      *rangeThreshold,
			SeriesDiscoveryMS: *seriesThreshold,
		},
		Checks: []CheckResult{},
	}
	
	// Run validation tests
	runInstantQueryTests(ctx, baseURL, report)
	runRangeQueryTests(ctx, baseURL, report)
	runSeriesDiscoveryTests(ctx, baseURL, report)
	runLabelDiscoveryTests(ctx, baseURL, report)
	runComplexQueryTests(ctx, baseURL, report)
	runConcurrentQueryTests(ctx, baseURL, report)
	
	// Calculate summary
	for _, check := range report.Checks {
		switch check.Status {
		case "PASS":
			report.Summary.Pass++
		case "WARN":
			report.Summary.Warn++
		case "FAIL":
			report.Summary.Fail++
		}
	}
	
	// Output report
	switch *outputFormat {
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
			os.Exit(1)
		}
	case "text":
		printTextReport(report)
	default:
		fmt.Fprintf(os.Stderr, "Unknown output format: %s\n", *outputFormat)
		os.Exit(1)
	}
	
	// Exit with appropriate code
	if report.Summary.Fail > 0 {
		os.Exit(1)
	}
}

func testConnectivity(ctx context.Context, baseURL string) bool {
	testURL := baseURL + "/query?query=up"
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	
	req, err := http.NewRequestWithContext(ctx, "GET", testURL, nil)
	if err != nil {
		return false
	}
	
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	
	return resp.StatusCode == 200
}

func executeQuery(ctx context.Context, queryURL string) (int64, int, string, error) {
	start := time.Now()
	
	req, err := http.NewRequestWithContext(ctx, "GET", queryURL, nil)
	if err != nil {
		return 0, 0, "", err
	}
	
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return time.Since(start).Milliseconds(), 0, "", err
	}
	defer resp.Body.Close()
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return time.Since(start).Milliseconds(), resp.StatusCode, "", err
	}
	
	return time.Since(start).Milliseconds(), resp.StatusCode, string(body), nil
}

func addResult(report *ValidationReport, check, status, message string, valueMS, thresholdMS int64) {
	report.Checks = append(report.Checks, CheckResult{
		Check:       check,
		Status:      status,
		Message:     message,
		ValueMS:     valueMS,
		ThresholdMS: thresholdMS,
	})
}

func validateResponse(body string) (bool, int) {
	var result QueryResult
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		return false, 0
	}
	if result.Status != "success" {
		return false, 0
	}
	
	// Count data points/series
	count := 0
	switch v := result.Data.(type) {
	case map[string]interface{}:
		if resultData, ok := v["result"]; ok {
			switch rt := resultData.(type) {
			case []interface{}:
				count = len(rt)
				if count > 0 {
					if vals, ok := rt[0].(map[string]interface{}); ok {
						if values, ok := vals["values"].([]interface{}); ok {
							count = len(values)
						}
					}
				}
			}
		}
		if data, ok := v["data"]; ok {
			switch dt := data.(type) {
			case []interface{}:
				count = len(dt)
			}
		}
	}
	
	return true, count
}

func runInstantQueryTests(ctx context.Context, baseURL string, report *ValidationReport) {
	queries := []string{
		"finops_cluster_total_hourly_cost_usd",
		"finops_workload_hourly_cost_usd",
		"finops_node_hourly_cost_usd",
		"finops_cluster_idle_cost_usd",
	}
	
	for _, query := range queries {
		queryURL := baseURL + "/query?query=" + url.QueryEscape(query)
		latencyMS, statusCode, body, err := executeQuery(ctx, queryURL)
		
		if err != nil {
			addResult(report, "instant_query_"+sanitize(query), "FAIL", 
				fmt.Sprintf("Query error: %v", err), latencyMS, int64(*instantThreshold))
			continue
		}
		
		if statusCode != 200 {
			addResult(report, "instant_query_"+sanitize(query), "FAIL",
				fmt.Sprintf("HTTP %d: %s", statusCode, body), latencyMS, int64(*instantThreshold))
			continue
		}
		
		valid, count := validateResponse(body)
		if !valid {
			addResult(report, "instant_query_"+sanitize(query), "FAIL",
				fmt.Sprintf("Invalid response format"), latencyMS, int64(*instantThreshold))
			continue
		}
		
		if latencyMS > int64(*instantThreshold) {
			addResult(report, "instant_query_"+sanitize(query), "FAIL",
				fmt.Sprintf("Latency %dms exceeds %dms threshold (series: %d)", latencyMS, *instantThreshold, count),
				latencyMS, int64(*instantThreshold))
		} else {
			addResult(report, "instant_query_"+sanitize(query), "PASS",
				fmt.Sprintf("Latency %dms within threshold (series: %d)", latencyMS, count),
				latencyMS, int64(*instantThreshold))
		}
	}
}

func runRangeQueryTests(ctx context.Context, baseURL string, report *ValidationReport) {
	tests := []struct {
		name   string
		query  string
		start  int64
		end    int64
		step   int64
	}{
		{
			name:  "workload_cost_30d_1h",
			query: "finops_workload_hourly_cost_usd",
			start: time.Now().Add(-30 * 24 * time.Hour).Unix(),
			end:   time.Now().Unix(),
			step:  3600,
		},
		{
			name:  "workload_cpu_1d_5m",
			query: "finops_workload_cpu_cores_used",
			start: time.Now().Add(-24 * time.Hour).Unix(),
			end:   time.Now().Unix(),
			step:  300,
		},
		{
			name:  "node_util_7d_1h",
			query: "finops_node_cpu_utilization_ratio",
			start: time.Now().Add(-7 * 24 * time.Hour).Unix(),
			end:   time.Now().Unix(),
			step:  3600,
		},
	}
	
	for _, test := range tests {
		queryURL := fmt.Sprintf("%s/query_range?query=%s&start=%d&end=%d&step=%d",
			baseURL, url.QueryEscape(test.query), test.start, test.end, test.step)
		
		latencyMS, statusCode, body, err := executeQuery(ctx, queryURL)
		
		if err != nil {
			addResult(report, "range_query_"+test.name, "FAIL",
				fmt.Sprintf("Query error: %v", err), latencyMS, int64(*rangeThreshold))
			continue
		}
		
		if statusCode != 200 {
			addResult(report, "range_query_"+test.name, "FAIL",
				fmt.Sprintf("HTTP %d: %s", statusCode, body), latencyMS, int64(*rangeThreshold))
			continue
		}
		
		valid, count := validateResponse(body)
		if !valid {
			addResult(report, "range_query_"+test.name, "FAIL",
				fmt.Sprintf("Invalid response format"), latencyMS, int64(*rangeThreshold))
			continue
		}
		
		if latencyMS > int64(*rangeThreshold) {
			addResult(report, "range_query_"+test.name, "FAIL",
				fmt.Sprintf("Latency %dms exceeds %dms threshold (data points: %d)", latencyMS, *rangeThreshold, count),
				latencyMS, int64(*rangeThreshold))
		} else {
			addResult(report, "range_query_"+test.name, "PASS",
				fmt.Sprintf("Latency %dms within threshold (data points: %d)", latencyMS, count),
				latencyMS, int64(*rangeThreshold))
		}
	}
}

func runSeriesDiscoveryTests(ctx context.Context, baseURL string, report *ValidationReport) {
	matches := []string{
		"finops_workload_hourly_cost_usd",
		"finops_node_hourly_cost_usd",
		"finops_workload_cpu_cores_used",
		"finops_workload_memory_bytes_used",
	}
	
	for _, match := range matches {
		queryURL := baseURL + "/series?match[]=" + url.QueryEscape(match)
		latencyMS, statusCode, body, err := executeQuery(ctx, queryURL)
		
		if err != nil {
			addResult(report, "series_discovery_"+sanitize(match), "FAIL",
				fmt.Sprintf("Query error: %v", err), latencyMS, int64(*seriesThreshold))
			continue
		}
		
		if statusCode != 200 {
			addResult(report, "series_discovery_"+sanitize(match), "FAIL",
				fmt.Sprintf("HTTP %d: %s", statusCode, body), latencyMS, int64(*seriesThreshold))
			continue
		}
		
		valid, count := validateResponse(body)
		if !valid {
			addResult(report, "series_discovery_"+sanitize(match), "FAIL",
				fmt.Sprintf("Invalid response format"), latencyMS, int64(*seriesThreshold))
			continue
		}
		
		if latencyMS > int64(*seriesThreshold) {
			addResult(report, "series_discovery_"+sanitize(match), "FAIL",
				fmt.Sprintf("Latency %dms exceeds %dms threshold (series: %d)", latencyMS, *seriesThreshold, count),
				latencyMS, int64(*seriesThreshold))
		} else {
			addResult(report, "series_discovery_"+sanitize(match), "PASS",
				fmt.Sprintf("Latency %dms within threshold (series: %d)", latencyMS, count),
				latencyMS, int64(*seriesThreshold))
		}
	}
}

func runLabelDiscoveryTests(ctx context.Context, baseURL string, report *ValidationReport) {
	// Labels
	queryURL := baseURL + "/labels"
	latencyMS, statusCode, body, err := executeQuery(ctx, queryURL)
	
	if err != nil {
		addResult(report, "label_discovery", "FAIL",
			fmt.Sprintf("Query error: %v", err), latencyMS, int64(*seriesThreshold))
	} else if statusCode != 200 {
		addResult(report, "label_discovery", "FAIL",
			fmt.Sprintf("HTTP %d: %s", statusCode, body), latencyMS, int64(*seriesThreshold))
	} else {
		valid, count := validateResponse(body)
		if !valid {
			addResult(report, "label_discovery", "FAIL",
				fmt.Sprintf("Invalid response format"), latencyMS, int64(*seriesThreshold))
		} else if latencyMS > int64(*seriesThreshold) {
			addResult(report, "label_discovery", "FAIL",
				fmt.Sprintf("Latency %dms exceeds %dms threshold (labels: %d)", latencyMS, *seriesThreshold, count),
				latencyMS, int64(*seriesThreshold))
		} else {
			addResult(report, "label_discovery", "PASS",
				fmt.Sprintf("Latency %dms within threshold (labels: %d)", latencyMS, count),
				latencyMS, int64(*seriesThreshold))
		}
	}
	
	// Label values
	labelNames := []string{"namespace", "cost_center", "team", "workload", "kind"}
	for _, labelName := range labelNames {
		queryURL := baseURL + "/label/" + url.QueryEscape(labelName) + "/values"
		latencyMS, statusCode, body, err := executeQuery(ctx, queryURL)
		
		if err != nil {
			addResult(report, "label_values_"+labelName, "FAIL",
				fmt.Sprintf("Query error: %v", err), latencyMS, int64(*seriesThreshold))
			continue
		}
		
		if statusCode != 200 {
			addResult(report, "label_values_"+labelName, "FAIL",
				fmt.Sprintf("HTTP %d: %s", statusCode, body), latencyMS, int64(*seriesThreshold))
			continue
		}
		
		valid, count := validateResponse(body)
		if !valid {
			addResult(report, "label_values_"+labelName, "FAIL",
				fmt.Sprintf("Invalid response format"), latencyMS, int64(*seriesThreshold))
			continue
		}
		
		if latencyMS > int64(*seriesThreshold) {
			addResult(report, "label_values_"+labelName, "FAIL",
				fmt.Sprintf("Latency %dms exceeds %dms threshold (values: %d)", latencyMS, *seriesThreshold, count),
				latencyMS, int64(*seriesThreshold))
		} else {
			addResult(report, "label_values_"+labelName, "PASS",
				fmt.Sprintf("Latency %dms within threshold (values: %d)", latencyMS, count),
				latencyMS, int64(*seriesThreshold))
		}
	}
}

func runComplexQueryTests(ctx context.Context, baseURL string, report *ValidationReport) {
	queries := []struct {
		name  string
		query string
	}{
		{"rate_query", "rate(finops_workload_cpu_cores_used[5m])"},
		{"sum_by_namespace", "sum by (namespace) (finops_workload_hourly_cost_usd)"},
		{"topk_10", "topk(10, finops_workload_hourly_cost_usd)"},
		{"histogram_quantile", "histogram_quantile(0.95, rate(finops_span_duration_seconds_bucket[5m]))"},
	}
	
	for _, q := range queries {
		queryURL := baseURL + "/query?query=" + url.QueryEscape(q.query)
		latencyMS, statusCode, body, err := executeQuery(ctx, queryURL)
		
		if err != nil {
			addResult(report, "complex_"+q.name, "WARN",
				fmt.Sprintf("Query error: %v", err), latencyMS, int64(*instantThreshold))
			continue
		}
		
		if statusCode != 200 {
			addResult(report, "complex_"+q.name, "WARN",
				fmt.Sprintf("HTTP %d: %s", statusCode, body), latencyMS, int64(*instantThreshold))
			continue
		}
		
		valid, _ := validateResponse(body)
		if !valid {
			addResult(report, "complex_"+q.name, "WARN",
				fmt.Sprintf("Invalid response format"), latencyMS, int64(*instantThreshold))
			continue
		}
		
		if latencyMS > int64(*instantThreshold) {
			addResult(report, "complex_"+q.name, "WARN",
				fmt.Sprintf("Latency %dms exceeds %dms threshold", latencyMS, *instantThreshold),
				latencyMS, int64(*instantThreshold))
		} else {
			addResult(report, "complex_"+q.name, "PASS",
				fmt.Sprintf("Latency %dms within threshold", latencyMS),
				latencyMS, int64(*instantThreshold))
		}
	}
}

func runConcurrentQueryTests(ctx context.Context, baseURL string, report *ValidationReport) {
	query := "finops_cluster_total_hourly_cost_usd"
	queryURL := baseURL + "/query?query=" + url.QueryEscape(query)
	
	for _, concurrency := range []int{*concurrent, *concurrent * 2} {
		var wg sync.WaitGroup
		latencies := make([]int64, concurrency)
		statusCodes := make([]int, concurrency)
		errors := make([]error, concurrency)
		
		startAll := time.Now()
		
		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				latencyMS, statusCode, _, err := executeQuery(ctx, queryURL)
				latencies[idx] = latencyMS
				statusCodes[idx] = statusCode
				errors[idx] = err
			}(i)
		}
		
		wg.Wait()
		totalTime := time.Since(startAll).Milliseconds()
		
		successCount := 0
		var maxLatency int64
		var totalLatency int64
		
		for i := 0; i < concurrency; i++ {
			if errors[i] == nil && statusCodes[i] == 200 {
				successCount++
				totalLatency += latencies[i]
				if latencies[i] > maxLatency {
					maxLatency = latencies[i]
				}
			}
		}
		
		avgLatency := int64(0)
		if successCount > 0 {
			avgLatency = totalLatency / int64(successCount)
		}
		
		if successCount == concurrency {
			if maxLatency > int64(*instantThreshold) {
				addResult(report, fmt.Sprintf("concurrent_%d", concurrency), "WARN",
					fmt.Sprintf("Max latency %dms exceeds %dms (avg: %dms, total: %dms)", maxLatency, *instantThreshold, avgLatency, totalTime),
					maxLatency, int64(*instantThreshold))
			} else {
				addResult(report, fmt.Sprintf("concurrent_%d", concurrency), "PASS",
					fmt.Sprintf("All %d queries succeeded (max: %dms, avg: %dms, total: %dms)", concurrency, maxLatency, avgLatency, totalTime),
					maxLatency, int64(*instantThreshold))
			}
		} else {
			addResult(report, fmt.Sprintf("concurrent_%d", concurrency), "FAIL",
				fmt.Sprintf("Only %d/%d queries succeeded", successCount, concurrency),
				maxLatency, int64(*instantThreshold))
		}
	}
}

func sanitize(s string) string {
	// Replace non-alphanumeric with underscore
	result := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, s)
	// Trim underscores
	result = strings.Trim(result, "_")
	return result
}

func printTextReport(report *ValidationReport) {
	fmt.Printf("Query Performance Validation Report\n")
	fmt.Printf("==================================\n")
	fmt.Printf("Timestamp: %s\n", report.Timestamp)
	fmt.Printf("Thresholds: Instant=%dms, Range=%dms, Series=%dms\n",
		report.Thresholds.InstantQueryMS, report.Thresholds.RangeQueryMS, report.Thresholds.SeriesDiscoveryMS)
	fmt.Printf("\n")
	
	// Group by category
	categories := map[string][]CheckResult{
		"Instant Queries":        {},
		"Range Queries":          {},
		"Series Discovery":       {},
		"Label Discovery":        {},
		"Complex Queries":        {},
		"Concurrent Queries":     {},
	}
	
	for _, check := range report.Checks {
		switch {
		case strings.HasPrefix(check.Check, "instant_query"):
			categories["Instant Queries"] = append(categories["Instant Queries"], check)
		case strings.HasPrefix(check.Check, "range_query"):
			categories["Range Queries"] = append(categories["Range Queries"], check)
		case strings.HasPrefix(check.Check, "series_discovery"):
			categories["Series Discovery"] = append(categories["Series Discovery"], check)
		case strings.HasPrefix(check.Check, "label_discovery"), strings.HasPrefix(check.Check, "label_values"):
			categories["Label Discovery"] = append(categories["Label Discovery"], check)
		case strings.HasPrefix(check.Check, "complex_"):
			categories["Complex Queries"] = append(categories["Complex Queries"], check)
		case strings.HasPrefix(check.Check, "concurrent_"):
			categories["Concurrent Queries"] = append(categories["Concurrent Queries"], check)
		}
	}
	
	for category, checks := range categories {
		if len(checks) == 0 {
			continue
		}
		fmt.Printf("%s:\n", category)
		for _, check := range checks {
			statusIcon := ""
			switch check.Status {
			case "PASS":
				statusIcon = "✓"
			case "WARN":
				statusIcon = "⚠"
			case "FAIL":
				statusIcon = "✗"
			}
			fmt.Printf("  %s [%s] %s (latency=%dms, threshold=%dms)\n", 
				statusIcon, check.Status, check.Message, check.ValueMS, check.ThresholdMS)
		}
		fmt.Printf("\n")
	}
	
	fmt.Printf("Summary: PASS=%d, WARN=%d, FAIL=%d\n", report.Summary.Pass, report.Summary.Warn, report.Summary.Fail)
}
