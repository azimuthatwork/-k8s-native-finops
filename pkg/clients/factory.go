package clients

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/yourorg/kube-native-finops/pkg/cache"
	"github.com/yourorg/kube-native-finops/pkg/clients/cloud"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

// ClientFactory creates and manages all clients
type ClientFactory struct {
	kubeClient         kubernetes.Interface
	dynamicClient      dynamic.Interface
	ksmClient          *KSMClient
	metricsClient      *MetricsClient
	nodeExporterClient *NodeExporterClient
	cloudPricingClient cloud.CloudPricingClient
	informerCache      *K8sInformerCache
	dashboardCache     *cache.DashboardCache
	redisClient        cache.RedisClient
	stopCh             chan struct{}
}

// ClientConfig holds configuration for all clients
type ClientConfig struct {
	KSMURL               string
	KSMScrapeInterval    time.Duration
	MetricsScrapeInterval time.Duration
	NodeExporterURL      string
	NodeExporterInterval time.Duration
	CloudProvider        string // aws, gcp, azure, static
	CloudRegion          string
	CloudProjectID       string // for GCP
	CloudSubscriptionID  string // for Azure
	StaticConfigMapNS    string
	StaticConfigMapName  string
	InformerResyncPeriod time.Duration
	CacheTTL             time.Duration
	SpotCacheTTL         time.Duration
	RedisConfig          RedisConfig
}

// RedisConfig holds Redis connection configuration
type RedisConfig struct {
	Address  string
	Password string
	DB       int
	Enabled  bool
}

// NewClientFactory creates a new client factory
func NewClientFactory(ctx context.Context, config *ClientConfig) (*ClientFactory, error) {
	// Load kubeconfig
	kubeConfig, err := loadKubeConfig()
	if err != nil {
		return nil, err
	}

	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}

	// Create dynamic client
	dynamicClient, err := dynamic.NewForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}

	factory := &ClientFactory{
		kubeClient:    kubeClient,
		dynamicClient: dynamicClient,
		stopCh:        make(chan struct{}),
	}

	// Initialize KSM client
	if config.KSMURL != "" {
		factory.ksmClient = NewKSMClient(config.KSMURL, config.KSMScrapeInterval)
	}

	// Initialize Metrics client
	factory.metricsClient = NewMetricsClient(kubeConfig, config.MetricsScrapeInterval)

	// Initialize Node Exporter client
	if config.NodeExporterURL != "" {
		factory.nodeExporterClient = NewNodeExporterClient(config.NodeExporterURL, config.NodeExporterInterval)
	}

	// Initialize Cloud Pricing client
	factory.cloudPricingClient, err = createCloudClient(ctx, config, kubeClient)
	if err != nil {
		klog.ErrorS(err, "Failed to create cloud client, using static fallback")
		factory.cloudPricingClient = cloud.NewStaticCloudClient(kubeClient, config.StaticConfigMapNS, config.StaticConfigMapName, config.CacheTTL)
	}

	// Initialize Informer cache
	factory.informerCache = NewK8sInformerCache(kubeClient, dynamicClient, config.InformerResyncPeriod)

	// Start informers
	factory.informerCache.Start(factory.stopCh)

	// Wait for cache sync
	if err := factory.informerCache.WaitForCacheSync(ctx); err != nil {
		return nil, err
	}

	// Initialize Redis client for caching (optional)
	redisClient, err := NewRedisClient(klog.Background(), config.RedisConfig)
	if err != nil {
		klog.Warning("Redis not available, using in-memory cache only", "error", err)
	}
	factory.redisClient = redisClient

	// Initialize Dashboard cache with 5-minute default TTL
	factory.dashboardCache = cache.NewDashboardCache(klog.Background(), 5*time.Minute, factory.redisClient)

	klog.Info("All clients initialized and caches synced")
	return factory, nil
}

// createCloudClient creates the appropriate cloud client based on provider
func createCloudClient(ctx context.Context, config *ClientConfig, kubeClient kubernetes.Interface) (cloud.CloudPricingClient, error) {
	switch config.CloudProvider {
	case "aws":
		return cloud.NewAWSCloudClient(config.CloudRegion, config.CacheTTL, config.SpotCacheTTL)
	case "gcp":
		return cloud.NewGCPCloudClient(config.CloudProjectID, config.CloudRegion, config.CacheTTL, config.SpotCacheTTL)
	case "azure":
		return cloud.NewAzureCloudClient(config.CloudSubscriptionID, config.CloudRegion, config.CacheTTL, config.SpotCacheTTL)
	case "static":
		return cloud.NewStaticCloudClient(kubeClient, config.StaticConfigMapNS, config.StaticConfigMapName, config.CacheTTL), nil
	default:
		return nil, fmt.Errorf("unknown cloud provider: %s", config.CloudProvider)
	}
}

// Stop stops all clients
func (f *ClientFactory) Stop() {
	close(f.stopCh)
	if f.ksmClient != nil {
		f.ksmClient.Stop()
	}
	if f.nodeExporterClient != nil {
		f.nodeExporterClient.Stop()
	}
	if f.informerCache != nil {
		f.informerCache.Stop()
	}
	if f.redisClient != nil {
		f.redisClient.Close()
	}
}

// Getters for clients
func (f *ClientFactory) KSMClient() *KSMClient { return f.ksmClient }
func (f *ClientFactory) MetricsClient() *MetricsClient { return f.metricsClient }
func (f *ClientFactory) NodeExporterClient() *NodeExporterClient { return f.nodeExporterClient }
func (f *ClientFactory) CloudPricingClient() cloud.CloudPricingClient { return f.cloudPricingClient }
func (f *ClientFactory) InformerCache() *K8sInformerCache { return f.informerCache }
func (f *ClientFactory) KubeClient() kubernetes.Interface { return f.kubeClient }
func (f *ClientFactory) DynamicClient() dynamic.Interface { return f.dynamicClient }
func (f *ClientFactory) DashboardCache() *cache.DashboardCache { return f.dashboardCache }

// loadKubeConfig loads kubernetes config from in-cluster or kubeconfig file
func loadKubeConfig() (*rest.Config, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err == nil {
		return config, nil
	}

	// Fall back to kubeconfig file
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = clientcmd.RecommendedHomeFile
	}
	return clientcmd.BuildConfigFromFlags("", kubeconfig)
}
