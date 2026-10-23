package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LogEventSpec defines the desired state of LogEvent
type LogEventSpec struct {
	// EventType: OOMKilling, Evicted, FailedScheduling, CrashLoopBackOff, Error, Warning
	EventType string `json:"eventType"`

	// Namespace where event occurred
	Namespace string `json:"namespace"`

	// Workload name
	Workload string `json:"workload,omitempty"`

	// Kind of workload
	Kind string `json:"kind,omitempty"`

	// Pod name
	Pod string `json:"pod,omitempty"`

	// Container name
	Container string `json:"container,omitempty"`

	// Node name
	Node string `json:"node,omitempty"`

	// Reason for the event
	Reason string `json:"reason"`

	// Message detailed message
	Message string `json:"message"`

	// CostCenter from namespace labels
	CostCenter string `json:"costCenter,omitempty"`

	// Team from namespace labels
	Team string `json:"team,omitempty"`

	// Count of occurrences
	Count int `json:"count,omitempty"`

	// FirstTimestamp first occurrence
	FirstTimestamp metav1.Time `json:"firstTimestamp,omitempty"`

	// LastTimestamp last occurrence
	LastTimestamp metav1.Time `json:"lastTimestamp,omitempty"`
}

// LogEventStatus defines the observed state
type LogEventStatus struct {
	// Resolved whether the event has been resolved
	Resolved bool `json:"resolved,omitempty"`

	// ResolvedAt when it was resolved
	ResolvedAt *metav1.Time `json:"resolvedAt,omitempty"`

	// LastUpdate timestamp of last reconciliation
	LastUpdate metav1.Time `json:"lastUpdate"`

	// Conditions represent the current state
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// LogEvent is the Schema for the logevents API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=.spec.eventType
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=.spec.namespace
// +kubebuilder:printcolumn:name="Workload",type=string,JSONPath=.spec.workload
// +kubebuilder:printcolumn:name="Pod",type=string,JSONPath=.spec.pod
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=.spec.reason
// +kubebuilder:printcolumn:name="Count",type=integer,JSONPath=.spec.count
// +kubebuilder:printcolumn:name="CostCenter",type=string,JSONPath=.spec.costCenter
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=.metadata.creationTimestamp
type LogEvent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LogEventSpec   `json:"spec,omitempty"`
	Status LogEventStatus `json:"status,omitempty"`
}

// LogEventList contains a list of LogEvent
// +kubebuilder:object:root=true
type LogEventList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LogEvent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LogEvent{}, &LogEventList{})
}
