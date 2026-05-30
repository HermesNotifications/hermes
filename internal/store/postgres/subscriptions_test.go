// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

//go:build integration

package postgres_test

import (
	"context"
	"testing"
)

func TestCreateSubscription_And_GetByID(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "subscriptions", "subscription_categories")

	ctx := context.Background()
	cat, _ := s.CreateCategory(ctx, "billing", "Billing", []string{"email"}, "on", 0)

	sub, err := s.CreateSubscription(ctx, cat.ID, "invoices", "Invoices", 0)
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}

	got, err := s.GetSubscriptionByID(ctx, sub.ID)
	if err != nil {
		t.Fatalf("GetSubscriptionByID: %v", err)
	}
	if got.Slug != "invoices" {
		t.Errorf("expected slug 'invoices', got %q", got.Slug)
	}
	if got.CategoryID != cat.ID {
		t.Errorf("expected category_id %q, got %q", cat.ID, got.CategoryID)
	}
}

func TestListSubscriptionsByCategory(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "subscriptions", "subscription_categories")

	ctx := context.Background()
	cat, _ := s.CreateCategory(ctx, "billing", "Billing", []string{"email"}, "on", 0)

	s.CreateSubscription(ctx, cat.ID, "invoices", "Invoices", 0)
	s.CreateSubscription(ctx, cat.ID, "payments", "Payments", 1)

	subs, err := s.ListSubscriptionsByCategory(ctx, cat.ID)
	if err != nil {
		t.Fatalf("ListSubscriptionsByCategory: %v", err)
	}
	if len(subs) != 2 {
		t.Fatalf("expected 2 subscriptions, got %d", len(subs))
	}
}

func TestCreateSubscription_DuplicateSlugInCategory(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "subscriptions", "subscription_categories")

	ctx := context.Background()
	cat, _ := s.CreateCategory(ctx, "billing", "Billing", []string{"email"}, "on", 0)

	_, err := s.CreateSubscription(ctx, cat.ID, "invoices", "Invoices", 0)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}

	_, err = s.CreateSubscription(ctx, cat.ID, "invoices", "Invoices Duplicate", 1)
	if err == nil {
		t.Fatal("expected unique constraint error on duplicate slug in same category, got nil")
	}
}

func TestCreateSubscription_DuplicateSlugDifferentCategory(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "subscriptions", "subscription_categories")

	ctx := context.Background()
	cat1, _ := s.CreateCategory(ctx, "billing", "Billing", []string{"email"}, "on", 0)
	cat2, _ := s.CreateCategory(ctx, "security", "Security", []string{"email"}, "required", 1)

	_, err := s.CreateSubscription(ctx, cat1.ID, "alerts", "Billing Alerts", 0)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}

	// Same slug in different category should succeed (composite unique index)
	_, err = s.CreateSubscription(ctx, cat2.ID, "alerts", "Security Alerts", 0)
	if err != nil {
		t.Fatalf("expected success for same slug in different category, got: %v", err)
	}
}

func TestCreateSubscription_InvalidCategoryID(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "subscriptions", "subscription_categories")

	ctx := context.Background()
	_, err := s.CreateSubscription(ctx, "sct-nonexistent", "invoices", "Invoices", 0)
	if err == nil {
		t.Fatal("expected FK violation error, got nil")
	}
}

func TestDeleteSubscription(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "subscriptions", "subscription_categories")

	ctx := context.Background()
	cat, _ := s.CreateCategory(ctx, "billing", "Billing", []string{"email"}, "on", 0)
	sub, _ := s.CreateSubscription(ctx, cat.ID, "invoices", "Invoices", 0)

	if err := s.DeleteSubscription(ctx, sub.ID); err != nil {
		t.Fatalf("DeleteSubscription: %v", err)
	}

	_, err := s.GetSubscriptionByID(ctx, sub.ID)
	if err == nil {
		t.Fatal("expected error after delete")
	}
}
