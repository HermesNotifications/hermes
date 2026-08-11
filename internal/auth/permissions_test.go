// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hermes-notifications/hermes/internal/auth"
)

// Finding 3. RequirePermission was `func(http.Handler) http.Handler`, which cannot be
// applied to a Huma operation — Huma handlers are func(ctx, input) and never see an
// http.Handler. That shape is *why* it had zero production call sites while being fully
// unit-tested: the tests exercised a function no route could use. CheckPermission takes a
// context instead, which is what a Huma handler actually has.

func TestCheckPermission_AllowsAKeyHoldingThePermission(t *testing.T) {
	ctx := auth.WithValidatedKey(context.Background(), &auth.ValidatedKey{
		ID:          "key_abc123",
		Permissions: []string{auth.PermNotificationsSend, auth.PermTemplatesManage},
	})

	if err := auth.CheckPermission(ctx, auth.PermNotificationsSend); err != nil {
		t.Fatalf("expected the permission to be granted, got: %v", err)
	}
}

func TestCheckPermission_DeniesAKeyWithoutThePermission(t *testing.T) {
	ctx := auth.WithValidatedKey(context.Background(), &auth.ValidatedKey{
		ID:          "key_abc123",
		Permissions: []string{auth.PermNotificationsSend},
	})

	err := auth.CheckPermission(ctx, auth.PermAPIKeysManage)
	if !errors.Is(err, auth.ErrInsufficientPermission) {
		t.Fatalf("expected ErrInsufficientPermission, got: %v", err)
	}
	// The caller has to distinguish 403 from 401, so the two must not collapse.
	if errors.Is(err, auth.ErrNoAPIKey) {
		t.Error("a key that lacks a permission must not read as a missing key")
	}
}

// The critical one. The three inline checks this replaces were written
// `if key != nil && !HasPermission(...)`, which PASSES when the key is nil. That is
// fail-open: a route reached without a validated key was granted every permission.
// It was unreachable in production because the middleware 401s first, but it is exactly
// the kind of latent hazard that becomes live the moment a route is mounted differently.
func TestCheckPermission_FailsClosedWithNoKeyInContext(t *testing.T) {
	err := auth.CheckPermission(context.Background(), auth.PermNotificationsSend)

	if err == nil {
		t.Fatal("expected an error with no key in context; a missing key must never grant access")
	}
	if !errors.Is(err, auth.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got: %v", err)
	}
}

func TestCheckPermission_DeniesAKeyWithNoPermissionsAtAll(t *testing.T) {
	ctx := auth.WithValidatedKey(context.Background(), &auth.ValidatedKey{ID: "key_empty"})

	if err := auth.CheckPermission(ctx, auth.PermNotificationsSend); !errors.Is(err, auth.ErrInsufficientPermission) {
		t.Fatalf("expected ErrInsufficientPermission, got: %v", err)
	}
}

// SkipAuthMiddleware is covered in middleware_test.go, beside the implementation.

func TestValidatePermissions(t *testing.T) {
	valid := []string{auth.PermNotificationsSend, auth.PermTemplatesManage}
	if err := auth.ValidatePermissions(valid); err != nil {
		t.Fatalf("expected valid: %v", err)
	}

	invalid := []string{auth.PermNotificationsSend, "foo:bar"}
	if err := auth.ValidatePermissions(invalid); err == nil {
		t.Fatal("expected error for unknown permission")
	}
}
