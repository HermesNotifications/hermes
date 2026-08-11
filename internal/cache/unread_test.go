// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

//go:build integration

package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/internal/cache"
	"github.com/redis/go-redis/v9"
)

const testCap = 1000

// testUser returns a user ID unique to this test run, so concurrent runs and leftover keys from
// a previous run cannot make an assertion pass or fail for the wrong reason.
func testUser(t *testing.T) string {
	t.Helper()
	return "usr_" + t.Name() + "_" + time.Now().Format("150405.000000000")
}

func newCache(t *testing.T) *cache.Client {
	t.Helper()
	c, err := cache.Connect(testRedisURL(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// rawRedis opens a second, uninstrumented connection. The Client deliberately exposes no way to
// write a malformed value, so poisoning a key -- which is the whole point of two of these tests
// -- has to happen out of band.
func rawRedis(t *testing.T) *redis.Client {
	t.Helper()
	opts, err := redis.ParseURL(testRedisURL(t))
	if err != nil {
		t.Fatalf("ParseURL: %v", err)
	}
	rdb := redis.NewClient(opts)
	t.Cleanup(func() { rdb.Close() })
	return rdb
}

// This is the regression test for the bug that motivated the Lua script: a plain INCR on a
// missing key created it at 1, with no expiry, for a user whose real count was anything at all.
// That value never expired and never healed.
func TestIncrUnreadCount_DoesNotCreateKey(t *testing.T) {
	c, ctx := newCache(t), context.Background()
	user := testUser(t)

	got, err := c.IncrUnreadCount(ctx, user, testCap)
	if err != nil {
		t.Fatalf("IncrUnreadCount: %v", err)
	}
	if got != cache.UnreadCountMiss {
		t.Fatalf("incrementing an absent count = %d, want %d (miss)", got, cache.UnreadCountMiss)
	}

	if _, found, err := c.GetUnreadCount(ctx, user); err != nil {
		t.Fatalf("GetUnreadCount: %v", err)
	} else if found {
		t.Fatal("the increment created the key; it must leave the fill to an authoritative read")
	}
}

// The expiry is a bounded correctness window, not an idle timer. If arrivals re-armed it, the
// users who most need a periodic recount -- a steady drip, panel never opened -- would be the
// ones who never got one.
func TestIncrUnreadCount_DoesNotExtendTTL(t *testing.T) {
	c, ctx := newCache(t), context.Background()
	user := testUser(t)

	if err := c.SetUnreadCount(ctx, user, 5, 30*time.Second); err != nil {
		t.Fatalf("SetUnreadCount: %v", err)
	}
	if _, err := c.IncrUnreadCount(ctx, user, testCap); err != nil {
		t.Fatalf("IncrUnreadCount: %v", err)
	}

	_, ttl, found, err := c.GetUnreadCountWithTTL(ctx, user)
	if err != nil {
		t.Fatalf("GetUnreadCountWithTTL: %v", err)
	}
	if !found {
		t.Fatal("count vanished after increment")
	}
	if ttl > 30*time.Second {
		t.Fatalf("TTL = %v, want no more than the original 30s", ttl)
	}
	if ttl == 0 {
		t.Fatal("TTL was cleared; the key would now never expire or heal")
	}
}

func TestIncrUnreadCount_ClampsAtCap(t *testing.T) {
	c, ctx := newCache(t), context.Background()
	user := testUser(t)

	if err := c.SetUnreadCount(ctx, user, testCap, time.Minute); err != nil {
		t.Fatalf("SetUnreadCount: %v", err)
	}
	got, err := c.IncrUnreadCount(ctx, user, testCap)
	if err != nil {
		t.Fatalf("IncrUnreadCount: %v", err)
	}
	if got != testCap {
		t.Fatalf("incrementing at the cap = %d, want it held at %d", got, testCap)
	}
}

func TestIncrUnreadCount_DeletesPoisonValue(t *testing.T) {
	c, ctx := newCache(t), context.Background()
	user := testUser(t)

	if err := rawRedis(t).Set(ctx, "unread:"+user, "not-a-number", time.Minute).Err(); err != nil {
		t.Fatalf("seed poison value: %v", err)
	}

	got, err := c.IncrUnreadCount(ctx, user, testCap)
	if err != nil {
		t.Fatalf("IncrUnreadCount on a non-numeric value: %v", err)
	}
	if got != cache.UnreadCountMiss {
		t.Fatalf("got %d, want %d (miss)", got, cache.UnreadCountMiss)
	}
	if _, found, err := c.GetUnreadCount(ctx, user); err != nil {
		t.Fatalf("GetUnreadCount: %v", err)
	} else if found {
		t.Fatal("poison value survived; every later increment would keep failing on it")
	}
}

func TestDecrUnreadCount_MissAndFloor(t *testing.T) {
	c, ctx := newCache(t), context.Background()
	user := testUser(t)

	got, err := c.DecrUnreadCount(ctx, user)
	if err != nil {
		t.Fatalf("DecrUnreadCount: %v", err)
	}
	if got != cache.UnreadCountMiss {
		t.Fatalf("decrementing an absent count = %d, want %d (miss)", got, cache.UnreadCountMiss)
	}

	if err := c.SetUnreadCount(ctx, user, 0, time.Minute); err != nil {
		t.Fatalf("SetUnreadCount: %v", err)
	}
	if got, err = c.DecrUnreadCount(ctx, user); err != nil {
		t.Fatalf("DecrUnreadCount: %v", err)
	} else if got != 0 {
		t.Fatalf("decrementing zero = %d, want it floored at 0", got)
	}
}

// Before the guard, tonumber("abc") returned nil and `nil > 0` raised a Lua error that reached
// the caller as an opaque script failure rather than a miss it could recover from.
func TestDecrUnreadCount_DeletesPoisonValue(t *testing.T) {
	c, ctx := newCache(t), context.Background()
	user := testUser(t)

	if err := rawRedis(t).Set(ctx, "unread:"+user, "not-a-number", time.Minute).Err(); err != nil {
		t.Fatalf("seed poison value: %v", err)
	}

	got, err := c.DecrUnreadCount(ctx, user)
	if err != nil {
		t.Fatalf("DecrUnreadCount on a non-numeric value: %v", err)
	}
	if got != cache.UnreadCountMiss {
		t.Fatalf("got %d, want %d (miss)", got, cache.UnreadCountMiss)
	}
}

func TestGetUnreadCountWithTTL(t *testing.T) {
	c, ctx := newCache(t), context.Background()
	user := testUser(t)

	if _, _, found, err := c.GetUnreadCountWithTTL(ctx, user); err != nil {
		t.Fatalf("GetUnreadCountWithTTL on a miss: %v", err)
	} else if found {
		t.Fatal("reported a hit for a key that was never written")
	}

	if err := c.SetUnreadCount(ctx, user, 42, time.Minute); err != nil {
		t.Fatalf("SetUnreadCount: %v", err)
	}
	count, ttl, found, err := c.GetUnreadCountWithTTL(ctx, user)
	if err != nil {
		t.Fatalf("GetUnreadCountWithTTL: %v", err)
	}
	if !found || count != 42 {
		t.Fatalf("got (%d, found=%v), want (42, found=true)", count, found)
	}
	if ttl <= 0 || ttl > time.Minute {
		t.Fatalf("TTL = %v, want a positive value no greater than the 1m it was set with", ttl)
	}
}

// A key with no expiry should no longer be reachable, but one written before the INCR fix would
// otherwise report a negative TTL and so never look due for refresh.
func TestGetUnreadCountWithTTL_TreatsImmortalKeyAsDue(t *testing.T) {
	c, ctx := newCache(t), context.Background()
	user := testUser(t)

	if err := rawRedis(t).Set(ctx, "unread:"+user, 7, 0).Err(); err != nil {
		t.Fatalf("seed key with no expiry: %v", err)
	}

	count, ttl, found, err := c.GetUnreadCountWithTTL(ctx, user)
	if err != nil {
		t.Fatalf("GetUnreadCountWithTTL: %v", err)
	}
	if !found || count != 7 {
		t.Fatalf("got (%d, found=%v), want (7, found=true)", count, found)
	}
	if ttl != 0 {
		t.Fatalf("TTL = %v, want 0 so the caller treats it as due for refresh", ttl)
	}
}

func TestFillUnreadCount_DoesNotClobber(t *testing.T) {
	c, ctx := newCache(t), context.Background()
	user := testUser(t)

	won, err := c.FillUnreadCount(ctx, user, 3, time.Minute)
	if err != nil {
		t.Fatalf("FillUnreadCount: %v", err)
	}
	if !won {
		t.Fatal("the first fill lost against an absent key")
	}

	// A second filler holding a staler answer must not replace the first.
	won, err = c.FillUnreadCount(ctx, user, 99, time.Minute)
	if err != nil {
		t.Fatalf("FillUnreadCount: %v", err)
	}
	if won {
		t.Fatal("the second fill overwrote a value that was already there")
	}

	count, _, _, err := c.GetUnreadCountWithTTL(ctx, user)
	if err != nil {
		t.Fatalf("GetUnreadCountWithTTL: %v", err)
	}
	if count != 3 {
		t.Fatalf("count = %d, want the original 3", count)
	}
}

func TestTryUnreadRefreshLease_SingleFlights(t *testing.T) {
	c, ctx := newCache(t), context.Background()
	user := testUser(t)

	won, err := c.TryUnreadRefreshLease(ctx, user, 10*time.Second)
	if err != nil {
		t.Fatalf("TryUnreadRefreshLease: %v", err)
	}
	if !won {
		t.Fatal("the first caller did not get the lease")
	}

	won, err = c.TryUnreadRefreshLease(ctx, user, 10*time.Second)
	if err != nil {
		t.Fatalf("TryUnreadRefreshLease: %v", err)
	}
	if won {
		t.Fatal("a second caller got the lease; every concurrent request would recount")
	}
}

func TestMarkUnreadCounted_IsIdempotent(t *testing.T) {
	c, ctx := newCache(t), context.Background()
	notifID := "ntf_" + time.Now().Format("150405.000000000")

	first, err := c.MarkUnreadCounted(ctx, notifID, time.Hour)
	if err != nil {
		t.Fatalf("MarkUnreadCounted: %v", err)
	}
	if !first {
		t.Fatal("the first delivery was not treated as new")
	}

	// This is the redelivery: the Centrifugo publish failed, the message was nacked, and the
	// worker is running Send again for the same notification.
	first, err = c.MarkUnreadCounted(ctx, notifID, time.Hour)
	if err != nil {
		t.Fatalf("MarkUnreadCounted: %v", err)
	}
	if first {
		t.Fatal("a redelivery was treated as new; the user's count would be incremented twice")
	}
}
