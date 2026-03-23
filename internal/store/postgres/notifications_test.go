//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/hermes-notifications/hermes/internal/id"
	"github.com/hermes-notifications/hermes/internal/models"
)

func TestCreateNotification_And_GetByID(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notifications", "users", "notification_types", "notification_groups", "tenants")

	ctx := context.Background()
	tenantID := uuid.New().String()
	_, err := s.CreateTenant(ctx, tenantID, "Test Tenant")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	user, err := s.EnsureUser(ctx, tenantID, "ext-notif-1")
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}

	group, err := s.CreateGroup(ctx, "general", "General", []string{"inbox"})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	notifID := id.New()
	n := &models.Notification{
		ID:       notifID,
		TenantID: tenantID,
		UserID:   user.ID,
		GroupID:  group.ID,
		Title:    "Hello",
		Body:     "World",
		Channels: []string{"inbox"},
		Status:   models.StatusPending,
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

func TestGetNotificationByIdempotencyKey(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notifications", "users", "notification_types", "notification_groups", "tenants")

	ctx := context.Background()
	tenantID := uuid.New().String()
	_, err := s.CreateTenant(ctx, tenantID, "Test Tenant")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	user, err := s.EnsureUser(ctx, tenantID, "ext-idem-1")
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}

	group, err := s.CreateGroup(ctx, "alerts", "Alerts", []string{"inbox"})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	idemKey := "unique-key-" + id.New()
	n := &models.Notification{
		ID:             id.New(),
		TenantID:       tenantID,
		UserID:         user.ID,
		GroupID:        group.ID,
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

	got, err := s.GetNotificationByIdempotencyKey(ctx, tenantID, idemKey)
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
