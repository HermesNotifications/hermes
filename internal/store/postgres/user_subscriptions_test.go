// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestUserSubscriptions_SetGetDeleteLifecycle(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "user_subscriptions", "subscriptions", "subscription_categories", "users", "organizations")

	ctx := context.Background()

	// Create organization
	organizationID := uuid.New().String()
	_, err := s.CreateOrganization(ctx, organizationID, "Sub Test Organization")
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}

	// Create user
	user, err := s.EnsureUser(ctx, organizationID, "ext-sub-001")
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}

	// Create category + subscription
	cat, err := s.CreateCategory(ctx, "sub-cat", "Sub Cat", []string{"email"}, "on", 0)
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	sub, err := s.CreateSubscription(ctx, cat.ID, "sub-item", "Sub Item", 0)
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}

	// Set subscription opt-in
	us, err := s.SetUserSubscription(ctx, user.ID, sub.ID, true)
	if err != nil {
		t.Fatalf("SetUserSubscription: %v", err)
	}
	if us.UserID != user.ID {
		t.Fatalf("expected user_id %s, got %s", user.ID, us.UserID)
	}
	if us.SubscriptionID != sub.ID {
		t.Fatalf("expected subscription_id %s, got %s", sub.ID, us.SubscriptionID)
	}
	if !us.OptedIn {
		t.Fatal("expected opted_in=true")
	}

	// Get subscription
	got, err := s.GetUserSubscription(ctx, user.ID, sub.ID)
	if err != nil {
		t.Fatalf("GetUserSubscription: %v", err)
	}
	if got.UserID != user.ID || got.SubscriptionID != sub.ID {
		t.Fatalf("got wrong subscription: %+v", got)
	}

	// Update (upsert to opt-out)
	updated, err := s.SetUserSubscription(ctx, user.ID, sub.ID, false)
	if err != nil {
		t.Fatalf("SetUserSubscription (update): %v", err)
	}
	if updated.OptedIn {
		t.Fatal("expected opted_in=false after update")
	}

	// Delete subscription
	if err := s.DeleteUserSubscription(ctx, user.ID, sub.ID); err != nil {
		t.Fatalf("DeleteUserSubscription: %v", err)
	}

	// Verify gone — GetUserSubscription should wrap pgx.ErrNoRows
	_, err = s.GetUserSubscription(ctx, user.ID, sub.ID)
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected pgx.ErrNoRows, got: %v", err)
	}
}

func TestGetUserSubscription_NotFound(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "user_subscriptions")

	ctx := context.Background()
	_, err := s.GetUserSubscription(ctx, "nonexistent-user", "nonexistent-sub")
	if err == nil {
		t.Fatal("expected error for missing subscription, got nil")
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected pgx.ErrNoRows, got: %v", err)
	}
}

func TestDeleteUserSubscription_NotFound(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "user_subscriptions")

	ctx := context.Background()
	err := s.DeleteUserSubscription(ctx, "nonexistent-user", "nonexistent-sub")
	if err == nil {
		t.Fatal("expected error when deleting nonexistent subscription, got nil")
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected pgx.ErrNoRows, got: %v", err)
	}
}
