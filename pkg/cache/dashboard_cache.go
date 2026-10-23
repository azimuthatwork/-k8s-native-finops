package cache

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/yourorg/kube-native-finops/pkg/api"
)

// DashboardCache provides caching for dashboard data with in-memory and optional Redis support
type DashboardCache struct {
	logger        *zap.Logger
	mu            sync.RWMutex
	inMemoryCache *InMemoryCache
	redisClient   RedisClient
	useRedis      bool
	defaultTTL    time.Duration
}

// InMemoryCache is a simple thread-safe in-memory cache
type InMemoryCache struct {
	mu    sync.RWMutex
	data  map[string]*CacheEntry
	stats CacheStats
}

// CacheEntry represents a cached item with metadata
type CacheEntry struct {
	Data        interface{}
	ExpiresAt   time.Time
	CreatedAt   time.Time
	AccessCount int64
}

// CacheStats tracks cache performance metrics
type CacheStats struct {
	Hits      int64
	Misses    int64
	StaleHits int64
	Errors    int64
	Evictions int64
}

// RedisClient interface for optional Redis backend
type RedisClient interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	Close() error
}

// NewDashboardCache creates a new dashboard cache
func NewDashboardCache(logger *zap.Logger, defaultTTL time.Duration, redisClient RedisClient) *DashboardCache {
	cache := &DashboardCache{
		logger:        logger,
		inMemoryCache: NewInMemoryCache(),
		defaultTTL:    defaultTTL,
		redisClient:   redisClient,
		useRedis:      redisClient != nil,
	}

	// Start cleanup goroutine for in-memory cache
	go cache.cleanupLoop()

	return cache
}

// NewInMemoryCache creates a new in-memory cache
func NewInMemoryCache() *InMemoryCache {
	return &InMemoryCache{
		data: make(map[string]*CacheEntry),
	}
}

// GetOverview retrieves the dashboard overview from cache
func (c *DashboardCache) GetOverview(ctx context.Context) (*api.DashboardOverview, string, error) {
	const cacheKey = "dashboard:overview"

	// Try in-memory cache first (fastest)
	if entry, ok := c.inMemoryCache.Get(cacheKey); ok {
		if overview, ok := entry.Data.(*api.DashboardOverview); ok {
			c.inMemoryCache.stats.Hits++
			return overview, "HIT", nil
		}
	}

	// Try Redis if enabled
	if c.useRedis && c.redisClient != nil {
		data, err := c.redisClient.Get(ctx, cacheKey)
		if err == nil && data != "" {
			var overview api.DashboardOverview
			if err := json.Unmarshal([]byte(data), &overview); err == nil {
				// Populate in-memory cache for faster subsequent access
				c.inMemoryCache.Set(cacheKey, &overview, c.defaultTTL)
				c.inMemoryCache.stats.Hits++
				return &overview, "HIT", nil
			}
		}
	}

	c.inMemoryCache.stats.Misses++
	return nil, "MISS", nil
}

// SetOverview stores the dashboard overview in cache
func (c *DashboardCache) SetOverview(ctx context.Context, overview *api.DashboardOverview, ttl time.Duration) error {
	const cacheKey = "dashboard:overview"

	// Store in in-memory cache
	c.inMemoryCache.Set(cacheKey, overview, ttl)

	// Store in Redis if enabled
	if c.useRedis && c.redisClient != nil {
		data, err := json.Marshal(overview)
		if err != nil {
			c.logger.Warn("Failed to marshal dashboard overview for Redis", zap.Error(err))
			return err
		}
		if err := c.redisClient.Set(ctx, cacheKey, string(data), ttl); err != nil {
			c.logger.Warn("Failed to set dashboard overview in Redis", zap.Error(err))
			c.inMemoryCache.stats.Errors++
			return err
		}
	}

	return nil
}

// InvalidateOverview removes the dashboard overview from cache
func (c *DashboardCache) InvalidateOverview(ctx context.Context) error {
	const cacheKey = "dashboard:overview"

	c.inMemoryCache.Delete(cacheKey)

	if c.useRedis && c.redisClient != nil {
		return c.redisClient.Delete(ctx, cacheKey)
	}

	return nil
}

// GetStats returns cache statistics
func (c *DashboardCache) GetStats() CacheStats {
	c.inMemoryCache.mu.RLock()
	defer c.inMemoryCache.mu.RUnlock()
	return c.inMemoryCache.stats
}

// Get retrieves an entry from in-memory cache
func (c *InMemoryCache) Get(key string) (*CacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.data[key]
	if !ok {
		return nil, false
	}

	// Check expiration
	if time.Now().After(entry.ExpiresAt) {
		// Expired - will be cleaned up by cleanupLoop
		return nil, false
	}

	entry.AccessCount++
	return entry, true
}

// Set stores an entry in in-memory cache
func (c *InMemoryCache) Set(key string, data interface{}, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data[key] = &CacheEntry{
		Data:        data,
		ExpiresAt:   time.Now().Add(ttl),
		CreatedAt:   time.Now(),
		AccessCount: 0,
	}
}

// Delete removes an entry from in-memory cache
func (c *InMemoryCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, key)
}

// cleanupLoop periodically removes expired entries
func (c *DashboardCache) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.inMemoryCache.cleanup()
	}
}

// cleanup removes expired entries from in-memory cache
func (c *InMemoryCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, entry := range c.data {
		if now.After(entry.ExpiresAt) {
			delete(c.data, key)
			c.stats.Evictions++
		}
	}
}

// MockRedisClient is a mock implementation for testing or when Redis is not available
type MockRedisClient struct {
	data map[string]string
	mu   sync.RWMutex
}

// NewMockRedisClient creates a new mock Redis client
func NewMockRedisClient() *MockRedisClient {
	return &MockRedisClient{
		data: make(map[string]string),
	}
}

// Get retrieves a value from mock Redis
func (m *MockRedisClient) Get(ctx context.Context, key string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	val, ok := m.data[key]
	if !ok {
		return "", nil // Not found is not an error
	}
	return val, nil
}

// Set stores a value in mock Redis
func (m *MockRedisClient) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	m.data[key] = string(data)
	return nil
}

// Delete removes a value from mock Redis
func (m *MockRedisClient) Delete(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

// Exists checks if a key exists in mock Redis
func (m *MockRedisClient) Exists(ctx context.Context, key string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.data[key]
	return ok, nil
}

// Close closes the mock Redis client
func (m *MockRedisClient) Close() error {
	return nil
}
