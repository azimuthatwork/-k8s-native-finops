package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NodeCostSpec defines the desired state of NodeCost
type NodeCostSpec struct {
	// NodeName is the name of the node
	NodeName string `json:"nodeName"`

	// InstanceType of the node
	InstanceType string `json:"instanceType"`

	// Zone where the node runs
	Zone string `json:"zone"`

	// CostCenter from node labels
	CostCenter string `json:"costCenter,omitempty"`

	// CapacityType: on-demand or spot
	CapacityType string `json:"capacityType"`
}

// NodeCostStatus defines the observed state of NodeCost
type NodeCostStatus struct {
	// HourlyCostUSD is the total hourly cost of the node in USD
	HourlyCostUSD float64 `json:"hourlyCostUSD"`

	// CPUAllocatable in cores
	CPUAllocatable float64 `json:"cpuAllocatable"`

	// MemoryAllocatable in bytes
	MemoryAllocatable int64 `json:"memoryAllocatable"`

	// CPUAllocated in cores (sum of pod requests)
	CPUAllocated float64 `json:"cpuAllocated"`

	// MemoryAllocated in bytes (sum of pod requests)
	MemoryAllocated int64 `json:"memoryAllocated"`

	// CPUUtilization ratio (allocated/allocatable)
	CPUUtilization float64 `json:"cpuUtilization"`

	// MemoryUtilization ratio (allocated/allocatable)
	MemoryUtilization float64 `json:"memoryUtilization"`

	// UnallocatedCostUSD cost of unallocated capacity
	UnallocatedCostUSD float64 `json:"unallocatedCostUSD"`

	// SharedCostUSD control plane / shared infrastructure cost
	SharedCostUSD float64 `json:"sharedCostUSD"`

	// DaemonSetCostUSD DaemonSet overhead cost
	DaemonSetCostUSD float64 `json:"daemonSetCostUSD"`

	// IsSpot indicates if node is spot/preemptible
	IsSpot bool `json:"isSpot"`

	// GPUAllocatable count
	GPUAllocatable int `json:"gpuAllocatable,omitempty"`

	// GPUAllocated count
	GPUAllocated int `json:"gpuAllocated,omitempty"`

	// LastUpdate timestamp of last reconciliation
	LastUpdate metav1.Time `json:"lastUpdate"`

	// Conditions represent the current state
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// NodeCost is the Schema for the nodecosts API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Node",type=string,JSONPath=.spec.nodeName
// +kubebuilder:printcolumn:name="InstanceType",type=string,JSONPath=.spec.instanceType
// +kubebuilder:printcolumn:name="Zone",type=string,JSONPath=.spec.zone
// +kubebuilder:printcolumn:name="HourlyCost",type=string,JSONPath=.status.hourlyCostUSD
// +kubebuilder:printcolumn:name="CPUUtil",type=string,JSONPath=.status.cpuUtilization
// +kubebuilder:printcolumn:name="MemUtil",type=string,JSONPath=.status.memoryUtilization
// +kubebuilder:printcolumn:name="CapacityType",type=string,JSONPath=.spec.capacityType
// +kubebuilder:printcolumn:name="CostCenter",type=string,JSONPath=.spec.costCenter
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=.metadata.creationTimestamp
type NodeCost struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodeCostSpec   `json:"spec,omitempty"`
	Status NodeCostStatus `json:"status,omitempty"`
}

// NodeCostList contains a list of NodeCost
// +kubebuilder:object:root=true
type NodeCostList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodeCost `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NodeCost{}, &NodeCostList{})
}
