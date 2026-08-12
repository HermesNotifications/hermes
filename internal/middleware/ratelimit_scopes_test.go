// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package middleware

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/hermesnotifications/hermes/internal/cache"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeSharedLimiter stands in for Redis, typed against the real interface so a
// signature change breaks the test rather than silently diverging from it.
type fakeSharedLimiter struct {
	decision cache.RateLimitDecision
	err      error

	calls     int
	gotKey    string
	gotBurst  int
	gotPerSec int
}

func (f *fakeSharedLimiter) AllowRequest(_ context.Context, key string, burst, perSecond int) (cache.RateLimitDecision, error) {
	f.calls++
	f.gotKey = key
	f.gotBurst = burst
	f.gotPerSec = perSecond
	if f.err != nil {
		return cache.RateLimitDecision{}, f.err
	}
	return f.decision, nil
}

// headerKey buckets by a request header, so a test can drive several callers
// through one limiter.
func headerKey(r *http.Request) string { return r.Header.Get("X-Caller") }

func doAs(h http.Handler, caller string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Caller", caller)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// A credential carrying its own limit must not be held to the service default.
func TestRateLimit_PerCallerLimitOverridesTheDefault(t *testing.T) {
	c := &clock{t: time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)}
	rl := NewRateLimiter(headerKey, 2, 2).WithLimitFunc(func(r *http.Request) (int, int) {
		if r.Header.Get("X-Caller") == "premium" {
			return 10, 10
		}
		return 0, 0 // unset: keep the service default
	})
	rl.now = c.now
	rl.lastSweep.Store(c.t.UnixNano())
	h := rl.Middleware(okHandler())

	// The default caller gets the default burst of 2.
	for i := range 2 {
		if code := doAs(h, "standard").Code; code != http.StatusOK {
			t.Errorf("standard request %d: expected 200, got %d", i+1, code)
		}
	}
	if code := doAs(h, "standard").Code; code != http.StatusTooManyRequests {
		t.Errorf("standard should be limited after its burst, got %d", code)
	}

	// The premium caller gets its own burst of 10, in the same limiter.
	for i := range 10 {
		if code := doAs(h, "premium").Code; code != http.StatusOK {
			t.Errorf("premium request %d: expected 200, got %d", i+1, code)
		}
	}
	if code := doAs(h, "premium").Code; code != http.StatusTooManyRequests {
		t.Errorf("premium should be limited after its own burst, got %d", code)
	}
}

// The advertised limit must be the caller's own, not the service default.
func TestRateLimit_HeadersReportThePerCallerLimit(t *testing.T) {
	rl := NewRateLimiter(headerKey, 2, 2).WithLimitFunc(func(*http.Request) (int, int) {
		return 10, 7
	})
	h := rl.Middleware(okHandler())

	rec := doAs(h, "someone")
	if got := rec.Header().Get("RateLimit-Limit"); got != "7" {
		t.Errorf("expected RateLimit-Limit 7, got %q", got)
	}
}

// The advertised limit comes from the caller's entitlement, so it stays stable
// whichever path decided the request.
func TestRateLimit_HeaderAdvertisesEntitlementOnEitherPath(t *testing.T) {
	shared := &fakeSharedLimiter{decision: cache.RateLimitDecision{Allowed: true, Remaining: 42}}
	rl := NewRateLimiter(fixedKey, 100, 100).WithShared(shared, quietLogger())
	h := rl.Middleware(okHandler())

	viaShared := do(h, "/")
	if got := viaShared.Header().Get("RateLimit-Limit"); got != "100" {
		t.Errorf("shared path: expected the entitlement 100, got %q", got)
	}
	if got := viaShared.Header().Get("RateLimit-Remaining"); got != "42" {
		t.Errorf("shared path: expected the cluster-wide remaining 42, got %q", got)
	}

	// Same limiter, backend now failing: the local bucket decides and the
	// advertised limit must not change under the client's feet.
	shared.err = errors.New("redis down")
	viaLocal := do(h, "/")
	if got := viaLocal.Header().Get("RateLimit-Limit"); got != "100" {
		t.Errorf("local path: expected the entitlement 100, got %q", got)
	}
}

