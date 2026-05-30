// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

//go:build integration

package postgres_test

import (
	"context"
	"testing"
)

func TestCreateCategory_And_GetBySlug(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "subscription_categories")

	ctx := context.Background()
	c, err := s.CreateCategory(ctx, "billing", "Billing", []string{"email", "inbox"}, "on", 0)
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	if c.Slug != "billing" {
		t.Fatalf("expected slug billing, got %s", c.Slug)
	}

	got, err := s.GetCategoryBySlug(ctx, "billing")
	if err != nil {
		t.Fatalf("GetCategoryBySlug: %v", err)
	}
	if got.ID != c.ID {
		t.Fatalf("expected ID %s, got %s", c.ID, got.ID)
	}
}

func TestListCategories(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "subscription_categories")

	ctx := context.Background()
	s.CreateCategory(ctx, "billing", "Billing", []string{"email"}, "on", 0)
	s.CreateCategory(ctx, "security", "Security", []string{"email", "sms"}, "required", 1)

	categories, err := s.ListCategories(ctx)
	if err != nil {
		t.Fatalf("ListCategories: %v", err)
	}
	if len(categories) != 2 {
		t.Fatalf("expected 2 categories, got %d", len(categories))
	}
}

func TestUpdateCategory(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "subscription_categories")

	ctx := context.Background()
	c, _ := s.CreateCategory(ctx, "billing", "Billing", []string{"email"}, "on", 0)

	updated, err := s.UpdateCategory(ctx, c.ID, "Billing Notifications", []string{"email", "inbox"}, "on", 0)
	if err != nil {
		t.Fatalf("UpdateCategory: %v", err)
	}
	if updated.Name != "Billing Notifications" {
		t.Fatalf("expected updated name, got %s", updated.Name)
	}
	if len(updated.DefaultChannels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(updated.DefaultChannels))
	}
}

func TestCreateCategory_DuplicateSlug(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "subscription_categories")

	ctx := context.Background()
	_, err := s.CreateCategory(ctx, "billing", "Billing", []string{"email"}, "on", 0)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}

	_, err = s.CreateCategory(ctx, "billing", "Billing Duplicate", []string{"email"}, "on", 1)
	if err == nil {
		t.Fatal("expected error on duplicate slug, got nil")
	}
}

func TestUpdateCategory_NotFound(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "subscription_categories")

	ctx := context.Background()
	_, err := s.UpdateCategory(ctx, "sct-nonexistent", "Nope", []string{}, "on", 0)
	if err == nil {
		t.Fatal("expected error updating non-existent category, got nil")
	}
}

func TestDeleteCategory(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "subscription_categories")

	ctx := context.Background()
	c, _ := s.CreateCategory(ctx, "billing", "Billing", []string{"email"}, "on", 0)

	if err := s.DeleteCategory(ctx, c.ID); err != nil {
		t.Fatalf("DeleteCategory: %v", err)
	}

	_, err := s.GetCategoryByID(ctx, c.ID)
	if err == nil {
		t.Fatal("expected error after delete")
	}
}
