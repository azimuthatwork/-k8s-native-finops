package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	pricingtypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
	"github.com/prometheus/common/model"
	"k8s.io/klog/v2"
)

// AWSCloudClient implements CloudPricingClient for AWS
type AWSCloudClient struct {
	ec2Client    *ec2.Client
	pricingClient *pricing.Client
	cache        *CloudPriceCache
	mu           sync.RWMutex
	region       string
	cacheTTL     time.Duration
	spotCacheTTL time.Duration
	lastUpdate   time.Time
	spotUpdate   time.Time
}

// NewAWSCloudClient creates a new AWS cloud client
func NewAWSCloudClient(region string, cacheTTL, spotCacheTTL time.Duration) (*AWSCloudClient, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &AWSCloudClient{
		ec2Client:     ec2.NewFromConfig(cfg),
		pricingClient: pricing.NewFromConfig(cfg),
		cache:         &CloudPriceCache{},
		region:        region,
		cacheTTL:      cacheTTL,
		spotCacheTTL:  spotCacheTTL,
	}, nil
}

// GetNodePrice returns the hourly price for a node instance type
func (a *AWSCloudClient) GetNodePrice(ctx context.Context, instanceType string, isSpot bool) (float64, error) {
	a.mu.RLock()
	if time.Since(a.lastUpdate) < a.cacheTTL && !isSpot {
		if price, ok := a.cache.OnDemandPrices[instanceType]; ok {
			a.mu.RUnlock()
			return price, nil
		}
	}
	if isSpot && time.Since(a.spotUpdate) < a.spotCacheTTL {
		if price, ok := a.cache.SpotPrices[instanceType]; ok {
			a.mu.RUnlock()
			return price, nil
		}
	}
	a.mu.RUnlock()

	// Need to refresh cache
	return a.refreshAndGetPrice(ctx, instanceType, isSpot)
}

// refreshAndGetPrice refreshes the price cache and returns the price
func (a *AWSCloudClient) refreshAndGetPrice(ctx context.Context, instanceType string, isSpot bool) (float64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Double-check after acquiring lock
	if !isSpot && time.Since(a.lastUpdate) < a.cacheTTL {
		if price, ok := a.cache.OnDemandPrices[instanceType]; ok {
			return price, nil
		}
	}
	if isSpot && time.Since(a.spotUpdate) < a.spotCacheTTL {
		if price, ok := a.cache.SpotPrices[instanceType]; ok {
			return price, nil
		}
	}

	// Fetch from Pricing API
	if isSpot {
		price, err := a.fetchSpotPrice(ctx, instanceType)
		if err != nil {
			return 0, err
		}
		a.cache.SpotPrices[instanceType] = price
		a.spotUpdate = time.Now()
		return price, nil
	} else {
		price, err := a.fetchOnDemandPrice(ctx, instanceType)
		if err != nil {
			return 0, err
		}
		a.cache.OnDemandPrices[instanceType] = price
		a.lastUpdate = time.Now()
		return price, nil
	}
}

// fetchOnDemandPrice fetches on-demand price from AWS Pricing API
func (a *AWSCloudClient) fetchOnDemandPrice(ctx context.Context, instanceType string) (float64, error) {
	input := &pricing.GetProductsInput{
		ServiceCode: aws.String("AmazonEC2"),
		Filters: []pricingtypes.Filter{
			{Type: aws.String("TERM_MATCH"), Field: aws.String("instanceType"), Value: aws.String(instanceType)},
			{Type: aws.String("TERM_MATCH"), Field: aws.String("location"), Value: aws.String(a.regionName())},
			{Type: aws.String("TERM_MATCH"), Field: aws.String("operatingSystem"), Value: aws.String("Linux")},
			{Type: aws.String("TERM_MATCH"), Field: aws.String("preInstalledSw"), Value: aws.String("NA")},
			{Type: aws.String("TERM_MATCH"), Field: aws.String("tenancy"), Value: aws.String("Shared")},
			{Type: aws.String("TERM_MATCH"), Field: aws.String("capacitystatus"), Value: aws.String("Used")},
		},
		MaxResults: aws.Int32(10),
	}

	resp, err := a.pricingClient.GetProducts(ctx, input)
	if err != nil {
		return 0, fmt.Errorf("failed to get pricing: %w", err)
	}

	if len(resp.PriceList) == 0 {
		return 0, fmt.Errorf("no pricing found for instance type %s in region %s", instanceType, a.region)
	}

	// Parse the first price list item
	var priceList map[string]interface{}
	if err := json.Unmarshal([]byte(resp.PriceList[0]), &priceList); err != nil {
		return 0, fmt.Errorf("failed to parse price list: %w", err)
	}

	// Extract OnDemand terms
	terms, ok := priceList["terms"].(map[string]interface{})
	if !ok {
		return 0, fmt.Errorf("no terms in price list")
	}

	onDemand, ok := terms["OnDemand"].(map[string]interface{})
	if !ok {
		return 0, fmt.Errorf("no OnDemand terms")
	}

	for _, term := range onDemand {
		termMap, ok := term.(map[string]interface{})
		if !ok {
			continue
		}
		priceDimensions, ok := termMap["priceDimensions"].(map[string]interface{})
		if !ok {
			continue
		}
		for _, dim := range priceDimensions {
			dimMap, ok := dim.(map[string]interface{})
			if !ok {
				continue
			}
			unit, _ := dimMap["unit"].(string)
			if unit != "Hrs" {
				continue
			}
			pricePerUnit, ok := dimMap["pricePerUnit"].(map[string]interface{})
			if !ok {
				continue
			}
			usdStr, ok := pricePerUnit["USD"].(string)
			if !ok {
				continue
			}
			price, err := strconv.ParseFloat(usdStr, 64)
			if err != nil {
				continue
			}
			return price, nil
		}
	}

	return 0, fmt.Errorf("no hourly price found for %s", instanceType)
}

