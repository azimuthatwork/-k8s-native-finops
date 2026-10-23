package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CostHistorySpec defines the desired state of CostHistory
type CostHistorySpec struct {
	// Timestamp of the snapshot
	Timestamp metav1.Time `json:"timestamp"`

	// Cluster name
	Cluster string `json:"cluster"`

	// TotalHourlyCostUSD cluster total hourly cost
	TotalHourlyCostUSD float64 `json:"totalHourlyCostUSD"`

	// IdleCostUSD cluster idle cost
	IdleCostUSD float64 `json:"idleCostUSD"`

	// WorkloadCount number of workloads
	WorkloadCount int `json:"workloadCount"`

	// NodeCount number of nodes
	NodeCount int `json:"nodeCount"`
}

// CostHistoryStatus defines the observed state
type CostHistoryStatus struct {
	// LastUpdate timestamp of last reconciliation
	LastUpdate metav1.Time `json:"lastUpdate"`

	// Conditions represent the current state
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// CostHistory is the Schema for the costhistories API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Timestamp",type=date,JSONPath=.spec.timestamp
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=.spec.cluster
// +kubebuilder:printcolumn:name="TotalHourlyCost",type=string,JSONPath=.spec.totalHourlyCostUSD
// +kubebuilder:printcolumn:name="IdleCost",type=string,JSONPath=.spec.idleCostUSD
// +kubebuilder:printcolumn:name="Workloads",type=integer,JSONPath=.spec.workloadCount
// +kubebuilder:printcolumn:name="Nodes",type=integer,JSONPath=.spec.nodeCount
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=.metadata.creationTimestamp
type CostHistory struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CostHistorySpec   `json:"spec,omitempty"`
	Status CostHistoryStatus `json:"status,omitempty"`
}

// CostHistoryList contains a list of CostHistory
// +kubebuilder:object:root=true
type CostHistoryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CostHistory `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CostHistory{}, &CostHistoryList{})
}
