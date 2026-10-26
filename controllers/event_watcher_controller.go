package controllers

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	finopsv1alpha1 "github.com/yourorg/kube-native-finops/api/v1alpha1"
)

// EventWatcherController watches Kubernetes events and creates LogEvent CRDs
type EventWatcherController struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// SetupWithManager sets up the controller with the Manager.
func (r *EventWatcherController) SetupWithManager(mgr ctrl.Manager) error {
	// Create a new controller for watching Kubernetes events
	c, err := controller.New("event-watcher", mgr, controller.Options{
		Reconciler: r,
	})
	if err != nil {
		return err
	}

	// Watch for Kubernetes events
	err = c.Watch(
		source.Kind(mgr.GetCache(), &corev1.Event{}),
		&handler.EnqueueRequestForObject{},
		builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				return r.isRelevantEvent(e.Object.(*corev1.Event))
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				return r.isRelevantEvent(e.ObjectNew.(*corev1.Event))
			},
		}),
	)
	if err != nil {
		return err
	}

	return nil
}

// isRelevantEvent checks if the event is one we care about
func (r *EventWatcherController) isRelevantEvent(k8sEvent *corev1.Event) bool {
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
	return relevantReasons[k8sEvent.Reason]
}

// Reconcile processes Kubernetes events and creates LogEvent CRDs
func (r *EventWatcherController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the Kubernetes event
	k8sEvent := &corev1.Event{}
	err := r.Get(ctx, req.NamespacedName, k8sEvent)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get Kubernetes event")
		return ctrl.Result{}, err
	}

	// Check if it's a relevant event
	if !r.isRelevantEvent(k8sEvent) {
		return ctrl.Result{}, nil
	}

	// Create LogEvent from Kubernetes event
	logEvent, err := CreateLogEventFromK8sEvent(ctx, r.Client, r.Scheme, k8sEvent)
	if err != nil {
		logger.Error(err, "Failed to create LogEvent from Kubernetes event")
		return ctrl.Result{}, err
	}

	if logEvent == nil {
		return ctrl.Result{}, nil
	}

	// Check if LogEvent already exists
	existing := &finopsv1alpha1.LogEvent{}
	err = r.Get(ctx, types.NamespacedName{Name: logEvent.Name, Namespace: logEvent.Namespace}, existing)
	if err == nil {
		// Update existing event count and timestamp
		existing.Spec.Count = k8sEvent.Count
		existing.Spec.LastTimestamp = k8sEvent.LastTimestamp
		existing.Spec.Message = k8sEvent.Message
		if err := r.Update(ctx, existing); err != nil {
			logger.Error(err, "Failed to update existing LogEvent")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	if !errors.IsNotFound(err) {
		logger.Error(err, "Failed to check existing LogEvent")
		return ctrl.Result{}, err
	}

	// Create new LogEvent
	if err := r.Create(ctx, logEvent); err != nil {
		logger.Error(err, "Failed to create LogEvent")
		return ctrl.Result{}, err
	}

	logger.Info("Created LogEvent from Kubernetes event", "logEvent", logEvent.Name, "reason", k8sEvent.Reason)
	return ctrl.Result{}, nil
}

// AddEventWatcherController adds the event watcher controller to the manager
func AddEventWatcherController(mgr manager.Manager) error {
	return (&EventWatcherController{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("event-watcher"),
	}).SetupWithManager(mgr)
}
