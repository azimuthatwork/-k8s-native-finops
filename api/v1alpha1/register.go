package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apiserver/pkg/registry/rest"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion is the API version
	GroupVersion = schema.GroupVersion{Group: "finops.io", Version: "v1alpha1"}

	// SchemeBuilder builds the scheme
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the types to the scheme
	AddToScheme = SchemeBuilder.AddToScheme
)

// Resource takes an unqualified resource and returns a Group qualified GroupResource
func Resource(resource string) schema.GroupResource {
	return GroupVersion.WithResource(resource).GroupResource()
}

// Kind takes an unqualified kind and returns a Group qualified GroupKind
func Kind(kind string) schema.GroupKind {
	return GroupVersion.WithKind(kind).GroupKind()
}

// Codecs provides access to encoding/decoding
var Codecs = serializer.NewCodecFactory(SchemeBuilder.Scheme)

// ParameterCodec handles parameter encoding
var ParameterCodec = runtime.NewParameterCodec(SchemeBuilder.Scheme)

// RESTOptionsGetter provides REST options
type RESTOptionsGetter interface {
	GetRESTOptions(resource schema.GroupResource) (rest.ClusterScoped, error)
}

// GroupName is the group name for this API
const GroupName = "finops.io"

// SchemeGroupVersion is group version used to register these objects
var SchemeGroupVersion = schema.GroupVersion{Group: GroupName, Version: "v1alpha1"}

// addKnownTypes adds the set of known types to the scheme
func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&CostAllocation{},
		&CostAllocationList{},
		&NodeCost{},
		&NodeCostList{},
		&PVCost{},
		&PVCostList{},
		&CostHistory{},
		&CostHistoryList{},
		&RightsizingRecommendation{},
		&RightsizingRecommendationList{},
		&NamespaceBudget{},
		&NamespaceBudgetList{},
		&LogEvent{},
		&LogEventList{},
	)
	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
	return nil
}