// With a backend installed it decides, not the local bucket — otherwise the
// limit would still be per replica.
func TestRateLimit_SharedBackendIsAuthoritative(t *testing.T) {
	shared := &fakeSharedLimiter{decision: cache.RateLimitDecision{Allowed: false, RetryAfter: 2 * time.Second}}
	// A local burst of 100 would admit this freely; the backend says no.
	rl := NewRateLimiter(fixedKey, 100, 100).WithShared(shared, quietLogger())
	h := rl.Middleware(okHandler())

	rec := do(h, "/")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected the backend's refusal to win, got %d", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "2" {
		t.Errorf("expected the backend's Retry-After of 2, got %q", got)
	}
	if shared.calls != 1 {
		t.Errorf("expected exactly one backend call, got %d", shared.calls)
	}
}

// The whole reason for keeping the local bucket: an outage must degrade, not
// fail requests and not remove the limit.
func TestRateLimit_FallsBackToLocalWhenBackendFails(t *testing.T) {
	const burst = 3
	shared := &fakeSharedLimiter{err: errors.New("redis unavailable")}
	rl := NewRateLimiter(fixedKey, burst, 100).WithShared(shared, quietLogger())
	h := rl.Middleware(okHandler())

	// The local bucket takes over and still enforces its own burst.
	for i := range burst {
		if code := do(h, "/").Code; code != http.StatusOK {
			t.Errorf("request %d should be served locally, got %d", i+1, code)
		}
	}
	if code := do(h, "/").Code; code != http.StatusTooManyRequests {
		t.Errorf("the local bucket should still limit past its burst, got %d", code)
	}
}

// The backend must be asked for the caller's own limit, not the service default,
// or per-credential quotas silently stop applying once Redis is in the path.
func TestRateLimit_SharedBackendReceivesPerCallerLimit(t *testing.T) {
	shared := &fakeSharedLimiter{decision: cache.RateLimitDecision{Allowed: true, Remaining: 5}}
	rl := NewRateLimiter(headerKey, 10, 10).
		WithScope(ScopeCredential).
		WithLimitFunc(func(*http.Request) (int, int) { return 77, 33 }).
		WithShared(shared, quietLogger())
	h := rl.Middleware(okHandler())

	doAs(h, "key_abc")

	if shared.gotBurst != 77 || shared.gotPerSec != 33 {
		t.Errorf("expected the per-caller limit (77, 33), got (%d, %d)", shared.gotBurst, shared.gotPerSec)
	}
	// The scope namespaces the key, so a user and an API key of the same name
	// cannot share a bucket.
	if want := ScopeCredential + ":key_abc"; shared.gotKey != want {
		t.Errorf("expected key %q, got %q", want, shared.gotKey)
	}
}

// A disabled limiter must not pay for a Redis round trip it will ignore.
func TestRateLimit_DisabledSkipsTheBackend(t *testing.T) {
	shared := &fakeSharedLimiter{decision: cache.RateLimitDecision{Allowed: true}}
	rl := NewRateLimiter(fixedKey, 0, 0).WithShared(shared, quietLogger())
	h := rl.Middleware(okHandler())

	if code := do(h, "/").Code; code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if shared.calls != 0 {
		t.Errorf("expected no backend call when limiting is off, got %d", shared.calls)
	}
}

// Health probes bypass the backend as well as the bucket; a Redis stall must not
// be able to fail a readiness check.
func TestRateLimit_HealthEndpointsSkipTheBackend(t *testing.T) {
	shared := &fakeSharedLimiter{err: errors.New("redis unavailable")}
	rl := NewRateLimiter(fixedKey, 1, 1).WithShared(shared, quietLogger())
	h := rl.Middleware(okHandler())

	for _, path := range []string{"/healthz", "/readyz"} {
		if code := do(h, path).Code; code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", path, code)
		}
	}
	if shared.calls != 0 {
		t.Errorf("expected no backend call for probes, got %d", shared.calls)
	}
}