// fetchSpotPrice fetches spot price from EC2 API
func (a *AWSCloudClient) fetchSpotPrice(ctx context.Context, instanceType string) (float64, error) {
	input := &ec2.DescribeSpotPriceHistoryInput{
		InstanceTypes: []ec2types.InstanceType{ec2types.InstanceType(instanceType)},
		ProductDescriptions: []string{"Linux/UNIX"},
		MaxResults: aws.Int32(1),
		StartTime:  aws.Time(time.Now().Add(-1 * time.Hour)),
	}

	resp, err := a.ec2Client.DescribeSpotPriceHistory(ctx, input)
	if err != nil {
		return 0, fmt.Errorf("failed to get spot price history: %w", err)
	}

	if len(resp.SpotPriceHistory) == 0 {
		return 0, fmt.Errorf("no spot price history for %s", instanceType)
	}

	// Get the most recent price
	sort.Slice(resp.SpotPriceHistory, func(i, j int) bool {
		return resp.SpotPriceHistory[i].Timestamp.After(*resp.SpotPriceHistory[j].Timestamp)
	})

	priceStr := *resp.SpotPriceHistory[0].SpotPrice
	price, err := strconv.ParseFloat(priceStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse spot price: %w", err)
	}

	return price, nil
}

// GetInstanceInfo returns CPU, Memory, GPU info for an instance type
func (a *AWSCloudClient) GetInstanceInfo(ctx context.Context, instanceType string) (*InstanceInfo, error) {
	a.mu.RLock()
	if info, ok := a.cache.InstanceInfo[instanceType]; ok {
		a.mu.RUnlock()
		return info, nil
	}
	a.mu.RUnlock()

	a.mu.Lock()
	defer a.mu.Unlock()

	// Double-check
	if info, ok := a.cache.InstanceInfo[instanceType]; ok {
		return info, nil
	}

	input := &ec2.DescribeInstanceTypesInput{
		InstanceTypes: []string{instanceType},
	}

	resp, err := a.ec2Client.DescribeInstanceTypes(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("failed to describe instance type: %w", err)
	}

	if len(resp.InstanceTypes) == 0 {
		return nil, fmt.Errorf("instance type %s not found", instanceType)
	}

	it := resp.InstanceTypes[0]
	info := &InstanceInfo{
		InstanceType: instanceType,
		VCPU:         int(*it.VCpuInfo.DefaultVCpus),
		MemoryGiB:    float64(*it.MemoryInfo.SizeInMiB) / 1024,
	}

	if it.GpuInfo != nil {
		for _, gpu := range it.GpuInfo.Gpus {
			info.GPUCount += int(gpu.Count)
			info.GPUType = *gpu.Name
		}
	}

	a.cache.InstanceInfo[instanceType] = info
	return info, nil
}

// GetNodeInstanceType returns the instance type for a node
func (a *AWSCloudClient) GetNodeInstanceType(ctx context.Context, nodeName string) (string, error) {
	// In practice, this would query EC2 for the instance ID from the node name
	// For now, we'll use a label-based approach or assume the node name contains the instance ID
	// This is a simplified implementation
	input := &ec2.DescribeInstancesInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("private-dns-name"), Values: []string{nodeName}},
		},
	}

	resp, err := a.ec2Client.DescribeInstances(ctx, input)
	if err != nil {
		return "", fmt.Errorf("failed to describe instances: %w", err)
	}

	for _, reservation := range resp.Reservations {
		for _, instance := range reservation.Instances {
			if instance.InstanceType != "" {
				return string(instance.InstanceType), nil
			}
		}
	}

	return "", fmt.Errorf("instance not found for node %s", nodeName)
}

// regionName converts AWS region code to pricing API region name
func (a *AWSCloudClient) regionName() string {
	regionMap := map[string]string{
		"us-east-1":      "US East (N. Virginia)",
		"us-east-2":      "US East (Ohio)",
		"us-west-1":      "US West (N. California)",
		"us-west-2":      "US West (Oregon)",
		"eu-west-1":      "EU (Ireland)",
		"eu-west-2":      "EU (London)",
		"eu-west-3":      "EU (Paris)",
		"eu-central-1":   "EU (Frankfurt)",
		"ap-northeast-1": "Asia Pacific (Tokyo)",
		"ap-northeast-2": "Asia Pacific (Seoul)",
		"ap-southeast-1": "Asia Pacific (Singapore)",
		"ap-southeast-2": "Asia Pacific (Sydney)",
		"sa-east-1":      "South America (São Paulo)",
	}
	if name, ok := regionMap[a.region]; ok {
		return name
	}
	return a.region
}

// CloudPriceCache holds cached pricing information
type CloudPriceCache struct {
	OnDemandPrices map[string]float64
	SpotPrices     map[string]float64
	InstanceInfo   map[string]*InstanceInfo
}

// InstanceInfo holds compute specifications for an instance type
type InstanceInfo struct {
	InstanceType string
	VCPU         int
	MemoryGiB    float64
	GPUCount     int
	GPUType      string
}
