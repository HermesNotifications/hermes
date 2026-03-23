package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Client struct {
	rdb *redis.Client
}

func Connect(redisURL string) (*Client, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis URL: %w", err)
	}
	rdb := redis.NewClient(opts)
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return &Client{rdb: rdb}, nil
}

// SetIdempotencyKey attempts to set an idempotency key. Returns "" if the key was new,
// or the existing notification_id if the key already existed.
func (c *Client) SetIdempotencyKey(ctx context.Context, key, notificationID string, ttl time.Duration) (string, error) {
	err := c.rdb.SetArgs(ctx, "idem:"+key, notificationID, redis.SetArgs{
		Mode: "NX",
		TTL:  ttl,
	}).Err()
	if err == nil {
		return "", nil // new key — NX succeeded
	}
	if !errors.Is(err, redis.Nil) {
		return "", fmt.Errorf("set nx: %w", err)
	}
	// NX failed (key exists) — return stored value
	existing, err := c.rdb.Get(ctx, "idem:"+key).Result()
	if err != nil {
		return "", fmt.Errorf("get existing: %w", err)
	}
	return existing, nil
}

func (c *Client) GetTypeConfig(ctx context.Context, slug string) ([]byte, error) {
	val, err := c.rdb.Get(ctx, "type:"+slug).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get type config: %w", err)
	}
	return val, nil
}

func (c *Client) SetTypeConfig(ctx context.Context, slug string, data []byte, ttl time.Duration) error {
	return c.rdb.Set(ctx, "type:"+slug, data, ttl).Err()
}

func (c *Client) InvalidateTypeConfig(ctx context.Context, slug string) error {
	return c.rdb.Del(ctx, "type:"+slug).Err()
}

func (c *Client) GetJWTSigningKeys(ctx context.Context) ([]byte, error) {
	val, err := c.rdb.Get(ctx, "jwt:signing_keys").Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get jwt signing keys: %w", err)
	}
	return val, nil
}

func (c *Client) SetJWTSigningKeys(ctx context.Context, data []byte, ttl time.Duration) error {
	return c.rdb.Set(ctx, "jwt:signing_keys", data, ttl).Err()
}

func (c *Client) InvalidateJWTSigningKeys(ctx context.Context) error {
	return c.rdb.Del(ctx, "jwt:signing_keys").Err()
}

// GetUnreadCount returns the cached unread count for a user.
// Returns (count, true, nil) on hit, (0, false, nil) on miss.
func (c *Client) GetUnreadCount(ctx context.Context, userID string) (int, bool, error) {
	val, err := c.rdb.Get(ctx, "unread:"+userID).Int()
	if errors.Is(err, redis.Nil) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("get unread count: %w", err)
	}
	return val, true, nil
}

// SetUnreadCount sets the cached unread count for a user.
func (c *Client) SetUnreadCount(ctx context.Context, userID string, count int, ttl time.Duration) error {
	return c.rdb.Set(ctx, "unread:"+userID, count, ttl).Err()
}

// IncrUnreadCount atomically increments the unread count and returns the new value.
// If the key does not exist, Redis INCR creates it at 0 then increments to 1.
func (c *Client) IncrUnreadCount(ctx context.Context, userID string) (int64, error) {
	return c.rdb.Incr(ctx, "unread:"+userID).Result()
}

// decrIfPositive is a Lua script that decrements a key only if it exists and is > 0.
// Returns the new value, or -1 if the key does not exist.
var decrIfPositive = redis.NewScript(`
local val = redis.call('GET', KEYS[1])
if val == false then
	return -1
end
val = tonumber(val)
if val > 0 then
	return redis.call('DECR', KEYS[1])
end
return 0
`)

// DecrUnreadCount atomically decrements the unread count (floor at 0).
// Returns the new value. Returns -1 if the key does not exist (cache miss).
func (c *Client) DecrUnreadCount(ctx context.Context, userID string) (int64, error) {
	result, err := decrIfPositive.Run(ctx, c.rdb, []string{"unread:" + userID}).Int64()
	if err != nil {
		return 0, fmt.Errorf("decr unread count: %w", err)
	}
	return result, nil
}

// DeleteUnreadCount removes the cached unread count for a user.
func (c *Client) DeleteUnreadCount(ctx context.Context, userID string) error {
	return c.rdb.Del(ctx, "unread:"+userID).Err()
}

// GetAPIKey returns the cached API key data (JSON bytes) for the given key ID.
// Returns (data, nil) on hit, (nil, nil) on miss.
func (c *Client) GetAPIKey(ctx context.Context, keyID string) ([]byte, error) {
	val, err := c.rdb.Get(ctx, "apikey:"+keyID).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get api key: %w", err)
	}
	return val, nil
}

// SetAPIKey caches API key data (JSON bytes) for the given key ID.
func (c *Client) SetAPIKey(ctx context.Context, keyID string, data []byte, ttl time.Duration) error {
	return c.rdb.Set(ctx, "apikey:"+keyID, data, ttl).Err()
}

// InvalidateAPIKey removes the cached API key for the given key ID.
func (c *Client) InvalidateAPIKey(ctx context.Context, keyID string) error {
	return c.rdb.Del(ctx, "apikey:"+keyID).Err()
}

func (c *Client) Close() {
	err := c.rdb.Close()
	if err != nil {
		return
	}
}
