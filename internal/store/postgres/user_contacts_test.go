//go:build integration

// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestUserContacts_DualWriteAndLoad(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "user_subscriptions", "notifications", "users", "subscription_categories", "tenants")
	ctx := context.Background()

	tenantID := uuid.New().String()
	if _, err := s.CreateTenant(ctx, tenantID, "Contacts Tenant"); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	u, err := s.EnsureUser(ctx, tenantID, "ext-contacts-dualwrite")
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}

	email, phone := "a@b.c", "+15551234"
	updated, err := s.UpdateUserContacts(ctx, u.ID, &email, &phone)
	if err != nil {
		t.Fatalf("UpdateUserContacts: %v", err)
	}
	if updated.Contacts["email"] != email || updated.Contacts["phone"] != phone {
		t.Fatalf("update contacts: %+v", updated.Contacts)
	}

	got, err := s.GetUserByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if got.Contacts["email"] != email || got.Contacts["phone"] != phone {
		t.Fatalf("reload contacts: %+v", got.Contacts)
	}
}
