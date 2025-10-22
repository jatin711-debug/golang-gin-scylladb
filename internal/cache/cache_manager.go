package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"
)

// CacheManager orchestrates multi-tier caching with intelligent fallback
// Architecture: L1 (Local BigCache) → L2 (Redis) → L3 (Database/Source)
type CacheManager struct {
	local  *LocalCache
	redis  *RedisClient
	config *CacheManagerConfig
}

// CacheManagerConfig holds cache manager configuration
type CacheManagerConfig struct {
	// LocalTTL is default TTL for local cache
	LocalTTL time.Duration

	// RedisTTL is default TTL for Redis cache
	RedisTTL time.Duration

	// EnableLocalCache enables L1 caching
	EnableLocalCache bool

	// EnableRedisCache enables L2 caching
	EnableRedisCache bool

	// GracefulDegradation continues operation if Redis is down
	GracefulDegradation bool

	// WriteThrough writes to all cache tiers simultaneously
	WriteThrough bool

	// Name for logging
	Name string
}

// DefaultCacheManagerConfig returns sensible production defaults
func DefaultCacheManagerConfig() *CacheManagerConfig {
	return &CacheManagerConfig{
		LocalTTL:            1 * time.Minute,  // Short TTL for local
		RedisTTL:            10 * time.Minute, // Longer TTL for Redis
		EnableLocalCache:    true,
		EnableRedisCache:    true,
		GracefulDegradation: true, // Don't fail if Redis is down
		WriteThrough:        true, // Write to all tiers
		Name:                "default",
	}
}

// NewCacheManager creates a production-ready cache manager
func NewCacheManager(local *LocalCache, redis *RedisClient, config *CacheManagerConfig) *CacheManager {
	if config == nil {
		config = DefaultCacheManagerConfig()
	}

	log.Printf("[CacheManager:%s] Initialized - Local: %v, Redis: %v, Graceful: %v",
		config.Name, config.EnableLocalCache, config.EnableRedisCache, config.GracefulDegradation)

	return &CacheManager{
		local:  local,
		redis:  redis,
		config: config,
	}
}

// Get retrieves a value from cache with automatic tier fallback
// Returns (value, source, error) where source is "local", "redis", or "miss"
func (cm *CacheManager) Get(ctx context.Context, key string) (string, string, error) {
	// L1: Check local cache first (fastest - ~0.001ms)
	if cm.config.EnableLocalCache && cm.local != nil {
		value, err := cm.local.GetString(key)
		if err == nil {
			return value, "local", nil
		}

		// Only log if it's not a cache miss
		if !errors.Is(err, ErrCacheMiss) {
			log.Printf("[CacheManager:%s] Local cache error for key '%s': %v", cm.config.Name, key, err)
		}
	}

	// L2: Check Redis cache (~0.5-2ms)
	if cm.config.EnableRedisCache && cm.redis != nil {
		value, err := cm.redis.Get(ctx, key)
		if err == nil {
			// Found in Redis - populate local cache (write-back)
			if cm.config.EnableLocalCache && cm.local != nil {
				if setErr := cm.local.SetString(key, value); setErr != nil {
					log.Printf("[CacheManager:%s] Failed to write-back to local cache: %v", cm.config.Name, setErr)
				}
			}
			return value, "redis", nil
		}

		// Check if it's a cache miss or actual error
		if errors.Is(err, ErrCacheMiss) {
			return "", "miss", ErrCacheMiss
		}

		// Redis is down/error
		if cm.config.GracefulDegradation {
			log.Printf("[CacheManager:%s] Redis unavailable, continuing without cache: %v", cm.config.Name, err)
			return "", "miss", ErrCacheMiss
		}

		return "", "error", err
	}

	// Cache miss on all tiers
	return "", "miss", ErrCacheMiss
}

