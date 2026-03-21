package middleware

import (
	"net/http"
	"sync"
	"time"
)

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

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFunc(r)
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
