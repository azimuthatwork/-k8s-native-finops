package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	finopsv1alpha1 "github.com/your-org/finops-operator/api/v1alpha1"
	"github.com/your-org/finops-operator/pkg/cache"
	"github.com/your-org/finops-operator/pkg/clients"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	// EventControllerName is the name of the controller
	EventControllerName = "event-watcher-controller"
)

// Filter reasons for events we care about
var filterReasons = map[string]bool{
	"OOMKilling":          true,
	"Evicted":             true,
	"FailedScheduling":    true,
	"Killing":             true,
	"FailedMount":         true,
	"CrashLoopBackOff":    true,
	"BackOff":             true,
}

// EventWatcherController watches Kubernetes events and creates LogEvent CRDs
type EventWatcherController struct {
	client.Client
	Scheme         *runtime.Scheme
	Recorder       record.EventRecorder
	Logger         logr.Logger
	KSMClient      *clients.KSMClient
	MetricsClient  *clients.MetricsClient
	NodeExporter   *clients.NodeExporterClient
	CloudPricing   clients.CloudPricingClient
	DashboardCache *cache.DashboardCache
}

// EventWatcherReconciler reconciles Event objects
type EventWatcherReconciler struct {
	*EventWatcherController
}

// Metrics for event processing
var (
	eventsProcessedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "finops_events_processed_total",
			Help: "Total number of events processed by the event watcher",
		},
		[]string{"reason", "namespace", "workload", "cost_center", "result"},
	)

	eventsFilteredTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "finops_events_filtered_total",
			Help: "Total number of events filtered out by the event watcher",
		},
		[]string{"reason"},
	)

	eventProcessingDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "finops_event_processing_duration_seconds",
			Help:    "Duration of event processing in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"reason"},
	)

	finopsEventsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "finops_events_total",
			Help: "Total number of FinOps events by reason, namespace, workload, and cost center",
		},
		[]string{"reason", "namespace", "workload", "cost_center"},
	)

	finopsLogErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "finops_log_errors_total",
			Help: "Total number of log errors by namespace, workload, container, and level",
		},
		[]string{"namespace", "workload", "container", "level"},
	)
)

func init() {
	// Register metrics with the global prometheus registry
	metrics.Registry.MustRegister(
		eventsProcessedTotal,
		eventsFilteredTotal,
		eventProcessingDuration,
		finopsEventsTotal,
		finopsLogErrorsTotal,
	)
}

// AddEventWatcherController adds the event watcher controller to the manager
func AddEventWatcherController(mgr ctrl.Manager, ksmClient *clients.KSMClient, metricsClient *clients.MetricsClient, nodeExporter *clients.NodeExporterClient, cloudPricing clients.CloudPricingClient, dashboardCache *cache.DashboardCache) error {
	controller := &EventWatcherController{
		Client:         mgr.GetClient(),
		Scheme:         mgr.GetScheme(),
		Recorder:       mgr.GetEventRecorderFor(EventControllerName),
		Logger:         log.Log.WithName(EventControllerName),
		KSMClient:      ksmClient,
		MetricsClient:  metricsClient,
		NodeExporter:   nodeExporter,
		CloudPricing:   cloudPricing,
		DashboardCache: dashboardCache,
	}

	reconciler := &EventWatcherReconciler{
		EventWatcherController: controller,
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named(EventControllerName).
		For(&corev1.Event{}).
		WithEventFilter(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				return reconciler.shouldProcessEvent(e.Object)
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				return reconciler.shouldProcessEvent(e.ObjectNew)
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return false // Don't process deletions
			},
		}).
		Complete(reconciler)
}

