//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestEnsureUser_CreatesOnFirstCall(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notifications", "users", "notification_types", "notification_groups", "tenants")

	ctx := context.Background()
	tenantID := uuid.New().String()
	_, err := s.CreateTenant(ctx, tenantID, "Test Tenant")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	user, err := s.EnsureUser(ctx, tenantID, "ext-123")
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}
	if user.ExternalID != "ext-123" {
		t.Fatalf("expected external_id ext-123, got %s", user.ExternalID)
	}
	if user.TenantID != tenantID {
		t.Fatalf("expected tenant_id %s, got %s", tenantID, user.TenantID)
	}
}

func TestEnsureUser_ReturnsSameOnSecondCall(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notifications", "users", "notification_types", "notification_groups", "tenants")

	ctx := context.Background()
	tenantID := uuid.New().String()
	_, err := s.CreateTenant(ctx, tenantID, "Test Tenant")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	first, err := s.EnsureUser(ctx, tenantID, "ext-456")
	if err != nil {
		t.Fatalf("EnsureUser first: %v", err)
	}

	second, err := s.EnsureUser(ctx, tenantID, "ext-456")
	if err != nil {
		t.Fatalf("EnsureUser second: %v", err)
	}

	if first.ID != second.ID {
		t.Fatalf("expected same ID on second call, got %s and %s", first.ID, second.ID)
	}
}

func TestUpdateUserContacts(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "user_preferences", "notifications", "users", "notification_types", "notification_groups", "tenants")

	ctx := context.Background()
	tenantID := uuid.New().String()
	_, err := s.CreateTenant(ctx, tenantID, "Contacts Test Tenant")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	user, err := s.EnsureUser(ctx, tenantID, "ext-contacts-1")
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}

	// Update email only
	email := "test@example.com"
	updated, err := s.UpdateUserContacts(ctx, user.ID, &email, nil)
	if err != nil {
		t.Fatalf("UpdateUserContacts (email): %v", err)
	}
	if updated.Email == nil || *updated.Email != email {
		t.Fatalf("expected email %q, got %v", email, updated.Email)
	}
	if updated.Phone != nil {
		t.Fatalf("expected nil phone, got %v", updated.Phone)
	}

	// Update phone only — email should remain
	phone := "+15551234567"
	updated, err = s.UpdateUserContacts(ctx, user.ID, nil, &phone)
	if err != nil {
		t.Fatalf("UpdateUserContacts (phone): %v", err)
	}
	if updated.Phone == nil || *updated.Phone != phone {
		t.Fatalf("expected phone %q, got %v", phone, updated.Phone)
	}
	if updated.Email == nil || *updated.Email != email {
		t.Fatalf("expected email still %q, got %v", email, updated.Email)
	}

	// Update both
	email2 := "new@example.com"
	phone2 := "+15559999999"
	updated, err = s.UpdateUserContacts(ctx, user.ID, &email2, &phone2)
	if err != nil {
		t.Fatalf("UpdateUserContacts (both): %v", err)
	}
	if updated.Email == nil || *updated.Email != email2 {
		t.Fatalf("expected email %q, got %v", email2, updated.Email)
	}
	if updated.Phone == nil || *updated.Phone != phone2 {
		t.Fatalf("expected phone %q, got %v", phone2, updated.Phone)
	}
}

func TestGetUserPreferences(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "user_preferences", "notifications", "users", "notification_types", "notification_groups", "tenants")

	ctx := context.Background()
	tenantID := uuid.New().String()
	_, err := s.CreateTenant(ctx, tenantID, "Prefs List Tenant")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	user, err := s.EnsureUser(ctx, tenantID, "ext-prefs-list-1")
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}

	// Empty preferences
	prefs, err := s.GetUserPreferences(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserPreferences (empty): %v", err)
	}
	if len(prefs) != 0 {
		t.Fatalf("expected 0 preferences, got %d", len(prefs))
	}

	// Create two groups and set preferences
	g1, err := s.CreateGroup(ctx, "prefs-group-1", "Group 1", []string{"email"})
	if err != nil {
		t.Fatalf("CreateGroup 1: %v", err)
	}
	g2, err := s.CreateGroup(ctx, "prefs-group-2", "Group 2", []string{"sms"})
	if err != nil {
		t.Fatalf("CreateGroup 2: %v", err)
	}

	_, err = s.SetUserPreference(ctx, user.ID, g1.ID, []string{"email", "inbox"})
	if err != nil {
		t.Fatalf("SetUserPreference 1: %v", err)
	}
	_, err = s.SetUserPreference(ctx, user.ID, g2.ID, []string{"sms"})
	if err != nil {
		t.Fatalf("SetUserPreference 2: %v", err)
	}

	prefs, err = s.GetUserPreferences(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserPreferences: %v", err)
	}
	if len(prefs) != 2 {
		t.Fatalf("expected 2 preferences, got %d", len(prefs))
	}
}
