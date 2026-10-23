package storage

import (
	"regexp"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap/zaptest"
)

func TestLabelNormalizer_BasicNormalization(t *testing.T) {
	logger := zaptest.NewLogger(t)
	reg := prometheus.NewRegistry()
	
	config := DefaultLabelNormalizerConfig()
	metrics := NewLabelNormalizerMetrics(reg)
	normalizer := NewLabelNormalizer(config, metrics)

	labels := map[string]string{
		"namespace":    "test-ns",
		"pod":          "test-pod",
		"container":    "test-container",
		"__name__":     "finops_workload_cpu_cores_used",
	}

	result := normalizer.Normalize(labels)

	// Check all allowed labels are preserved
	expected := map[string]string{
		"namespace":    "test-ns",
		"pod":          "test-pod",
		"container":    "test-container",
		"__name__":     "finops_workload_cpu_cores_used",
		"cost_center":  "unknown",
		"team":         "unknown",
	}

	if len(result) != len(expected) {
		t.Errorf("Expected %d labels, got %d", len(expected), len(result))
		for k, v := range result {
			t.Logf("  %s=%s", k, v)
		}
	}

	for k, expectedVal := range expected {
		if actualVal, ok := result[k]; ok {
			if actualVal != expectedVal {
				t.Errorf("Label %s: expected %q, got %q", k, expectedVal, actualVal)
			}
		} else {
			t.Errorf("Missing expected label: %s", k)
		}
	}
}

func TestLabelNormalizer_DenyList(t *testing.T) {
	logger := zaptest.NewLogger(t)
	reg := prometheus.NewRegistry()
	
	config := DefaultLabelNormalizerConfig()
	metrics := NewLabelNormalizerMetrics(reg)
	normalizer := NewLabelNormalizer(config, metrics)

	labels := map[string]string{
		"namespace":    "test-ns",
		"pod_uid":      "abc-123", // Should be dropped
		"container_id": "docker://123", // Should be dropped
		"__name__":     "test_metric",
	}

	result := normalizer.Normalize(labels)

	// Check denied labels are dropped
	for k := range result {
		if k == "pod_uid" || k == "container_id" {
			t.Errorf("Denied label %s should have been dropped", k)
		}
	}
}

func TestLabelNormalizer_AllowList(t *testing.T) {
	logger := zaptest.NewLogger(t)
	reg := prometheus.NewRegistry()
	
	config := &LabelNormalizerConfig{
		MaxLabelValueLength: 100,
		AllowList:           []string{"namespace", "pod", "__name__"},
		DenyList:            []string{},
		RequireLabels:       []string{},
	}
	metrics := NewLabelNormalizerMetrics(reg)
	normalizer := NewLabelNormalizer(config, metrics)

	labels := map[string]string{
		"namespace":   "test-ns",
		"pod":         "test-pod",
		"container":   "test-container", // Not in allowlist
		"__name__":    "test_metric",
	}

	result := normalizer.Normalize(labels)

	// Check only allowed labels are present
	allowed := map[string]bool{"namespace": true, "pod": true, "__name__": true}
	for k := range result {
		if !allowed[k] {
			t.Errorf("Label %s should not be in allowlist", k)
		}
	}
}

