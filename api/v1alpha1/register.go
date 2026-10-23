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
