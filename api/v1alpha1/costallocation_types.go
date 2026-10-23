package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CostAllocationSpec defines the desired state of CostAllocation
type CostAllocationSpec struct {
	// WorkloadRef references the workload this allocation belongs to
	WorkloadRef WorkloadReference `json:"workloadRef"`

	// Namespace is the namespace of the workload
	Namespace string `json:"namespace"`

	// CostCenter is the cost center label from namespace
	CostCenter string `json:"costCenter,omitempty"`

	// Team is the team label from namespace
	Team string `json:"team,omitempty"`
}

// WorkloadReference identifies a workload
type WorkloadReference struct {
	// Name of the workload
	Name string `json:"name"`

	// Kind of the workload (Deployment, StatefulSet, DaemonSet, Job, CronJob, Pod)
	Kind string `json:"kind"`

	// UID of the workload owner
	UID string `json:"uid,omitempty"`
}

// ContainerAllocation represents cost allocation for a single container
type ContainerAllocation struct {
	// Name of the container
	Name string `json:"name"`

	// CPURequested in cores
	CPURequested float64 `json:"cpuRequested"`

	// CPULimit in cores
	CPULimit float64 `json:"cpuLimit,omitempty"`

	// CPUUsed in cores
	CPUUsed float64 `json:"cpuUsed"`

	// MemoryRequested in bytes
	MemoryRequested int64 `json:"memoryRequested"`

	// MemoryLimit in bytes
	MemoryLimit int64 `json:"memoryLimit,omitempty"`

	// MemoryUsed in bytes
	MemoryUsed int64 `json:"memoryUsed"`

	// GPURequested count
	GPURequested int `json:"gpuRequested,omitempty"`

	// HourlyCost in USD
	HourlyCost float64 `json:"hourlyCost"`

	// CPUShare of node allocatable
	CPUShare float64 `json:"cpuShare,omitempty"`

	// MemoryShare of node allocatable
	MemoryShare float64 `json:"memoryShare,omitempty"`
}

// PVAllocation represents cost allocation for a PVC
type PVAllocation struct {
	// PVCName is the name of the PVC
	PVCName string `json:"pvcName"`

	// StorageClass name
	StorageClass string `json:"storageClass"`

	// RequestedGB in GiB
	RequestedGB float64 `json:"requestedGB"`

	// UsedGB in GiB
	UsedGB float64 `json:"usedGB"`

	// MonthlyCost in USD
	MonthlyCost float64 `json:"monthlyCost"`

	// HourlyCost in USD
	HourlyCost float64 `json:"hourlyCost"`
}

// CostAllocationStatus defines the observed state of CostAllocation
type CostAllocationStatus struct {
	// HourlyCostUSD is the total hourly cost in USD
	HourlyCostUSD float64 `json:"hourlyCostUSD"`

	// CPUCoresRequested total CPU cores requested
	CPUCoresRequested float64 `json:"cpuCoresRequested"`

	// CPUCoresUsed total CPU cores used
	CPUCoresUsed float64 `json:"cpuCoresUsed"`

	// MemoryBytesRequested total memory requested in bytes
	MemoryBytesRequested int64 `json:"memoryBytesRequested"`

	// MemoryBytesUsed total memory used in bytes
	MemoryBytesUsed int64 `json:"memoryBytesUsed"`

	// EfficiencyRatio is max(CPUUsed/CPURequested, MemoryUsed/MemoryRequested)
	EfficiencyRatio float64 `json:"efficiencyRatio"`

	// Containers allocation breakdown
	Containers []ContainerAllocation `json:"containers,omitempty"`

	// PVAllocations storage allocation breakdown
	PVAllocations []PVAllocation `json:"pvAllocations,omitempty"`

	// NetworkAllocationUSD network cost in USD per hour
	NetworkAllocationUSD float64 `json:"networkAllocationUSD,omitempty"`

	// LastUpdate timestamp of last reconciliation
	LastUpdate metav1.Time `json:"lastUpdate"`

	// Conditions represent the current state
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// CostAllocation is the Schema for the costallocations API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Workload",type=string,JSONPath=.spec.workloadRef.name
// +kubebuilder:printcolumn:name="Kind",type=string,JSONPath=.spec.workloadRef.kind
// +kubebuilder:printcolumn:name="HourlyCost",type=string,JSONPath=.status.hourlyCostUSD
// +kubebuilder:printcolumn:name="Efficiency",type=string,JSONPath=.status.efficiencyRatio
// +kubebuilder:printcolumn:name="CostCenter",type=string,JSONPath=.spec.costCenter
// +kubebuilder:printcolumn:name="Team",type=string,JSONPath=.spec.team
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=.metadata.creationTimestamp
type CostAllocation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CostAllocationSpec   `json:"spec,omitempty"`
	Status CostAllocationStatus `json:"status,omitempty"`
}

// CostAllocationList contains a list of CostAllocation
// +kubebuilder:object:root=true
type CostAllocationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CostAllocation `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CostAllocation{}, &CostAllocationList{})
}
