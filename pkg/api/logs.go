package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	finopsv1alpha1 "github.com/your-org/finops-operator/api/v1alpha1"
	"github.com/your-org/finops-operator/pkg/controllers"
)

const (
	LogIngestionPath = "/api/v1/logs"
)

// LogEntry represents a single log entry from Fluent Bit
type LogEntry struct {
	Timestamp   string                 `json:"timestamp"`
	Log         string                 `json:"log"`
	Stream      string                 `json:"stream"`
	Kubernetes  KubernetesMetadata     `json:"kubernetes"`
	ClusterName string                 `json:"cluster_name,omitempty"`
	CostCenter  string                 `json:"cost_center,omitempty"`
	Team        string                 `json:"team,omitempty"`
}

// KubernetesMetadata contains Kubernetes enrichment data
type KubernetesMetadata struct {
	PodName       string            `json:"pod_name"`
	Namespace     string            `json:"namespace"`
	PodID         string            `json:"pod_id"`
	Labels        map[string]string `json:"labels"`
	Annotations   map[string]string `json:"annotations"`
	Host          string            `json:"host"`
	ContainerName string            `json:"container_name"`
	ContainerID   string            `json:"container_id"`
}

// LogIngestionHandler handles log ingestion from Fluent Bit
type LogIngestionHandler struct {
	client.Client
	Logger         logr.Logger
	EventReconciler *controllers.EventWatcherReconciler
}

// NewLogIngestionHandler creates a new log ingestion handler
func NewLogIngestionHandler(client client.Client, logger logr.Logger, eventReconciler *controllers.EventWatcherReconciler) *LogIngestionHandler {
	return &LogIngestionHandler{
		Client:          client,
		Logger:          logger.WithName("log-ingestion"),
		EventReconciler: eventReconciler,
	}
}

