// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

// bucketKey derives the map key a caller's bucket is stored under.
//
// Finding 21. The API-key services pass `r.Header.Get("Authorization")` through as the
// key, so without this the raw bearer token — the secret itself, not even stripped of its
// "Bearer " prefix — was a live map key retained for up to 30 minutes by the eviction
// sweep. Anything that reads process memory, including a heap profile shipped to a
// debugging tool, would hand over working credentials.
//
// Hashing here rather than at each call site means no caller can forget it, and the
// keyFunc signature stays unchanged. SHA-256 is not doing password-hashing work — there
// is no offline-guessing threat model for a map key — it is doing "this value is no
// longer a credential" work, and a fast hash is the right tool for that.
//
// Note this does NOT bound the map: an unauthenticated caller sending garbage tokens
// still mints one bucket per distinct value, because RateLimit is applied outside the
// auth middleware in send and admin. That is finding 39 and needs the ordering changed,
// not a different hash.
func bucketKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

type tokenBucket struct {
	tokens     float64
	maxTokens  float64
	refillRate float64
	lastRefill time.Time
	mu         sync.Mutex
}

func (b *tokenBucket) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens += elapsed * b.refillRate
	if b.tokens > b.maxTokens {
		b.tokens = b.maxTokens
	}
	b.lastRefill = now

	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

func RateLimit(keyFunc func(*http.Request) string, burst int, sustained int) func(http.Handler) http.Handler {
	buckets := sync.Map{}

	// Evict stale entries every 5 minutes
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			buckets.Range(func(key, value any) bool {
				b := value.(*tokenBucket)
				b.mu.Lock()
				stale := time.Since(b.lastRefill) > 30*time.Minute
				b.mu.Unlock()
				if stale {
					buckets.Delete(key)
				}
				return true
			})
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := bucketKey(keyFunc(r))
			val, _ := buckets.LoadOrStore(key, &tokenBucket{
				tokens:     float64(burst),
				maxTokens:  float64(burst),
				refillRate: float64(sustained),
				lastRefill: time.Now(),
			})
			bucket := val.(*tokenBucket)

			if !bucket.allow() {
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
