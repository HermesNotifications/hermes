// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package auth

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/hermesnotifications/hermes/internal/models"
)

// APIKeyCacheTTL is how long a validated key's record is held in Redis.
const APIKeyCacheTTL = 5 * time.Minute

// APIKeyLookup is the store slice needed to resolve a key.
type APIKeyLookup interface {
	GetAPIKeyByID(ctx context.Context, id string) (*models.APIKey, error)
}

// APIKeyCache is the cache slice needed to resolve a key.
type APIKeyCache interface {
	GetAPIKey(ctx context.Context, keyID string) ([]byte, error)
	SetAPIKey(ctx context.Context, keyID string, data []byte, ttl time.Duration) error
}

// CachedAPIKey is the record held in Redis between lookups.
//
// Note that it caches the key HASH, never the secret, so a cache hit still has
// to pass HMAC verification — the cache saves a database round trip, not the
// check itself.
//
// The rate limit fields are omitempty and read back as zero when absent, and
// zero already means "use the service default". That is what lets this struct
// gain fields without a cache version bump: entries written by an older build
// simply resolve to the default.
type CachedAPIKey struct {
	KeyHash            string   `json:"key_hash"`
	Permissions        []string `json:"permissions"`
	RateLimitPerSecond int      `json:"rate_limit_per_second,omitempty"`
	RateLimitBurst     int      `json:"rate_limit_burst,omitempty"`
}

// ResolveAPIKey turns a raw credential into a ValidatedKey, or nil if it is not
// valid.
//
// Both the Send and Admin APIs authenticate identically, and previously each
// held its own copy of this flow — including its own copy of the cache entry
// struct, which is the part that has to stay in sync for a cached record written
// by one service to be readable by the other.
//
// keyCache may be nil, in which case every lookup goes to the store. Callers
// holding a concrete cache pointer must convert explicitly rather than passing a
// possibly-nil pointer through this interface, since a typed nil in an interface
// is not nil.
func ResolveAPIKey(
	ctx context.Context,
	rawKey string,
	store APIKeyLookup,
	keyCache APIKeyCache,
	hmacSecret string,
	logger *slog.Logger,
) *ValidatedKey {
	keyID, secret, err := ParseAPIKey(rawKey)
	if err != nil {
		return nil
	}

	var entry CachedAPIKey

	if keyCache != nil {
		cached, err := keyCache.GetAPIKey(ctx, keyID)
		if err != nil {
			// Fail open to the store. A Redis outage must not become an
			// authentication outage.
			logger.Error("cache get api key failed", "error", err)
		} else if cached != nil {
			if json.Unmarshal(cached, &entry) != nil {
				entry = CachedAPIKey{}
			}
		}
	}

	if entry.KeyHash == "" {
		k, err := store.GetAPIKeyByID(ctx, keyID)
		if err != nil || k == nil {
			return nil
		}
		entry = CachedAPIKey{
			KeyHash:            k.KeyHash,
			Permissions:        k.Permissions,
			RateLimitPerSecond: derefOrZero(k.RateLimitPerSecond),
			RateLimitBurst:     derefOrZero(k.RateLimitBurst),
		}

		if keyCache != nil {
			encoded, _ := json.Marshal(entry)
			if err := keyCache.SetAPIKey(ctx, keyID, encoded, APIKeyCacheTTL); err != nil {
				logger.Error("cache set api key failed", "error", err)
			}
		}
	}

	if !HMACVerifyAPIKey(secret, entry.KeyHash, hmacSecret) {
		return nil
	}

	return &ValidatedKey{
		ID:                 keyID,
		Permissions:        entry.Permissions,
		RateLimitPerSecond: entry.RateLimitPerSecond,
		RateLimitBurst:     entry.RateLimitBurst,
	}
}

func derefOrZero(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
