// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
)

type Client struct {
	rdb *redis.Client
	// limiter is stateless over rdb; it is held here so the GCRA Lua script is
	// registered once rather than per call.
	limiter *redis_rate.Limiter
}

// Options bound the Redis client. The zero value uses Defaults.
type Options struct {
	PoolSize int
	// Timeout applies to reads and writes alike.
	Timeout time.Duration
}

// Defaults are applied when Connect is given the zero Options.
var Defaults = Options{PoolSize: 16, Timeout: 500 * time.Millisecond}

// Connect dials Redis with the default bounds.
func Connect(redisURL string) (*Client, error) {
	return ConnectWithOptions(redisURL, Options{})
}

// ConnectWithOptions dials Redis with explicit bounds.
//
// The timeouts matter more than the pool size. go-redis defaults to a 3-second read timeout,
// and every inbox read consults the unread-count cache — so a Redis hiccup would block each
// request for three seconds before falling back to Postgres, piling up in-flight requests until
// the HTTP tier fell over because of a dependency it can serve correctly without. Failing fast
// to the database is the entire value of having a fallback.
func ConnectWithOptions(redisURL string, o Options) (*Client, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis URL: %w", err)
	}

	if o.PoolSize <= 0 {
		o.PoolSize = Defaults.PoolSize
	}
	if o.Timeout <= 0 {
		o.Timeout = Defaults.Timeout
	}
	opts.PoolSize = o.PoolSize
	opts.MinIdleConns = 2
	opts.ConnMaxIdleTime = 5 * time.Minute
	opts.ConnMaxLifetime = 30 * time.Minute
	// Dialing is allowed longer than a command: establishing a connection includes a TCP
	// handshake and, in production, a TLS one.
	opts.DialTimeout = 2 * time.Second
	opts.ReadTimeout = o.Timeout
	opts.WriteTimeout = o.Timeout
	// Waiting for a free connection is itself bounded, or pool exhaustion becomes an unbounded
	// queue in front of an already-struggling dependency.
	opts.PoolTimeout = 2 * o.Timeout
	opts.MaxRetries = 2

	rdb := redis.NewClient(opts)
	if err := redisotel.InstrumentTracing(rdb); err != nil {
		return nil, fmt.Errorf("redis tracing instrument: %w", err)
	}
	if err := redisotel.InstrumentMetrics(rdb); err != nil {
		return nil, fmt.Errorf("redis metrics instrument: %w", err)
	}
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return &Client{rdb: rdb, limiter: redis_rate.NewLimiter(rdb)}, nil
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

func (c *Client) GetTemplateConfig(ctx context.Context, slug string) ([]byte, error) {
	val, err := c.rdb.Get(ctx, "template:"+slug).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get template config: %w", err)
	}
	return val, nil
}

func (c *Client) SetTemplateConfig(ctx context.Context, slug string, data []byte, ttl time.Duration) error {
	return c.rdb.Set(ctx, "template:"+slug, data, ttl).Err()
}

func (c *Client) InvalidateTemplateConfig(ctx context.Context, slug string) error {
	return c.rdb.Del(ctx, "template:"+slug).Err()
}

func (c *Client) GetSubscription(ctx context.Context, id string) ([]byte, error) {
	val, err := c.rdb.Get(ctx, "subscription:"+id).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get subscription: %w", err)
	}
	return val, nil
}

func (c *Client) SetSubscription(ctx context.Context, id string, data []byte, ttl time.Duration) error {
	return c.rdb.Set(ctx, "subscription:"+id, data, ttl).Err()
}

func (c *Client) InvalidateSubscription(ctx context.Context, id string) error {
	return c.rdb.Del(ctx, "subscription:"+id).Err()
}

func (c *Client) GetCategory(ctx context.Context, id string) ([]byte, error) {
	val, err := c.rdb.Get(ctx, "category:"+id).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get category: %w", err)
	}
	return val, nil
}

