package clients

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/common/model"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	metricsapi "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	"k8s.io/metrics/pkg/client/clientset/versioned"
)

// MetricsClient fetches real-time resource usage from Metrics Server
type MetricsClient struct {
	clientset    *versioned.Clientset
	kubeClient   *kubernetes.Clientset
	cache        *MetricsCache
	mu           sync.RWMutex
	scrapeInterval time.Duration
	stopCh       chan struct{}
}

// MetricsCache holds the latest resource usage metrics
type MetricsCache struct {
	PodMetrics  map[string]*metricsapi.PodMetrics  // key: namespace/pod
	NodeMetrics map[string]*metricsapi.NodeMetrics // key: node
	LastUpdate  time.Time
}

// NewMetricsClient creates a new Metrics Server client
func NewMetricsClient(config *rest.Config, scrapeInterval time.Duration) (*MetricsClient, error) {
	clientset, err := versioned.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics clientset: %w", err)
	}
	
	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}

	return &MetricsClient{
		clientset:      clientset,
		kubeClient:     kubeClient,
		cache:          &MetricsCache{},
		scrapeInterval: scrapeInterval,
		stopCh:         make(chan struct{}),
	}, nil
}

// Start begins periodic scraping
func (m *MetricsClient) Start(ctx context.Context) {
	m.scrape(ctx)
	ticker := time.NewTicker(m.scrapeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.scrape(ctx)
		}
	}
}

// Stop stops the scraper
func (m *MetricsClient) Stop() {
	close(m.stopCh)
}

// scrape fetches pod and node metrics
func (m *MetricsClient) scrape(ctx context.Context) {
	newCache := &MetricsCache{
		PodMetrics:  make(map[string]*metricsapi.PodMetrics),
		NodeMetrics: make(map[string]*metricsapi.NodeMetrics),
		LastUpdate:  time.Now(),
	}

	// Fetch pod metrics
	podMetricsList, err := m.clientset.MetricsV1beta1().PodMetricses("").List(ctx, metav1.ListOptions{})
	if err != nil {
		klog.ErrorS(err, "Failed to list pod metrics")
	} else {
		for i := range podMetricsList.Items {
			pod := &podMetricsList.Items[i]
			key := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
			newCache.PodMetrics[key] = pod
		}
	}

	// Fetch node metrics
	nodeMetricsList, err := m.clientset.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{})
	if err != nil {
		klog.ErrorS(err, "Failed to list node metrics")
	} else {
		for i := range nodeMetricsList.Items {
			node := &nodeMetricsList.Items[i]
			newCache.NodeMetrics[node.Name] = node
		}
	}

	m.mu.Lock()
	m.cache = newCache
	m.mu.Unlock()

	klog.V(4).InfoS("Metrics Server scrape completed", 
		"pods", len(newCache.PodMetrics), 
		"nodes", len(newCache.NodeMetrics),
		"duration", time.Since(newCache.LastUpdate))
}

// GetCache returns a copy of the current cache
func (m *MetricsClient) GetCache() *MetricsCache {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cache
}

// GetPodUsage returns CPU and memory usage for a specific pod
func (m *MetricsClient) GetPodUsage(namespace, podName string) (cpuCores model.SampleValue, memBytes model.SampleValue, found bool) {
	cache := m.GetCache()
	key := fmt.Sprintf("%s/%s", namespace, podName)
	podMetrics, ok := cache.PodMetrics[key]
	if !ok {
		return 0, 0, false
	}

	var totalCPU, totalMem model.SampleValue
	for _, container := range podMetrics.Containers {
		if cpu, ok := container.Usage[corev1.ResourceCPU]; ok {
			totalCPU += model.SampleValue(cpu.MilliValue()) / 1000
		}
		if mem, ok := container.Usage[corev1.ResourceMemory]; ok {
			totalMem += model.SampleValue(mem.Value())
		}
	}
	return totalCPU, totalMem, true
}

// GetNodeUsage returns CPU and memory usage for a specific node
func (m *MetricsClient) GetNodeUsage(nodeName string) (cpuCores model.SampleValue, memBytes model.SampleValue, found bool) {
	cache := m.GetCache()
	nodeMetrics, ok := cache.NodeMetrics[nodeName]
	if !ok {
		return 0, 0, false
	}

	var cpu, mem model.SampleValue
	if cpuQty, ok := nodeMetrics.Usage[corev1.ResourceCPU]; ok {
		cpu = model.SampleValue(cpuQty.MilliValue()) / 1000
	}
	if memQty, ok := nodeMetrics.Usage[corev1.ResourceMemory]; ok {
		mem = model.SampleValue(memQty.Value())
	}
	return cpu, mem, true
}

// GetPodMetrics returns the full pod metrics object
func (m *MetricsClient) GetPodMetrics(namespace, podName string) (*metricsapi.PodMetrics, bool) {
	cache := m.GetCache()
	key := fmt.Sprintf("%s/%s", namespace, podName)
	pod, ok := cache.PodMetrics[key]
	return pod, ok
}

// GetNodeMetrics returns the full node metrics object
func (m *MetricsClient) GetNodeMetrics(nodeName string) (*metricsapi.NodeMetrics, bool) {
	cache := m.GetCache()
	node, ok := cache.NodeMetrics[nodeName]
	return node, ok
}
