// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

//go:build integration

package cache_test

import (
	"context"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/hermes-notifications/hermes/internal/cache"
)

var keySeq atomic.Int64

// uniqueKey returns a bucket no other test — or earlier run of this one — shares.
//
// The PID matters as much as the test name. Buckets live in Redis and outlive the
// process, and these limits refill at one token per second, so a key derived from
// the test name alone starts its second run already drained. That failure looks
// like a broken limiter rather than a dirty fixture, which is exactly the kind of
// flake worth designing out.
func uniqueKey(t *testing.T) string {
	t.Helper()
	return "test:" + t.Name() + ":" +
		strconv.Itoa(os.Getpid()) + ":" +
		strconv.FormatInt(keySeq.Add(1), 10)
}

func TestAllowRequest_AdmitsTheBurstThenRefuses(t *testing.T) {
	c, err := cache.Connect(testRedisURL(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()

	ctx := context.Background()
	key := uniqueKey(t)
	const burst = 5

	for i := range burst {
		dec, err := c.AllowRequest(ctx, key, burst, 1)
		if err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
		if !dec.Allowed {
			t.Fatalf("request %d should have been admitted", i+1)
		}
	}

	dec, err := c.AllowRequest(ctx, key, burst, 1)
	if err != nil {
		t.Fatalf("AllowRequest: %v", err)
	}
	if dec.Allowed {
		t.Error("expected the request past the burst to be refused")
	}
	if dec.RetryAfter <= 0 {
		t.Errorf("a refusal must say how long to wait, got %v", dec.RetryAfter)
	}
}

// The point of moving the check into Redis: two callers sharing a key share the
// budget, rather than each getting a full one as they would per replica.
func TestAllowRequest_TwoClientsShareOneBudget(t *testing.T) {
	a, err := cache.Connect(testRedisURL(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer a.Close()
	b, err := cache.Connect(testRedisURL(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer b.Close()

	ctx := context.Background()
	key := uniqueKey(t)
	const burst = 10

	allowed := 0
	for range burst {
		if dec, err := a.AllowRequest(ctx, key, burst, 1); err == nil && dec.Allowed {
			allowed++
		}
		if dec, err := b.AllowRequest(ctx, key, burst, 1); err == nil && dec.Allowed {
			allowed++
		}
	}

	if allowed != burst {
		t.Errorf("expected the two clients to share one budget of %d, got %d", burst, allowed)
	}
}

// GCRA runs inside a single Lua script, so concurrent callers cannot both take
// the last token. Without atomicity this over-admits.
func TestAllowRequest_IsAtomicUnderConcurrency(t *testing.T) {
	c, err := cache.Connect(testRedisURL(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()

	ctx := context.Background()
	key := uniqueKey(t)
	const burst = 20
	const callers = 100

	var mu sync.Mutex
	allowed := 0

	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dec, err := c.AllowRequest(ctx, key, burst, 1)
			if err != nil || !dec.Allowed {
				return
			}
			mu.Lock()
			allowed++
			mu.Unlock()
		}()
	}
	wg.Wait()

	if allowed != burst {
		t.Errorf("expected exactly %d admissions across %d concurrent callers, got %d",
			burst, callers, allowed)
	}
}

// Separate keys must not contend, or one busy credential would throttle every
// other one.
func TestAllowRequest_KeysAreIndependent(t *testing.T) {
	c, err := cache.Connect(testRedisURL(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()

	ctx := context.Background()
	base := uniqueKey(t)

	// Exhaust the first key.
	for range 3 {
		if _, err := c.AllowRequest(ctx, base+":a", 3, 1); err != nil {
			t.Fatalf("AllowRequest: %v", err)
		}
	}
	if dec, _ := c.AllowRequest(ctx, base+":a", 3, 1); dec.Allowed {
		t.Fatal("expected the first key to be exhausted")
	}

	dec, err := c.AllowRequest(ctx, base+":b", 3, 1)
	if err != nil {
		t.Fatalf("AllowRequest: %v", err)
	}
	if !dec.Allowed {
		t.Error("a different key should have its own budget")
	}
}

// A zero rate is how "limiting is off" is expressed; it must not consult Redis
// or refuse anything.
func TestAllowRequest_ZeroRateAlwaysAllows(t *testing.T) {
	c, err := cache.Connect(testRedisURL(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()

	// One key throughout: the point is that repeated use never exhausts.
	key := uniqueKey(t)
	for i := range 50 {
		dec, err := c.AllowRequest(context.Background(), key, 0, 0)
		if err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
		if !dec.Allowed {
			t.Fatalf("request %d should be allowed when limiting is off", i+1)
		}
	}
}

// A cancelled or already-expired context must surface as an error so the caller
// falls back locally, rather than being reported as a refusal.
func TestAllowRequest_ContextFailureIsAnErrorNotARefusal(t *testing.T) {
	c, err := cache.Connect(testRedisURL(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dec, err := c.AllowRequest(ctx, uniqueKey(t), 10, 10)
	if err == nil {
		t.Fatalf("expected an error from a cancelled context, got decision %+v", dec)
	}
	if dec.Allowed {
		t.Error("a failed check must not report an allowed decision")
	}
}

// Remaining is what the client sees in RateLimit-Remaining, so it has to count
// down rather than being a constant.
func TestAllowRequest_RemainingCountsDown(t *testing.T) {
	c, err := cache.Connect(testRedisURL(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()

	ctx := context.Background()
	key := uniqueKey(t)
	const burst = 5

	var seen []int
	for range 3 {
		dec, err := c.AllowRequest(ctx, key, burst, 1)
		if err != nil {
			t.Fatalf("AllowRequest: %v", err)
		}
		seen = append(seen, dec.Remaining)
	}

	for i := 1; i < len(seen); i++ {
		if seen[i] >= seen[i-1] {
			t.Errorf("expected remaining to fall, got %v", seen)
			break
		}
	}
}
