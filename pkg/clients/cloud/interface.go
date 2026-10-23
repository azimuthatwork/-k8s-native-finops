package cloud

import (
	"context"

	"github.com/prometheus/common/model"
)

// CloudPricingClient defines the interface for cloud pricing clients
type CloudPricingClient interface {
	// GetNodePrice returns the hourly price for an instance type
	GetNodePrice(ctx context.Context, instanceType string, isSpot bool) (float64, error)
	
	// GetInstanceInfo returns compute specifications for an instance type
	GetInstanceInfo(ctx context.Context, instanceType string) (*InstanceInfo, error)
	
	// GetNodeInstanceType returns the instance type for a given node name
	GetNodeInstanceType(ctx context.Context, nodeName string) (string, error)
}

// InstanceInfo holds compute specifications for an instance type
type InstanceInfo struct {
	InstanceType string
	VCPU         int
	MemoryGiB    float64
	GPUCount     int
	GPUType      string
}

// CloudPriceCache holds cached pricing information
type CloudPriceCache struct {
	OnDemandPrices map[string]float64
	SpotPrices     map[string]float64
	InstanceInfo   map[string]*InstanceInfo
}
