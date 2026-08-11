// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/hermes-notifications/hermes/internal/models"
)

// storedInternalSecret returns the secret on the hermes-internal signing key,
// failing the test if the row is absent.
func storedInternalSecret(t *testing.T, st interface {
	ListActiveJWTSigningKeys(context.Context) ([]models.JWTSigningKey, error)
}, ctx context.Context) string {
	t.Helper()
	keys, err := st.ListActiveJWTSigningKeys(ctx)
	if err != nil {
		t.Fatalf("list signing keys: %v", err)
	}
	for _, k := range keys {
		if k.ID == "hermes-internal" {
			return k.Secret
		}
	}
	t.Fatal("hermes-internal key not found")
	return ""
}

func TestEnsureHermesSigningKey_CreatesOnFirstCall(t *testing.T) {
	st, pool := testStore(t)
	cleanTable(t, pool, "jwt_signing_keys")
	ctx := context.Background()

	if err := st.EnsureHermesSigningKey(ctx, "secret-1"); err != nil {
		t.Fatalf("first ensure: %v", err)
	}

	if got := storedInternalSecret(t, st, ctx); got != "secret-1" {
		t.Errorf("stored secret = %q, want %q", got, "secret-1")
	}
}

// Finding 4. This assertion was previously the exact opposite — it required the
// second call to overwrite the stored secret, pinning the bug as correct. The
// upsert ran at startup in admin, inbox and user, each passing cfg.JWTSecret,
// which defaults to the literal "hermes-jwt-secret". So one service booting
// without the environment variable silently replaced a properly rotated signing
// key and invalidated every token issued under it.
//
// First write wins. A caller disagreeing with the stored key is ignored, and
// EnsureHermesSigningKey logs a warning so the disagreement is visible rather
// than silent. The warning itself is deliberately not asserted on: log text is
// not the contract and changes for unrelated reasons. What is asserted is the
// observable consequence — the stored secret is unchanged.
func TestEnsureHermesSigningKey_DoesNotOverwriteOnSecondCall(t *testing.T) {
	st, pool := testStore(t)
	cleanTable(t, pool, "jwt_signing_keys")
	ctx := context.Background()

	if err := st.EnsureHermesSigningKey(ctx, "secret-1"); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if err := st.EnsureHermesSigningKey(ctx, "secret-2"); err != nil {
		t.Fatalf("second ensure: %v", err)
	}

	if got := storedInternalSecret(t, st, ctx); got != "secret-1" {
		t.Errorf("stored secret = %q, want %q — the second call overwrote the first", got, "secret-1")
	}
}

// A repeated call with the SAME secret is the normal case: every service restart
// does it. It must not error, and must leave the row alone.
func TestEnsureHermesSigningKey_IsIdempotentForTheSameSecret(t *testing.T) {
	st, pool := testStore(t)
	cleanTable(t, pool, "jwt_signing_keys")
	ctx := context.Background()

	for i := range 3 {
		if err := st.EnsureHermesSigningKey(ctx, "secret-1"); err != nil {
			t.Fatalf("ensure call %d: %v", i+1, err)
		}
	}

	if got := storedInternalSecret(t, st, ctx); got != "secret-1" {
		t.Errorf("stored secret = %q, want %q", got, "secret-1")
	}
}
