package cloud

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v4"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/costmanagement/armcostmanagement"
	"github.com/prometheus/common/model"
	"k8s.io/klog/v2"
)

// AzureCloudClient implements CloudPricingClient for Azure
type AzureCloudClient struct {
	subscriptionID string
	computeClient  *armcompute.VirtualMachineSizesClient
	pricingClient  *armcostmanagement.QueryClient
	cred           azcore.TokenCredential
	cache          *CloudPriceCache
	mu             sync.RWMutex
	region         string
	cacheTTL       time.Duration
	spotCacheTTL   time.Duration
	lastUpdate     time.Time
	spotUpdate     time.Time
}

// NewAzureCloudClient creates a new Azure cloud client
func NewAzureCloudClient(subscriptionID, region string, cacheTTL, spotCacheTTL time.Duration) (*AzureCloudClient, error) {
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Azure credential: %w", err)
	}

	computeClient, err := armcompute.NewVirtualMachineSizesClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create compute client: %w", err)
	}

	pricingClient, err := armcostmanagement.NewQueryClient(cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create cost management client: %w", err)
	}

	return &AzureCloudClient{
		subscriptionID: subscriptionID,
		computeClient:  computeClient,
		pricingClient:  pricingClient,
		cred:           cred,
		cache:          &CloudPriceCache{},
		region:         region,
		cacheTTL:       cacheTTL,
		spotCacheTTL:   spotCacheTTL,
	}, nil
}

// GetNodePrice returns the hourly price for a VM size
func (a *AzureCloudClient) GetNodePrice(ctx context.Context, vmSize string, isSpot bool) (float64, error) {
	a.mu.RLock()
	if time.Since(a.lastUpdate) < a.cacheTTL && !isSpot {
		if price, ok := a.cache.OnDemandPrices[vmSize]; ok {
			a.mu.RUnlock()
			return price, nil
		}
	}
	if isSpot && time.Since(a.spotUpdate) < a.spotCacheTTL {
		if price, ok := a.cache.SpotPrices[vmSize]; ok {
			a.mu.RUnlock()
			return price, nil
		}
	}
	a.mu.RUnlock()

	return a.refreshAndGetPrice(ctx, vmSize, isSpot)
}

// refreshAndGetPrice refreshes the price cache and returns the price
func (a *AzureCloudClient) refreshAndGetPrice(ctx context.Context, vmSize string, isSpot bool) (float64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Double-check
	if !isSpot && time.Since(a.lastUpdate) < a.cacheTTL {
		if price, ok := a.cache.OnDemandPrices[vmSize]; ok {
			return price, nil
		}
	}
	if isSpot && time.Since(a.spotUpdate) < a.spotCacheTTL {
		if price, ok := a.cache.SpotPrices[vmSize]; ok {
			return price, nil
		}
	}

	// Fetch from Azure Retail Prices API (via Cost Management)
	price, err := a.fetchPriceFromRetailAPI(ctx, vmSize, isSpot)
	if err != nil {
		return 0, err
	}

	if isSpot {
		a.cache.SpotPrices[vmSize] = price
		a.spotUpdate = time.Now()
	} else {
		a.cache.OnDemandPrices[vmSize] = price
		a.lastUpdate = time.Now()
	}

	return price, nil
}

// fetchPriceFromRetailAPI fetches price from Azure Retail Prices
func (a *AzureCloudClient) fetchPriceFromRetailAPI(ctx context.Context, vmSize string, isSpot bool) (float64, error) {
	// Use Cost Management Query API to get retail prices
	// This is a simplified implementation - in practice you'd use the Retail Prices API directly
	query := fmt.Sprintf(`
		SELECT 
			skuName,
			unitPrice,
			currencyCode
		FROM 
			RetailPrices
		WHERE 
			armSkuName == '%s' 
			AND armRegionName == '%s'
			AND type == 'Consumption'
			AND serviceName == 'Virtual Machines'
	`, vmSize, a.region)

	if isSpot {
		query += " AND skuName CONTAINS 'Spot' OR skuName CONTAINS 'Low Priority'"
	} else {
		query += " AND NOT (skuName CONTAINS 'Spot' OR skuName CONTAINS 'Low Priority')"
	}

	queryDef := armcostmanagement.QueryDefinition{
		Type:  toPtr(armcostmanagement.QueryTypeUsage),
		Query: toPtr(query),
	}

	resp, err := a.pricingClient.Query(ctx, queryDef, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to query retail prices: %w", err)
	}

	if resp.Rows == nil || len(resp.Rows) == 0 {
		return 0, fmt.Errorf("no price found for VM size %s (spot=%v)", vmSize, isSpot)
	}

	// Parse the first row
	row := resp.Rows[0]
	if len(row) < 2 {
		return 0, fmt.Errorf("unexpected row format")
	}

	priceStr := fmt.Sprintf("%v", row[1])
	price, err := strconv.ParseFloat(priceStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse price: %w", err)
	}

	return price, nil
}

// GetInstanceInfo returns CPU, Memory, GPU info for a VM size
func (a *AzureCloudClient) GetInstanceInfo(ctx context.Context, vmSize string) (*InstanceInfo, error) {
	a.mu.RLock()
	if info, ok := a.cache.InstanceInfo[vmSize]; ok {
		a.mu.RUnlock()
		return info, nil
	}
	a.mu.RUnlock()

	a.mu.Lock()
	defer a.mu.Unlock()

	if info, ok := a.cache.InstanceInfo[vmSize]; ok {
		return info, nil
	}

	// List VM sizes in region
	pager := a.computeClient.NewListPager(a.region, nil)
	for pager.More() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list VM sizes: %w", err)
		}

		for _, size := range page.Value {
			if *size.Name == vmSize {
				info := &InstanceInfo{
					InstanceType: vmSize,
					VCPU:         int(*size.NumberOfCores),
					MemoryGiB:    float64(*size.MemoryInMB) / 1024,
				}

				// Check for GPUs (would need to parse from capabilities)
				// Azure doesn't expose GPU info directly in VM sizes API easily
				// This would require additional logic or static mapping

				a.cache.InstanceInfo[vmSize] = info
				return info, nil
			}
		}
	}

	return nil, fmt.Errorf("VM size %s not found in region %s", vmSize, a.region)
}

// GetNodeVMSize returns the VM size for a node
func (a *AzureCloudClient) GetNodeVMSize(ctx context.Context, nodeName string) (string, error) {
	// In Azure, node name typically matches VM name
	// Would need to query VMs in the resource group
	// This is a simplified implementation
	vmClient, err := armcompute.NewVirtualMachinesClient(a.subscriptionID, a.cred, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create VM client: %w", err)
	}

	// Would need to know resource group - simplified
	// In practice, you'd list all VMs or use tags
	return "", fmt.Errorf("not implemented: need resource group to lookup VM")
}

func toPtr[T any](v T) *T {
	return &v
}
