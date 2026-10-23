package clients

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// RedisConfig holds Redis connection configuration
type RedisConfig struct {
	Address  string
	Password string
	DB       int
	Enabled  bool
}

// NewRedisClient creates a new Redis client for caching
func NewRedisClient(logger *zap.Logger, config RedisConfig) (*RedisCacheClient, error) {
	if !config.Enabled {
		return nil, nil
	}

	client := redis.NewClient(&redis.Options{
		Addr:     config.Address,
		Password: config.Password,
		DB:       config.DB,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		logger.Warn("Failed to connect to Redis, caching will use in-memory only", zap.Error(err))
		return nil, err
	}

	logger.Info("Connected to Redis for caching", zap.String("address", config.Address))

	return &RedisCacheClient{client: client}, nil
}

// RedisCacheClient wraps the Redis client for caching operations
type RedisCacheClient struct {
	client *redis.Client
}

// Get retrieves a value from Redis
func (r *RedisCacheClient) Get(ctx context.Context, key string) (string, error) {
	return r.client.Get(ctx, key).Result()
}

// Set stores a value in Redis with TTL
func (r *RedisCacheClient) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return r.client.Set(ctx, key, value, ttl).Err()
}

// Delete removes a value from Redis
func (r *RedisCacheClient) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

// Exists checks if a key exists in Redis
func (r *RedisCacheClient) Exists(ctx context.Context, key string) (bool, error) {
	count, err := r.client.Exists(ctx, key).Result()
	return count > 0, err
}

// Close closes the Redis connection
func (r *RedisCacheClient) Close() error {
	return r.client.Close()
}