// Reconcile processes Kubernetes events and creates/updates LogEvent CRDs
func (r *EventWatcherReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.WithValues("event", req.NamespacedName)
	startTime := time.Now()

	// Fetch the Event
	var k8sEvent corev1.Event
	if err := r.Get(ctx, req.NamespacedName, &k8sEvent); err != nil {
		if errors.IsNotFound(err) {
			logger.V(1).Info("Event not found, may have been deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Event")
		return ctrl.Result{}, err
	}

	// Check if we should process this event
	if !r.shouldProcessEvent(&k8sEvent) {
		eventsFilteredTotal.WithLabelValues(k8sEvent.Reason).Inc()
		return ctrl.Result{}, nil
	}

	// Process the event
	result, err := r.processEvent(ctx, &k8sEvent)
	duration := time.Since(startTime).Seconds()

	// Record metrics
	reason := k8sEvent.Reason
	eventProcessingDuration.WithLabelValues(reason).Observe(duration)

	if err != nil {
		eventsProcessedTotal.WithLabelValues(reason, k8sEvent.Namespace, "unknown", "unknown", "error").Inc()
		logger.Error(err, "Failed to process event")
		return ctrl.Result{}, err
	}

	eventsProcessedTotal.WithLabelValues(reason, k8sEvent.Namespace, result.workload, result.costCenter, "success").Inc()
	return ctrl.Result{}, nil
}

// shouldProcessEvent checks if the event should be processed based on reason
func (r *EventWatcherReconciler) shouldProcessEvent(obj client.Object) bool {
	event, ok := obj.(*corev1.Event)
	if !ok {
		return false
	}
	return filterReasons[event.Reason]
}

// processEventResult holds the result of event processing
type processEventResult struct {
	workload   string
	costCenter string
}

// processEvent processes a single event: enriches it and creates/updates LogEvent CRD
func (r *EventWatcherReconciler) processEvent(ctx context.Context, k8sEvent *corev1.Event) (*processEventResult, error) {
	logger := r.Logger.WithValues("event", k8sEvent.Name, "namespace", k8sEvent.Namespace, "reason", k8sEvent.Reason)

	// Step 1: Enrich event with workload and namespace information
	workloadRef, costCenter, err := r.enrichEvent(ctx, k8sEvent)
	if err != nil {
		logger.Error(err, "Failed to enrich event")
		// Continue with empty values rather than failing
		workloadRef = &finopsv1alpha1.WorkloadReference{}
		costCenter = ""
	}

	// Step 2: Get node cost information if applicable
	nodeCost, err := r.getNodeCost(ctx, k8sEvent)
	if err != nil {
		logger.V(1).Info("Could not get node cost", "error", err)
	}

	// Step 3: Create or update LogEvent CRD
	logEvent, err := r.createOrUpdateLogEvent(ctx, k8sEvent, workloadRef, costCenter, nodeCost)
	if err != nil {
		logger.Error(err, "Failed to create/update LogEvent")
		return nil, err
	}

	// Step 4: Increment TSDB counter
	r.incrementEventCounter(k8sEvent.Reason, k8sEvent.Namespace, workloadRef.Name, costCenter)

	logger.V(1).Info("Successfully processed event", "logEvent", logEvent.Name)
	return &processEventResult{
		workload:   workloadRef.Name,
		costCenter: costCenter,
	}, nil
}

// enrichEvent enriches the event with workload reference and cost center
func (r *EventWatcherReconciler) enrichEvent(ctx context.Context, k8sEvent *corev1.Event) (*finopsv1alpha1.WorkloadReference, string, error) {
	// Get the involved object (typically a Pod)
	involvedObject := k8sEvent.InvolvedObject

	// We only process Pod events for now
	if involvedObject.Kind != "Pod" {
		return &finopsv1alpha1.WorkloadReference{
			Name: involvedObject.Name,
			Kind: involvedObject.Kind,
			UID:  string(involvedObject.UID),
		}, "", nil
	}

	// Get the Pod
	podKey := types.NamespacedName{
		Namespace: involvedObject.Namespace,
		Name:      involvedObject.Name,
	}

	var pod corev1.Pod
	if err := r.Get(ctx, podKey, &pod); err != nil {
		if errors.IsNotFound(err) {
			// Pod may have been deleted, use info from event
			return &finopsv1alpha1.WorkloadReference{
				Name: involvedObject.Name,
				Kind: "Pod",
				UID:  string(involvedObject.UID),
			}, "", nil
		}
		return nil, "", fmt.Errorf("failed to get pod: %w", err)
	}

	// Get workload owner (Deployment, StatefulSet, DaemonSet, Job, CronJob, ReplicaSet)
	workloadRef := r.getWorkloadOwner(&pod)

	// Get cost center from namespace labels
	costCenter := r.getCostCenter(ctx, pod.Namespace)

	return workloadRef, costCenter, nil
}

// getWorkloadOwner determines the workload owner of a pod
func (r *EventWatcherReconciler) getWorkloadOwner(pod *corev1.Pod) *finopsv1alpha1.WorkloadReference {
	// Check owner references
	for _, ownerRef := range pod.OwnerReferences {
		if ownerRef.Controller != nil && *ownerRef.Controller {
			// Map Kubernetes kinds to our workload kinds
			kind := r.mapToWorkloadKind(ownerRef.Kind)
			return &finopsv1alpha1.WorkloadReference{
				Name: ownerRef.Name,
				Kind: kind,
				UID:  string(ownerRef.UID),
			}
		}
	}

	// No controller owner, return pod itself
	return &finopsv1alpha1.WorkloadReference{
		Name: pod.Name,
		Kind: "Pod",
		UID:  string(pod.UID),
	}
}

// mapToWorkloadKind maps Kubernetes resource kinds to workload kinds
func (r *EventWatcherReconciler) mapToWorkloadKind(k8sKind string) string {
	switch k8sKind {
	case "ReplicaSet":
		// ReplicaSet is usually owned by Deployment
		return "Deployment"
	case "StatefulSet", "DaemonSet", "Job", "CronJob", "Deployment":
		return k8sKind
	default:
		return k8sKind
	}
}

// getCostCenter retrieves the cost-center label from the namespace
func (r *EventWatcherReconciler) getCostCenter(ctx context.Context, namespace string) string {
	// Try to get from KSM cache first (namespace labels)
	if r.KSMClient != nil {
		cache := r.KSMClient.GetCache()
		if cache != nil && cache.NamespaceLabels != nil {
			if labels, ok := cache.NamespaceLabels[namespace]; ok {
				if costCenter, ok := labels["cost-center"]; ok {
					return costCenter
				}
			}
		}
	}

	// Fallback: query Kubernetes API for namespace
	var ns corev1.Namespace
	if err := r.Get(ctx, types.NamespacedName{Name: namespace}, &ns); err == nil {
		if ns.Labels != nil {
			if costCenter, ok := ns.Labels["cost-center"]; ok {
				return costCenter
			}
		}
	}

	return ""
}

// getNodeCost retrieves node cost information for the event
func (r *EventWatcherReconciler) getNodeCost(ctx context.Context, k8sEvent *corev1.Event) (*finopsv1alpha1.NodeCost, error) {
	// Get node name from event (if available)
	nodeName := k8sEvent.Source.Host
	if nodeName == "" {
		// Try to get from involved object if it's a pod
		if k8sEvent.InvolvedObject.Kind == "Pod" {
			var pod corev1.Pod
			podKey := types.NamespacedName{
				Namespace: k8sEvent.InvolvedObject.Namespace,
				Name:      k8sEvent.InvolvedObject.Name,
			}
			if err := r.Get(ctx, podKey, &pod); err == nil {
				nodeName = pod.Spec.NodeName
			}
		}
	}

	if nodeName == "" {
		return nil, nil
	}

	// Try to get NodeCost CRD
	var nodeCost finopsv1alpha1.NodeCost
	nodeCostKey := types.NamespacedName{Name: nodeName}
	if err := r.Get(ctx, nodeCostKey, &nodeCost); err != nil {
		if errors.IsNotFound(err) {
			// NodeCost CRD doesn't exist yet, try to get from cloud pricing
			return r.getNodeCostFromCloud(ctx, nodeName)
		}
		return nil, err
	}

	return &nodeCost, nil
}

// getNodeCostFromCloud retrieves node cost from cloud pricing client
func (r *EventWatcherReconciler) getNodeCostFromCloud(ctx context.Context, nodeName string) (*finopsv1alpha1.NodeCost, error) {
	if r.CloudPricing == nil {
		return nil, nil
	}

	// Get node to find instance type
	var node corev1.Node
	if err := r.Get(ctx, types.NamespacedName{Name: nodeName}, &node); err != nil {
		return nil, err
	}

	// Get instance type from node labels
	instanceType := node.Labels["node.kubernetes.io/instance-type"]
	if instanceType == "" {
		instanceType = node.Labels["beta.kubernetes.io/instance-type"]
	}
	if instanceType == "" {
		return nil, nil
	}

	// Check if spot
	isSpot := false
	if val, ok := node.Labels["node.kubernetes.io/lifecycle"]; ok && val == "spot" {
		isSpot = true
	}

	// Get price from cloud provider
	price, err := r.CloudPricing.GetNodePrice(ctx, instanceType, isSpot)
	if err != nil {
		return nil, err
	}

	// Get instance info
	instanceInfo, err := r.CloudPricing.GetInstanceInfo(ctx, instanceType)
	if err != nil {
		return nil, err
	}

	// Create NodeCost object (not persisted, just for enrichment)
	return &finopsv1alpha1.NodeCost{
		ObjectMeta: metav1.ObjectMeta{
			Name: nodeName,
		},
		Spec: finopsv1alpha1.NodeCostSpec{
			NodeName:     nodeName,
			InstanceType: instanceType,
			ProviderID:   node.Spec.ProviderID,
			IsSpot:       isSpot,
		},
		Status: finopsv1alpha1.NodeCostStatus{
			HourlyPriceUSD: price,
			VCPU:           instanceInfo.VCPU,
			MemoryGiB:      instanceInfo.MemoryGiB,
		},
	}, nil
}

// createOrUpdateLogEvent creates or updates a LogEvent CRD from the enriched event
func (r *EventWatcherReconciler) createOrUpdateLogEvent(ctx context.Context, k8sEvent *corev1.Event, workloadRef *finopsv1alpha1.WorkloadReference, costCenter string, nodeCost *finopsv1alpha1.NodeCost) (*finopsv1alpha1.LogEvent, error) {
	// Generate a deterministic name for the LogEvent
	logEventName := r.generateLogEventName(k8sEvent, workloadRef)

	logEvent := &finopsv1alpha1.LogEvent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      logEventName,
			Namespace: k8sEvent.Namespace,
		},
	}

	// Create or update
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, logEvent, func() error {
		// Set spec
		logEvent.Spec = finopsv1alpha1.LogEventSpec{
			EventRef: finopsv1alpha1.EventReference{
				Name:      k8sEvent.Name,
				Namespace: k8sEvent.Namespace,
				UID:       string(k8sEvent.UID),
				Reason:    k8sEvent.Reason,
				Message:   k8sEvent.Message,
				Source:    k8sEvent.Source.Component,
				FirstTimestamp: &metav1.Time{Time: k8sEvent.FirstTimestamp.Time},
				LastTimestamp:  &metav1.Time{Time: k8sEvent.LastTimestamp.Time},
				Count:         k8sEvent.Count,
				Type:          k8sEvent.Type,
			},
			InvolvedObject: finopsv1alpha1.ObjectReference{
				Kind:      k8sEvent.InvolvedObject.Kind,
				Namespace: k8sEvent.InvolvedObject.Namespace,
				Name:      k8sEvent.InvolvedObject.Name,
				UID:       k8sEvent.InvolvedObject.UID,
			},
			WorkloadRef: workloadRef,
			CostCenter:  costCenter,
			NodeName:    k8sEvent.Source.Host,
		}

		// Add node cost if available
		if nodeCost != nil {
			logEvent.Spec.NodeCost = &finopsv1alpha1.NodeCostRef{
				NodeName:     nodeCost.Name,
				InstanceType: nodeCost.Spec.InstanceType,
				HourlyPrice:  nodeCost.Status.HourlyPriceUSD,
				IsSpot:       nodeCost.Spec.IsSpot,
			}
		}

		// Set status
		logEvent.Status = finopsv1alpha1.LogEventStatus{
			ProcessedAt:      &metav1.Time{Time: time.Now()},
			ProcessedBy:      EventControllerName,
			EventReason:      k8sEvent.Reason,
			EventMessage:     k8sEvent.Message,
		}

		// Set owner reference to the involved object (if it's a namespaced resource)
		if k8sEvent.InvolvedObject.Namespace != "" {
			ownerRef := metav1.OwnerReference{
				APIVersion: k8sEvent.InvolvedObject.APIVersion,
				Kind:       k8sEvent.InvolvedObject.Kind,
				Name:       k8sEvent.InvolvedObject.Name,
				UID:        k8sEvent.InvolvedObject.UID,
			}
			logEvent.SetOwnerReferences([]metav1.OwnerReference{ownerRef})
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create/update LogEvent: %w", err)
	}

	return logEvent, nil
}

