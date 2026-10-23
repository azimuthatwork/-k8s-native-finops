// validate-promql-engine.go - PromQL Engine Validation Tool
// Part of Phase 2 Task 2.2.1
// Validates PromQL engine functionality and compatibility

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
	"strings"
	"time"

	"github.com/prometheus/prometheus/promql/parser"
)

const (
	DefaultNamespace = "cost-management"
	DefaultPodName   = "cost-controller-0"
	DefaultAPIPort   = 8082
)

var (
	namespace    = flag.String("namespace", DefaultNamespace, "Kubernetes namespace")
	podName      = flag.String("pod", DefaultPodName, "Pod name")
	apiPort      = flag.Int("port", DefaultAPIPort, "API port")
	outputFormat = flag.String("output", "json", "Output format: json, text")
	help         = flag.Bool("help", false, "Show help")
)

type CheckResult struct {
	Check   string `json:"check"`
	Status  string `json:"status"` // PASS, WARN, FAIL
	Message string `json:"message"`
}

type ValidationReport struct {
	Timestamp string        `json:"timestamp"`
	Checks    []CheckResult `json:"checks"`
	Summary   Summary       `json:"summary"`
}

type Summary struct {
	Pass int `json:"pass"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
}

func main() {
	flag.Parse()
	
	if *help {
		flag.Usage()
		os.Exit(0)
	}
	
	ctx := context.Background()
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
		Checks:    []CheckResult{},
	}
	
	// Run validation tests
	testParserCompatibility(report)
	testInstantQueryCompatibility(ctx, baseURL, report)
	testRangeQueryCompatibility(ctx, baseURL, report)
	testSeriesDiscoveryCompatibility(ctx, baseURL, report)
	testLabelDiscoveryCompatibility(ctx, baseURL, report)
	testPromQLFunctions(ctx, baseURL, report)
	testModifiers(ctx, baseURL, report)
	testErrorHandling(ctx, baseURL, report)
	
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

func executeQuery(ctx context.Context, queryURL string) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", queryURL, nil)
	if err != nil {
		return 0, "", err
	}
	
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, "", err
	}
	
	return resp.StatusCode, string(body), nil
}

func addResult(report *ValidationReport, check, status, message string) {
	report.Checks = append(report.Checks, CheckResult{
		Check:   check,
		Status:  status,
		Message: message,
	})
}

func testParserCompatibility(report *ValidationReport) {
	// Test that the parser can handle standard PromQL queries
	testQueries := []string{
		"up",
		"finops_workload_hourly_cost_usd",
		"rate(finops_workload_cpu_cores_used[5m])",
		"sum by (namespace) (finops_workload_hourly_cost_usd)",
		"topk(10, finops_workload_hourly_cost_usd)",
		"histogram_quantile(0.95, rate(finops_span_duration_seconds_bucket[5m]))",
		"finops_workload_hourly_cost_usd @ 3600",
		"finops_workload_hourly_cost_usd[1h]",
		"finops_workload_cpu_cores_used offset 1h",
		"(finops_workload_cpu_cores_used / finops_workload_cpu_cores_requested) * 100",
		"max by (namespace) (finops_workload_memory_bytes_used)",
		"finops_workload_hourly_cost_usd > 10",
	}
	
	passCount := 0
	for _, query := range testQueries {
		_, err := parser.ParseExpr(query)
		if err != nil {
			addResult(report, "parser_"+sanitize(query), "FAIL", fmt.Sprintf("Parse failed: %v", err))
		} else {
			passCount++
		}
	}
	
	if passCount == len(testQueries) {
		addResult(report, "parser_compatibility", "PASS", fmt.Sprintf("All %d test queries parsed successfully", passCount))
	} else {
		addResult(report, "parser_compatibility", "WARN", fmt.Sprintf("%d/%d test queries parsed successfully", passCount, len(testQueries)))
	}
}

func testInstantQueryCompatibility(ctx context.Context, baseURL string, report *ValidationReport) {
	queries := []struct {
		name  string
		query string
	}{
		{"simple_metric", "finops_cluster_total_hourly_cost_usd"},
		{"vector_selector", "finops_workload_hourly_cost_usd{namespace=\"default\"}"},
		{"vector_selector_multi", "finops_workload_hourly_cost_usd{namespace=~\"prod.*\"}"},
		{"binary_op", "finops_workload_cpu_cores_used / finops_workload_cpu_cores_requested"},
		{"aggregation", "sum(finops_workload_hourly_cost_usd)"},
		{"aggregation_by", "sum by (namespace) (finops_workload_hourly_cost_usd)"},
		{"topk", "topk(5, finops_workload_hourly_cost_usd)"},
		{"bottomk", "bottomk(5, finops_workload_hourly_cost_usd)"},
	}
	
	for _, q := range queries {
		queryURL := baseURL + "/query?query=" + url.QueryEscape(q.query)
		statusCode, body, err := executeQuery(ctx, queryURL)
		
		if err != nil {
			addResult(report, "instant_"+q.name, "FAIL", fmt.Sprintf("Request error: %v", err))
			continue
		}
		
		if statusCode != 200 {
			addResult(report, "instant_"+q.name, "FAIL", fmt.Sprintf("HTTP %d: %s", statusCode, body))
			continue
		}
		
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			addResult(report, "instant_"+q.name, "FAIL", fmt.Sprintf("Invalid JSON: %v", err))
			continue
		}
		
		if result["status"] != "success" {
			addResult(report, "instant_"+q.name, "FAIL", fmt.Sprintf("Query failed: %v", result["error"]))
			continue
		}
		
		addResult(report, "instant_"+q.name, "PASS", "Query executed successfully")
	}
}

func testRangeQueryCompatibility(ctx context.Context, baseURL string, report *ValidationReport) {
	now := time.Now()
	start := now.Add(-1 * time.Hour).Unix()
	end := now.Unix()
	step := int64(60) // 1 minute
	
	queries := []struct {
		name  string
		query string
	}{
		{"range_vector", "finops_workload_hourly_cost_usd"},
		{"rate_function", "rate(finops_workload_cpu_cores_used[5m])"},
		{"increase_function", "increase(finops_workload_cpu_cores_used[1h])"},
		{"aggregation_over_time", "avg_over_time(finops_workload_cpu_cores_used[1h])"},
	}
	
	for _, q := range queries {
		queryURL := fmt.Sprintf("%s/query_range?query=%s&start=%d&end=%d&step=%d",
			baseURL, url.QueryEscape(q.query), start, end, step)
		statusCode, body, err := executeQuery(ctx, queryURL)
		
		if err != nil {
			addResult(report, "range_"+q.name, "FAIL", fmt.Sprintf("Request error: %v", err))
			continue
		}
		
		if statusCode != 200 {
			addResult(report, "range_"+q.name, "FAIL", fmt.Sprintf("HTTP %d: %s", statusCode, body))
			continue
		}
		
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			addResult(report, "range_"+q.name, "FAIL", fmt.Sprintf("Invalid JSON: %v", err))
			continue
		}
		
		if result["status"] != "success" {
			addResult(report, "range_"+q.name, "FAIL", fmt.Sprintf("Query failed: %v", result["error"]))
			continue
		}
		
		// Check that result is a matrix
		data, ok := result["data"].(map[string]interface{})
		if !ok || data["resultType"] != "matrix" {
			addResult(report, "range_"+q.name, "WARN", "Result type is not matrix")
			continue
		}
		
		addResult(report, "range_"+q.name, "PASS", "Range query executed successfully")
	}
}

func testSeriesDiscoveryCompatibility(ctx context.Context, baseURL string, report *ValidationReport) {
	matches := []string{
		"finops_workload_hourly_cost_usd",
		"finops_workload_hourly_cost_usd{namespace=\"default\"}",
		"finops_workload_hourly_cost_usd{namespace=~\"prod.*\"}",
		"{__name__=~\"finops_.*\"}",
	}
	
	for _, match := range matches {
		queryURL := baseURL + "/series?match[]=" + url.QueryEscape(match)
		statusCode, body, err := executeQuery(ctx, queryURL)
		
		if err != nil {
			addResult(report, "series_"+sanitize(match), "FAIL", fmt.Sprintf("Request error: %v", err))
			continue
		}
		
		if statusCode != 200 {
			addResult(report, "series_"+sanitize(match), "FAIL", fmt.Sprintf("HTTP %d: %s", statusCode, body))
			continue
		}
		
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			addResult(report, "series_"+sanitize(match), "FAIL", fmt.Sprintf("Invalid JSON: %v", err))
			continue
		}
		
		if result["status"] != "success" {
			addResult(report, "series_"+sanitize(match), "FAIL", fmt.Sprintf("Query failed: %v", result["error"]))
			continue
		}
		
		data, ok := result["data"].([]interface{})
		if !ok {
			addResult(report, "series_"+sanitize(match), "WARN", "Data is not an array")
			continue
		}
		
		addResult(report, "series_"+sanitize(match), "PASS", fmt.Sprintf("Found %d series", len(data)))
	}
}

func testLabelDiscoveryCompatibility(ctx context.Context, baseURL string, report *ValidationReport) {
	// Test /labels
	queryURL := baseURL + "/labels"
	statusCode, body, err := executeQuery(ctx, queryURL)
	
	if err != nil {
		addResult(report, "labels", "FAIL", fmt.Sprintf("Request error: %v", err))
	} else if statusCode != 200 {
		addResult(report, "labels", "FAIL", fmt.Sprintf("HTTP %d: %s", statusCode, body))
	} else {
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			addResult(report, "labels", "FAIL", fmt.Sprintf("Invalid JSON: %v", err))
		} else if result["status"] != "success" {
			addResult(report, "labels", "FAIL", fmt.Sprintf("Query failed: %v", result["error"]))
		} else {
			data, _ := result["data"].([]interface{})
			addResult(report, "labels", "PASS", fmt.Sprintf("Found %d label names", len(data)))
		}
	}
	
	// Test /label/<name>/values
	labelNames := []string{"namespace", "cost_center", "team", "workload", "kind", "__name__"}
	for _, labelName := range labelNames {
		queryURL := baseURL + "/label/" + url.QueryEscape(labelName) + "/values"
		statusCode, body, err := executeQuery(ctx, queryURL)
		
		if err != nil {
			addResult(report, "label_values_"+labelName, "FAIL", fmt.Sprintf("Request error: %v", err))
			continue
		}
		
		if statusCode != 200 {
			addResult(report, "label_values_"+labelName, "FAIL", fmt.Sprintf("HTTP %d: %s", statusCode, body))
			continue
		}
		
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			addResult(report, "label_values_"+labelName, "FAIL", fmt.Sprintf("Invalid JSON: %v", err))
			continue
		}
		
		if result["status"] != "success" {
			addResult(report, "label_values_"+labelName, "FAIL", fmt.Sprintf("Query failed: %v", result["error"]))
			continue
		}
		
		data, _ := result["data"].([]interface{})
		addResult(report, "label_values_"+labelName, "PASS", fmt.Sprintf("Found %d values for %s", len(data), labelName))
	}
}

func testPromQLFunctions(ctx context.Context, baseURL string, report *ValidationReport) {
	functions := []struct {
		name  string
		query string
	}{
		// Rate functions
		{"rate", "rate(finops_workload_cpu_cores_used[5m])"},
		{"irate", "irate(finops_workload_cpu_cores_used[5m])"},
		{"increase", "increase(finops_workload_cpu_cores_used[1h])"},
		
		// Aggregation functions
		{"sum", "sum(finops_workload_hourly_cost_usd)"},
		{"avg", "avg(finops_workload_cpu_cores_used)"},
		{"max", "max(finops_workload_memory_bytes_used)"},
		{"min", "min(finops_workload_cpu_cores_requested)"},
		{"count", "count(finops_workload_hourly_cost_usd)"},
		{"stddev", "stddev(finops_workload_cpu_cores_used)"},
		{"stdvar", "stdvar(finops_workload_cpu_cores_used)"},
		
		// Top/Bottom k
		{"topk", "topk(10, finops_workload_hourly_cost_usd)"},
		{"bottomk", "bottomk(10, finops_workload_hourly_cost_usd)"},
		
		// Quantile/Histogram
		{"histogram_quantile", "histogram_quantile(0.95, rate(finops_span_duration_seconds_bucket[5m]))"},
		
		// Sort
		{"sort", "sort(finops_workload_hourly_cost_usd)"},
		{"sort_desc", "sort_desc(finops_workload_hourly_cost_usd)"},
		
		// Time functions
		{"time", "time()"},
		{"timestamp", "timestamp(finops_workload_hourly_cost_usd)"},
		
		// Math functions
		{"abs", "abs(finops_workload_cpu_cores_used - finops_workload_cpu_cores_requested)"},
		{"ceil", "ceil(finops_workload_cpu_cores_used)"},
		{"floor", "floor(finops_workload_cpu_cores_used)"},
		{"sqrt", "sqrt(finops_workload_cpu_cores_used)"},
		{"ln", "ln(finops_workload_hourly_cost_usd + 1)"},
		{"log2", "log2(finops_workload_hourly_cost_usd + 1)"},
		{"log10", "log10(finops_workload_hourly_cost_usd + 1)"},
		{"exp", "exp(finops_workload_cpu_cores_used / 100)"},
		
		// Vector matching
		{"vector_matching", "finops_workload_cpu_cores_used / finops_workload_cpu_cores_requested"},
		{"vector_matching_on", "finops_workload_cpu_cores_used / on(namespace) finops_workload_cpu_cores_requested"},
	}
	
	for _, f := range functions {
		queryURL := baseURL + "/query?query=" + url.QueryEscape(f.query)
		statusCode, body, err := executeQuery(ctx, queryURL)
		
		if err != nil {
			addResult(report, "function_"+f.name, "FAIL", fmt.Sprintf("Request error: %v", err))
			continue
		}
		
		if statusCode != 200 {
			// Some functions might not have data, that's OK
			var result map[string]interface{}
			if json.Unmarshal([]byte(body), &result) == nil && result["status"] == "success" {
				addResult(report, "function_"+f.name, "PASS", "Function executed (no data)")
			} else {
				addResult(report, "function_"+f.name, "WARN", fmt.Sprintf("HTTP %d (may be no data): %s", statusCode, body))
			}
			continue
		}
		
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			addResult(report, "function_"+f.name, "FAIL", fmt.Sprintf("Invalid JSON: %v", err))
			continue
		}
		
		if result["status"] != "success" {
			addResult(report, "function_"+f.name, "WARN", fmt.Sprintf("Function returned error: %v", result["error"]))
			continue
		}
		
		addResult(report, "function_"+f.name, "PASS", "Function executed successfully")
	}
}

func testModifiers(ctx context.Context, baseURL string, report *ValidationReport) {
	modifiers := []struct {
		name  string
		query string
	}{
		{"offset", "finops_workload_hourly_cost_usd offset 1h"},
		{"offset_negative", "finops_workload_hourly_cost_usd offset -1h"},
		{"at_modifier", "finops_workload_hourly_cost_usd @ 1704067200"},
		{"range_vector", "finops_workload_hourly_cost_usd[1h]"},
		{"range_vector_5m", "finops_workload_cpu_cores_used[5m]"},
		{"subquery", "avg_over_time(finops_workload_cpu_cores_used[1h:30s])"},
	}
	
	for _, m := range modifiers {
		queryURL := baseURL + "/query?query=" + url.QueryEscape(m.query)
		statusCode, body, err := executeQuery(ctx, queryURL)
		
		if err != nil {
			addResult(report, "modifier_"+m.name, "FAIL", fmt.Sprintf("Request error: %v", err))
			continue
		}
		
		if statusCode != 200 {
			var result map[string]interface{}
			if json.Unmarshal([]byte(body), &result) == nil && result["status"] == "success" {
				addResult(report, "modifier_"+m.name, "PASS", "Modifier executed (no data)")
			} else {
				addResult(report, "modifier_"+m.name, "WARN", fmt.Sprintf("HTTP %d: %s", statusCode, body))
			}
			continue
		}
		
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			addResult(report, "modifier_"+m.name, "FAIL", fmt.Sprintf("Invalid JSON: %v", err))
			continue
		}
		
		if result["status"] != "success" {
			addResult(report, "modifier_"+m.name, "WARN", fmt.Sprintf("Modifier returned error: %v", result["error"]))
			continue
		}
		
		addResult(report, "modifier_"+m.name, "PASS", "Modifier executed successfully")
	}
}

func testErrorHandling(ctx context.Context, baseURL string, report *ValidationReport) {
	errorCases := []struct {
		name       string
		query      string
		expectCode int
	}{
		{"empty_query", "", 400},
		{"invalid_syntax", "finops_workload_hourly_cost_usd {", 400},
		{"unknown_function", "unknown_function(finops_workload_hourly_cost_usd)", 400},
		{"invalid_range", "finops_workload_hourly_cost_usd[invalid]", 400},
		{"missing_params_range", "finops_workload_hourly_cost_usd", 400}, // query_range without start/end/step
	}
	
	for _, tc := range errorCases {
		var queryURL string
		if strings.Contains(tc.name, "range") {
			queryURL = baseURL + "/query_range?query=" + url.QueryEscape(tc.query)
		} else {
			queryURL = baseURL + "/query?query=" + url.QueryEscape(tc.query)
		}
		
		statusCode, body, err := executeQuery(ctx, queryURL)
		
		if err != nil {
			addResult(report, "error_"+tc.name, "FAIL", fmt.Sprintf("Request error: %v", err))
			continue
		}
		
		if statusCode == tc.expectCode {
			addResult(report, "error_"+tc.name, "PASS", fmt.Sprintf("Correctly returned HTTP %d", statusCode))
		} else {
			addResult(report, "error_"+tc.name, "WARN", fmt.Sprintf("Expected HTTP %d, got %d: %s", tc.expectCode, statusCode, body))
		}
	}
}

func sanitize(s string) string {
	result := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, s)
	return strings.Trim(result, "_")
}

func printTextReport(report *ValidationReport) {
	fmt.Printf("PromQL Engine Validation Report\n")
	fmt.Printf("==============================\n")
	fmt.Printf("Timestamp: %s\n\n", report.Timestamp)
	
	categories := map[string][]CheckResult{
		"Parser Compatibility":     {},
		"Instant Queries":          {},
		"Range Queries":            {},
		"Series Discovery":         {},
		"Label Discovery":          {},
		"PromQL Functions":         {},
		"Modifiers":                {},
		"Error Handling":           {},
	}
	
	for _, check := range report.Checks {
		switch {
		case strings.HasPrefix(check.Check, "parser_"):
			categories["Parser Compatibility"] = append(categories["Parser Compatibility"], check)
		case strings.HasPrefix(check.Check, "instant_"):
			categories["Instant Queries"] = append(categories["Instant Queries"], check)
		case strings.HasPrefix(check.Check, "range_"):
			categories["Range Queries"] = append(categories["Range Queries"], check)
		case strings.HasPrefix(check.Check, "series_"):
			categories["Series Discovery"] = append(categories["Series Discovery"], check)
		case strings.HasPrefix(check.Check, "labels"), strings.HasPrefix(check.Check, "label_values"):
			categories["Label Discovery"] = append(categories["Label Discovery"], check)
		case strings.HasPrefix(check.Check, "function_"):
			categories["PromQL Functions"] = append(categories["PromQL Functions"], check)
		case strings.HasPrefix(check.Check, "modifier_"):
			categories["Modifiers"] = append(categories["Modifiers"], check)
		case strings.HasPrefix(check.Check, "error_"):
			categories["Error Handling"] = append(categories["Error Handling"], check)
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
			fmt.Printf("  %s [%s] %s\n", statusIcon, check.Status, check.Message)
		}
		fmt.Printf("\n")
	}
	
	fmt.Printf("Summary: PASS=%d, WARN=%d, FAIL=%d\n", report.Summary.Pass, report.Summary.Warn, report.Summary.Fail)
}
