package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NamespaceBudgetSpec defines the desired state of NamespaceBudget
type NamespaceBudgetSpec struct {
	// MonthlyLimitUSD maximum monthly budget in USD
	MonthlyLimitUSD float64 `json:"monthlyLimitUSD"`

	// AlertThreshold percentage (0-1) to trigger warning alert
	AlertThreshold float64 `json:"alertThreshold,omitempty"`

	// HardBlock whether to block pod creation when budget exceeded
	HardBlock bool `json:"hardBlock,omitempty"`

	// CostCenter this budget applies to (optional, defaults to namespace label)
	CostCenter string `json:"costCenter,omitempty"`
}

// NamespaceBudgetStatus defines the observed state
type NamespaceBudgetStatus struct {
	// CurrentMonthlyCommitmentUSD current committed spend per month
	CurrentMonthlyCommitmentUSD float64 `json:"currentMonthlyCommitmentUSD"`

	// CurrentMonthlyActualUSD actual spend per month (from allocations)
	CurrentMonthlyActualUSD float64 `json:"currentMonthlyActualUSD"`

	// UtilizationPercentage current/limit
	UtilizationPercentage float64 `json:"utilizationPercentage"`

	// Status: OK, Warning, Critical, Exceeded
	Status string `json:"status"`

	// LastUpdate timestamp of last reconciliation
	LastUpdate metav1.Time `json:"lastUpdate"`

	// Conditions represent the current state
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// NamespaceBudget is the Schema for the namespacebudgets API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="MonthlyLimit",type=string,JSONPath=.spec.monthlyLimitUSD
// +kubebuilder:printcolumn:name="Commitment",type=string,JSONPath=.status.currentMonthlyCommitmentUSD
// +kubebuilder:printcolumn:name="Actual",type=string,JSONPath=.status.currentMonthlyActualUSD
// +kubebuilder:printcolumn:name="Utilization",type=string,JSONPath=.status.utilizationPercentage
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=.status.status
// +kubebuilder:printcolumn:name="HardBlock",type=boolean,JSONPath=.spec.hardBlock
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=.metadata.creationTimestamp
type NamespaceBudget struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NamespaceBudgetSpec   `json:"spec,omitempty"`
	Status NamespaceBudgetStatus `json:"status,omitempty"`
}

// NamespaceBudgetList contains a list of NamespaceBudget
// +kubebuilder:object:root=true
type NamespaceBudgetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NamespaceBudget `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NamespaceBudget{}, &NamespaceBudgetList{})
}
