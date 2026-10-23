package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/common/model"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

// StaticCloudClient implements CloudPricingClient using a ConfigMap
type StaticCloudClient struct {
	kubeClient   kubernetes.Interface
	configMapNS  string
	configMapName string
	cache        *CloudPriceCache
	mu           sync.RWMutex
	refreshInterval time.Duration
	stopCh       chan struct{}
}

// NewStaticCloudClient creates a new static pricing client
func NewStaticCloudClient(kubeClient kubernetes.Interface, namespace, name string, refreshInterval time.Duration) *StaticCloudClient {
	return &StaticCloudClient{
		kubeClient:      kubeClient,
		configMapNS:     namespace,
		configMapName:   name,
		cache:           &CloudPriceCache{},
		refreshInterval: refreshInterval,
		stopCh:          make(chan struct{}),
	}
}

// Start begins periodic configmap refresh
func (s *StaticCloudClient) Start(ctx context.Context) {
	s.refresh(ctx)
	ticker := time.NewTicker(s.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.refresh(ctx)
		}
	}
}

// Stop stops the refresher
func (s *StaticCloudClient) Stop() {
	close(s.stopCh)
}

// refresh loads pricing from ConfigMap
func (s *StaticCloudClient) refresh(ctx context.Context) {
	cm, err := s.kubeClient.CoreV1().ConfigMaps(s.configMapNS).Get(ctx, s.configMapName, metav1.GetOptions{})
	if err != nil {
		klog.ErrorS(err, "Failed to get pricing ConfigMap", "namespace", s.configMapNS, "name", s.configMapName)
		return
	}

	newCache := &CloudPriceCache{
		OnDemandPrices: make(map[string]float64),
		SpotPrices:     make(map[string]float64),
		InstanceInfo:   make(map[string]*InstanceInfo),
	}

	// Parse instance types
	if data, ok := cm.Data["instance-types.json"]; ok {
		var instances map[string]InstanceInfo
		if err := json.Unmarshal([]byte(data), &instances); err != nil {
			klog.ErrorS(err, "Failed to parse instance-types.json")
		} else {
			for k, v := range instances {
				newCache.InstanceInfo[k] = &v
			}
		}
	}

	// Parse on-demand prices
	if data, ok := cm.Data["on-demand-prices.json"]; ok {
		var prices map[string]float64
		if err := json.Unmarshal([]byte(data), &prices); err != nil {
			klog.ErrorS(err, "Failed to parse on-demand-prices.json")
		} else {
			newCache.OnDemandPrices = prices
		}
	}

	// Parse spot prices
	if data, ok := cm.Data["spot-prices.json"]; ok {
		var prices map[string]float64
		if err := json.Unmarshal([]byte(data), &prices); err != nil {
			klog.ErrorS(err, "Failed to parse spot-prices.json")
		} else {
			newCache.SpotPrices = prices
		}
	}

	s.mu.Lock()
	s.cache = newCache
	s.mu.Unlock()

	klog.V(4).InfoS("Static pricing cache refreshed", 
		"instanceTypes", len(newCache.InstanceInfo),
		"onDemandPrices", len(newCache.OnDemandPrices),
		"spotPrices", len(newCache.SpotPrices))
}

// GetNodePrice returns the hourly price for an instance type
func (s *StaticCloudClient) GetNodePrice(ctx context.Context, instanceType string, isSpot bool) (float64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if isSpot {
		if price, ok := s.cache.SpotPrices[instanceType]; ok {
			return price, nil
		}
		// Fallback to on-demand if spot not configured
		if price, ok := s.cache.OnDemandPrices[instanceType]; ok {
			return price, nil
		}
	} else {
		if price, ok := s.cache.OnDemandPrices[instanceType]; ok {
			return price, nil
		}
	}

	return 0, fmt.Errorf("no price found for instance type %s (spot=%v)", instanceType, isSpot)
}

// GetInstanceInfo returns CPU, Memory, GPU info for an instance type
func (s *StaticCloudClient) GetInstanceInfo(ctx context.Context, instanceType string) (*InstanceInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if info, ok := s.cache.InstanceInfo[instanceType]; ok {
		return info, nil
	}

	return nil, fmt.Errorf("instance type %s not found in static config", instanceType)
}

// GetNodeInstanceType returns the instance type for a node (from node labels)
func (s *StaticCloudClient) GetNodeInstanceType(ctx context.Context, nodeName string) (string, error) {
	node, err := s.kubeClient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get node: %w", err)
	}

	// Check for instance type label (set by cloud provider or node labeller)
	if instanceType, ok := node.Labels["node.kubernetes.io/instance-type"]; ok {
		return instanceType, nil
	}
	if instanceType, ok := node.Labels["beta.kubernetes.io/instance-type"]; ok {
		return instanceType, nil
	}
	if instanceType, ok := node.Labels["kubernetes.azure.com/vm-size"]; ok {
		return instanceType, nil
	}
	if instanceType, ok := node.Labels["cloud.google.com/machine-type"]; ok {
		return instanceType, nil
	}

	return "", fmt.Errorf("instance type label not found on node %s", nodeName)
}

// Example ConfigMap data structure:
//
// apiVersion: v1
// kind: ConfigMap
// metadata:
//   name: cloud-pricing
//   namespace: cost-management
// data:
//   instance-types.json: |
//     {
//       "m5.xlarge": {"instanceType": "m5.xlarge", "vcpu": 4, "memoryGiB": 16, "gpuCount": 0},
//       "m5.2xlarge": {"instanceType": "m5.2xlarge", "vcpu": 8, "memoryGiB": 32, "gpuCount": 0},
//       "p3.2xlarge": {"instanceType": "p3.2xlarge", "vcpu": 8, "memoryGiB": 61, "gpuCount": 1, "gpuType": "V100"}
//     }
//   on-demand-prices.json: |
//     {
//       "m5.xlarge": 0.192,
//       "m5.2xlarge": 0.384,
//       "p3.2xlarge": 3.06
//     }
//   spot-prices.json: |
//     {
//       "m5.xlarge": 0.058,
//       "m5.2xlarge": 0.115,
//       "p3.2xlarge": 0.92
//     }
