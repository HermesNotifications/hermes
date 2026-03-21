package middleware

import (
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
