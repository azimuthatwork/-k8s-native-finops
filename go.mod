module github.com/yourorg/kube-native-finops

go 1.22

require (
	cloud.google.com/go/billing/catalog/apiv1 v1.3.0
	cloud.google.com/go/compute/metadata v0.2.3
	cloud.google.com/go/compute/v1 v1.19.0
	github.com/Azure/azure-sdk-for-go/sdk/azcore v1.12.0
	github.com/Azure/azure-sdk-for-go/sdk/azidentity v1.6.0
	github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v4 v4.2.0
	github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/costmanagement/armcostmanagement v1.1.0
	github.com/aws/aws-sdk-go-v2 v1.24.0
	github.com/aws/aws-sdk-go-v2/config v1.27.0
	github.com/aws/aws-sdk-go-v2/service/ec2 v1.40.0
	github.com/aws/aws-sdk-go-v2/service/pricing v1.19.0
	github.com/go-chi/chi/v5 v5.0.12
	github.com/go-logr/logr v1.3.0
	github.com/go-logr/stdr v1.3.0
	github.com/googleapis/gax-go/v2 v2.12.0
	github.com/prometheus/client_golang v1.19.0
	github.com/prometheus/common v0.47.0
	github.com/prometheus/prometheus v2.52.0
	github.com/prometheus/prometheus/model/labels v0.0.0-20240501120000-1234567890ab
	github.com/prometheus/prometheus/prompb v0.0.0-20240501120000-1234567890ab
	github.com/prometheus/prometheus/tsdb v0.15.0
	github.com/redis/go-redis/v9 v9.3.0
	go.uber.org/zap v1.26.0
	go.uber.org/multierr v1.11.0
	golang.org/x/net v0.25.0
	golang.org/x/oauth2 v0.19.0
	golang.org/x/sync v0.6.0
	golang.org/x/sys v0.20.0
	golang.org/x/text v0.15.0
	google.golang.org/api v0.186.0
	google.golang.org/genproto/googleapis/api v0.0.0-20240501120000-1234567890ab
	google.golang.org/grpc v1.63.0
	google.golang.org/protobuf v1.33.0
	k8s.io/api v0.29.2
	k8s.io/apimachinery v0.29.2
	k8s.io/client-go v0.29.2
	k8s.io/klog/v2 v2.120.1
	k8s.io/metrics v0.29.2
	k8s.io/utils v0.0.0-20240501120000-1234567890ab
	sigs.k8s.io/controller-runtime v0.18.2
	sigs.k8s.io/json v0.0.0-20240501120000-1234567890ab
	sigs.k8s.io/structured-merge-diff/v4 v4.4.1
)

require (
	github.com/Azure/go-autorest/autorest v0.11.28 // indirect
	github.com/Azure/go-autorest/autorest/adal v0.9.22 // indirect
	github.com/Azure/go-autorest/autorest/azure v0.21.27 // indirect
	github.com/Azure/go-autorest/autorest/azure/auth v0.5.11 // indirect
	github.com/Azure/go-autorest/autorest/date v0.3.0 // indirect
	github.com/Azure/go-autorest/autorest/to v0.4.0 // indirect
	github.com/Azure/go-autorest/autorest/validation v0.3.1 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/go-logr/logr v1.3.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/google/btree v1.1.2 // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/uuid v1.5.0 // indirect
	github.com/gorilla/websocket v1.5.1 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware v1.4.0 // indirect
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0 // indirect
	github.com/imdario/mergo v0.3.16 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.4 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/montanaflynn/stats v0.7.1 // indirect
	github.com/pelletier/go-toml/v2 v2.1.0 // indirect
	github.com/prometheus/client_golang v1.19.0 // indirect
	github.com/prometheus/client_model v0.5.0 // indirect
	github.com/prometheus/common v0.47.0 // indirect
	github.com/prometheus/procfs v0.13.0 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/otel v1.23.1 // indirect
	go.opentelemetry.io/otel/metric v1.23.1 // indirect
	go.opentelemetry.io/otel/trace v1.23.1 // indirect
	golang.org/x/arch v0.6.0 // indirect
	golang.org/x/crypto v0.22.0 // indirect
	golang.org/x/net v0.25.0 // indirect
	golang.org/x/oauth2 v0.19.0 // indirect
	golang.org/x/sync v0.6.0 // indirect
	golang.org/x/sys v0.20.0 // indirect
	golang.org/x/text v0.15.0 // indirect
	google.golang.org/appengine v1.6.8 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240501120000-1234567890ab // indirect
	google.golang.org/grpc v1.63.0 // indirect
	google.golang.org/protobuf v1.33.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/apiserver v0.29.2 // indirect
	k8s.io/component-base v0.29.2 // indirect
	k8s.io/kube-openapi v0.0.0-20240501120000-1234567890ab // indirect
	k8s.io/kubelet v0.29.2 // indirect
	sigs.k8s.io/yaml v1.4.0 // indirect
)
