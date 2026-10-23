package cache

import (
	"context"
	"testing"
	"time"

	"go.uber.org/zap"
	"github.com/yourorg/kube-native-finops/pkg/api"
)

func TestDashboardCache_InMemory(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cache := NewDashboardCache(logger, 10*time.Second, nil)

	ctx := context.Background()

	// Test cache miss
	overview, status, err := cache.GetOverview(ctx)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if overview != nil {
		t.Fatal("Expected nil overview on cache miss")
	}
	if status != "MISS" {
		t.Fatalf("Expected MISS status, got %s", status)
	}

	// Test cache set and hit
	testOverview := &api.DashboardOverview{
		ClusterSummary: api.ClusterSummary{
			TotalNodes: 3,
		},
		LastUpdated: time.Now(),
	}

	err = cache.SetOverview(ctx, testOverview, 10*time.Second)
	if err != nil {
		t.Fatalf("Failed to set overview: %v", err)
	}

	overview, status, err = cache.GetOverview(ctx)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if overview == nil {
		t.Fatal("Expected overview on cache hit")
	}
	if overview.ClusterSummary.TotalNodes != 3 {
		t.Fatalf("Expected 3 nodes, got %d", overview.ClusterSummary.TotalNodes)
	}
	if status != "HIT" {
		t.Fatalf("Expected HIT status, got %s", status)
	}

	// Test stats
	stats := cache.GetStats()
	if stats.Hits != 1 {
		t.Fatalf("Expected 1 hit, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Fatalf("Expected 1 miss, got %d", stats.Misses)
	}
}

func TestDashboardCache_WithMockRedis(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	mockRedis := NewMockRedisClient()
	cache := NewDashboardCache(logger, 10*time.Second, mockRedis)

	ctx := context.Background()

	// Test cache miss
	overview, status, err := cache.GetOverview(ctx)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if overview != nil {
		t.Fatal("Expected nil overview on cache miss")
	}
	if status != "MISS" {
		t.Fatalf("Expected MISS status, got %s", status)
	}

	// Test cache set and hit (should populate both in-memory and Redis)
	testOverview := &api.DashboardOverview{
		ClusterSummary: api.ClusterSummary{
			TotalNodes: 5,
		},
		LastUpdated: time.Now(),
	}

	err = cache.SetOverview(ctx, testOverview, 10*time.Second)
	if err != nil {
		t.Fatalf("Failed to set overview: %v", err)
	}

	// First hit should come from in-memory
	overview, status, err = cache.GetOverview(ctx)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if status != "HIT" {
		t.Fatalf("Expected HIT status, got %s", status)
	}

	// Create new cache with same mock Redis to test Redis persistence
	cache2 := NewDashboardCache(logger, 10*time.Second, mockRedis)
	overview, status, err = cache2.GetOverview(ctx)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if overview == nil {
		t.Fatal("Expected overview from Redis")
	}
	if overview.ClusterSummary.TotalNodes != 5 {
		t.Fatalf("Expected 5 nodes, got %d", overview.ClusterSummary.TotalNodes)
	}
	if status != "HIT" {
		t.Fatalf("Expected HIT status from Redis, got %s", status)
	}
}

func TestDashboardCache_Expiration(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cache := NewDashboardCache(logger, 10*time.Millisecond, nil)

	ctx := context.Background()

	testOverview := &api.DashboardOverview{
		ClusterSummary: api.ClusterSummary{
			TotalNodes: 1,
		},
		LastUpdated: time.Now(),
	}

	err := cache.SetOverview(ctx, testOverview, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to set overview: %v", err)
	}

	// Should hit immediately
	overview, status, err := cache.GetOverview(ctx)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if status != "HIT" {
		t.Fatalf("Expected HIT status, got %s", status)
	}

	// Wait for expiration
	time.Sleep(20 * time.Millisecond)

	// Should miss now
	overview, status, err = cache.GetOverview(ctx)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if overview != nil {
		t.Fatal("Expected nil overview after expiration")
	}
	if status != "MISS" {
		t.Fatalf("Expected MISS status after expiration, got %s", status)
	}
}

func TestDashboardCache_Invalidate(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cache := NewDashboardCache(logger, 10*time.Second, nil)

	ctx := context.Background()

	testOverview := &api.DashboardOverview{
		ClusterSummary: api.ClusterSummary{
			TotalNodes: 2,
		},
		LastUpdated: time.Now(),
	}

	err := cache.SetOverview(ctx, testOverview, 10*time.Second)
	if err != nil {
		t.Fatalf("Failed to set overview: %v", err)
	}

	// Verify it's cached
	overview, status, err := cache.GetOverview(ctx)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if status != "HIT" {
		t.Fatalf("Expected HIT status, got %s", status)
	}

	// Invalidate
	err = cache.InvalidateOverview(ctx)
	if err != nil {
		t.Fatalf("Failed to invalidate: %v", err)
	}

	// Should miss now
	overview, status, err = cache.GetOverview(ctx)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if overview != nil {
		t.Fatal("Expected nil overview after invalidation")
	}
	if status != "MISS" {
		t.Fatalf("Expected MISS status after invalidation, got %s", status)
	}
}
