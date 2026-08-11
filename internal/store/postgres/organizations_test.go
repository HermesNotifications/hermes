// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestCreateOrganization_And_GetByID(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notifications", "users", "notification_templates", "subscriptions", "subscription_categories", "organizations")

	ctx := context.Background()
	organizationID := uuid.New().String()
	organization, err := s.CreateOrganization(ctx, organizationID, "Test Organization")
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}

	got, err := s.GetOrganizationByID(ctx, organization.ID)
	if err != nil {
		t.Fatalf("GetOrganizationByID: %v", err)
	}
	if got.Name != "Test Organization" {
		t.Fatalf("expected Test Organization, got %s", got.Name)
	}
}
