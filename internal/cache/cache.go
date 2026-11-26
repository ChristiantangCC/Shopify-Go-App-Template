// Package cache is a read-through cache on Redis for data that is read far
// more often than it changes, like a shop's settings. Redis is never the
// source of truth: every value can be rebuilt from Postgres, so a Redis
// outage degrades to "every read hits the database", never to an error.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cache wraps a Redis client. A nil *Cache is valid and caches nothing,
// which keeps tests and tools that have no Redis simple.
type Cache struct {
	rdb    *redis.Client
	logger *slog.Logger
}

// Connect parses a redis:// (or rediss://) URL and verifies Redis answers.
func Connect(ctx context.Context, redisURL string, logger *slog.Logger) (*Cache, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("cache: %w", err)
	}
	rdb := redis.NewClient(opts)
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("cache: %w", err)
	}
	return &Cache{rdb: rdb, logger: logger}, nil
}

// Close releases the connection pool.
func (c *Cache) Close() error {
	if c == nil {
		return nil
	}
	return c.rdb.Close()
}

// Client exposes the Redis client for libraries that take one (the App
// Proxy's shared rate-limit counter). It is nil when caching is off.
func (c *Cache) Client() *redis.Client {
	if c == nil {
		return nil
	}
	return c.rdb
}

// Ping reports whether Redis answers, for the health check.
func (c *Cache) Ping(ctx context.Context) error {
	if c == nil {
		return nil
	}
	return c.rdb.Ping(ctx).Err()
}

// Delete drops keys, for invalidating after a write. It never fails the
// write it follows: a Redis error is logged and the entry expires on its
// TTL instead.
func (c *Cache) Delete(ctx context.Context, keys ...string) {
	if c == nil || len(keys) == 0 {
		return
	}
	if err := c.rdb.Del(ctx, keys...).Err(); err != nil {
		c.logger.Warn("[Cache] Delete failed", "keys", keys, "error", err)
	}
}

// GetOrLoad returns the cached value for key, or calls load, caches its
// result for ttl and returns it. Values are stored as JSON, so T must
// round-trip through encoding/json. Redis errors are logged and fall back to
// load; only load's own error is returned.
func GetOrLoad[T any](ctx context.Context, c *Cache, key string, ttl time.Duration, load func(ctx context.Context) (T, error)) (T, error) {
	if c == nil {
		return load(ctx)
	}

	raw, err := c.rdb.Get(ctx, key).Bytes()
	switch {
	case err == nil:
		var value T
		decodeErr := json.Unmarshal(raw, &value)
		if decodeErr == nil {
			return value, nil
		}
		c.logger.Warn("[Cache] Ignoring undecodable entry", "key", key, "error", decodeErr)
	case !errors.Is(err, redis.Nil):
		c.logger.Warn("[Cache] Read failed", "key", key, "error", err)
	}

	value, err := load(ctx)
	if err != nil {
		return value, err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		c.logger.Warn("[Cache] Encode failed", "key", key, "error", err)
		return value, nil
	}
	if err := c.rdb.Set(ctx, key, encoded, ttl).Err(); err != nil {
		c.logger.Warn("[Cache] Write failed", "key", key, "error", err)
	}
	return value, nil
}
