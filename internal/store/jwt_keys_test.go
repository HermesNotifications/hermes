//go:build integration

package store_test

import (
	"context"
	"testing"
)

func TestCreateAndListJWTSigningKeys(t *testing.T) {
	st, pool := testStore(t)
	cleanTable(t, pool, "jwt_signing_keys")
	ctx := context.Background()

	// Create a key
	k, err := st.CreateJWTSigningKey(ctx, "test-key", "HS256", "my-secret", "sub", "org_id")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if k.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if k.Name != "test-key" {
		t.Errorf("expected name test-key, got %s", k.Name)
	}
	if k.Secret != "my-secret" {
		t.Errorf("expected secret my-secret, got %s", k.Secret)
	}
	if k.UserIDClaim != "sub" {
		t.Errorf("expected user_id_claim sub, got %s", k.UserIDClaim)
	}
	if k.TenantIDClaim != "org_id" {
		t.Errorf("expected tenant_id_claim org_id, got %s", k.TenantIDClaim)
	}
	if !k.Active {
		t.Error("expected key to be active")
	}

	// List active keys (includes secrets)
	active, err := st.ListActiveJWTSigningKeys(ctx)
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(active) < 1 {
		t.Fatal("expected at least 1 active key")
	}
	found := false
	for _, ak := range active {
		if ak.ID == k.ID {
			found = true
			if ak.Secret != "my-secret" {
				t.Error("expected secret in active listing")
			}
		}
	}
	if !found {
		t.Error("created key not in active list")
	}

	// List all keys (no secrets in scan)
	all, err := st.ListJWTSigningKeys(ctx)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) < 1 {
		t.Fatal("expected at least 1 key")
	}

	// Delete
	if err := st.DeleteJWTSigningKey(ctx, k.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Delete non-existent
	if err := st.DeleteJWTSigningKey(ctx, "nonexistent"); err == nil {
		t.Error("expected error deleting non-existent key")
	}
}

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