// Disabling rate limiting must disable it for EVERY key, including the ones carrying their
// own limit. ResolveLimit(false, …) yields (0, 0) — Inf rate, zero burst — and applying a
// key's rate on top produced a finite rate with a burst of zero, which x/time/rate refuses
// every request against. Turning the feature off locked out exactly the configured keys.
func TestRateLimit_DisabledIgnoresPerCallerLimits(t *testing.T) {
	b, p := ResolveLimit(false, 0, 0, 5000, 2000)
	rl := NewRateLimiter(headerKey, b, p).WithLimitFunc(func(*http.Request) (int, int) {
		return 0, 500 // only the sustained rate is set on the key
	})
	h := rl.Middleware(okHandler())

	for i := range 5 {
		if code := doAs(h, "premium").Code; code != http.StatusOK {
			t.Fatalf("request %d returned %d while rate limiting is disabled", i+1, code)
		}
	}
}

// A key that sets only its sustained rate must still be usable: a finite rate with a zero
// burst admits nothing at all.
func TestRateLimit_PerCallerRateWithoutBurstIsUsable(t *testing.T) {
	rl := NewRateLimiter(headerKey, 10, 10).WithLimitFunc(func(*http.Request) (int, int) {
		return 0, 50
	})
	h := rl.Middleware(okHandler())

	if code := doAs(h, "rate-only").Code; code != http.StatusOK {
		t.Fatalf("expected the request to be admitted, got %d", code)
	}
	e := rl.entryFor("rate-only", nil)
	if e.burst < 1 {
		t.Errorf("expected a usable burst, got %d", e.burst)
	}
}

// Two services against one Redis must not share a caller's bucket. Send and Admin both key
// on the API key ID and pass different rates for it; without the service in the key, a send
// burst spent Admin's allowance and Admin returned 429s for something the caller never did.
func TestRateLimit_SharedKeyIsNamespacedByService(t *testing.T) {
	send := NewRateLimiter(fixedKey, 5000, 2000).WithService("send").WithScope(ScopeCredential)
	admin := NewRateLimiter(fixedKey, 1000, 500).WithService("admin").WithScope(ScopeCredential)

	if send.sharedKey("key_abc") == admin.sharedKey("key_abc") {
		t.Errorf("send and admin share the key %q", send.sharedKey("key_abc"))
	}
	if want := "send:credential:key_abc"; send.sharedKey("key_abc") != want {
		t.Errorf("expected %q, got %q", want, send.sharedKey("key_abc"))
	}
}

// Per-IP keying is unbounded by an attacker's choice, so the map needs a cap.
func TestRateLimit_EntryCapDivertsToASharedBucket(t *testing.T) {
	c := &clock{t: time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)}
	rl := NewRateLimiter(headerKey, 1, 1).WithMaxEntries(3)
	rl.now = c.now
	rl.lastSweep.Store(c.t.UnixNano())
	h := rl.Middleware(okHandler())

	// Three distinct callers each get their own bucket and their first request.
	for i := range 3 {
		if code := doAs(h, "caller-"+strconv.Itoa(i)).Code; code != http.StatusOK {
			t.Errorf("caller-%d should be admitted, got %d", i, code)
		}
	}
	if n := rl.entryCount.Load(); n != 3 {
		t.Fatalf("expected 3 entries, got %d", n)
	}

	// The fourth is diverted into the shared overflow bucket rather than
	// allocating a fourth entry. Its first request is admitted...
	if code := doAs(h, "caller-3").Code; code != http.StatusOK {
		t.Errorf("first overflow request should be admitted, got %d", code)
	}
	// ...and a fifth distinct caller contends with it for the same tokens.
	if code := doAs(h, "caller-4").Code; code != http.StatusTooManyRequests {
		t.Errorf("overflow callers should share one bucket, got %d", code)
	}

	// The cap held: only the overflow entry was added beyond the three.
	if n := rl.entryCount.Load(); n != 4 {
		t.Errorf("expected 3 caller entries plus 1 overflow, got %d", n)
	}
}

