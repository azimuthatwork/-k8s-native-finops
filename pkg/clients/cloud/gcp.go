package cloud

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/googleapis/gax-go/v2"
	"github.com/prometheus/common/model"
	"cloud.google.com/go/billing/catalog/apiv1"
	"cloud.google.com/go/compute/metadata"
	"cloud.google.com/go/compute/v1"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"k8s.io/klog/v2"
)

// GCPCloudClient implements CloudPricingClient for GCP
type GCPCloudClient struct {
	catalogClient *catalog.CloudCatalogClient
	instancesClient *compute.InstancesClient
	cache         *CloudPriceCache
	mu            sync.RWMutex
	projectID     string
	region        string
	cacheTTL      time.Duration
	spotCacheTTL  time.Duration
	lastUpdate    time.Time
	spotUpdate    time.Time
}

// NewGCPCloudClient creates a new GCP cloud client
func NewGCPCloudClient(projectID, region string, cacheTTL, spotCacheTTL time.Duration) (*GCPCloudClient, error) {
	ctx := context.Background()
	
	catalogClient, err := catalog.NewCloudCatalogClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create catalog client: %w", err)
	}

	instancesClient, err := compute.NewInstancesRESTClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create instances client: %w", err)
	}

	// Auto-detect project ID if not provided
	if projectID == "" {
		projectID, _ = metadata.ProjectID()
	}
	
	// Auto-detect region if not provided
	if region == "" {
		region, _ = metadata.Zone()
		if region != "" {
			// Extract region from zone (e.g., us-central1-a -> us-central1)
			if len(region) > 2 {
				region = region[:len(region)-2]
			}
		}
	}

	return &GCPCloudClient{
		catalogClient:   catalogClient,
		instancesClient: instancesClient,
		cache:           &CloudPriceCache{},
		projectID:       projectID,
		region:          region,
		cacheTTL:        cacheTTL,
		spotCacheTTL:    spotCacheTTL,
	}, nil
}

// GetNodePrice returns the hourly price for a machine type
func (g *GCPCloudClient) GetNodePrice(ctx context.Context, machineType string, isSpot bool) (float64, error) {
	g.mu.RLock()
	if time.Since(g.lastUpdate) < g.cacheTTL && !isSpot {
		if price, ok := g.cache.OnDemandPrices[machineType]; ok {
			g.mu.RUnlock()
			return price, nil
		}
	}
	if isSpot && time.Since(g.spotUpdate) < g.spotCacheTTL {
		if price, ok := g.cache.SpotPrices[machineType]; ok {
			g.mu.RUnlock()
			return price, nil
		}
	}
	g.mu.RUnlock()

	return g.refreshAndGetPrice(ctx, machineType, isSpot)
}

// refreshAndGetPrice refreshes the price cache and returns the price
func (g *GCPCloudClient) refreshAndGetPrice(ctx context.Context, machineType string, isSpot bool) (float64, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Double-check
	if !isSpot && time.Since(g.lastUpdate) < g.cacheTTL {
		if price, ok := g.cache.OnDemandPrices[machineType]; ok {
			return price, nil
		}
	}
	if isSpot && time.Since(g.spotUpdate) < g.spotCacheTTL {
		if price, ok := g.cache.SpotPrices[machineType]; ok {
			return price, nil
		}
	}

	// Fetch from Cloud Billing Catalog API
	price, err := g.fetchPriceFromCatalog(ctx, machineType, isSpot)
	if err != nil {
		return 0, err
	}

	if isSpot {
		g.cache.SpotPrices[machineType] = price
		g.spotUpdate = time.Now()
	} else {
		g.cache.OnDemandPrices[machineType] = price
		g.lastUpdate = time.Now()
	}

	return price, nil
}

// fetchPriceFromCatalog fetches price from Cloud Billing Catalog
func (g *GCPCloudClient) fetchPriceFromCatalog(ctx context.Context, machineType string, isSpot bool) (float64, error) {
	// List SKUs for Compute Engine
	req := &catalog.ListSkusRequest{
		Parent: fmt.Sprintf("services/%s", "6F81-5844-456A"), // Compute Engine service ID
		PageSize: 1000,
	}

	// Filter for the specific machine type and region
	// Note: The catalog API doesn't support direct filtering, so we iterate
	it := g.catalogClient.ListSkus(ctx, req)
	
	for {
		sku, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("failed to iterate SKUs: %w", err)
		}

		// Check if this SKU matches our machine type and region
		if !g.skuMatches(sku, machineType, isSpot) {
			continue
		}

		// Extract pricing info
		for _, pricingInfo := range sku.PricingInfo {
			for _, tier := range pricingInfo.PricingExpression.TieredRates {
				if tier.UnitPrice != nil {
					// Convert from nanos to dollars
					price := float64(tier.UnitPrice.Nanos) / 1e9
					if tier.UnitPrice.Units != 0 {
						price += float64(tier.UnitPrice.Units)
					}
					// Ensure it's hourly
					if pricingInfo.PricingExpression.UsageUnit == "h" || pricingInfo.PricingExpression.UsageUnit == "hour" {
						return price, nil
					}
					// Convert from per-second if needed
					if pricingInfo.PricingExpression.UsageUnit == "s" || pricingInfo.PricingExpression.UsageUnit == "second" {
						return price * 3600, nil
					}
				}
			}
		}
	}

	return 0, fmt.Errorf("no price found for machine type %s (spot=%v)", machineType, isSpot)
}

