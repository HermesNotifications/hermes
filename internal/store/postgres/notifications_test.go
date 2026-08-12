// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	id "github.com/hermes-notifications/hermes/internal/id/v2"
	"github.com/hermes-notifications/hermes/internal/models"
)

func TestCreateNotification_And_GetByID(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notifications", "users", "notification_templates", "subscription_categories", "organizations")

	ctx := context.Background()
	organizationID := uuid.New().String()
	_, err := s.CreateOrganization(ctx, organizationID, "Test Organization")
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}

	user, err := s.EnsureUser(ctx, organizationID, "ext-notif-1")
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}

	cat, err := s.CreateCategory(ctx, "general", "General", []string{"inbox"}, "on", 0)
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	notifID := id.Notification.New()
	n := &models.Notification{
		ID:         notifID,
		OrganizationID:   organizationID,
		UserID:     user.ID,
		CategoryID: cat.ID,
		Title:      "Hello",
		Body:       "World",
		Channels:   []string{"inbox"},
		Status:     models.StatusPending,
	}

	created, err := s.CreateNotification(ctx, n)
	if err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}
	if created.ID != notifID {
		t.Fatalf("expected ID %s, got %s", notifID, created.ID)
	}

	got, err := s.GetNotificationByID(ctx, notifID)
	if err != nil {
		t.Fatalf("GetNotificationByID: %v", err)
	}
	if got.Title != "Hello" {
		t.Fatalf("expected title Hello, got %s", got.Title)
	}
	if got.Body != "World" {
		t.Fatalf("expected body World, got %s", got.Body)
	}
	if got.UserID != user.ID {
		t.Fatalf("expected user_id %s, got %s", user.ID, got.UserID)
	}
}

func TestCreateNotification_MetadataRoundTrip(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notifications", "users", "notification_templates", "subscription_categories", "organizations")

	ctx := context.Background()
	organizationID := uuid.New().String()
	if _, err := s.CreateOrganization(ctx, organizationID, "Test Organization"); err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	user, err := s.EnsureUser(ctx, organizationID, "ext-metadata-1")
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}
	cat, err := s.CreateCategory(ctx, "general", "General", []string{"inbox"}, "on", 0)
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	notifID := id.Notification.New()
	if _, err := s.CreateNotification(ctx, &models.Notification{
		ID:             notifID,
		OrganizationID: organizationID,
		UserID:         user.ID,
		CategoryID:     cat.ID,
		Title:          "Hello",
		Body:           "World",
		Channels:       []string{"inbox"},
		Status:         models.StatusPending,
		Metadata: models.NotificationMetadata{
			"level":     "warning",
			"toast":     true,
			"invoiceId": "1041",
			"nested":    map[string]any{"tab": "billing"},
		},
	}); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	got, err := s.GetNotificationByID(ctx, notifID)
	if err != nil {
		t.Fatalf("GetNotificationByID: %v", err)
	}
	if level, ok := got.Metadata.Level(); !ok || level != "warning" {
		t.Errorf("level = (%q, %v), want (\"warning\", true)", level, ok)
	}
	if !got.Metadata.Toast() {
		t.Error("toast did not survive the round trip")
	}
	if got.Metadata["invoiceId"] != "1041" {
		t.Errorf("opaque key = %#v", got.Metadata["invoiceId"])
	}
	nested, ok := got.Metadata["nested"].(map[string]any)
	if !ok || nested["tab"] != "billing" {
		t.Errorf("nested object = %#v", got.Metadata["nested"])
	}
}

// A notification with no metadata must leave a real SQL NULL in the column.
//
// pgx writes NULL for a nil map today, so this passes with no special handling on the write
// side. It is here because the failure it guards against is invisible from Go: storing
// 'null'::jsonb instead would still scan back as a nil map, so every other assertion in this
// package would keep passing while `metadata IS NULL` quietly became false -- which is what any
// future partial-index or `WHERE metadata IS NOT NULL` query would be built on.
func TestCreateNotification_NoMetadataStoresSQLNull(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notifications", "users", "notification_templates", "subscription_categories", "organizations")

	ctx := context.Background()
	organizationID := uuid.New().String()
	if _, err := s.CreateOrganization(ctx, organizationID, "Test Organization"); err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	user, err := s.EnsureUser(ctx, organizationID, "ext-metadata-2")
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}
	cat, err := s.CreateCategory(ctx, "general", "General", []string{"inbox"}, "on", 0)
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	notifID := id.Notification.New()
	if _, err := s.CreateNotification(ctx, &models.Notification{
		ID:             notifID,
		OrganizationID: organizationID,
		UserID:         user.ID,
		CategoryID:     cat.ID,
		Title:          "Hello",
		Body:           "World",
		Channels:       []string{"inbox"},
		Status:         models.StatusPending,
	}); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	var isNull bool
	if err := pool.QueryRow(ctx,
		`SELECT metadata IS NULL FROM notifications WHERE id = $1`, notifID,
	).Scan(&isNull); err != nil {
		t.Fatalf("query metadata IS NULL: %v", err)
	}
	if !isNull {
		t.Error("metadata stored as 'null'::jsonb rather than SQL NULL")
	}

	got, err := s.GetNotificationByID(ctx, notifID)
	if err != nil {
		t.Fatalf("GetNotificationByID: %v", err)
	}
	if got.Metadata != nil {
		t.Errorf("metadata = %#v, want nil", got.Metadata)
	}
}

func TestGetNotificationByIdempotencyKey(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notifications", "users", "notification_templates", "subscription_categories", "organizations")

	ctx := context.Background()
	organizationID := uuid.New().String()
	_, err := s.CreateOrganization(ctx, organizationID, "Test Organization")
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}

	user, err := s.EnsureUser(ctx, organizationID, "ext-idem-1")
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}

	cat, err := s.CreateCategory(ctx, "alerts", "Alerts", []string{"inbox"}, "on", 0)
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	idemKey := "unique-key-" + id.Notification.New()
	n := &models.Notification{
		ID:             id.Notification.New(),
		OrganizationID:       organizationID,
		UserID:         user.ID,
		CategoryID:     cat.ID,
		Title:          "Alert",
		Body:           "Something happened",
		Channels:       []string{"inbox"},
		Status:         models.StatusPending,
		IdempotencyKey: &idemKey,
	}

	_, err = s.CreateNotification(ctx, n)
	if err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	got, err := s.GetNotificationByIdempotencyKey(ctx, organizationID, idemKey)
	if err != nil {
		t.Fatalf("GetNotificationByIdempotencyKey: %v", err)
	}
	if got.ID != n.ID {
		t.Fatalf("expected ID %s, got %s", n.ID, got.ID)
	}
	if *got.IdempotencyKey != idemKey {
		t.Fatalf("expected idempotency key %s, got %s", idemKey, *got.IdempotencyKey)
	}
}
