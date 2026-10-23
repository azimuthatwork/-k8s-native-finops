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

// PVCostReconciler reconciles a PVCost object
type PVCostReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Logger   logr.Logger
	Engine   *allocation.AllocationEngine
	ClusterID string
}

//+kubebuilder:rbac:groups=finops.io,resources=pvcosts,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=finops.io,resources=pvcosts/status,verbs=get;update;patch
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims;persistentvolumes;storageclasses,verbs=get;list;watch

func (r *PVCostReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.WithValues("pvcost", req.NamespacedName)
	logger.V(1).Info("Reconciling PVCost")

	pc := &finopsv1alpha1.PVCost{}
	err := r.Get(ctx, req.NamespacedName, pc)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.V(1).Info("PVCost resource not found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get PVCost")
		return ctrl.Result{}, err
	}

	// Check if PVC still exists
	pvc := &corev1.PersistentVolumeClaim{}
	err = r.Get(ctx, client.ObjectKey{Namespace: pc.Namespace, Name: pc.Spec.PVCName}, pvc)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("PVC no longer exists, deleting PVCost")
			if err := r.Delete(ctx, pc); err != nil {
				logger.Error(err, "Failed to delete PVCost")
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get PVC")
		return ctrl.Result{}, err
	}

	// Get allocation from engine
	key := pc.Namespace + "/" + pc.Spec.PVCName
	pa := r.Engine.GetPVCAllocation(key)
	if pa == nil {
		logger.V(1).Info("No allocation data yet for PVC")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	return r.updateStatus(ctx, pc, pa)
}

func (r *PVCostReconciler) updateStatus(ctx context.Context, pc *finopsv1alpha1.PVCost, pa *allocation.PVAllocation) (ctrl.Result, error) {
	utilization := 0.0
	if pa.RequestedGB > 0 {
		utilization = pa.UsedGB / pa.RequestedGB
	}

	newStatus := finopsv1alpha1.PVCostStatus{
		MonthlyCostUSD:     pa.MonthlyCost,
		HourlyCostUSD:      pa.HourlyCost,
		RequestedGB:        pa.RequestedGB,
		UsedGB:             pa.UsedGB,
		UtilizationRatio:   utilization,
		Phase:              string(pvc.Status.Phase), // Need access to pvc
		LastUpdate:         metav1.Now(),
		Conditions:         pc.Status.Conditions,
	}

	newStatus.Conditions = updateCondition(newStatus.Conditions, metav1.Condition{
		Type:    "Reconciled",
		Status:  metav1.ConditionTrue,
		Reason:  "ReconciliationSuccessful",
		Message: "PVC cost updated successfully",
	})

	if !pvcStatusEqual(pc.Status, newStatus) {
		pc.Status = newStatus
		if err := r.Status().Update(ctx, pc); err != nil {
			r.Logger.Error(err, "Failed to update PVCost status")
			return ctrl.Result{}, err
		}
		r.Logger.V(1).Info("Updated PVCost status", "pvc", pa.PVCName, "monthlyCost", pa.MonthlyCost)
		r.Recorder.Event(pc, "Normal", "Updated", "PVC cost updated")
	}

	return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
}

func pvcStatusEqual(old, new finopsv1alpha1.PVCostStatus) bool {
	return old.MonthlyCostUSD == new.MonthlyCostUSD &&
		old.HourlyCostUSD == new.HourlyCostUSD &&
		old.RequestedGB == new.RequestedGB &&
		old.UsedGB == new.UsedGB &&
		old.UtilizationRatio == new.UtilizationRatio
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

func (r *PVCostReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&finopsv1alpha1.PVCost{}).
		Complete(r)
}
