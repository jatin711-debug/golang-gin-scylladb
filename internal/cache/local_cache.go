package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/allegro/bigcache/v3"
)

// LocalCache provides an in-memory cache with zero GC overhead
// Uses BigCache - optimized for high-throughput, low-latency scenarios
type LocalCache struct {
	cache   *bigcache.BigCache
	metrics *LocalCacheMetrics
	name    string
}

// LocalCacheMetrics tracks local cache performance
type LocalCacheMetrics struct {
	Hits   atomic.Int64
	Misses atomic.Int64
	Sets   atomic.Int64
	Errors atomic.Int64
}

// LocalCacheConfig holds configuration for local cache
type LocalCacheConfig struct {
	// Shards is number of cache shards (must be power of 2)
	// More shards = less lock contention, typical: 1024 or 256
	Shards int

	// LifeWindow is how long items stay in cache
	LifeWindow time.Duration

	// CleanWindow is interval for cleaning expired items
	CleanWindow time.Duration

	// MaxEntriesInWindow is max number of entries expected in LifeWindow
	// Used to determine initial allocation
	MaxEntriesInWindow int

	// MaxEntrySize is max size in bytes for single entry
	MaxEntrySize int

	// HardMaxCacheSize is max cache size in MB (0 = no limit)
	HardMaxCacheSize int

	// Verbose enables logging
	Verbose bool

	// Name for identification
	Name string
}

// DefaultLocalCacheConfig returns sensible production defaults
// Optimized for ~10K items with 1 minute TTL
func DefaultLocalCacheConfig() *LocalCacheConfig {
	return &LocalCacheConfig{
		Shards:             1024,            // Good parallelism
		LifeWindow:         1 * time.Minute, // 1 min TTL
		CleanWindow:        5 * time.Minute, // Clean every 5 min
		MaxEntriesInWindow: 10000 * 60,      // 10K entries/sec * 60 sec
		MaxEntrySize:       500,             // 500 bytes per entry
		HardMaxCacheSize:   0,               // No hard limit
		Verbose:            false,
		Name:               "default",
	}
}

// NewLocalCache creates a production-ready local cache with zero GC overhead
func NewLocalCache(config *LocalCacheConfig) (*LocalCache, error) {
	if config == nil {
		config = DefaultLocalCacheConfig()
	}

	// Build BigCache config
	bigCacheConfig := bigcache.Config{
		Shards:             config.Shards,
		LifeWindow:         config.LifeWindow,
		CleanWindow:        config.CleanWindow,
		MaxEntriesInWindow: config.MaxEntriesInWindow,
		MaxEntrySize:       config.MaxEntrySize,
		HardMaxCacheSize:   config.HardMaxCacheSize,
		Verbose:            config.Verbose,

		// OnRemove callback for tracking evictions
		OnRemove: func(key string, entry []byte) {
			// Could track evictions here if needed
		},

		// OnRemoveWithReason for detailed eviction tracking
		OnRemoveWithReason: func(key string, entry []byte, reason bigcache.RemoveReason) {
			// Expired, NoSpace, Deleted
			if config.Verbose {
				log.Printf("[LocalCache:%s] Key '%s' removed: %v", config.Name, key, reason)
			}
		},
	}

	cache, err := bigcache.New(context.Background(), bigCacheConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create local cache: %w", err)
	}

	log.Printf("[LocalCache:%s] Initialized - Shards: %d, LifeWindow: %v, MaxEntries: %d",
		config.Name, config.Shards, config.LifeWindow, config.MaxEntriesInWindow)

	return &LocalCache{
		cache:   cache,
		metrics: &LocalCacheMetrics{},
		name:    config.Name,
	}, nil
}

// Set stores a byte slice value
func (l *LocalCache) Set(key string, value []byte) error {
	l.metrics.Sets.Add(1)

	err := l.cache.Set(key, value)
	if err != nil {
		l.metrics.Errors.Add(1)
		return fmt.Errorf("cache set failed: %w", err)
	}

	return nil
}

// SetString stores a string value (converts to []byte internally)
func (l *LocalCache) SetString(key string, value string) error {
	return l.Set(key, []byte(value))
}

// SetJSON stores any value as JSON
func (l *LocalCache) SetJSON(key string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		l.metrics.Errors.Add(1)
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	return l.Set(key, data)
}

// Get retrieves a value from cache as []byte
func (l *LocalCache) Get(key string) ([]byte, error) {
	value, err := l.cache.Get(key)
	if err != nil {
		if errors.Is(err, bigcache.ErrEntryNotFound) {
			l.metrics.Misses.Add(1)
			return nil, ErrCacheMiss
		}
		l.metrics.Errors.Add(1)
		return nil, fmt.Errorf("cache get failed: %w", err)
	}

	l.metrics.Hits.Add(1)
	return value, nil
}

// GetString retrieves a string value
func (l *LocalCache) GetString(key string) (string, error) {
	value, err := l.Get(key)
	if err != nil {
		return "", err
	}
	return string(value), nil
}

// GetJSON retrieves and unmarshals a JSON value
func (l *LocalCache) GetJSON(key string, dest interface{}) error {
	value, err := l.Get(key)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(value, dest); err != nil {
		l.metrics.Errors.Add(1)
		return fmt.Errorf("failed to unmarshal value: %w", err)
	}

	return nil
}

