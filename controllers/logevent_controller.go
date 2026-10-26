package controllers

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	finopsv1alpha1 "github.com/yourorg/kube-native-finops/api/v1alpha1"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	eventCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "finops_events_total",
			Help: "Total number of Kubernetes events processed by type",
		},
		[]string{"reason", "namespace", "workload", "cost_center", "type"},
	)
)

func init() {
	metrics.Registry.MustRegister(eventCounter)
}

// LogEventReconciler reconciles a LogEvent object
type LogEventReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=finops.io,resources=logevents,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=finops.io,resources=logevents/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=finops.io,resources=logevents/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=deployments;statefulsets;daemonsets,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *LogEventReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the LogEvent instance
	logEvent := &finopsv1alpha1.LogEvent{}
	err := r.Get(ctx, req.NamespacedName, logEvent)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			logger.Info("LogEvent resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		logger.Error(err, "Failed to get LogEvent")
		return ctrl.Result{}, err
	}

	// Check if already processed
	if logEvent.Status.Processed {
		logger.V(1).Info("LogEvent already processed", "name", logEvent.Name)
		return ctrl.Result{}, nil
	}

	// Process the event: emit metrics, enrich, etc.
	if err := r.processEvent(ctx, logEvent); err != nil {
		logger.Error(err, "Failed to process event")
		return ctrl.Result{}, err
	}

	// Mark as processed
	now := metav1.Now()
	logEvent.Status.Processed = true
	logEvent.Status.ProcessedAt = &now
	logEvent.Status.Conditions = []metav1.Condition{
		{
			Type:               "Processed",
			Status:             metav1.ConditionTrue,
			LastTransitionTime: now,
			Reason:             "EventProcessed",
			Message:            "Event has been processed and metrics emitted",
		},
	}

	if err := r.Status().Update(ctx, logEvent); err != nil {
		logger.Error(err, "Failed to update LogEvent status")
		return ctrl.Result{}, err
	}

	logger.Info("Successfully processed LogEvent", "name", logEvent.Name, "reason", logEvent.Spec.Reason)
	return ctrl.Result{}, nil
}

// processEvent handles the event processing logic
func (r *LogEventReconciler) processEvent(ctx context.Context, logEvent *finopsv1alpha1.LogEvent) error {
	// Emit Prometheus metric
	costCenter := logEvent.Spec.CostCenter
	if costCenter == "" {
		costCenter = "unknown"
	}
	workload := logEvent.Spec.WorkloadRef.Name
	if workload == "" {
		workload = "unknown"
	}
	eventType := logEvent.Spec.Type
	if eventType == "" {
		eventType = "Warning"
	}

	eventCounter.With(prometheus.Labels{
		"reason":      logEvent.Spec.Reason,
		"namespace":   logEvent.Spec.Namespace,
		"workload":    workload,
		"cost_center": costCenter,
		"type":        eventType,
	}).Add(float64(logEvent.Spec.Count))

	// Enrich with NodeCost if nodeName is available
	if logEvent.Spec.NodeName != "" {
		nodeCost := &finopsv1alpha1.NodeCost{}
		err := r.Get(ctx, types.NamespacedName{Name: logEvent.Spec.NodeName}, nodeCost)
		if err == nil {
			// Could enrich logEvent with node cost info here
			_ = nodeCost
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *LogEventReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&finopsv1alpha1.LogEvent{}).
		Complete(r)
}

// EventWatcher watches Kubernetes events and creates LogEvent CRDs
type EventWatcher struct {
	client.Client
	Scheme *runtime.Scheme
}

// Start begins watching for Kubernetes events
func (w *EventWatcher) Start(ctx context.Context) error {
	// This would typically be implemented as a separate controller or informer
	// that watches core/v1 events and creates LogEvent CRDs
	// For now, this is a placeholder for the event watching logic
	return nil
}

// CreateLogEventFromK8sEvent creates a LogEvent CRD from a Kubernetes event
func CreateLogEventFromK8sEvent(ctx context.Context, c client.Client, scheme *runtime.Scheme, k8sEvent *corev1.Event) (*finopsv1alpha1.LogEvent, error) {
	// Filter for relevant event reasons
	relevantReasons := map[string]bool{
		"OOMKilling":       true,
		"Evicted":          true,
		"FailedScheduling": true,
		"Killing":          true,
		"FailedMount":      true,
		"CrashLoopBackOff": true,
		"BackOff":          true,
		"OutOfMemory":      true,
		"MemoryPressure":   true,
		"DiskPressure":     true,
		"PIDPressure":      true,
	}

	if !relevantReasons[k8sEvent.Reason] {
		return nil, nil // Not a relevant event
	}

	// Determine workload reference
	workloadRef := finopsv1alpha1.WorkloadReference{}
	if k8sEvent.InvolvedObject.Kind == "Pod" {
		// Try to get the pod to find its owner
		pod := &corev1.Pod{}
		err := c.Get(ctx, types.NamespacedName{Namespace: k8sEvent.InvolvedObject.Namespace, Name: k8sEvent.InvolvedObject.Name}, pod)
		if err == nil && len(pod.OwnerReferences) > 0 {
			owner := pod.OwnerReferences[0]
			workloadRef = finopsv1alpha1.WorkloadReference{
				Name: owner.Name,
				Kind: owner.Kind,
				UID:  string(owner.UID),
			}
		} else {
			workloadRef = finopsv1alpha1.WorkloadReference{
				Name: k8sEvent.InvolvedObject.Name,
				Kind: "Pod",
				UID:  string(k8sEvent.InvolvedObject.UID),
			}
		}
	} else {
		workloadRef = finopsv1alpha1.WorkloadReference{
			Name: k8sEvent.InvolvedObject.Name,
			Kind: k8sEvent.InvolvedObject.Kind,
			UID:  string(k8sEvent.InvolvedObject.UID),
		}
	}

	// Get namespace labels for cost-center and team
	ns := &corev1.Namespace{}
	costCenter := ""
	team := ""
	err := c.Get(ctx, types.NamespacedName{Name: k8sEvent.InvolvedObject.Namespace}, ns)
	if err == nil {
		if ns.Labels != nil {
			costCenter = ns.Labels["cost-center"]
			team = ns.Labels["team"]
		}
	}

	logEvent := &finopsv1alpha1.LogEvent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s-%d", k8sEvent.InvolvedObject.Namespace, k8sEvent.InvolvedObject.Name, k8sEvent.LastTimestamp.Unix()),
			Namespace: k8sEvent.InvolvedObject.Namespace,
		},
		Spec: finopsv1alpha1.LogEventSpec{
			Namespace:      k8sEvent.InvolvedObject.Namespace,
			WorkloadRef:    workloadRef,
			Reason:         k8sEvent.Reason,
			Message:        k8sEvent.Message,
			Source:         k8sEvent.Source.Component,
			FirstTimestamp: k8sEvent.FirstTimestamp,
			LastTimestamp:  k8sEvent.LastTimestamp,
			Count:          k8sEvent.Count,
			Type:           k8sEvent.Type,
			NodeName:       k8sEvent.InvolvedObject.FieldPath,
			CostCenter:     costCenter,
			Team:           team,
		},
	}

	// Set owner reference if possible
	if k8sEvent.InvolvedObject.UID != "" {
		controllerutil.SetControllerReference(k8sEvent.InvolvedObject, logEvent, scheme)
	}

	return logEvent, nil
}
