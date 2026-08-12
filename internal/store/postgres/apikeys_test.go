// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

//go:build integration

package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hermesnotifications/hermes/internal/models"
)

// The API key store had no coverage at all. The admin handlers exercise it through a mock,
// which cannot catch a column list that does not match the table, a RETURNING clause that
// scans into the wrong field, or the CHECK constraint added with the rate limit columns —
// all of which are only real against a database.

func intp(n int) *int { return &n }

func TestCreateAPIKey_WithoutLimitsLeavesThemNull(t *testing.T) {
	st, pool := testStore(t)
	cleanTable(t, pool, "api_keys")
	ctx := context.Background()

	k, err := st.CreateAPIKey(ctx, "key_a", "hash-a", "No Limits",
		[]string{"notifications:send"}, models.RateLimitOverride{})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if k.RateLimitPerSecond != nil || k.RateLimitBurst != nil {
		t.Errorf("expected no override, got %v / %v", k.RateLimitPerSecond, k.RateLimitBurst)
	}

	// And it survives the round trip, which is what the auth path actually reads.
	got, err := st.GetAPIKeyByID(ctx, "key_a")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.RateLimitPerSecond != nil || got.RateLimitBurst != nil {
		t.Errorf("expected no override on read back, got %v / %v",
			got.RateLimitPerSecond, got.RateLimitBurst)
	}
}

func TestCreateAPIKey_PersistsLimits(t *testing.T) {
	st, pool := testStore(t)
	cleanTable(t, pool, "api_keys")
	ctx := context.Background()

	if _, err := st.CreateAPIKey(ctx, "key_b", "hash-b", "Premium",
		[]string{"notifications:send"},
		models.RateLimitOverride{PerSecond: intp(500), Burst: intp(1000)}); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := st.GetAPIKeyByID(ctx, "key_b")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.RateLimitPerSecond == nil || *got.RateLimitPerSecond != 500 {
		t.Errorf("per-second = %v, want 500", got.RateLimitPerSecond)
	}
	if got.RateLimitBurst == nil || *got.RateLimitBurst != 1000 {
		t.Errorf("burst = %v, want 1000", got.RateLimitBurst)
	}
}

// One limit without the other is a legitimate configuration: raise the burst, keep the
// default sustained rate.
func TestCreateAPIKey_AcceptsOneLimitWithoutTheOther(t *testing.T) {
	st, pool := testStore(t)
	cleanTable(t, pool, "api_keys")
	ctx := context.Background()

	k, err := st.CreateAPIKey(ctx, "key_c", "hash-c", "Burst Only",
		[]string{"notifications:send"}, models.RateLimitOverride{Burst: intp(250)})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if k.RateLimitPerSecond != nil {
		t.Errorf("expected per-second unset, got %v", k.RateLimitPerSecond)
	}
	if k.RateLimitBurst == nil || *k.RateLimitBurst != 250 {
		t.Errorf("burst = %v, want 250", k.RateLimitBurst)
	}
}

func TestUpdateAPIKeyRateLimits_ReplacesRatherThanMerges(t *testing.T) {
	st, pool := testStore(t)
	cleanTable(t, pool, "api_keys")
	ctx := context.Background()

	if _, err := st.CreateAPIKey(ctx, "key_d", "hash-d", "Premium",
		[]string{"notifications:send"},
		models.RateLimitOverride{PerSecond: intp(500), Burst: intp(1000)}); err != nil {
		t.Fatalf("create: %v", err)
	}

	k, err := st.UpdateAPIKeyRateLimits(ctx, "key_d",
		models.RateLimitOverride{PerSecond: intp(50)})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if k.RateLimitPerSecond == nil || *k.RateLimitPerSecond != 50 {
		t.Errorf("per-second = %v, want 50", k.RateLimitPerSecond)
	}
	if k.RateLimitBurst != nil {
		t.Errorf("expected burst cleared, got %v", k.RateLimitBurst)
	}

	// The RETURNING clause must agree with a fresh read, or the API would report one thing
	// and the limiter enforce another.
	got, _ := st.GetAPIKeyByID(ctx, "key_d")
	if got.RateLimitBurst != nil {
		t.Errorf("expected burst cleared on read back, got %v", got.RateLimitBurst)
	}
}

func TestUpdateAPIKeyRateLimits_EmptyOverrideClearsBoth(t *testing.T) {
	st, pool := testStore(t)
	cleanTable(t, pool, "api_keys")
	ctx := context.Background()

	if _, err := st.CreateAPIKey(ctx, "key_e", "hash-e", "Premium",
		[]string{"notifications:send"},
		models.RateLimitOverride{PerSecond: intp(500), Burst: intp(1000)}); err != nil {
		t.Fatalf("create: %v", err)
	}

	k, err := st.UpdateAPIKeyRateLimits(ctx, "key_e", models.RateLimitOverride{})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if k.RateLimitPerSecond != nil || k.RateLimitBurst != nil {
		t.Errorf("expected both cleared, got %v / %v", k.RateLimitPerSecond, k.RateLimitBurst)
	}
}

// (nil, nil) rather than an error, matching GetAPIKeyByID — the handler turns it into a 404.
func TestUpdateAPIKeyRateLimits_UnknownKeyIsNotAnError(t *testing.T) {
	st, pool := testStore(t)
	cleanTable(t, pool, "api_keys")

	k, err := st.UpdateAPIKeyRateLimits(context.Background(), "key_missing",
		models.RateLimitOverride{PerSecond: intp(50)})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if k != nil {
		t.Errorf("expected nil key, got %+v", k)
	}
}

// The CHECK constraint is the last line of defence behind the handler's minimum:1. A zero
// must be refused by the database rather than stored as a limit that admits nothing.
func TestAPIKeyRateLimits_RejectZeroAndNegative(t *testing.T) {
	st, pool := testStore(t)
	cleanTable(t, pool, "api_keys")
	ctx := context.Background()

	for name, limits := range map[string]models.RateLimitOverride{
		"zero per-second":     {PerSecond: intp(0)},
		"negative per-second": {PerSecond: intp(-1)},
		"zero burst":          {Burst: intp(0)},
		"negative burst":      {Burst: intp(-5)},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := st.CreateAPIKey(ctx, "key_bad", "hash-bad", "Bad",
				[]string{"notifications:send"}, limits)
			if err == nil {
				t.Fatal("expected the CHECK constraint to reject this")
			}
			if !strings.Contains(err.Error(), "api_keys_rate_limit") {
				t.Errorf("expected a rate limit constraint violation, got %v", err)
			}
		})
	}
}

func TestListAPIKeys_CarriesTheLimits(t *testing.T) {
	st, pool := testStore(t)
	cleanTable(t, pool, "api_keys")
	ctx := context.Background()

	if _, err := st.CreateAPIKey(ctx, "key_f", "hash-f", "Premium",
		[]string{"notifications:send"},
		models.RateLimitOverride{PerSecond: intp(500)}); err != nil {
		t.Fatalf("create: %v", err)
	}

	keys, err := st.ListAPIKeys(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	if keys[0].RateLimitPerSecond == nil || *keys[0].RateLimitPerSecond != 500 {
		t.Errorf("per-second = %v, want 500", keys[0].RateLimitPerSecond)
	}
}
