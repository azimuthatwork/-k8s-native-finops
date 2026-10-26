package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorkloadReference references a workload
type WorkloadReference struct {
	// Name of the workload
	Name string `json:"name"`

	// Kind of the workload (Deployment, StatefulSet, DaemonSet, Job, CronJob, Pod)
	Kind string `json:"kind"`

	// UID of the workload owner
	UID string `json:"uid,omitempty"`
}

// LogEventSpec defines the desired state of LogEvent
type LogEventSpec struct {
	// Namespace is the namespace where the event occurred
	Namespace string `json:"namespace"`

	// WorkloadRef references the workload this event belongs to
	WorkloadRef WorkloadReference `json:"workloadRef,omitempty"`

	// Reason is the reason for the event (e.g., OOMKilling, Evicted, FailedScheduling)
	Reason string `json:"reason"`

	// Message is the human-readable description of the event
	Message string `json:"message"`

	// Source is the component that generated the event (e.g., kubelet, scheduler)
	Source string `json:"source,omitempty"`

	// FirstTimestamp is the time of the first occurrence
	FirstTimestamp metav1.Time `json:"firstTimestamp"`

	// LastTimestamp is the time of the last occurrence
	LastTimestamp metav1.Time `json:"lastTimestamp"`

	// Count is the number of times this event has occurred
	Count int32 `json:"count"`

	// Type is the type of event (Normal, Warning)
	Type string `json:"type,omitempty"`

	// NodeName is the node where the event occurred
	NodeName string `json:"nodeName,omitempty"`

	// CostCenter is the cost center label from namespace
	CostCenter string `json:"costCenter,omitempty"`

	// Team is the team label from namespace
	Team string `json:"team,omitempty"`
}

// LogEventStatus defines the observed state of LogEvent
type LogEventStatus struct {
	// Processed indicates if the event has been processed for alerting/metrics
	Processed bool `json:"processed"`

	// ProcessedAt is the timestamp when the event was processed
	ProcessedAt *metav1.Time `json:"processedAt,omitempty"`

	// Conditions represent the latest available observations of the LogEvent's state
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=.spec.namespace
// +kubebuilder:printcolumn:name="Workload",type=string,JSONPath=.spec.workloadRef.name
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=.spec.reason
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=.spec.type
// +kubebuilder:printcolumn:name="Count",type=integer,JSONPath=.spec.count
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=.metadata.creationTimestamp

// LogEvent is the Schema for the logevents API
type LogEvent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LogEventSpec   `json:"spec,omitempty"`
	Status LogEventStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// LogEventList contains a list of LogEvent
type LogEventList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LogEvent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LogEvent{}, &LogEventList{})
}
