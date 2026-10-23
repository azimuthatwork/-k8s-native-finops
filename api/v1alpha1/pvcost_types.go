package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PVCostSpec defines the desired state of PVCost
type PVCostSpec struct {
	// PVCName is the name of the PVC
	PVCName string `json:"pvcName"`

	// Namespace of the PVC
	Namespace string `json:"namespace"`

	// StorageClass name
	StorageClass string `json:"storageClass"`

	// CostCenter from namespace labels
	CostCenter string `json:"costCenter,omitempty"`

	// Team from namespace labels
	Team string `json:"team,omitempty"`
}

// PVCostStatus defines the observed state of PVCost
type PVCostStatus struct {
	// MonthlyCostUSD projected monthly cost in USD
	MonthlyCostUSD float64 `json:"monthlyCostUSD"`

	// HourlyCostUSD current hourly cost in USD
	HourlyCostUSD float64 `json:"hourlyCostUSD"`

	// RequestedGB storage requested in GiB
	RequestedGB float64 `json:"requestedGB"`

	// UsedGB storage used in GiB
	UsedGB float64 `json:"usedGB"`

	// UtilizationRatio used/requested
	UtilizationRatio float64 `json:"utilizationRatio"`

	// VolumeBindingMode of the storage class
	VolumeBindingMode string `json:"volumeBindingMode,omitempty"`

	// Phase of the PVC (Bound, Lost, Pending)
	Phase string `json:"phase,omitempty"`

	// LastUpdate timestamp of last reconciliation
	LastUpdate metav1.Time `json:"lastUpdate"`

	// Conditions represent the current state
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// PVCost is the Schema for the pvcosts API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="PVC",type=string,JSONPath=.spec.pvcName
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=.spec.namespace
// +kubebuilder:printcolumn:name="StorageClass",type=string,JSONPath=.spec.storageClass
// +kubebuilder:printcolumn:name="MonthlyCost",type=string,JSONPath=.status.monthlyCostUSD
// +kubebuilder:printcolumn:name="RequestedGB",type=string,JSONPath=.status.requestedGB
// +kubebuilder:printcolumn:name="UsedGB",type=string,JSONPath=.status.usedGB
// +kubebuilder:printcolumn:name="Utilization",type=string,JSONPath=.status.utilizationRatio
// +kubebuilder:printcolumn:name="CostCenter",type=string,JSONPath=.spec.costCenter
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=.metadata.creationTimestamp
type PVCost struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PVCostSpec   `json:"spec,omitempty"`
	Status PVCostStatus `json:"status,omitempty"`
}

// PVCostList contains a list of PVCost
// +kubebuilder:object:root=true
type PVCostList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PVCost `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PVCost{}, &PVCostList{})
}
