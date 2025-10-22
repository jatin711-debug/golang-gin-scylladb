package cache

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	// ErrCacheMiss is returned when key doesn't exist (not an actual error)
	ErrCacheMiss = errors.New("cache miss")
	// ErrCacheUnavailable is returned when Redis is down or unreachable
	ErrCacheUnavailable = errors.New("cache unavailable")
)

type RedisClient struct {
	client  *redis.Client
	metrics *CacheMetrics
}

// CacheMetrics tracks cache performance for observability
type CacheMetrics struct {
	Hits   atomic.Int64
	Misses atomic.Int64
	Errors atomic.Int64
}

// RedisConfig holds production-ready Redis configuration
type RedisConfig struct {
	Host         string
	Port         string
	Password     string
	DB           int
	MaxRetries   int           // Number of retries for failed operations
	PoolSize     int           // Maximum number of socket connections
	MinIdleConns int           // Minimum idle connections in the pool
	DialTimeout  time.Duration // Timeout for establishing connections
	ReadTimeout  time.Duration // Timeout for socket reads
	WriteTimeout time.Duration // Timeout for socket writes
}

// DefaultRedisConfig returns sensible production defaults
func DefaultRedisConfig() *RedisConfig {
	return &RedisConfig{
		Host:         "localhost",
		Port:         "6379",
		Password:     "",
		DB:           0,
		MaxRetries:   3,
		PoolSize:     10,
		MinIdleConns: 5,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	}
}

// NewRedisClient creates a production-ready Redis client with connection validation
func NewRedisClient(config *RedisConfig) (*RedisClient, error) {
	if config == nil {
		config = DefaultRedisConfig()
	}

	// Create Redis client with production settings
	client := redis.NewClient(&redis.Options{
		Addr:         config.Host + ":" + config.Port,
		Password:     config.Password,
		DB:           config.DB,
		MaxRetries:   config.MaxRetries,
		PoolSize:     config.PoolSize,
		MinIdleConns: config.MinIdleConns,
		DialTimeout:  config.DialTimeout,
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,

		// Production optimizations
		PoolTimeout:  4 * time.Second,
		MaxIdleConns: 5,
	})

	// CRITICAL: Validate connection before returning (fail fast)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to connect to Redis at %s:%s: %w",
			config.Host, config.Port, err)
	}

	log.Printf("[Redis] Successfully connected to %s:%s (DB: %d)",
		config.Host, config.Port, config.DB)

	return &RedisClient{
		client:  client,
		metrics: &CacheMetrics{},
	}, nil
}

// Set stores a value with TTL - accepts context for proper timeout/cancellation
func (r *RedisClient) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	// Ensure we have a context with timeout
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
	}

	err := r.client.Set(ctx, key, value, ttl).Err()
	if err != nil {
		r.metrics.Errors.Add(1)
		log.Printf("[Redis] SET failed for key '%s': %v", key, err)
		return fmt.Errorf("cache set failed: %w", err)
	}

	return nil
}

// Get retrieves a value - properly distinguishes cache miss from errors
func (r *RedisClient) Get(ctx context.Context, key string) (string, error) {
	// Ensure we have a context with timeout
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
	}

	val, err := r.client.Get(ctx, key).Result()
	if err != nil {
		// Cache miss is NOT an error - it's an expected case
		if errors.Is(err, redis.Nil) {
			r.metrics.Misses.Add(1)
			return "", ErrCacheMiss
		}

		// Actual error (Redis down, network issue, timeout, etc.)
		r.metrics.Errors.Add(1)
		log.Printf("[Redis] GET failed for key '%s': %v", key, err)
		return "", fmt.Errorf("%w: %v", ErrCacheUnavailable, err)
	}

	r.metrics.Hits.Add(1)
	return val, nil
}

