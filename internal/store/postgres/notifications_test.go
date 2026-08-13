// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	id "github.com/hermesnotifications/hermes/internal/id/v2"
	"github.com/hermesnotifications/hermes/internal/models"
	"github.com/hermesnotifications/hermes/internal/store/postgres"
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

// seedForNotifications creates the organization, user and category a notification row needs, and
// returns a builder for rows that satisfy its foreign keys.
func seedForNotifications(t *testing.T, s *postgres.Store) func(id string) *models.Notification {
	t.Helper()
	ctx := context.Background()

	organizationID := uuid.New().String()
	if _, err := s.CreateOrganization(ctx, organizationID, "Batch Test Organization"); err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	user, err := s.EnsureUser(ctx, organizationID, "ext-batch-1")
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}
	cat, err := s.CreateCategory(ctx, "general", "General", []string{"inbox"}, "on", 0)
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	return func(id string) *models.Notification {
		return &models.Notification{
			ID:             id,
			OrganizationID: organizationID,
			UserID:         user.ID,
			CategoryID:     cat.ID,
			Title:          "Hello",
			Body:           "World",
			Channels:       []string{"inbox"},
			Status:         models.StatusPending,
		}
	}
}

func TestCreateNotifications_InsertsEveryRow(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notifications", "users", "notification_templates", "subscription_categories", "organizations")
	newNotification := seedForNotifications(t, s)

	ctx := context.Background()
	ns := []*models.Notification{
		newNotification(id.Notification.New()),
		newNotification(id.Notification.New()),
		newNotification(id.Notification.New()),
	}

	inserted, err := s.CreateNotifications(ctx, ns)
	if err != nil {
		t.Fatalf("CreateNotifications: %v", err)
	}
	if len(inserted) != 3 {
		t.Fatalf("inserted = %v, want all three IDs", inserted)
	}
	for _, n := range ns {
		got, err := s.GetNotificationByID(ctx, n.ID)
		if err != nil {
			t.Fatalf("GetNotificationByID(%s): %v", n.ID, err)
		}
		if got.Title != "Hello" || got.Status != models.StatusPending {
			t.Errorf("row %s = %+v, want the values it was inserted with", n.ID, got)
		}
		// The single-row path fills CreatedAt from RETURNING; the batch must too, or the two
		// paths hand the caller back different objects for the same operation.
		if n.CreatedAt.IsZero() {
			t.Errorf("row %s: CreatedAt not populated", n.ID)
		}
	}
}

// The redelivery case, which is the common one: dispatch re-persists a notification ID it has
// already written. That row must be skipped without taking the rest of the batch with it —
// before batching, the duplicate error belonged to one message and one message only.
func TestCreateNotifications_SkipsDuplicatesWithoutFailingTheBatch(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notifications", "users", "notification_templates", "subscription_categories", "organizations")
	newNotification := seedForNotifications(t, s)

	ctx := context.Background()
	already := newNotification(id.Notification.New())
	if _, err := s.CreateNotification(ctx, already); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	fresh := newNotification(id.Notification.New())
	other := newNotification(id.Notification.New())
	inserted, err := s.CreateNotifications(ctx, []*models.Notification{fresh, newNotification(already.ID), other})
	if err != nil {
		t.Fatalf("CreateNotifications with a duplicate: %v", err)
	}
	if len(inserted) != 2 {
		t.Fatalf("inserted = %v, want only the two new IDs", inserted)
	}
	for _, gotID := range inserted {
		if gotID == already.ID {
			t.Errorf("the pre-existing row %s was reported as inserted", already.ID)
		}
	}
	for _, n := range []*models.Notification{fresh, other} {
		if _, err := s.GetNotificationByID(ctx, n.ID); err != nil {
			t.Errorf("row %s was lost to its duplicate neighbour: %v", n.ID, err)
		}
	}
}

// The idempotency key has its own partial unique index, so it is a second way to collide. The
// bare ON CONFLICT DO NOTHING covers both, which is why it carries no conflict target.
func TestCreateNotifications_SkipsIdempotencyKeyCollisions(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notifications", "users", "notification_templates", "subscription_categories", "organizations")
	newNotification := seedForNotifications(t, s)

	ctx := context.Background()
	key := "idem-" + id.Notification.New()

	first := newNotification(id.Notification.New())
	first.IdempotencyKey = &key
	second := newNotification(id.Notification.New()) // different ID, same key
	second.IdempotencyKey = &key
	third := newNotification(id.Notification.New())

	inserted, err := s.CreateNotifications(ctx, []*models.Notification{first, second, third})
	if err != nil {
		t.Fatalf("CreateNotifications with an idempotency-key collision: %v", err)
	}
	if len(inserted) != 2 {
		t.Fatalf("inserted = %v, want two rows (the key collision skipped)", inserted)
	}
	if _, err := s.GetNotificationByID(ctx, third.ID); err != nil {
		t.Errorf("the unrelated row was lost to the key collision: %v", err)
	}
}

// A row the database will never accept must abort its own transaction and leave nothing behind,
// so the caller can tell "none of this landed" from "some of it did" and retry each row alone.
func TestCreateNotifications_PoisonRowRollsBackTheWholeBatch(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notifications", "users", "notification_templates", "subscription_categories", "organizations")
	newNotification := seedForNotifications(t, s)

	ctx := context.Background()
	good := newNotification(id.Notification.New())
	poison := newNotification(id.Notification.New())
	poison.UserID = "usr_does_not_exist" // violates the users foreign key

	if _, err := s.CreateNotifications(ctx, []*models.Notification{good, poison}); err == nil {
		t.Fatal("CreateNotifications = nil error with a foreign-key violation in the batch")
	}
	if _, err := s.GetNotificationByID(ctx, good.ID); err == nil {
		t.Error("a row from a failed batch was persisted; the transaction did not roll back")
	}

	// And the row that was blameless goes in on the caller's per-row retry.
	if _, err := s.CreateNotification(ctx, good); err != nil {
		t.Fatalf("per-row retry after a failed batch: %v", err)
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
