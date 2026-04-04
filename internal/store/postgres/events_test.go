//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	id "github.com/hermes-notifications/hermes/internal/id/v2"
	"github.com/hermes-notifications/hermes/internal/models"
)

func TestEventInsertAndStatusRollup(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notification_events", "notifications", "users", "subscription_categories", "tenants")

	ctx := context.Background()

	// 1. Create tenant, user, category, notification (status: pending)
	tenantID := uuid.Notification.New().String()
	_, err := s.CreateTenant(ctx, tenantID, "Event Rollup Tenant")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	user, err := s.EnsureUser(ctx, tenantID, "ext-event-rollup-1")
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
		TenantID:   tenantID,
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