// Exists checks if a key exists in cache
func (l *LocalCache) Exists(key string) bool {
	_, err := l.cache.Get(key)
	if err != nil {
		l.metrics.Misses.Add(1)
		return false
	}
	l.metrics.Hits.Add(1)
	return true
}

// Delete removes a key from cache
func (l *LocalCache) Delete(key string) error {
	err := l.cache.Delete(key)
	if err != nil && !errors.Is(err, bigcache.ErrEntryNotFound) {
		l.metrics.Errors.Add(1)
		return fmt.Errorf("cache delete failed: %w", err)
	}
	return nil
}

// Reset removes all items from cache
func (l *LocalCache) Reset() error {
	err := l.cache.Reset()
	if err != nil {
		l.metrics.Errors.Add(1)
		return fmt.Errorf("cache reset failed: %w", err)
	}
	log.Printf("[LocalCache:%s] Cache reset", l.name)
	return nil
}

// Len returns the number of items in cache
func (l *LocalCache) Len() int {
	return l.cache.Len()
}

// Capacity returns cache capacity in bytes
func (l *LocalCache) Capacity() int {
	return l.cache.Capacity()
}

// GetMetrics returns current cache performance metrics
func (l *LocalCache) GetMetrics() map[string]int64 {
	// Get BigCache's internal stats
	stats := l.cache.Stats()

	return map[string]int64{
		"hits":       l.metrics.Hits.Load(),
		"misses":     l.metrics.Misses.Load(),
		"sets":       l.metrics.Sets.Load(),
		"errors":     l.metrics.Errors.Load(),
		"entries":    int64(l.cache.Len()),
		"capacity":   int64(l.cache.Capacity()),
		"collisions": int64(stats.Collisions),
		"del_hits":   int64(stats.DelHits),
		"del_misses": int64(stats.DelMisses),
	}
}

// GetHitRate calculates cache hit rate as percentage
func (l *LocalCache) GetHitRate() float64 {
	hits := l.metrics.Hits.Load()
	misses := l.metrics.Misses.Load()
	total := hits + misses

	if total == 0 {
		return 0.0
	}

	return float64(hits) / float64(total) * 100.0
}

// GetStats returns BigCache internal statistics
func (l *LocalCache) GetStats() bigcache.Stats {
	return l.cache.Stats()
}

// Close gracefully closes the cache with final stats
func (l *LocalCache) Close() error {
	metrics := l.GetMetrics()

	log.Printf("[LocalCache:%s] Closing. Stats - Hits: %d, Misses: %d, Entries: %d, Hit Rate: %.2f%%",
		l.name, metrics["hits"], metrics["misses"], metrics["entries"], l.GetHitRate())

	return l.cache.Close()
}

// --- Multi-Tier Cache Helper ---

// MultiTierCache combines local and Redis caching
type MultiTierCache struct {
	local *LocalCache
	redis *RedisClient
	name  string
}

// NewMultiTierCache creates a cache with L1 (local) and L2 (Redis) tiers
func NewMultiTierCache(local *LocalCache, redis *RedisClient, name string) *MultiTierCache {
	log.Printf("[MultiTierCache:%s] Initialized with local + Redis tiers", name)
	return &MultiTierCache{
		local: local,
		redis: redis,
		name:  name,
	}
}

// Get checks L1 (local) first, then L2 (Redis), returns (value, source, error)
// source will be "local", "redis", or "miss"
func (m *MultiTierCache) Get(ctx context.Context, key string) (string, string, error) {
	// L1: Check local cache (0.001ms - fastest!)
	if value, err := m.local.GetString(key); err == nil {
		return value, "local", nil
	}

	// L2: Check Redis (0.5-2ms)
	value, err := m.redis.Get(ctx, key)
	if err == nil {
		// Found in Redis, populate local cache for next time (write-back)
		m.local.SetString(key, value)
		return value, "redis", nil
	}

	// Check if it's a cache miss or error
	if errors.Is(err, ErrCacheMiss) {
		return "", "miss", ErrCacheMiss
	}

	// Redis error
	return "", "error", err
}

// Set writes to both L1 and L2 (write-through pattern)
func (m *MultiTierCache) Set(ctx context.Context, key string, value string, redisTTL time.Duration) error {
	// Write to local cache (uses LifeWindow from config)
	m.local.SetString(key, value)

	// Write to Redis with custom TTL
	return m.redis.Set(ctx, key, value, redisTTL)
}

// Delete removes from both tiers
func (m *MultiTierCache) Delete(ctx context.Context, key string) error {
	// Remove from local
	if err := m.local.Delete(key); err != nil {
		log.Printf("[MultiTierCache:%s] Failed to delete from local cache: %v", m.name, err)
	}

	// Remove from Redis
	return m.redis.Delete(ctx, key)
}

// GetMetrics returns combined metrics from both tiers
func (m *MultiTierCache) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"local":          m.local.GetMetrics(),
		"redis":          m.redis.GetMetrics(),
		"local_hit_rate": m.local.GetHitRate(),
		"redis_hit_rate": m.redis.GetHitRate(),
	}
}
