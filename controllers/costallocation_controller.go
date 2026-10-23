package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	finopsv1alpha1 "github.com/yourorg/kube-native-finops/api/v1alpha1"
	"github.com/yourorg/kube-native-finops/pkg/allocation"
)

// CostAllocationReconciler reconciles a CostAllocation object
type CostAllocationReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Logger   logr.Logger
	Engine   *allocation.AllocationEngine
	ClusterID string
}

//+kubebuilder:rbac:groups=finops.io,resources=costallocations,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=finops.io,resources=costallocations/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=finops.io,resources=costallocations/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=pods;namespaces;nodes;persistentvolumeclaims;persistentvolumes;storageclasses,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *CostAllocationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger.WithValues("costallocation", req.NamespacedName)
	logger.V(1).Info("Reconciling CostAllocation")

	// Fetch the CostAllocation instance
	ca := &finopsv1alpha1.CostAllocation{}
	err := r.Get(ctx, req.NamespacedName, ca)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request
			logger.V(1).Info("CostAllocation resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get CostAllocation")
		return ctrl.Result{}, err
	}

	// Check if workload still exists
	workloadExists, err := r.workloadExists(ctx, ca)
	if err != nil {
		logger.Error(err, "Failed to check workload existence")
		return ctrl.Result{}, err
	}

	if !workloadExists {
		// Workload deleted, clean up CostAllocation
		logger.Info("Workload no longer exists, deleting CostAllocation")
		if err := r.Delete(ctx, ca); err != nil {
			logger.Error(err, "Failed to delete CostAllocation")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Get allocation from engine
	workloadKey := fmt.Sprintf("%s/%s/%s", ca.Namespace, ca.Spec.WorkloadRef.Name, ca.Spec.WorkloadRef.Kind)
	wa := r.Engine.GetWorkloadAllocation(workloadKey)
	if wa == nil {
		logger.V(1).Info("No allocation data yet for workload")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Update status
	return r.updateStatus(ctx, ca, wa)
}

func (r *CostAllocationReconciler) workloadExists(ctx context.Context, ca *finopsv1alpha1.CostAllocation) (bool, error) {
	// Try to get the workload based on kind
	var obj client.Object
	switch ca.Spec.WorkloadRef.Kind {
	case "Deployment":
		obj = &appsv1.Deployment{}
	case "StatefulSet":
		obj = &appsv1.StatefulSet{}
	case "DaemonSet":
		obj = &appsv1.DaemonSet{}
	case "Job":
		obj = &batchv1.Job{}
	case "CronJob":
		obj = &batchv1.CronJob{}
	case "Pod":
		obj = &corev1.Pod{}
	default:
		return false, fmt.Errorf("unknown workload kind: %s", ca.Spec.WorkloadRef.Kind)
	}

	key := types.NamespacedName{Namespace: ca.Namespace, Name: ca.Spec.WorkloadRef.Name}
	err := r.Get(ctx, key, obj)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (r *CostAllocationReconciler) updateStatus(ctx context.Context, ca *finopsv1alpha1.CostAllocation, wa *allocation.WorkloadAllocation) (ctrl.Result, error) {
	// Prepare new status
	newStatus := finopsv1alpha1.CostAllocationStatus{
		HourlyCostUSD:         wa.TotalHourlyCost,
		CPUCoresRequested:     wa.TotalCPURequested,
		CPUCoresUsed:          wa.TotalCPUUsed,
		MemoryBytesRequested:  int64(wa.TotalMemRequested * 1024 * 1024 * 1024),
		MemoryBytesUsed:       int64(wa.TotalMemUsed * 1024 * 1024 * 1024),
		EfficiencyRatio:       wa.EfficiencyRatio,
		NetworkAllocationUSD:  wa.NetworkAllocation,
		LastUpdate:            metav1.Now(),
		Conditions:            ca.Status.Conditions,
	}

	// Convert containers
	newStatus.Containers = make([]finopsv1alpha1.ContainerAllocation, len(wa.Containers))
	for i, c := range wa.Containers {
		newStatus.Containers[i] = finopsv1alpha1.ContainerAllocation{
			Name:             c.Name,
			CPURequested:     c.CPURequested,
			CPULimit:         c.CPULimit,
			CPUUsed:          c.CPUUsed,
			MemoryRequested:  int64(c.MemRequested * 1024 * 1024 * 1024),
			MemoryLimit:      int64(c.MemLimit * 1024 * 1024 * 1024),
			MemoryUsed:       int64(c.MemUsed * 1024 * 1024 * 1024),
			GPURequested:     c.GPURequested,
			HourlyCost:       c.HourlyCost,
			CPUShare:         c.CPUShare,
			MemoryShare:      c.MemShare,
		}
	}

	// Convert PV allocations
	newStatus.PVAllocations = make([]finopsv1alpha1.PVAllocation, len(wa.PVAllocations))
	for i, p := range wa.PVAllocations {
		newStatus.PVAllocations[i] = finopsv1alpha1.PVAllocation{
			PVCName:      p.PVCName,
			StorageClass: p.StorageClass,
			RequestedGB:  p.RequestedGB,
			UsedGB:       p.UsedGB,
			MonthlyCost:  p.MonthlyCost,
			HourlyCost:   p.HourlyCost,
		}
	}

	// Update conditions
	newStatus.Conditions = updateCondition(newStatus.Conditions, metav1.Condition{
		Type:    "Reconciled",
		Status:  metav1.ConditionTrue,
		Reason:  "ReconciliationSuccessful",
		Message: "Cost allocation updated successfully",
	})

	// Check if status changed
	if !statusEqual(ca.Status, newStatus) {
		ca.Status = newStatus
		if err := r.Status().Update(ctx, ca); err != nil {
			r.Logger.Error(err, "Failed to update CostAllocation status")
			return ctrl.Result{}, err
		}
		r.Logger.V(1).Info("Updated CostAllocation status", "hourlyCost", wa.TotalHourlyCost)
		r.Recorder.Event(ca, "Normal", "Updated", "Cost allocation updated")
	}

	return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
}

func statusEqual(old, new finopsv1alpha1.CostAllocationStatus) bool {
	if old.HourlyCostUSD != new.HourlyCostUSD ||
		old.CPUCoresRequested != new.CPUCoresRequested ||
		old.CPUCoresUsed != new.CPUCoresUsed ||
		old.MemoryBytesRequested != new.MemoryBytesRequested ||
		old.MemoryBytesUsed != new.MemoryBytesUsed ||
		old.EfficiencyRatio != new.EfficiencyRatio ||
		old.NetworkAllocationUSD != new.NetworkAllocationUSD {
		return false
	}
	return true
}

func updateCondition(conditions []metav1.Condition, newCond metav1.Condition) []metav1.Condition {
	now := metav1.Now()
	newCond.LastTransitionTime = now
	newCond.ObservedGeneration = 0 // Will be set by controller

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

// SetupWithManager sets up the controller with the Manager
func (r *CostAllocationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&finopsv1alpha1.CostAllocation{}).
		Complete(r)
}