// skuMatches checks if a SKU matches the machine type and spot/on-demand
func (g *GCPCloudClient) skuMatches(sku *catalog.Sku, machineType string, isSpot bool) bool {
	// Check region
	regionMatch := false
	for _, region := range sku.ServiceRegions {
		if region == g.region || region == "global" {
			regionMatch = true
			break
		}
	}
	if !regionMatch {
		return false
	}

	// Check machine type in description or category
	desc := sku.Description
	if desc == "" {
		desc = sku.Category.ResourceGroup
	}
	
	// Normalize machine type for comparison
	normalizedMachineType := machineType
	if isSpot {
		// Spot instances might have "preemptible" in description
		if !containsIgnoreCase(desc, "preemptible") && !containsIgnoreCase(desc, "spot") {
			return false
		}
	} else {
		if containsIgnoreCase(desc, "preemptible") || containsIgnoreCase(desc, "spot") {
			return false
		}
	}

	// Check if machine type matches
	return containsIgnoreCase(desc, normalizedMachineType)
}

// GetInstanceInfo returns CPU, Memory, GPU info for a machine type
func (g *GCPCloudClient) GetInstanceInfo(ctx context.Context, machineType string) (*InstanceInfo, error) {
	g.mu.RLock()
	if info, ok := g.cache.InstanceInfo[machineType]; ok {
		g.mu.RUnlock()
		return info, nil
	}
	g.mu.RUnlock()

	g.mu.Lock()
	defer g.mu.Unlock()

	if info, ok := g.cache.InstanceInfo[machineType]; ok {
		return info, nil
	}

	// Fetch from Compute API
	req := &compute.GetInstanceRequest{
		Project:  g.projectID,
		Zone:     g.region + "-a", // Use first zone in region
		Instance: machineType, // This won't work directly, need machineTypes API
	}

	// Actually need to use MachineTypes API
	machineTypesClient, err := compute.NewMachineTypesRESTClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create machine types client: %w", err)
	}
	defer machineTypesClient.Close()

	mtReq := &compute.GetMachineTypeRequest{
		Project: g.projectID,
		Zone:    g.region + "-a",
		MachineType: machineType,
	}

	mt, err := machineTypesClient.Get(ctx, mtReq)
	if err != nil {
		return nil, fmt.Errorf("failed to get machine type: %w", err)
	}

	info := &InstanceInfo{
		InstanceType: machineType,
		VCPU:         int(mt.GuestCpus),
		MemoryGiB:    float64(mt.MemoryMb) / 1024,
	}

	// Check for accelerators (GPUs)
	if mt.Accelerators != nil {
		for _, acc := range mt.Accelerators {
			info.GPUCount += int(acc.GuestAcceleratorCount)
			info.GPUType = acc.GuestAcceleratorType
		}
	}

	g.cache.InstanceInfo[machineType] = info
	return info, nil
}

// GetNodeMachineType returns the machine type for a node
func (g *GCPCloudClient) GetNodeMachineType(ctx context.Context, nodeName string) (string, error) {
	req := &compute.GetInstanceRequest{
		Project:  g.projectID,
		Zone:     g.region + "-a", // Would need to determine correct zone
		Instance: nodeName,
	}

	instance, err := g.instancesClient.Get(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to get instance: %w", err)
	}

	// Extract machine type from URL (e.g., "zones/us-central1-a/machineTypes/n1-standard-4")
	if instance.MachineType != "" {
		parts := splitLast(instance.MachineType, "/")
		if len(parts) > 0 {
			return parts[len(parts)-1], nil
		}
	}

	return "", fmt.Errorf("machine type not found for node %s", nodeName)
}

func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && 
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || 
		 containsSubstring(s, substr)))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func splitLast(s, sep string) []string {
	idx := -1
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			idx = i
		}
	}
	if idx == -1 {
		return []string{s}
	}
	return []string{s[:idx], s[idx+len(sep):]}
}
