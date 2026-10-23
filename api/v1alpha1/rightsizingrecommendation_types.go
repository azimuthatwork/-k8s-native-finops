package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RightsizingRecommendationSpec defines the desired state of RightsizingRecommendation
type RightsizingRecommendationSpec struct {
	// TargetRef references the workload to rightsize
	TargetRef WorkloadReference `json:"targetRef"`

	// Namespace of the workload
	Namespace string `json:"namespace"`

	// CurrentRequests current resource requests
	CurrentRequests ResourceList `json:"currentRequests"`

	// RecommendedRequests recommended resource requests
	RecommendedRequests ResourceList `json:"recommendedRequests"`

	// EstimatedMonthlySavingsUSD projected savings per month
	EstimatedMonthlySavingsUSD float64 `json:"estimatedMonthlySavingsUSD"`

	// Confidence level: High, Medium, Low
	Confidence string `json:"confidence"`

	// WindowDays analysis window in days
	WindowDays int `json:"windowDays"`

	// Policy mode: Auto, RecommendationOnly, Off
	Policy string `json:"policy"`
}

// ResourceList represents resource quantities
type ResourceList struct {
	// CPU in cores
	CPU string `json:"cpu,omitempty"`

	// Memory in bytes
	Memory string `json:"memory,omitempty"`

	// GPU count
	GPU int `json:"gpu,omitempty"`
}

// RightsizingRecommendationStatus defines the observed state
type RightsizingRecommendationStatus struct {
	// Status: Pending, Approved, Applied, Rejected, Expired
	Status string `json:"status"`

	// ApprovedBy who approved the recommendation
	ApprovedBy string `json:"approvedBy,omitempty"`

	// ApprovedAt when it was approved
	ApprovedAt *metav1.Time `json:"approvedAt,omitempty"`

	// AppliedAt when it was applied
	AppliedAt *metav1.Time `json:"appliedAt,omitempty"`

	// RejectedBy who rejected the recommendation
	RejectedBy string `json:"rejectedBy,omitempty"`

	// RejectedAt when it was rejected
	RejectedAt *metav1.Time `json:"rejectedAt,omitempty"`

	// RejectionReason why it was rejected
	RejectionReason string `json:"rejectionReason,omitempty"`

	// LastUpdate timestamp of last reconciliation
	LastUpdate metav1.Time `json:"lastUpdate"`

	// Conditions represent the current state
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// RightsizingRecommendation is the Schema for the rightsizingrecommendations API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Workload",type=string,JSONPath=.spec.targetRef.name
// +kubebuilder:printcolumn:name="Kind",type=string,JSONPath=.spec.targetRef.kind
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=.spec.namespace
// +kubebuilder:printcolumn:name="CurrentCPU",type=string,JSONPath=.spec.currentRequests.cpu
// +kubebuilder:printcolumn:name="RecommendedCPU",type=string,JSONPath=.spec.recommendedRequests.cpu
// +kubebuilder:printcolumn:name="SavingsUSD/mo",type=string,JSONPath=.spec.estimatedMonthlySavingsUSD
// +kubebuilder:printcolumn:name="Confidence",type=string,JSONPath=.spec.confidence
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=.status.status
// +kubebuilder:printcolumn:name="Policy",type=string,JSONPath=.spec.policy
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=.metadata.creationTimestamp
type RightsizingRecommendation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RightsizingRecommendationSpec   `json:"spec,omitempty"`
	Status RightsizingRecommendationStatus `json:"status,omitempty"`
}

// RightsizingRecommendationList contains a list of RightsizingRecommendation
// +kubebuilder:object:root=true
type RightsizingRecommendationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RightsizingRecommendation `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RightsizingRecommendation{}, &RightsizingRecommendationList{})
}
