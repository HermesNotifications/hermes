//go:build integration

// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestUserContacts_DualWriteAndLoad(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "user_subscriptions", "notifications", "users", "subscription_categories", "organizations")
	ctx := context.Background()

	organizationID := uuid.New().String()
	if _, err := s.CreateOrganization(ctx, organizationID, "Contacts Organization"); err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	u, err := s.EnsureUser(ctx, organizationID, "ext-contacts-dualwrite")
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
