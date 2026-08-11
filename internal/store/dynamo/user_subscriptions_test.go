// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

//go:build integration

package dynamo_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hermes-notifications/hermes/internal/store/dynamo"
	"github.com/jackc/pgx/v5"
)

func testUserSubStore(t *testing.T) *dynamo.UserSubscriptionStore {
	t.Helper()
	return dynamo.NewUserSubscriptionStore(testClient(t))
}

// TestGetUserSubscription_Miss returns pgx.ErrNoRows for a non-existent item,
// matching the Postgres store's behaviour.
func TestGetUserSubscription_Miss(t *testing.T) {
	st := testUserSubStore(t)
	ctx := context.Background()

	_, err := st.GetUserSubscription(ctx, uuid.New().String(), uuid.New().String())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("expected pgx.ErrNoRows, got %v", err)
	}
}

// TestSetUserSubscription_Create creates a new item and reads it back.
func TestSetUserSubscription_Create(t *testing.T) {
	st := testUserSubStore(t)
	ctx := context.Background()

	userID := uuid.New().String()
	subID := uuid.New().String()

	us, err := st.SetUserSubscription(ctx, userID, subID, true)
	if err != nil {
		t.Fatalf("SetUserSubscription: %v", err)
	}
	if us.UserID != userID {
		t.Errorf("UserID: want %s, got %s", userID, us.UserID)
	}
	if us.SubscriptionID != subID {
		t.Errorf("SubscriptionID: want %s, got %s", subID, us.SubscriptionID)
	}
	if !us.OptedIn {
		t.Error("OptedIn: want true, got false")
	}
	if us.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}

	// Read back via GetUserSubscription
	got, err := st.GetUserSubscription(ctx, userID, subID)
	if err != nil {
		t.Fatalf("GetUserSubscription: %v", err)
	}
	if got.UserID != userID || got.SubscriptionID != subID || !got.OptedIn {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

// TestSetUserSubscription_Upsert_PreservesCreatedAt verifies that a second call with
// a different opted_in value updates opted_in but preserves the original created_at.
func TestSetUserSubscription_Upsert_PreservesCreatedAt(t *testing.T) {
	st := testUserSubStore(t)
	ctx := context.Background()

	userID := uuid.New().String()
	subID := uuid.New().String()

	// First write — opted in
	first, err := st.SetUserSubscription(ctx, userID, subID, true)
	if err != nil {
		t.Fatalf("SetUserSubscription (create): %v", err)
	}

	// Small sleep to ensure a later wall clock time won't match.
	time.Sleep(5 * time.Millisecond)

	// Second write — opt out
	second, err := st.SetUserSubscription(ctx, userID, subID, false)
	if err != nil {
		t.Fatalf("SetUserSubscription (update): %v", err)
	}
	if second.OptedIn {
		t.Error("OptedIn: want false after update, got true")
	}
	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Errorf("CreatedAt changed: first=%v second=%v", first.CreatedAt, second.CreatedAt)
	}

	// Confirm via read
	got, err := st.GetUserSubscription(ctx, userID, subID)
	if err != nil {
		t.Fatalf("GetUserSubscription: %v", err)
	}
	if got.OptedIn {
		t.Error("read-back OptedIn: want false, got true")
	}
	if !got.CreatedAt.Equal(first.CreatedAt) {
		t.Errorf("read-back CreatedAt changed: first=%v got=%v", first.CreatedAt, got.CreatedAt)
	}
}

// TestGetUserSubscriptions_Empty returns an empty (non-nil) slice for a user with
// no subscriptions.
func TestGetUserSubscriptions_Empty(t *testing.T) {
	st := testUserSubStore(t)
	ctx := context.Background()

	got, err := st.GetUserSubscriptions(ctx, uuid.New().String())
	if err != nil {
		t.Fatalf("GetUserSubscriptions: %v", err)
	}
	if got == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("expected 0 subscriptions, got %d", len(got))
	}
}

// TestGetUserSubscriptions_Multiple verifies that all subscriptions for a user are
// returned, and that subscriptions for other users are not included.
func TestGetUserSubscriptions_Multiple(t *testing.T) {
	st := testUserSubStore(t)
	ctx := context.Background()

	userID := uuid.New().String()
	otherUserID := uuid.New().String()
	subIDs := []string{uuid.New().String(), uuid.New().String(), uuid.New().String()}

	for _, subID := range subIDs {
		if _, err := st.SetUserSubscription(ctx, userID, subID, true); err != nil {
			t.Fatalf("SetUserSubscription(%s): %v", subID, err)
		}
	}
	// Add a subscription for a different user — must not appear in results.
	if _, err := st.SetUserSubscription(ctx, otherUserID, uuid.New().String(), true); err != nil {
		t.Fatalf("SetUserSubscription(other user): %v", err)
	}

	got, err := st.GetUserSubscriptions(ctx, userID)
	if err != nil {
		t.Fatalf("GetUserSubscriptions: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 subscriptions, got %d", len(got))
	}
	seen := make(map[string]bool)
	for _, us := range got {
		if us.UserID != userID {
			t.Errorf("found subscription with wrong user_id %s", us.UserID)
		}
		seen[us.SubscriptionID] = true
	}
	for _, subID := range subIDs {
		if !seen[subID] {
			t.Errorf("subscription %s missing from results", subID)
		}
	}
}

// TestDeleteUserSubscription removes an item and confirms subsequent reads return a miss.
func TestDeleteUserSubscription(t *testing.T) {
	st := testUserSubStore(t)
	ctx := context.Background()

	userID := uuid.New().String()
	subID := uuid.New().String()

	if _, err := st.SetUserSubscription(ctx, userID, subID, true); err != nil {
		t.Fatalf("SetUserSubscription: %v", err)
	}

	if err := st.DeleteUserSubscription(ctx, userID, subID); err != nil {
		t.Fatalf("DeleteUserSubscription: %v", err)
	}

	_, err := st.GetUserSubscription(ctx, userID, subID)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("expected pgx.ErrNoRows after delete, got %v", err)
	}

	// GetUserSubscriptions should also return empty.
	all, err := st.GetUserSubscriptions(ctx, userID)
	if err != nil {
		t.Fatalf("GetUserSubscriptions after delete: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("expected 0 subscriptions after delete, got %d", len(all))
	}
}

// TestDeleteUserSubscription_NotFound returns pgx.ErrNoRows when deleting an item
// that doesn't exist, matching the Postgres behaviour.
func TestDeleteUserSubscription_NotFound(t *testing.T) {
	st := testUserSubStore(t)
	ctx := context.Background()

	err := st.DeleteUserSubscription(ctx, uuid.New().String(), uuid.New().String())
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("expected pgx.ErrNoRows, got %v", err)
	}
}