// Exists checks if a key exists - useful for email uniqueness checks
func (r *RedisClient) Exists(ctx context.Context, key string) (bool, error) {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
	}

	count, err := r.client.Exists(ctx, key).Result()
	if err != nil {
		r.metrics.Errors.Add(1)
		log.Printf("[Redis] EXISTS failed for key '%s': %v", key, err)
		return false, fmt.Errorf("%w: %v", ErrCacheUnavailable, err)
	}

	if count > 0 {
		r.metrics.Hits.Add(1)
		return true, nil
	}

	r.metrics.Misses.Add(1)
	return false, nil
}

// SetNX sets key only if it doesn't exist (atomic check-and-set)
// Returns true if key was set, false if it already existed
// PERFECT for email uniqueness - no race conditions!
func (r *RedisClient) SetNX(ctx context.Context, key string, value any, ttl time.Duration) (bool, error) {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
	}

	success, err := r.client.SetNX(ctx, key, value, ttl).Result()
	if err != nil {
		r.metrics.Errors.Add(1)
		log.Printf("[Redis] SETNX failed for key '%s': %v", key, err)
		return false, fmt.Errorf("cache setnx failed: %w", err)
	}

	if success {
		r.metrics.Hits.Add(1)
	} else {
		r.metrics.Misses.Add(1)
	}

	return success, nil
}

// Delete removes a key from cache
func (r *RedisClient) Delete(ctx context.Context, key string) error {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
	}

	err := r.client.Del(ctx, key).Err()
	if err != nil {
		r.metrics.Errors.Add(1)
		log.Printf("[Redis] DELETE failed for key '%s': %v", key, err)
		return fmt.Errorf("cache delete failed: %w", err)
	}

	return nil
}

// Incr atomically increments a counter - useful for rate limiting
func (r *RedisClient) Incr(ctx context.Context, key string) (int64, error) {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
	}

	val, err := r.client.Incr(ctx, key).Result()
	if err != nil {
		r.metrics.Errors.Add(1)
		log.Printf("[Redis] INCR failed for key '%s': %v", key, err)
		return 0, fmt.Errorf("cache incr failed: %w", err)
	}

	return val, nil
}

// Expire sets a timeout on a key - useful with Incr for rate limiting
func (r *RedisClient) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
	}

	err := r.client.Expire(ctx, key, ttl).Err()
	if err != nil {
		r.metrics.Errors.Add(1)
		log.Printf("[Redis] EXPIRE failed for key '%s': %v", key, err)
		return fmt.Errorf("cache expire failed: %w", err)
	}

	return nil
}

// GetMetrics returns current cache performance metrics
func (r *RedisClient) GetMetrics() map[string]int64 {
	return map[string]int64{
		"hits":   r.metrics.Hits.Load(),
		"misses": r.metrics.Misses.Load(),
		"errors": r.metrics.Errors.Load(),
	}
}

// GetHitRate calculates cache hit rate as a percentage
func (r *RedisClient) GetHitRate() float64 {
	hits := r.metrics.Hits.Load()
	misses := r.metrics.Misses.Load()
	total := hits + misses

	if total == 0 {
		return 0.0
	}

	return float64(hits) / float64(total) * 100.0
}

// HealthCheck verifies Redis is responsive - critical for health endpoints
func (r *RedisClient) HealthCheck(ctx context.Context) error {
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
	}

	if err := r.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis health check failed: %w", err)
	}

	return nil
}

// GetPoolStats returns connection pool statistics for monitoring
func (r *RedisClient) GetPoolStats() *redis.PoolStats {
	return r.client.PoolStats()
}

// Close gracefully closes the Redis connection with final stats logging
func (r *RedisClient) Close() error {
	hits := r.metrics.Hits.Load()
	misses := r.metrics.Misses.Load()
	errors := r.metrics.Errors.Load()

	log.Printf("[Redis] Closing connection. Final stats - Hits: %d, Misses: %d, Errors: %d, Hit Rate: %.2f%%",
		hits, misses, errors, r.GetHitRate())

	return r.client.Close()
}
