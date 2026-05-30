// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestEnsureUser_CreatesOnFirstCall(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notifications", "users", "notification_templates", "subscription_categories", "tenants")

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
	cleanTable(t, pool, "notifications", "users", "notification_templates", "subscription_categories", "tenants")

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
	cleanTable(t, pool, "user_subscriptions", "notifications", "users", "notification_templates", "subscription_categories", "tenants")

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

func TestGetUserSubscriptions(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "user_subscriptions", "notifications", "users", "notification_templates", "subscription_categories", "tenants")

	ctx := context.Background()
	tenantID := uuid.New().String()
	_, err := s.CreateTenant(ctx, tenantID, "Subs List Tenant")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	user, err := s.EnsureUser(ctx, tenantID, "ext-subs-list-1")
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}

	// Empty subscriptions
	subs, err := s.GetUserSubscriptions(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserSubscriptions (empty): %v", err)
	}
	if len(subs) != 0 {
		t.Fatalf("expected 0 subscriptions, got %d", len(subs))
	}

	// Create two categories + subscriptions and set user subscriptions
	cat1, err := s.CreateCategory(ctx, "subs-cat-1", "Category 1", []string{"email"}, "on", 0)
	if err != nil {
		t.Fatalf("CreateCategory 1: %v", err)
	}
	cat2, err := s.CreateCategory(ctx, "subs-cat-2", "Category 2", []string{"sms"}, "off", 1)
	if err != nil {
		t.Fatalf("CreateCategory 2: %v", err)
	}

	sub1, err := s.CreateSubscription(ctx, cat1.ID, "subs-item-1", "Sub Item 1", 0)
	if err != nil {
		t.Fatalf("CreateSubscription 1: %v", err)
	}
	sub2, err := s.CreateSubscription(ctx, cat2.ID, "subs-item-2", "Sub Item 2", 0)
	if err != nil {
		t.Fatalf("CreateSubscription 2: %v", err)
	}

	_, err = s.SetUserSubscription(ctx, user.ID, sub1.ID, true)
	if err != nil {
		t.Fatalf("SetUserSubscription 1: %v", err)
	}
	_, err = s.SetUserSubscription(ctx, user.ID, sub2.ID, false)
	if err != nil {
		t.Fatalf("SetUserSubscription 2: %v", err)
	}

	subs, err = s.GetUserSubscriptions(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserSubscriptions: %v", err)
	}
	if len(subs) != 2 {
		t.Fatalf("expected 2 subscriptions, got %d", len(subs))
	}
}
