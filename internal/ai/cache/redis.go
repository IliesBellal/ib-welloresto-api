package cache

import (
	"context"
	"time"

	"welloresto-api/internal/infrastructure/redis"
	"welloresto-api/internal/logger"

	"go.uber.org/zap"
)

const keyPrefix = "ai:"

// Cache is a Redis-backed cache layer specialised for LLM responses.
// All keys are automatically namespaced with the "ai:" prefix to avoid
// collisions with other application cache entries.
type Cache struct {
	redis *redis.Client
}

// New creates a Cache backed by the provided Redis client.
// Passing a nil redis client is safe — all operations become no-ops.
func New(r *redis.Client) *Cache {
	return &Cache{redis: r}
}

// Get retrieves a cached LLM response by key.
// Returns (value, true, nil) on hit, ("", false, nil) on miss.
// Errors from the underlying Redis client are absorbed (consistent with the
// project-wide Redis policy) and always return nil here.
func (c *Cache) Get(ctx context.Context, key string) (string, bool, error) {
	if c == nil || c.redis == nil {
		return "", false, nil
	}

	val, found := c.redis.Get(ctx, keyPrefix+key)

	log := logger.FromContext(ctx)
	if found {
		log.Info("ai cache hit",
			zap.String("cache_layer", "redis"),
			zap.String("key", key),
		)
	} else {
		log.Info("ai cache miss",
			zap.String("cache_layer", "redis"),
			zap.String("key", key),
		)
	}

	return val, found, nil
}

// Set stores an LLM response in the cache with the given TTL.
// Errors from Redis are absorbed — a cache write failure must never block a flow.
func (c *Cache) Set(ctx context.Context, key string, value string, ttl time.Duration) error {
	if c == nil || c.redis == nil {
		return nil
	}

	c.redis.Set(ctx, keyPrefix+key, value, ttl)
	return nil
}

// Delete removes a cached entry.
func (c *Cache) Delete(ctx context.Context, key string) error {
	if c == nil || c.redis == nil {
		return nil
	}

	c.redis.Delete(ctx, keyPrefix+key)
	return nil
}

// IsAvailable reports whether the cache has a usable Redis client.
// This is a local in-memory check and does not perform network calls.
func (c *Cache) IsAvailable() bool {
	return c != nil && c.redis != nil
}

// DeleteByPattern removes all cached entries whose key matches the given glob pattern.
// The "ai:" prefix is applied automatically, consistent with Get/Set/Delete.
// Returns the number of keys deleted.
// Uses SCAN + DEL in batches of 100 — never uses KEYS to avoid blocking Redis.
func (c *Cache) DeleteByPattern(ctx context.Context, pattern string) (int, error) {
	if c == nil || c.redis == nil {
		return 0, nil
	}
	return c.redis.ScanDeleteByPattern(ctx, keyPrefix+pattern)
}
