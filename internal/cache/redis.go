package cache

import (
	"context"
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
	set, err := c.rdb.SetNX(ctx, "idem:"+key, notificationID, ttl).Result()
	if err != nil {
		return "", fmt.Errorf("setnx: %w", err)
	}
	if set {
		return "", nil // new key
	}
	// Key already existed — return stored value
	existing, err := c.rdb.Get(ctx, "idem:"+key).Result()
	if err != nil {
		return "", fmt.Errorf("get existing: %w", err)
	}
	return existing, nil
}

func (c *Client) GetTypeConfig(ctx context.Context, slug string) ([]byte, error) {
	val, err := c.rdb.Get(ctx, "type:"+slug).Bytes()
	if err == redis.Nil {
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

func (c *Client) Close() {
	c.rdb.Close()
}
