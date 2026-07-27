// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

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