// generateLogEventName generates a deterministic name for the LogEvent
func (r *EventWatcherReconciler) generateLogEventName(k8sEvent *corev1.Event, workloadRef *finopsv1alpha1.WorkloadReference) string {
	// Format: <reason>-<namespace>-<workload>-<timestamp>
	// Truncate to fit Kubernetes naming constraints (max 253 chars)
	base := fmt.Sprintf("%s-%s-%s-%d",
		sanitizeForDNS(k8sEvent.Reason),
		sanitizeForDNS(k8sEvent.Namespace),
		sanitizeForDNS(workloadRef.Name),
		k8sEvent.LastTimestamp.Unix(),
	)

	if len(base) > 253 {
		base = base[:253]
	}

	return base
}

// sanitizeForDNS sanitizes a string for use in DNS names
func sanitizeForDNS(s string) string {
	// Replace invalid characters with hyphens
	result := make([]rune, 0, len(s))
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result = append(result, r)
		} else if r >= 'A' && r <= 'Z' {
			result = append(result, r-'A'+'a')
		} else {
			result = append(result, '-')
		}
	}
	// Trim leading/trailing hyphens
	return string(result)
}

// incrementEventCounter increments the TSDB counter for the event
func (r *EventWatcherReconciler) incrementEventCounter(reason, namespace, workload, costCenter string) {
	// Use "unknown" for empty values to avoid empty label values
	if workload == "" {
		workload = "unknown"
	}
	if costCenter == "" {
		costCenter = "unknown"
	}
	if namespace == "" {
		namespace = "unknown"
	}

	finopsEventsTotal.WithLabelValues(reason, namespace, workload, costCenter).Inc()
}

// IncrementLogErrorCounter increments the log error counter for container logs
func (r *EventWatcherReconciler) IncrementLogErrorCounter(namespace, workload, container, level string) {
	if workload == "" {
		workload = "unknown"
	}
	if namespace == "" {
		namespace = "unknown"
	}
	if container == "" {
		container = "unknown"
	}
	if level == "" {
		level = "error"
	}

	finopsLogErrorsTotal.WithLabelValues(namespace, workload, container, level).Inc()
}

// SetupWithManager sets up the controller with the Manager
func (r *EventWatcherReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Event{}).
		WithEventFilter(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				return r.shouldProcessEvent(e.Object)
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				return r.shouldProcessEvent(e.ObjectNew)
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return false
			},
		}).
		Complete(r)
}
