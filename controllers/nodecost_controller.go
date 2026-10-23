package controllers

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	finopsv1alpha1 "github.com/yourorg/kube-native-finops/api/v1alpha1"
	"github.com/yourorg/kube-native-finops/pkg/allocation"
)

// NodeCostReconciler reconciles a NodeCost object
type NodeCostReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Logger   logr.Logger
	Engine   *allocation.AllocationEngine
	ClusterID string
}

//+kubebuilder:rbac:groups=finops.io,resources=nodecosts,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=finops.io,resources=nodecosts/status,verbs=get;update;patch
//+kubebuilder:rbac:groups="",resources=nodes;pods,verbs=get;list;watch

func (r *NodeCostReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.WithValues("nodecost", req.NamespacedName)
	logger.V(1).Info("Reconciling NodeCost")

	nc := &finopsv1alpha1.NodeCost{}
	err := r.Get(ctx, req.NamespacedName, nc)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.V(1).Info("NodeCost resource not found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get NodeCost")
		return ctrl.Result{}, err
	}

	// Check if node still exists
	node := &corev1.Node{}
	err = r.Get(ctx, client.ObjectKey{Name: nc.Spec.NodeName}, node)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Node no longer exists, deleting NodeCost")
			if err := r.Delete(ctx, nc); err != nil {
				logger.Error(err, "Failed to delete NodeCost")
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get node")
		return ctrl.Result{}, err
	}

	// Get allocation from engine
	na := r.Engine.GetNodeAllocation(nc.Spec.NodeName)
	if na == nil {
		logger.V(1).Info("No allocation data yet for node")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	return r.updateStatus(ctx, nc, na)
}

func (r *NodeCostReconciler) updateStatus(ctx context.Context, nc *finopsv1alpha1.NodeCost, na *allocation.NodeAllocation) (ctrl.Result, error) {
	newStatus := finopsv1alpha1.NodeCostStatus{
		HourlyCostUSD:       na.HourlyCost,
		CPUAllocatable:      na.CPUAllocatable,
		MemoryAllocatable:   int64(na.MemAllocatable * 1024 * 1024 * 1024),
		CPUAllocated:        na.CPUAllocated,
		MemoryAllocated:     int64(na.MemAllocated * 1024 * 1024 * 1024),
		CPUUtilization:      na.CPUUtilization,
		MemoryUtilization:   na.MemUtilization,
		UnallocatedCostUSD:  na.UnallocatedCost,
		SharedCostUSD:       na.SharedCost,
		DaemonSetCostUSD:    na.DaemonSetCost,
		IsSpot:              na.IsSpot,
		LastUpdate:          metav1.Now(),
		Conditions:          nc.Status.Conditions,
	}

	newStatus.Conditions = updateCondition(newStatus.Conditions, metav1.Condition{
		Type:    "Reconciled",
		Status:  metav1.ConditionTrue,
		Reason:  "ReconciliationSuccessful",
		Message: "Node cost updated successfully",
	})

	if !nodeStatusEqual(nc.Status, newStatus) {
		nc.Status = newStatus
		if err := r.Status().Update(ctx, nc); err != nil {
			r.Logger.Error(err, "Failed to update NodeCost status")
			return ctrl.Result{}, err
		}
		r.Logger.V(1).Info("Updated NodeCost status", "node", na.NodeName, "hourlyCost", na.HourlyCost)
		r.Recorder.Event(nc, "Normal", "Updated", "Node cost updated")
	}

	return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
}

func nodeStatusEqual(old, new finopsv1alpha1.NodeCostStatus) bool {
	return old.HourlyCostUSD == new.HourlyCostUSD &&
		old.CPUAllocatable == new.CPUAllocatable &&
		old.MemoryAllocatable == new.MemoryAllocatable &&
		old.CPUAllocated == new.CPUAllocated &&
		old.MemoryAllocated == new.MemoryAllocated &&
		old.CPUUtilization == new.CPUUtilization &&
		old.MemoryUtilization == new.MemoryUtilization &&
		old.UnallocatedCostUSD == new.UnallocatedCostUSD &&
		old.SharedCostUSD == new.SharedCostUSD &&
		old.DaemonSetCostUSD == new.DaemonSetCostUSD &&
		old.IsSpot == new.IsSpot
}

func updateCondition(conditions []metav1.Condition, newCond metav1.Condition) []metav1.Condition {
	now := metav1.Now()
	newCond.LastTransitionTime = now

	for i, c := range conditions {
		if c.Type == newCond.Type {
			if c.Status != newCond.Status {
				newCond.LastTransitionTime = now
			} else {
				newCond.LastTransitionTime = c.LastTransitionTime
			}
			conditions[i] = newCond
			return conditions
		}
	}
	return append(conditions, newCond)
}

func (r *NodeCostReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&finopsv1alpha1.NodeCost{}).
		Complete(r)
}