// Set stores a value in cache (write-through to all enabled tiers)
func (cm *CacheManager) Set(ctx context.Context, key string, value any) error {
	var localErr, redisErr error

	// Write to local cache
	if cm.config.EnableLocalCache && cm.local != nil {
		localErr = cm.local.SetJSON(key, value)
		if localErr != nil {
			log.Printf("[CacheManager:%s] Failed to set in local cache: %v", cm.config.Name, localErr)
		}
	}

	// Write to Redis cache
	if cm.config.EnableRedisCache && cm.redis != nil {
		redisErr = cm.redis.Set(ctx, key, value, cm.config.RedisTTL)
		if redisErr != nil {
			log.Printf("[CacheManager:%s] Failed to set in Redis: %v", cm.config.Name, redisErr)

			if !cm.config.GracefulDegradation {
				return redisErr
			}
		}
	}

	// Return error only if both failed and graceful degradation is off
	if localErr != nil && redisErr != nil && !cm.config.GracefulDegradation {
		return fmt.Errorf("failed to set in cache: local=%v, redis=%v", localErr, redisErr)
	}

	return nil
}

// SetWithTTL stores a value with custom TTLs for each tier
func (cm *CacheManager) SetWithTTL(ctx context.Context, key string, value string, localTTL, redisTTL time.Duration) error {
	var localErr, redisErr error

	// Note: BigCache doesn't support per-key TTL, uses global LifeWindow
	// So localTTL is ignored, but keeping parameter for API consistency
	if cm.config.EnableLocalCache && cm.local != nil {
		localErr = cm.local.SetString(key, value)
		if localErr != nil {
			log.Printf("[CacheManager:%s] Failed to set in local cache: %v", cm.config.Name, localErr)
		}
	}

	// Write to Redis with custom TTL
	if cm.config.EnableRedisCache && cm.redis != nil {
		redisErr = cm.redis.Set(ctx, key, value, redisTTL)
		if redisErr != nil {
			log.Printf("[CacheManager:%s] Failed to set in Redis: %v", cm.config.Name, redisErr)

			if !cm.config.GracefulDegradation {
				return redisErr
			}
		}
	}

	if localErr != nil && redisErr != nil && !cm.config.GracefulDegradation {
		return fmt.Errorf("failed to set in cache: local=%v, redis=%v", localErr, redisErr)
	}

	return nil
}

// Delete removes a key from all cache tiers
func (cm *CacheManager) Delete(ctx context.Context, key string) error {
	var localErr, redisErr error

	// Delete from local cache
	if cm.config.EnableLocalCache && cm.local != nil {
		localErr = cm.local.Delete(key)
		if localErr != nil {
			log.Printf("[CacheManager:%s] Failed to delete from local cache: %v", cm.config.Name, localErr)
		}
	}

	// Delete from Redis
	if cm.config.EnableRedisCache && cm.redis != nil {
		redisErr = cm.redis.Delete(ctx, key)
		if redisErr != nil {
			log.Printf("[CacheManager:%s] Failed to delete from Redis: %v", cm.config.Name, redisErr)
		}
	}

	// Best effort - only error if both failed
	if localErr != nil && redisErr != nil {
		return fmt.Errorf("failed to delete from cache: local=%v, redis=%v", localErr, redisErr)
	}

	return nil
}

// Exists checks if a key exists in any cache tier
func (cm *CacheManager) Exists(ctx context.Context, key string) (bool, error) {
	// Check local cache first
	if cm.config.EnableLocalCache && cm.local != nil {
		if cm.local.Exists(key) {
			return true, nil
		}
	}

	// Check Redis
	if cm.config.EnableRedisCache && cm.redis != nil {
		exists, err := cm.redis.Exists(ctx, key)
		if err != nil {
			if cm.config.GracefulDegradation {
				log.Printf("[CacheManager:%s] Redis exists check failed, assuming not exists: %v", cm.config.Name, err)
				return false, nil
			}
			return false, err
		}

		return exists, nil
	}

	return false, nil
}