// Reclaiming idle entries must take precedence over diverting live ones.
func TestRateLimit_CapForcesASweepBeforeOverflowing(t *testing.T) {
	c := &clock{t: time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)}
	rl := NewRateLimiter(headerKey, 5, 5).WithMaxEntries(2)
	rl.now = c.now
	rl.lastSweep.Store(c.t.UnixNano())
	h := rl.Middleware(okHandler())

	doAs(h, "old-1")
	doAs(h, "old-2")
	if n := rl.entryCount.Load(); n != 2 {
		t.Fatalf("expected 2 entries, got %d", n)
	}

	// Move past the idle TTL so both existing entries are reclaimable.
	c.advance(entryTTL + time.Minute)

	// A new caller should trigger the reclaim and get a bucket of its own rather
	// than being pushed into the shared one.
	doAs(h, "fresh")
	if n := rl.entryCount.Load(); n != 1 {
		t.Errorf("expected the idle entries to be reclaimed, got %d entries", n)
	}
	if _, overflowed := rl.entries.Load(overflowKey); overflowed {
		t.Error("a reclaimable map should not have diverted to the overflow bucket")
	}
}

// The sweep must keep the count in step with the map, or the cap drifts until
// every caller is diverted to the shared bucket.
func TestRateLimit_SweepKeepsTheEntryCountAccurate(t *testing.T) {
	c := &clock{t: time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)}
	rl := NewRateLimiter(headerKey, 5, 5)
	rl.now = c.now
	rl.lastSweep.Store(c.t.UnixNano())
	h := rl.Middleware(okHandler())

	for i := range 5 {
		doAs(h, "caller-"+strconv.Itoa(i))
	}
	if n := rl.entryCount.Load(); n != 5 {
		t.Fatalf("expected 5 entries, got %d", n)
	}

	c.advance(entryTTL + sweepInterval)
	doAs(h, "trigger") // crosses the sweep interval

	// The five originals are gone; only the request that triggered the sweep
	// remains.
	if n := rl.entryCount.Load(); n != 1 {
		t.Errorf("expected 1 entry after the sweep, got %d", n)
	}
}

// The per-IP bound must not describe itself using the headers the client
// contract reserves for a credential's quota.
func TestRateLimit_IPScopeDoesNotAdvertiseACredentialQuota(t *testing.T) {
	rl := NewRateLimiter(headerKey, 1, 1).WithScope(ScopeIP)
	h := rl.Middleware(okHandler())

	allowed := doAs(h, "1.2.3.4")
	if got := allowed.Header().Get("RateLimit-Limit"); got != "" {
		t.Errorf("allowed response should carry no RateLimit-Limit, got %q", got)
	}

	limited := doAs(h, "1.2.3.4")
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", limited.Code)
	}
	if got := limited.Header().Get("RateLimit-Limit"); got != "" {
		t.Errorf("429 should carry no RateLimit-Limit, got %q", got)
	}
	// Retry-After is the part a client can act on, so it must still be there.
	if got := limited.Header().Get("Retry-After"); got == "" {
		t.Error("429 must still carry Retry-After")
	}
}

// Scope reaches the limiter, since it namespaces both the metric and the Redis
// keys used for reconciliation.
func TestRateLimit_ScopeDefaultsToCredential(t *testing.T) {
	rl := NewRateLimiter(fixedKey, 1, 1)
	if rl.Scope() != ScopeCredential {
		t.Errorf("expected the default scope %q, got %q", ScopeCredential, rl.Scope())
	}
	if rl.WithScope(ScopeIP).Scope() != ScopeIP {
		t.Error("WithScope should change the scope")
	}
}

// A nil TrustedProxies must still produce a usable key func, since that is what
// every service constructs before configuration is applied.
func TestRateLimit_NilTrustedProxiesStillKeysByPeer(t *testing.T) {
	var tp *TrustedProxies
	rl := NewRateLimiter(tp.ClientIP, 1, 1).WithScope(ScopeIP)
	h := rl.Middleware(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.5:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if _, ok := rl.entries.Load("203.0.113.5"); !ok {
		t.Error("expected a bucket keyed by the peer address")
	}
}