func (c *Client) SetCategory(ctx context.Context, id string, data []byte, ttl time.Duration) error {
	return c.rdb.Set(ctx, "category:"+id, data, ttl).Err()
}

func (c *Client) InvalidateCategory(ctx context.Context, id string) error {
	return c.rdb.Del(ctx, "category:"+id).Err()
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

// UnreadCountMiss is returned by IncrUnreadCount and DecrUnreadCount when there is no cached
// count to adjust. It is deliberately distinct from a real count: the caller must recompute
// from the store rather than treat the absence as zero.
const UnreadCountMiss = -1

func unreadKey(userID string) string { return "unread:" + userID }

// GetUnreadCount returns the cached unread count for a user.
// Returns (count, true, nil) on hit, (0, false, nil) on miss.
func (c *Client) GetUnreadCount(ctx context.Context, userID string) (int, bool, error) {
	val, err := c.rdb.Get(ctx, unreadKey(userID)).Int()
	if errors.Is(err, redis.Nil) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("get unread count: %w", err)
	}
	return val, true, nil
}

// GetUnreadCountWithTTL returns the cached count along with how long it has left to live, so a
// caller can decide whether the value is fresh enough or worth recomputing ahead of expiry.
// Both reads travel in one pipeline, so this costs one round trip rather than two.
//
// A key with no expiry reports a negative TTL, which is reported here as zero remaining --
// treat it as due for refresh. Such a key should no longer be possible (see incrIfPresent), but
// one written before that fix would otherwise never be revisited.
func (c *Client) GetUnreadCountWithTTL(ctx context.Context, userID string) (int, time.Duration, bool, error) {
	key := unreadKey(userID)
	pipe := c.rdb.Pipeline()
	get := pipe.Get(ctx, key)
	pttl := pipe.PTTL(ctx, key)
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return 0, 0, false, fmt.Errorf("get unread count with ttl: %w", err)
	}

	count, err := get.Int()
	if errors.Is(err, redis.Nil) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, fmt.Errorf("get unread count: %w", err)
	}

	ttl := pttl.Val()
	if ttl < 0 {
		ttl = 0
	}
	return count, ttl, true, nil
}

// SetUnreadCount sets the cached unread count for a user, overwriting any existing value.
func (c *Client) SetUnreadCount(ctx context.Context, userID string, count int, ttl time.Duration) error {
	return c.rdb.Set(ctx, unreadKey(userID), count, ttl).Err()
}

// FillUnreadCount seeds the count only when no value is cached, reporting whether it won.
//
// SET NX rather than SET because a filler always races: it read the store some milliseconds
// ago, and in that window an increment or a fresher fill may have landed. The only writer it
// could beat is another filler holding the same answer, so refusing to overwrite costs nothing
// and removes the chance of replacing a newer value with an older one.
func (c *Client) FillUnreadCount(ctx context.Context, userID string, count int, ttl time.Duration) (bool, error) {
	ok, err := c.rdb.SetNX(ctx, unreadKey(userID), count, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("fill unread count: %w", err)
	}
	return ok, nil
}

// TryUnreadRefreshLease reports whether this caller should perform a refresh-ahead recount.
//
// Without it, every concurrent request for a user whose entry has aged past the refresh
// threshold fires its own store count -- turning one expiry into a small thundering herd
// against the very query the cache exists to avoid. The losers serve the slightly stale cached
// value, which is the entire point of refreshing ahead of expiry rather than at it.
func (c *Client) TryUnreadRefreshLease(ctx context.Context, userID string, ttl time.Duration) (bool, error) {
	ok, err := c.rdb.SetNX(ctx, "unread:refresh:"+userID, 1, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("try unread refresh lease: %w", err)
	}
	return ok, nil
}

