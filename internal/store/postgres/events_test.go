// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	id "github.com/hermesnotifications/hermes/internal/id/v2"
	"github.com/hermesnotifications/hermes/internal/models"
)

func TestEventInsertAndStatusRollup(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notification_events", "notifications", "users", "subscription_categories", "organizations")

	ctx := context.Background()

	// 1. Create organization, user, category, notification (status: pending)
	organizationID := uuid.New().String()
	_, err := s.CreateOrganization(ctx, organizationID, "Event Rollup Organization")
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}

	user, err := s.EnsureUser(ctx, organizationID, "ext-event-rollup-1")
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}

	cat, err := s.CreateCategory(ctx, "events-test-cat", "Events Test Category", []string{"email", "inbox"}, "on", 0)
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	notifID := id.Notification.New()
	n := &models.Notification{
		ID:         notifID,
		OrganizationID:   organizationID,
		UserID:     user.ID,
		CategoryID: cat.ID,
		Title:      "Test Notification",
		Body:       "Testing event insert and status rollup",
		Channels:   []string{"email", "inbox"},
		Status:     models.StatusPending,
	}

	_, err = s.CreateNotification(ctx, n)
	if err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	// 2. Insert 2 events via InsertEvents (email.routed, inbox.routed)
	events := []models.NotificationEvent{
		{
			NotificationID: notifID,
			Channel:        "email",
			Event:          "email.routed",
			Severity:       "info",
			Metadata:       []byte(`{"provider":"sendgrid"}`),
		},
		{
			NotificationID: notifID,
			Channel:        "inbox",
			Event:          "inbox.routed",
			Severity:       "info",
			Metadata:       []byte(`{"destination":"user_inbox"}`),
		},
	}

	if err := s.InsertEvents(ctx, events); err != nil {
		t.Fatalf("InsertEvents: %v", err)
	}

	// 3. Verify events via GetNotificationEvents
	got, err := s.GetNotificationEvents(ctx, notifID)
	if err != nil {
		t.Fatalf("GetNotificationEvents: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d", len(got))
	}

	// Verify event IDs were generated (non-empty)
	for i, e := range got {
		if e.ID == "" {
			t.Fatalf("event[%d] has empty ID", i)
		}
		if e.NotificationID != notifID {
			t.Fatalf("event[%d] notification_id: expected %s, got %s", i, notifID, e.NotificationID)
		}
	}

	// 4. UpdateNotificationStatus to `sent` — verify status and sent_at
	sentTime := time.Now().UTC().Truncate(time.Millisecond)
	if err := s.UpdateNotificationStatus(ctx, notifID, models.StatusSent, sentTime); err != nil {
		t.Fatalf("UpdateNotificationStatus(sent): %v", err)
	}

	notif, err := s.GetNotificationByID(ctx, notifID)
	if err != nil {
		t.Fatalf("GetNotificationByID after sent: %v", err)
	}
	if notif.Status != models.StatusSent {
		t.Fatalf("expected status 'sent', got %q", notif.Status)
	}
	if notif.SentAt == nil {
		t.Fatal("expected sent_at to be set, got nil")
	}

	// 5. UpdateNotificationStatus to `delivered` — verify status and delivered_at
	deliveredTime := sentTime.Add(time.Second)
	if err := s.UpdateNotificationStatus(ctx, notifID, models.StatusDelivered, deliveredTime); err != nil {
		t.Fatalf("UpdateNotificationStatus(delivered): %v", err)
	}

	notif, err = s.GetNotificationByID(ctx, notifID)
	if err != nil {
		t.Fatalf("GetNotificationByID after delivered: %v", err)
	}
	if notif.Status != models.StatusDelivered {
		t.Fatalf("expected status 'delivered', got %q", notif.Status)
	}
	if notif.DeliveredAt == nil {
		t.Fatal("expected delivered_at to be set, got nil")
	}

	// 6. Attempt regression to `sent` — verify status stays `delivered` (no regression)
	if err := s.UpdateNotificationStatus(ctx, notifID, models.StatusSent, sentTime); err != nil {
		t.Fatalf("UpdateNotificationStatus(regression attempt): %v", err)
	}

	notif, err = s.GetNotificationByID(ctx, notifID)
	if err != nil {
		t.Fatalf("GetNotificationByID after regression attempt: %v", err)
	}
	if notif.Status != models.StatusDelivered {
		t.Fatalf("status regression occurred: expected 'delivered', got %q", notif.Status)
	}
}