// GetOrSet retrieves a value from cache, or sets it using the provided function
// This is the most common pattern: check cache, if miss, fetch from source and cache
func (cm *CacheManager) GetOrSet(ctx context.Context, key string, fetchFunc func() (string, error)) (string, error) {
	// Try to get from cache
	value, source, err := cm.Get(ctx, key)
	if err == nil {
		log.Printf("[CacheManager:%s] Cache hit for key '%s' from %s", cm.config.Name, key, source)
		return value, nil
	}

	// Only fetch if it's a cache miss
	if !errors.Is(err, ErrCacheMiss) {
		return "", fmt.Errorf("cache error: %w", err)
	}

	// Cache miss - fetch from source
	log.Printf("[CacheManager:%s] Cache miss for key '%s', fetching from source", cm.config.Name, key)
	value, err = fetchFunc()
	if err != nil {
		return "", fmt.Errorf("fetch function failed: %w", err)
	}

	// Store in cache for next time
	if setErr := cm.Set(ctx, key, value); setErr != nil {
		log.Printf("[CacheManager:%s] Failed to cache fetched value: %v", cm.config.Name, setErr)
		// Don't fail the request, we have the value
	}

	return value, nil
}

// InvalidatePattern invalidates all keys matching a pattern (Redis only)
// Pattern examples: "user:*", "session:*", "email:*"
func (cm *CacheManager) InvalidatePattern(ctx context.Context, pattern string) error {
	if !cm.config.EnableRedisCache || cm.redis == nil {
		return fmt.Errorf("redis cache is not enabled")
	}

	// This requires scanning keys - use carefully in production
	// For high-scale, consider using Redis keyspace notifications instead
	log.Printf("[CacheManager:%s] Warning: InvalidatePattern is expensive, pattern: %s", cm.config.Name, pattern)

	// Note: You'll need to implement key scanning in RedisClient
	// For now, return not implemented
	return fmt.Errorf("pattern invalidation not implemented - use specific key deletion")
}

// GetMetrics returns combined metrics from all cache tiers
func (cm *CacheManager) GetMetrics() map[string]interface{} {
	metrics := make(map[string]interface{})

	if cm.config.EnableLocalCache && cm.local != nil {
		metrics["local"] = cm.local.GetMetrics()
		metrics["local_hit_rate"] = cm.local.GetHitRate()
	}

	if cm.config.EnableRedisCache && cm.redis != nil {
		metrics["redis"] = cm.redis.GetMetrics()
		metrics["redis_hit_rate"] = cm.redis.GetHitRate()
	}

	return metrics
}

// SetJSON stores any object as JSON in cache
func (cm *CacheManager) SetJSON(ctx context.Context, key string, value interface{}) error {
	// Marshal to JSON
	jsonData, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal to JSON: %w", err)
	}

	// Store as string
	return cm.Set(ctx, key, string(jsonData))
}

