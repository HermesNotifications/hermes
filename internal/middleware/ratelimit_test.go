// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Finding 21. The two API-key services pass `r.Header.Get("Authorization")` straight
// through as the bucket key, so the raw bearer token — the secret itself, not even
// stripped of its "Bearer " prefix — became a live map key retained for up to 30 minutes
// by the eviction sweep. Anything that dumps process memory, or a heap profile, hands
// over working credentials.
//
// bucketKey is what stops the raw secret being the retained value. It is applied inside
// RateLimit rather than at each call site so no caller can forget it.
func TestBucketKey_DoesNotRetainTheRawSecret(t *testing.T) {
	raw := "Bearer hms_key_abc_supersecretvalue"
	got := bucketKey(raw)

	if got == raw {
		t.Fatal("bucket key is the raw credential")
	}
	if strings.Contains(got, "supersecretvalue") {
		t.Errorf("bucket key %q still contains the secret", got)
	}
	// A prefix is not good enough: a partial credential is still a credential leak,
	// and it narrows a brute force enormously.
	if strings.Contains(got, "hms_key_abc") {
		t.Errorf("bucket key %q still contains part of the credential", got)
	}
}

func TestBucketKey_IsDeterministicAndDistinct(t *testing.T) {
	a, b := "Bearer token-one", "Bearer token-two"

	if bucketKey(a) != bucketKey(a) {
		t.Error("same input produced different keys; each request would get a fresh bucket")
	}
	if bucketKey(a) == bucketKey(b) {
		t.Error("different inputs collided; one caller's usage would count against another")
	}
	if bucketKey("") == bucketKey(a) {
		t.Error("empty and non-empty inputs collided")
	}
}

// The security property must not have cost the behaviour: two requests bearing the same
// credential still have to share one bucket, or the limit does not limit.
func TestRateLimit_SameCredentialSharesABucket(t *testing.T) {
	handler := RateLimit(func(r *http.Request) string {
		return r.Header.Get("Authorization")
	}, 1, 1)(okHandler())

	send := func() int {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer same-token")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr.Code
	}

	if code := send(); code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", code)
	}
	if code := send(); code != http.StatusTooManyRequests {
		t.Fatalf("second request with the same credential: expected 429, got %d", code)
	}
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func keyFunc(r *http.Request) string {
	return "test-key"
}

func TestRateLimit_AllowsBurst(t *testing.T) {
	const burst = 3
	rl := RateLimit(keyFunc, burst, 100)
	handler := rl(okHandler())

	for i := 0; i < burst; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, rec.Code)
		}
	}
}

func TestRateLimit_RejectsAfterBurst(t *testing.T) {
	const burst = 3
	rl := RateLimit(keyFunc, burst, 100)
	handler := rl(okHandler())

	for i := 0; i < burst; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, rec.Code)
		}
	}

	// burst+1 request should be rejected
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 after burst exhausted, got %d", rec.Code)
	}
}

func TestRateLimit_RefillsOverTime(t *testing.T) {
	const burst = 3
	// sustained = 100 tokens/sec means ~10ms per token
	rl := RateLimit(keyFunc, burst, 100)
	handler := rl(okHandler())

	// Exhaust the burst
	for i := 0; i < burst; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// Confirm it's now rate limited
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 immediately after burst, got %d", rec.Code)
	}

	// Wait long enough for at least 1 token to refill (100 tokens/s = 10ms per token)
	time.Sleep(20 * time.Millisecond)

	// Should now be allowed
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 after refill, got %d", rec.Code)
	}
}