func TestDeleteEventsOlderThan(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notification_events", "notifications", "users", "subscription_categories", "organizations")

	ctx := context.Background()

	// Setup: organization, user, category, notification.
	organizationID := uuid.New().String()
	_, err := s.CreateOrganization(ctx, organizationID, "Retention Test Organization")
	if err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}

	user, err := s.EnsureUser(ctx, organizationID, "ext-retention-1")
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}

	cat, err := s.CreateCategory(ctx, "retention-test-cat", "Retention Test Category", []string{"email"}, "on", 0)
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	notifID := id.Notification.New()
	n := &models.Notification{
		ID:         notifID,
		OrganizationID:   organizationID,
		UserID:     user.ID,
		CategoryID: cat.ID,
		Title:      "Retention Test",
		Body:       "Testing event retention cleanup",
		Channels:   []string{"email"},
		Status:     models.StatusPending,
	}
	if _, err := s.CreateNotification(ctx, n); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	// Insert 3 events: 2 old (>90 days), 1 recent.
	now := time.Now().UTC()
	oldTime := now.Add(-91 * 24 * time.Hour)

	oldEvents := []models.NotificationEvent{
		{
			ID:             id.Notification.New(),
			NotificationID: notifID,
			Channel:        "email",
			Event:          "email.routed",
			Severity:       "info",
		},
		{
			ID:             id.Notification.New(),
			NotificationID: notifID,
			Channel:        "email",
			Event:          "email.sent",
			Severity:       "info",
		},
	}
	if err := s.InsertEvents(ctx, oldEvents); err != nil {
		t.Fatalf("InsertEvents (old): %v", err)
	}

	// Backdate the old events.
	_, err = pool.Exec(ctx, "UPDATE notification_events SET created_at = $1 WHERE id = ANY($2)",
		oldTime, []string{oldEvents[0].ID, oldEvents[1].ID})
	if err != nil {
		t.Fatalf("backdate events: %v", err)
	}

	recentEvent := []models.NotificationEvent{
		{
			ID:             id.Notification.New(),
			NotificationID: notifID,
			Channel:        "email",
			Event:          "email.delivered",
			Severity:       "info",
		},
	}
	if err := s.InsertEvents(ctx, recentEvent); err != nil {
		t.Fatalf("InsertEvents (recent): %v", err)
	}

	// Delete events older than 90 days.
	cutoff := now.Add(-90 * 24 * time.Hour)
	deleted, err := s.DeleteEventsOlderThan(ctx, cutoff, 100)
	if err != nil {
		t.Fatalf("DeleteEventsOlderThan: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("expected 2 deleted, got %d", deleted)
	}

	// Verify only the recent event remains.
	remaining, err := s.GetNotificationEvents(ctx, notifID)
	if err != nil {
		t.Fatalf("GetNotificationEvents: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("expected 1 remaining event, got %d", len(remaining))
	}
	if remaining[0].ID != recentEvent[0].ID {
		t.Fatalf("remaining event ID mismatch: expected %s, got %s", recentEvent[0].ID, remaining[0].ID)
	}

	// Second call should return 0.
	deleted, err = s.DeleteEventsOlderThan(ctx, cutoff, 100)
	if err != nil {
		t.Fatalf("DeleteEventsOlderThan (second call): %v", err)
	}
	if deleted != 0 {
		t.Fatalf("expected 0 deleted on second call, got %d", deleted)
	}
}