// GetJSON retrieves and unmarshals a JSON object from cache
// Returns the value, source, and error
func (cm *CacheManager) GetJSON(ctx context.Context, key string, dest interface{}) (string, error) {
	// Get from cache
	jsonString, source, err := cm.Get(ctx, key)
	if err != nil {
		return source, err
	}

	// Unmarshal JSON
	if err := json.Unmarshal([]byte(jsonString), dest); err != nil {
		return source, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return source, nil
}

// GetOrSetJSON retrieves from cache or fetches and stores as JSON
func (cm *CacheManager) GetOrSetJSON(ctx context.Context, key string, dest interface{}, fetchFunc func() (interface{}, error)) (string, error) {
	// Try to get from cache
	source, err := cm.GetJSON(ctx, key, dest)
	if err == nil {
		log.Printf("[CacheManager:%s] JSON cache hit for key '%s' from %s", cm.config.Name, key, source)
		return source, nil
	}

	// Only fetch if it's a cache miss
	if !errors.Is(err, ErrCacheMiss) {
		return "", fmt.Errorf("cache error: %w", err)
	}

	// Cache miss - fetch from source
	log.Printf("[CacheManager:%s] JSON cache miss for key '%s', fetching from source", cm.config.Name, key)
	value, err := fetchFunc()
	if err != nil {
		return "", fmt.Errorf("fetch function failed: %w", err)
	}

	// Store in cache as JSON
	if setErr := cm.SetJSON(ctx, key, value); setErr != nil {
		log.Printf("[CacheManager:%s] Failed to cache JSON: %v", cm.config.Name, setErr)
		// Don't fail the request
	}

	// Also populate the destination
	jsonData, _ := json.Marshal(value)
	json.Unmarshal(jsonData, dest)

	return "database", nil
}

// HealthCheck verifies cache system health
func (cm *CacheManager) HealthCheck(ctx context.Context) map[string]string {
	health := make(map[string]string)

	// Check local cache
	if cm.config.EnableLocalCache && cm.local != nil {
		health["local"] = "healthy"
		health["local_entries"] = fmt.Sprintf("%d", cm.local.Len())
	} else {
		health["local"] = "disabled"
	}

	// Check Redis
	if cm.config.EnableRedisCache && cm.redis != nil {
		if err := cm.redis.HealthCheck(ctx); err != nil {
			health["redis"] = fmt.Sprintf("unhealthy: %v", err)
		} else {
			health["redis"] = "healthy"
		}
	} else {
		health["redis"] = "disabled"
	}

	return health
}

// Close gracefully shuts down the cache manager
func (cm *CacheManager) Close() error {
	log.Printf("[CacheManager:%s] Shutting down...", cm.config.Name)

	var localErr, redisErr error

	if cm.local != nil {
		localErr = cm.local.Close()
	}

	if cm.redis != nil {
		redisErr = cm.redis.Close()
	}

	if localErr != nil || redisErr != nil {
		return fmt.Errorf("close errors - local: %v, redis: %v", localErr, redisErr)
	}

	log.Printf("[CacheManager:%s] Shutdown complete", cm.config.Name)
	return nil
}

// --- Helper Functions for Common Patterns ---

// CacheEmailExists checks if an email exists using atomic SetNX (Redis only)
// Returns true if email was successfully reserved, false if already exists
func (cm *CacheManager) CacheEmailExists(ctx context.Context, email string, userID string, ttl time.Duration) (bool, error) {
	key := "email:" + email

	// Check local cache first (fast path)
	if cm.config.EnableLocalCache && cm.local != nil {
		if cm.local.Exists(key) {
			return false, nil // Email exists
		}
	}

	// Use Redis SetNX for atomic check-and-set
	if cm.config.EnableRedisCache && cm.redis != nil {
		reserved, err := cm.redis.SetNX(ctx, key, userID, ttl)
		if err != nil {
			if cm.config.GracefulDegradation {
				log.Printf("[CacheManager:%s] Redis SetNX failed, skipping cache: %v", cm.config.Name, err)
				return true, nil // Assume we can proceed
			}
			return false, err
		}

		// Update local cache if reserved
		if reserved && cm.config.EnableLocalCache && cm.local != nil {
			cm.local.SetString(key, userID)
		}

		return reserved, nil
	}

	// Cache disabled
	return true, nil
}

// GetWithStats returns value and detailed stats about cache performance
func (cm *CacheManager) GetWithStats(ctx context.Context, key string) (value string, stats CacheStats, err error) {
	start := time.Now()

	value, source, err := cm.Get(ctx, key)

	stats = CacheStats{
		Key:      key,
		Source:   source,
		Latency:  time.Since(start),
		HitLocal: source == "local",
		HitRedis: source == "redis",
		Miss:     source == "miss",
	}

	return value, stats, err
}

// CacheStats provides detailed cache operation statistics
type CacheStats struct {
	Key      string
	Source   string
	Latency  time.Duration
	HitLocal bool
	HitRedis bool
	Miss     bool
}
