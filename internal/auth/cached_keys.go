package auth

import (
	"sync"
	"time"
)

// CachedKeyProvider wraps a JWTKeyProvider with an in-process TTL cache.
// Keys rarely change, so a short cache (1-2 min) avoids hitting the DB on every request.
func CachedKeyProvider(upstream JWTKeyProvider, ttl time.Duration) JWTKeyProvider {
	var (
		mu        sync.RWMutex
		cached    []JWTSigningConfig
		expiresAt time.Time
	)

	return func() []JWTSigningConfig {
		mu.RLock()
		if time.Now().Before(expiresAt) && cached != nil {
			defer mu.RUnlock()
			return cached
		}
		mu.RUnlock()

		mu.Lock()
		defer mu.Unlock()

		// Double-check after acquiring write lock
		if time.Now().Before(expiresAt) && cached != nil {
			return cached
		}

		fresh := upstream()
		if fresh != nil {
			cached = fresh
			expiresAt = time.Now().Add(ttl)
		}
		return cached
	}
}