// ServeHTTP handles incoming log entries
func (h *LogIngestionHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := h.Logger.WithValues("remote", r.RemoteAddr)

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse JSON array of log entries
	var entries []LogEntry
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&entries); err != nil {
		logger.Error(err, "Failed to decode log entries")
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if len(entries) == 0 {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Process each log entry
	processed := 0
	errors := 0
	for _, entry := range entries {
		if err := h.processLogEntry(ctx, &entry); err != nil {
			logger.Error(err, "Failed to process log entry", "entry", entry)
			errors++
		} else {
			processed++
		}
	}

	logger.V(1).Info("Processed log batch", "total", len(entries), "processed", processed, "errors", errors)

	response := map[string]interface{}{
		"processed": processed,
		"errors":    errors,
		"total":     len(entries),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// processLogEntry processes a single log entry
func (h *LogIngestionHandler) processLogEntry(ctx context.Context, entry *LogEntry) error {
	// Extract key information
	namespace := entry.Kubernetes.Namespace
	podName := entry.Kubernetes.PodName
	containerName := entry.Kubernetes.ContainerName
	logMessage := entry.Log

	// Determine log level
	level := h.detectLogLevel(logMessage)
	if level == "" {
		// Not an error log, skip
		return nil
	}

	// Get workload reference from pod
	workloadRef, costCenter, err := h.getWorkloadInfo(ctx, namespace, podName)
	if err != nil {
		h.Logger.V(1).Info("Could not get workload info", "namespace", namespace, "pod", podName, "error", err)
		workloadRef = &finopsv1alpha1.WorkloadReference{
			Name: podName,
			Kind: "Pod",
		}
		costCenter = entry.CostCenter
		if costCenter == "" {
			costCenter = "unknown"
		}
	}

	// Increment TSDB counter
	if h.EventReconciler != nil {
		h.EventReconciler.IncrementLogErrorCounter(namespace, workloadRef.Name, containerName, level)
	}

	// Create LogEvent CRD for significant errors
	if h.shouldCreateLogEvent(level, logMessage) {
		logEvent := h.createLogEventFromEntry(entry, workloadRef, costCenter, level, logMessage)
		if err := h.Create(ctx, logEvent); err != nil {
			// If already exists, try to update
			if client.IgnoreAlreadyExists(err) != nil {
				return fmt.Errorf("failed to create LogEvent: %w", err)
			}
		}
	}

	return nil
}

// detectLogLevel detects the log level from the message
func (h *LogIngestionHandler) detectLogLevel(message string) string {
	messageLower := strings.ToLower(message)
	
	// Check for critical patterns first
	if strings.Contains(messageLower, "oom") || strings.Contains(messageLower, "outofmemory") {
		return "critical"
	}
	if strings.Contains(messageLower, "killed") || strings.Contains(messageLower, "crashloopbackoff") {
		return "critical"
	}
	if strings.Contains(messageLower, "fatal") || strings.Contains(messageLower, "panic") {
		return "critical"
	}
	if strings.Contains(messageLower, "error") || strings.Contains(messageLower, "exception") {
		return "error"
	}
	if strings.Contains(messageLower, "warn") || strings.Contains(messageLower, "warning") {
		return "warning"
	}
	
	return ""
}

// shouldCreateLogEvent determines if a LogEvent CRD should be created
func (h *LogIngestionHandler) shouldCreateLogEvent(level, message string) bool {
	// Create LogEvent for critical and error levels
	return level == "critical" || level == "error"
}

// getWorkloadInfo retrieves workload reference and cost center for a pod
func (h *LogIngestionHandler) getWorkloadInfo(ctx context.Context, namespace, podName string) (*finopsv1alpha1.WorkloadReference, string, error) {
	var pod finopsv1alpha1.Pod
	// Note: This would need to use the actual corev1.Pod type
	// For now, return empty and let the caller handle it
	return nil, "", fmt.Errorf("not implemented - would query pod from API")
}

// createLogEventFromEntry creates a LogEvent CRD from a log entry
func (h *LogIngestionHandler) createLogEventFromEntry(entry *LogEntry, workloadRef *finopsv1alpha1.WorkloadReference, costCenter, level, message string) *finopsv1alpha1.LogEvent {
	now := time.Now()
	name := fmt.Sprintf("log-%s-%s-%s-%d",
		strings.ToLower(level),
		entry.Kubernetes.Namespace,
		workloadRef.Name,
		now.Unix(),
	)

	// Truncate to 253 chars
	if len(name) > 253 {
		name = name[:253]
	}

	reason := "LogError"
	if level == "critical" {
		reason = "LogCritical"
	}

	return &finopsv1alpha1.LogEvent{
		ObjectMeta: ctrl.ObjectMeta{
			Name:      name,
			Namespace: entry.Kubernetes.Namespace,
		},
		Spec: finopsv1alpha1.LogEventSpec{
			EventRef: finopsv1alpha1.EventReference{
				Name:      name,
				Namespace: entry.Kubernetes.Namespace,
				Reason:    reason,
				Message:   message,
				Source:    "fluent-bit",
				FirstTimestamp: &metav1.Time{Time: now},
				LastTimestamp:  &metav1.Time{Time: now},
				Count:         1,
				Type:          "Warning",
			},
			InvolvedObject: finopsv1alpha1.ObjectReference{
				Kind:      "Pod",
				Namespace: entry.Kubernetes.Namespace,
				Name:      entry.Kubernetes.PodName,
				UID:       entry.Kubernetes.PodID,
			},
			WorkloadRef: workloadRef,
			CostCenter:  costCenter,
			NodeName:    entry.Kubernetes.Host,
		},
		Status: finopsv1alpha1.LogEventStatus{
			ProcessedAt:  &metav1.Time{Time: now},
			ProcessedBy:  "fluent-bit-ingestion",
			EventReason:  reason,
			EventMessage: message,
		},
	}
}

// RegisterLogIngestionEndpoint registers the log ingestion HTTP handler
func RegisterLogIngestionEndpoint(mgr ctrl.Manager, client client.Client, logger logr.Logger, eventReconciler *controllers.EventWatcherReconciler) {
	handler := NewLogIngestionHandler(client, logger, eventReconciler)
	
	// Add to the manager's HTTP server
	// This would typically be done by adding to the manager's webhook server
	// or creating a separate HTTP server
	// For now, we'll add it as a handler to the metrics server
	// In production, you might want a separate server on port 8081
}
