// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

//go:build integration

package postgres_test

import (
	"context"
	"testing"
)

func TestEnsureHermesSigningKey(t *testing.T) {
	st, pool := testStore(t)
	cleanTable(t, pool, "jwt_signing_keys")
	ctx := context.Background()

	// First call creates
	if err := st.EnsureHermesSigningKey(ctx, "secret-1"); err != nil {
		t.Fatalf("first ensure: %v", err)
	}

	// Second call updates secret
	if err := st.EnsureHermesSigningKey(ctx, "secret-2"); err != nil {
		t.Fatalf("second ensure: %v", err)
	}

	// Verify the key exists with updated secret
	keys, err := st.ListActiveJWTSigningKeys(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := false
	for _, k := range keys {
		if k.ID == "hermes-internal" {
			found = true
			if k.Secret != "secret-2" {
				t.Errorf("expected secret-2, got %s", k.Secret)
			}
		}
	}
	if !found {
		t.Error("hermes-internal key not found")
	}
}