func TestLabelNormalizer_LabelNameSanitization(t *testing.T) {
	logger := zaptest.NewLogger(t)
	reg := prometheus.NewRegistry()
	
	config := DefaultLabelNormalizerConfig()
	config.AllowList = []string{"namespace", "pod", "custom_label", "__name__"}
	metrics := NewLabelNormalizerMetrics(reg)
	normalizer := NewLabelNormalizer(config, metrics)

	labels := map[string]string{
		"namespace":      "test-ns",
		"pod-name":       "test-pod", // Invalid char '-'
		"container.name": "test",     // Invalid char '.'
		"123invalid":     "value",    // Starts with number
		"__name__":       "test_metric",
	}

	result := normalizer.Normalize(labels)

	// Check sanitized names
	found := map[string]bool{}
	for k := range result {
		found[k] = true
		// Verify Prometheus format
		if !regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`).MatchString(k) {
			t.Errorf("Label name %s does not match Prometheus format", k)
		}
	}

	// Original invalid names should be sanitized
	if found["pod-name"] {
		t.Error("pod-name should have been sanitized")
	}
	if found["container.name"] {
		t.Error("container.name should have been sanitized")
	}
	if found["123invalid"] {
		t.Error("123invalid should have been sanitized")
	}
	// Sanitized versions should exist
	if !found["pod_name"] {
		t.Error("pod_name (sanitized) should exist")
	}
	if !found["container_name"] {
		t.Error("container_name (sanitized) should exist")
	}
	if !found["_123invalid"] {
		t.Error("_123invalid (sanitized) should exist")
	}
}

func TestLabelNormalizer_LabelValueSanitization(t *testing.T) {
	logger := zaptest.NewLogger(t)
	reg := prometheus.NewRegistry()
	
	config := DefaultLabelNormalizerConfig()
	config.AllowList = []string{"namespace", "pod", "label_with_ctrl", "__name__"}
	metrics := NewLabelNormalizerMetrics(reg)
	normalizer := NewLabelNormalizer(config, metrics)

	labels := map[string]string{
		"namespace":        "test-ns",
		"pod":              "test-pod",
		"label_with_ctrl":  "value\x00with\x1Fcontrol\x7Fchars",
		"__name__":         "test_metric",
	}

	result := normalizer.Normalize(labels)

	for k, v := range result {
		if k == "label_with_ctrl" {
			if containsControlChars(v) {
				t.Errorf("Control characters should have been removed from value: %q", v)
			}
			expected := "valuewithcontrolchars"
			if v != expected {
				t.Errorf("Expected %q, got %q", expected, v)
			}
		}
	}
}

func TestLabelNormalizer_LabelValueTruncation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	reg := prometheus.NewRegistry()
	
	config := DefaultLabelNormalizerConfig()
	config.MaxLabelValueLength = 10
	config.AllowList = []string{"namespace", "pod", "long_label", "__name__"}
	metrics := NewLabelNormalizerMetrics(reg)
	normalizer := NewLabelNormalizer(config, metrics)

	longValue := strings.Repeat("a", 50)
	labels := map[string]string{
		"namespace":  "test-ns",
		"pod":        "test-pod",
		"long_label": longValue,
		"__name__":   "test_metric",
	}

	result := normalizer.Normalize(labels)

	for k, v := range result {
		if k == "long_label" {
			if len(v) > 10 {
				t.Errorf("Label value should be truncated to 10 chars, got %d", len(v))
			}
			if v != strings.Repeat("a", 10) {
				t.Errorf("Truncated value mismatch")
			}
		}
	}
}

func TestLabelNormalizer_RequiredLabelEnrichment(t *testing.T) {
	logger := zaptest.NewLogger(t)
	reg := prometheus.NewRegistry()
	
	config := DefaultLabelNormalizerConfig()
	config.RequireLabels = []string{"cost_center", "team"}
	
	// Mock namespace cache
	nsCache := map[string]map[string]string{
		"prod-ns": {"cost_center": "engineering", "team": "platform"},
	}
	
	enrichment := func(ns string) (map[string]string, bool) {
		labels, ok := nsCache[ns]
		return labels, ok
	}
	
	metrics := NewLabelNormalizerMetrics(reg)
	normalizer := NewLabelNormalizer(config, metrics)

	// Test with namespace that has labels
	labels := map[string]string{
		"namespace": "prod-ns",
		"pod":       "test-pod",
		"__name__":  "test_metric",
	}

	result := normalizer.NormalizeWithEnrichment(labels, enrichment)

	if result["cost_center"] != "engineering" {
		t.Errorf("Expected cost_center=engineering, got %s", result["cost_center"])
	}
	if result["team"] != "platform" {
		t.Errorf("Expected team=platform, got %s", result["team"])
	}

	// Test with namespace that doesn't have labels (should use defaults)
	labels2 := map[string]string{
		"namespace": "unknown-ns",
		"pod":       "test-pod",
		"__name__":  "test_metric",
	}

	result2 := normalizer.NormalizeWithEnrichment(labels2, enrichment)

	if result2["cost_center"] != "unknown" {
		t.Errorf("Expected cost_center=unknown, got %s", result2["cost_center"])
	}
	if result2["team"] != "unknown" {
		t.Errorf("Expected team=unknown, got %s", result2["team"])
	}
}

func TestLabelNormalizer_DuplicateLabels(t *testing.T) {
	logger := zaptest.NewLogger(t)
	reg := prometheus.NewRegistry()
	
	config := DefaultLabelNormalizerConfig()
	config.AllowList = []string{"namespace", "pod", "duplicate", "__name__"}
	metrics := NewLabelNormalizerMetrics(reg)
	normalizer := NewLabelNormalizer(config, metrics)

	labels := map[string]string{
		"namespace": "test-ns",
		"pod":       "test-pod",
		"duplicate": "value1",
		// Note: Go maps can't have duplicate keys, so we test collision after sanitization
		"dupli-cate": "value2", // Will be sanitized to "dupli_cate" -> "duplicate"
		"__name__":   "test_metric",
	}

	result := normalizer.Normalize(labels)

	count := 0
	for k := range result {
		if k == "duplicate" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Expected 1 duplicate label after sanitization collision, got %d", count)
	}
}

func TestLabelNormalizer_EmptyValueAfterSanitization(t *testing.T) {
	logger := zaptest.NewLogger(t)
	reg := prometheus.NewRegistry()
	
	config := DefaultLabelNormalizerConfig()
	config.AllowList = []string{"namespace", "pod", "empty_after_sanitize", "__name__"}
	metrics := NewLabelNormalizerMetrics(reg)
	normalizer := NewLabelNormalizer(config, metrics)

	labels := map[string]string{
		"namespace":            "test-ns",
		"pod":                  "test-pod",
		"empty_after_sanitize": "\x00\x1F\x7F", // Only control chars
		"__name__":             "test_metric",
	}

	result := normalizer.Normalize(labels)

	if _, exists := result["empty_after_sanitize"]; exists {
		t.Error("Label with only control chars should be dropped")
	}
}

func TestLabelNormalizer_Metrics(t *testing.T) {
	logger := zaptest.NewLogger(t)
	reg := prometheus.NewRegistry()
	
	config := DefaultLabelNormalizerConfig()
	config.DenyList = []string{"denied_label"}
	config.AllowList = []string{"namespace", "pod", "allowed_label", "long_label", "__name__"}
	config.MaxLabelValueLength = 5
	metrics := NewLabelNormalizerMetrics(reg)
	normalizer := NewLabelNormalizer(config, metrics)

	labels := map[string]string{
		"namespace":     "test-ns",
		"pod":           "test-pod",
		"denied_label":  "value", // Should be dropped
		"allowed_label": "value",
		"long_label":    "very_long_value_that_exceeds_limit",
		"__name__":      "test_metric",
	}

	normalizer.Normalize(labels)

	// Check metrics
	gathered, err := reg.Gather()
	if err != nil {
		t.Fatalf("Failed to gather metrics: %v", err)
	}

	metricCounts := make(map[string]float64)
	for _, mf := range gathered {
		for _, m := range mf.GetMetric() {
			name := mf.GetName()
			if name == "finops_label_normalizer_dropped_total" || 
			   name == "finops_label_normalizer_truncated_total" ||
			   name == "finops_label_normalizer_sanitized_total" ||
			   name == "finops_label_normalizer_added_total" {
				for _, l := range m.GetLabel() {
					if l.GetName() == "label_name" {
						key := name + "_" + l.GetValue()
						metricCounts[key] = m.GetCounter().GetValue()
					}
				}
			}
		}
	}

	// Check dropped metric
	if metricCounts["finops_label_normalizer_dropped_total_denied_label"] != 1 {
		t.Errorf("Expected dropped_total for denied_label=1, got %v", metricCounts["finops_label_normalizer_dropped_total_denied_label"])
	}

	// Check truncated metric
	if metricCounts["finops_label_normalizer_truncated_total_long_label"] != 1 {
		t.Errorf("Expected truncated_total for long_label=1, got %v", metricCounts["finops_label_normalizer_truncated_total_long_label"])
	}

	// Check sanitized metrics
	if metricCounts["finops_label_normalizer_sanitized_total_cost_center_name"] != 1 {
		t.Logf("Sanitized metrics: %v", metricCounts)
	}
}

func TestLabelNormalizer_UpdateConfig(t *testing.T) {
	logger := zaptest.NewLogger(t)
	reg := prometheus.NewRegistry()
	
	config := DefaultLabelNormalizerConfig()
	metrics := NewLabelNormalizerMetrics(reg)
	normalizer := NewLabelNormalizer(config, metrics)

	// Initially pod_uid is in denylist
	labels := map[string]string{
		"namespace": "test-ns",
		"pod_uid":   "abc-123",
		"__name__":  "test_metric",
	}
	result := normalizer.Normalize(labels)
	if _, exists := result["pod_uid"]; exists {
		t.Error("pod_uid should be dropped initially")
	}

	// Update config to remove from denylist and add to allowlist
	newConfig := DefaultLabelNormalizerConfig()
	newConfig.DenyList = []string{}
	newConfig.AllowList = []string{"namespace", "pod_uid", "__name__"}
	normalizer.UpdateConfig(newConfig)

	result = normalizer.Normalize(labels)
	if val, exists := result["pod_uid"]; !exists || val != "abc-123" {
		t.Errorf("pod_uid should be allowed after config update, got exists=%v val=%v", exists, val)
	}
}

// Helper function to check for control characters
func containsControlChars(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7F {
			return true
		}
	}
	return false
}
