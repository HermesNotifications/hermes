//go:build integration

package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestUserPreferences_SetGetDeleteLifecycle(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "user_preferences", "users", "notification_groups", "tenants")

	ctx := context.Background()

	// Create tenant
	tenantID := uuid.New().String()
	_, err := s.CreateTenant(ctx, tenantID, "Pref Test Tenant")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	// Create user
	user, err := s.EnsureUser(ctx, tenantID, "ext-pref-001")
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}

	// Create group
	group, err := s.CreateGroup(ctx, "pref-group", "Pref Group", []string{"email"})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	// Set preference
	channels := []string{"email", "sms"}
	pref, err := s.SetUserPreference(ctx, user.ID, group.ID, channels)
	if err != nil {
		t.Fatalf("SetUserPreference: %v", err)
	}
	if pref.UserID != user.ID {
		t.Fatalf("expected user_id %s, got %s", user.ID, pref.UserID)
	}
	if pref.GroupID != group.ID {
		t.Fatalf("expected group_id %s, got %s", group.ID, pref.GroupID)
	}
	if len(pref.Channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(pref.Channels))
	}

	// Get preference
	got, err := s.GetUserPreference(ctx, user.ID, group.ID)
	if err != nil {
		t.Fatalf("GetUserPreference: %v", err)
	}
	if got.UserID != user.ID || got.GroupID != group.ID {
		t.Fatalf("got wrong preference: %+v", got)
	}
	if len(got.Channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(got.Channels))
	}

	// Update preference (upsert)
	updated, err := s.SetUserPreference(ctx, user.ID, group.ID, []string{"inbox"})
	if err != nil {
		t.Fatalf("SetUserPreference (update): %v", err)
	}
	if len(updated.Channels) != 1 || updated.Channels[0] != "inbox" {
		t.Fatalf("expected updated channels [inbox], got %v", updated.Channels)
	}

	// Delete preference
	if err := s.DeleteUserPreference(ctx, user.ID, group.ID); err != nil {
		t.Fatalf("DeleteUserPreference: %v", err)
	}

	// Verify gone — GetUserPreference should wrap pgx.ErrNoRows
	_, err = s.GetUserPreference(ctx, user.ID, group.ID)
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected pgx.ErrNoRows, got: %v", err)
	}
}

func TestGetUserPreference_NotFound(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "user_preferences")

	ctx := context.Background()
	_, err := s.GetUserPreference(ctx, "nonexistent-user", "nonexistent-group")
	if err == nil {
		t.Fatal("expected error for missing preference, got nil")
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected pgx.ErrNoRows, got: %v", err)
	}
}

func TestDeleteUserPreference_NotFound(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "user_preferences")

	ctx := context.Background()
	err := s.DeleteUserPreference(ctx, "nonexistent-user", "nonexistent-group")
	if err == nil {
		t.Fatal("expected error when deleting nonexistent preference, got nil")
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected pgx.ErrNoRows, got: %v", err)
	}
}
