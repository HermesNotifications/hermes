// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// fixedKey buckets every request together, so tests exercise one bucket.
func fixedKey(*http.Request) string { return "test-key" }

// clock is a hand-advanced time source. The limiter takes one so refill can be
// tested by moving time rather than by sleeping, which is both slow and flaky.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }
func (c *clock) advance(d time.Duration) {
	c.t = c.t.Add(d)
}

// newTestLimiter returns a limiter and the clock driving it.
func newTestLimiter(t *testing.T, burst, perSecond int) (*RateLimiter, *clock) {
	t.Helper()
	c := &clock{t: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)}
	rl := NewRateLimiter(fixedKey, burst, perSecond)
	rl.now = c.now
	rl.lastSweep.Store(c.t.UnixNano())
	return rl, c
}

// do sends one request through the limiter and returns the recorder.
func do(h http.Handler, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRateLimit_AllowsBurstThenRejects(t *testing.T) {
	const burst = 3
	rl, _ := newTestLimiter(t, burst, 100)
	h := rl.Middleware(okHandler())

	for i := range burst {
		if code := do(h, "/").Code; code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, code)
		}
	}
	if code := do(h, "/").Code; code != http.StatusTooManyRequests {
		t.Errorf("expected 429 once the burst is spent, got %d", code)
	}
}

// The 429 has to be actionable and has to look like every other error this API
// returns. Previously it was bare http.Error text with no Retry-After, so a
// client had nothing to back off against.
func TestRateLimit_RejectionCarriesRetryAfterAndJSONEnvelope(t *testing.T) {
	rl, _ := newTestLimiter(t, 1, 1)
	h := rl.Middleware(okHandler())

	do(h, "/") // spend the single token
	rec := do(h, "/")

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "1" {
		t.Errorf("Retry-After = %q, want \"1\"", got)
	}
	if got := rec.Header().Get("RateLimit-Limit"); got != "1" {
		t.Errorf("RateLimit-Limit = %q, want \"1\"", got)
	}
	if got := rec.Header().Get("RateLimit-Remaining"); got != "0" {
		t.Errorf("RateLimit-Remaining = %q, want \"0\"", got)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v (%q)", err, rec.Body.String())
	}
	if body["error"] == "" {
		t.Errorf("body %v has no \"error\" field", body)
	}
}

// Regression test for the reservation leak. x/time/rate's Reserve always takes
// the token; a limiter that rejects instead of waiting has to hand it back.
// Without the cancel, each rejected retry pushes the next success further out,
// so a client in a retry loop never recovers.
func TestRateLimit_RejectionDoesNotConsumeFutureCapacity(t *testing.T) {
	rl, c := newTestLimiter(t, 1, 1) // one token, one per second
	h := rl.Middleware(okHandler())

	if code := do(h, "/").Code; code != http.StatusOK {
		t.Fatalf("first request should be allowed")
	}
	// Two rejected attempts while the bucket is empty.
	for i := range 2 {
		if code := do(h, "/").Code; code != http.StatusTooManyRequests {
			t.Fatalf("retry %d should be rejected, got %d", i+1, code)
		}
	}

	// One second later exactly one token has refilled, and it must be available.
	c.advance(time.Second)
	if code := do(h, "/").Code; code != http.StatusOK {
		t.Errorf("expected 200 one second after refill, got %d — "+
			"rejected requests consumed future capacity", code)
	}
}

func TestRateLimit_RefillsOverTime(t *testing.T) {
	rl, c := newTestLimiter(t, 2, 10) // 10/s => 100ms per token
	h := rl.Middleware(okHandler())

	do(h, "/")
	do(h, "/")
	if code := do(h, "/").Code; code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 with the burst spent, got %d", code)
	}

	c.advance(100 * time.Millisecond)
	if code := do(h, "/").Code; code != http.StatusOK {
		t.Errorf("expected 200 after a token refilled, got %d", code)
	}
}

// Health probes bypass auth, so they all carry the same empty key. Limiting them
// would put every probe in one bucket and let probe traffic fail readiness for
// reasons unrelated to the service.
func TestRateLimit_HealthEndpointsAreNeverLimited(t *testing.T) {
	rl, _ := newTestLimiter(t, 1, 1)
	h := rl.Middleware(okHandler())

	do(h, "/") // exhaust the shared bucket

	for _, path := range []string{"/healthz", "/readyz"} {
		for i := range 5 {
			if code := do(h, path).Code; code != http.StatusOK {
				t.Errorf("%s request %d: expected 200, got %d", path, i+1, code)
			}
		}
	}
}

func TestRateLimit_DistinctKeysGetDistinctBuckets(t *testing.T) {
	rl := NewRateLimiter(func(r *http.Request) string {
		return r.Header.Get("X-Caller")
	}, 1, 1)
	h := rl.Middleware(okHandler())

	send := func(caller string) int {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Caller", caller)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := send("alice"); code != http.StatusOK {
		t.Fatalf("alice first request: got %d", code)
	}
	if code := send("bob"); code != http.StatusOK {
		t.Errorf("bob should have his own bucket, got %d", code)
	}
	if code := send("alice"); code != http.StatusTooManyRequests {
		t.Errorf("alice's second request should be limited, got %d", code)
	}
}

// A perSecond of zero is how HERMES_RATELIMIT_ENABLED=false is expressed.
func TestRateLimit_DisabledWhenPerSecondIsZero(t *testing.T) {
	rl, _ := newTestLimiter(t, 0, 0)
	h := rl.Middleware(okHandler())

	for i := range 50 {
		rec := do(h, "/")
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200 with limiting disabled, got %d", i+1, rec.Code)
		}
		if got := rec.Header().Get("RateLimit-Limit"); got != "" {
			t.Errorf("disabled limiter should advertise no limit, got %q", got)
		}
	}
}

func TestRateLimit_AllowedResponsesReportRemaining(t *testing.T) {
	rl, _ := newTestLimiter(t, 5, 1)
	h := rl.Middleware(okHandler())

	rec := do(h, "/")
	if got := rec.Header().Get("RateLimit-Limit"); got != "1" {
		t.Errorf("RateLimit-Limit = %q, want \"1\"", got)
	}
	// Five tokens, one spent by this request.
	if got := rec.Header().Get("RateLimit-Remaining"); got != "4" {
		t.Errorf("RateLimit-Remaining = %q, want \"4\"", got)
	}
}

// The sweep reclaims idle buckets. It runs inline on the first request past the
// interval rather than on a background ticker, so there is no goroutine to leak.
func TestRateLimit_SweepEvictsIdleEntries(t *testing.T) {
	rl := NewRateLimiter(func(r *http.Request) string {
		return r.Header.Get("X-Caller")
	}, 10, 10)
	c := &clock{t: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)}
	rl.now = c.now
	rl.lastSweep.Store(c.t.UnixNano())
	h := rl.Middleware(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Caller", "idle-caller")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if _, ok := rl.entries.Load("idle-caller"); !ok {
		t.Fatal("bucket was not created")
	}

	// Past both the TTL and the sweep interval, then drive one request from a
	// different caller to trigger the inline sweep.
	c.advance(entryTTL + sweepInterval)
	other := httptest.NewRequest(http.MethodGet, "/", nil)
	other.Header.Set("X-Caller", "active-caller")
	h.ServeHTTP(httptest.NewRecorder(), other)

	if _, ok := rl.entries.Load("idle-caller"); ok {
		t.Error("idle bucket survived the sweep")
	}
	if _, ok := rl.entries.Load("active-caller"); !ok {
		t.Error("active bucket was swept")
	}
}
