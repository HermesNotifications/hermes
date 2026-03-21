package auth

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// RedisCacheClient is the subset of cache.Client needed for the Redis tier.
type RedisCacheClient interface {
	GetJWTSigningKeys(ctx context.Context) ([]byte, error)
	SetJWTSigningKeys(ctx context.Context, data []byte, ttl time.Duration) error
}

// CachedKeys provides a two-tier cache (in-process + optional Redis) for JWT signing keys.
type CachedKeys struct {
	mu        sync.RWMutex
	cached    []JWTSigningConfig
	expiresAt time.Time
	upstream  JWTKeyProvider
	redis     RedisCacheClient // optional, nil = skip Redis tier
	ttl       time.Duration
	redisTTL  time.Duration
}

// NewCachedKeyProvider creates a two-tier cached key provider.
// redis can be nil, in which case in-process misses fall through directly to upstream.
func NewCachedKeyProvider(upstream JWTKeyProvider, ttl time.Duration, redis RedisCacheClient) *CachedKeys {
	return &CachedKeys{
		upstream: upstream,
		redis:    redis,
		ttl:      ttl,
		redisTTL: 10 * time.Minute,
	}
}

// Provider returns a JWTKeyProvider function for use in middleware.
func (c *CachedKeys) Provider() JWTKeyProvider {
	return func() []JWTSigningConfig {
		return c.get()
	}
}

func (c *CachedKeys) get() []JWTSigningConfig {
	c.mu.RLock()
	if time.Now().Before(c.expiresAt) && c.cached != nil {
		defer c.mu.RUnlock()
		return c.cached
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock.
	if time.Now().Before(c.expiresAt) && c.cached != nil {
		return c.cached
	}

	// Try Redis tier.
	if c.redis != nil {
		if keys := c.fromRedis(); keys != nil {
			c.cached = keys
			c.expiresAt = time.Now().Add(c.ttl)
			return c.cached
		}
	}

	// Fall through to upstream (DB).
	fresh := c.upstream()
	if fresh != nil {
		c.cached = fresh
		c.expiresAt = time.Now().Add(c.ttl)
		c.populateRedis(fresh)
	}
	return c.cached
}

func (c *CachedKeys) fromRedis() []JWTSigningConfig {
	data, err := c.redis.GetJWTSigningKeys(context.Background())
	if err != nil || data == nil {
		return nil
	}
	var keys []JWTSigningConfig
	if err := json.Unmarshal(data, &keys); err != nil {
		return nil
	}
	return keys
}

func (c *CachedKeys) populateRedis(keys []JWTSigningConfig) {
	if c.redis == nil {
		return
	}
	data, err := json.Marshal(keys)
	if err != nil {
		return
	}
	c.redis.SetJWTSigningKeys(context.Background(), data, c.redisTTL)
}