// MarkUnreadCounted reports whether this notification has not yet been counted, claiming it if
// so. Delivery is at-least-once, so without this guard a Centrifugo publish failure -- which
// nacks the message and has it redelivered -- increments the user's count a second time, and
// the overcount persists until the entry expires.
func (c *Client) MarkUnreadCounted(ctx context.Context, notificationID string, ttl time.Duration) (bool, error) {
	ok, err := c.rdb.SetNX(ctx, "unread:counted:"+notificationID, 1, ttl).Result()
	if err != nil {
		return false, fmt.Errorf("mark unread counted: %w", err)
	}
	return ok, nil
}

// incrIfPresent increments an existing count, and makes two refusals that a plain INCR does not.
//
// It will not create the key. Redis INCR on a missing key mints a 1 -- with no expiry, because
// INCR sets no TTL -- for a user whose real count may be 47. That value then never expires and
// never heals, so the badge is permanently wrong. Reporting a miss instead leaves the next
// authoritative read to fill the key from the store, which is the only party that knows.
//
// It will not extend the TTL. The expiry is a bounded correctness window, not an idle timer. A
// user receiving a steady drip of notifications who never opens the panel is exactly who needs
// a periodic recount; re-arming on every arrival is how such a user's count would drift for
// days.
//
// Returns the new value, the ceiling if already at or above it, or UnreadCountMiss when the key is
// absent or holds a non-number (deleted on sight -- there is nothing to salvage, and leaving it
// would make every future increment fail).
var incrIfPresent = redis.NewScript(`
local val = redis.call('GET', KEYS[1])
if val == false then
	return -1
end
val = tonumber(val)
if val == nil then
	redis.call('DEL', KEYS[1])
	return -1
end
if val >= tonumber(ARGV[1]) then
	return val
end
return redis.call('INCR', KEYS[1])
`)

// IncrUnreadCount atomically increments a cached unread count, clamped at maxCount.
// Returns the new value, or UnreadCountMiss if nothing was cached.
func (c *Client) IncrUnreadCount(ctx context.Context, userID string, maxCount int) (int64, error) {
	result, err := incrIfPresent.Run(ctx, c.rdb, []string{unreadKey(userID)}, maxCount).Int64()
	if err != nil {
		return 0, fmt.Errorf("incr unread count: %w", err)
	}
	return result, nil
}

// decrIfPositive decrements a key only if it exists and is above zero, flooring at zero.
// Returns the new value, or -1 if the key is absent or holds a non-number.
//
// The non-numeric branch matters: tonumber("abc") is nil, and comparing nil raises a Lua error
// that surfaces as an opaque script failure rather than a cache miss the caller can recover
// from.
var decrIfPositive = redis.NewScript(`
local val = redis.call('GET', KEYS[1])
if val == false then
	return -1
end
val = tonumber(val)
if val == nil then
	redis.call('DEL', KEYS[1])
	return -1
end
if val > 0 then
	return redis.call('DECR', KEYS[1])
end
return 0
`)

// DecrUnreadCount atomically decrements the unread count (floor at 0).
// Returns the new value, or UnreadCountMiss if nothing was cached.
func (c *Client) DecrUnreadCount(ctx context.Context, userID string) (int64, error) {
	result, err := decrIfPositive.Run(ctx, c.rdb, []string{unreadKey(userID)}).Int64()
	if err != nil {
		return 0, fmt.Errorf("decr unread count: %w", err)
	}
	return result, nil
}

// DeleteUnreadCount removes the cached unread count for a user.
func (c *Client) DeleteUnreadCount(ctx context.Context, userID string) error {
	return c.rdb.Del(ctx, unreadKey(userID)).Err()
}

// OrganizationExists checks whether an organization ID is cached as known.
// Returns true on hit, false on miss.
func (c *Client) OrganizationExists(ctx context.Context, id string) (bool, error) {
	err := c.rdb.Get(ctx, "organization:"+id).Err()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get organization: %w", err)
	}
	return true, nil
}

// SetOrganizationExists marks an organization ID as known in the cache.
func (c *Client) SetOrganizationExists(ctx context.Context, id string, ttl time.Duration) error {
	return c.rdb.Set(ctx, "organization:"+id, "1", ttl).Err()
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
