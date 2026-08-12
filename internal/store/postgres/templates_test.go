// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/hermesnotifications/hermes/internal/models"
)

func TestCreateTemplate_And_GetBySlug(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notification_templates", "subscription_categories")

	ctx := context.Background()
	cat, _ := s.CreateCategory(ctx, "billing", "Billing", []string{"email", "inbox"}, "on", 0)
	sub, err := s.CreateSubscription(ctx, cat.ID, "invoice", "Invoice", 0)
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}

	subID := sub.ID
	nt, err := s.CreateTemplate(ctx, &models.NotificationTemplate{
		SubscriptionID:  &subID,
		Slug:            "invoice.paid",
		Name:            "Invoice Paid",
		DefaultChannels: []string{"email"},
		Content: map[string]map[string]string{
			"email": {"subject": "Invoice {{.invoice_number}} paid"},
		},
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	got, err := s.GetTemplateBySlug(ctx, "invoice.paid")
	if err != nil {
		t.Fatalf("GetTemplateBySlug: %v", err)
	}
	if got.ID != nt.ID {
		t.Fatalf("expected ID %s, got %s", nt.ID, got.ID)
	}
}

func TestCreateTemplate_DuplicateSlug(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notification_templates", "subscription_categories")

	ctx := context.Background()
	_, err := s.CreateTemplate(ctx, &models.NotificationTemplate{
		Slug: "welcome", Name: "Welcome", DefaultChannels: []string{},
	})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}

	_, err = s.CreateTemplate(ctx, &models.NotificationTemplate{
		Slug: "welcome", Name: "Welcome Duplicate", DefaultChannels: []string{},
	})
	if err == nil {
		t.Fatal("expected error on duplicate slug, got nil")
	}
}

func TestUpdateTemplate(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notification_templates", "subscription_categories")

	ctx := context.Background()
	nt, err := s.CreateTemplate(ctx, &models.NotificationTemplate{
		Slug: "welcome", Name: "Welcome", DefaultChannels: []string{"email"},
		Content: map[string]map[string]string{
			"email": {"subject": "Hello"},
		},
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	updated, err := s.UpdateTemplate(ctx, &models.NotificationTemplate{
		ID: nt.ID, Name: "Welcome Updated", DefaultChannels: []string{"email", "inbox"},
		Content: map[string]map[string]string{
			"email": {"subject": "Hello Updated"},
		},
	})
	if err != nil {
		t.Fatalf("UpdateTemplate: %v", err)
	}
	if updated.Name != "Welcome Updated" {
		t.Errorf("expected name 'Welcome Updated', got %q", updated.Name)
	}
	if updated.Content["email"]["subject"] != "Hello Updated" {
		t.Errorf("expected content email.subject 'Hello Updated', got %q", updated.Content["email"]["subject"])
	}
}

func TestUpdateTemplate_NotFound(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notification_templates", "subscription_categories")

	ctx := context.Background()
	_, err := s.UpdateTemplate(ctx, &models.NotificationTemplate{
		ID: "ntpl-nonexistent", Name: "Nope",
	})
	if err == nil {
		t.Fatal("expected error updating non-existent template, got nil")
	}
}

func TestListTemplates(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notification_templates", "subscription_categories")

	ctx := context.Background()
	s.CreateTemplate(ctx, &models.NotificationTemplate{
		Slug: "welcome", Name: "Welcome", DefaultChannels: []string{},
	})
	s.CreateTemplate(ctx, &models.NotificationTemplate{
		Slug: "invoice", Name: "Invoice", DefaultChannels: []string{},
	})

	templates, err := s.ListTemplates(ctx)
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(templates) != 2 {
		t.Fatalf("expected 2 templates, got %d", len(templates))
	}
}

func TestCreateTemplate_InvalidSubscriptionID(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notification_templates", "subscription_categories")

	ctx := context.Background()
	badID := "sub-nonexistent"
	_, err := s.CreateTemplate(ctx, &models.NotificationTemplate{
		Slug: "test", Name: "Test", SubscriptionID: &badID, DefaultChannels: []string{},
	})
	if err == nil {
		t.Fatal("expected FK violation error, got nil")
	}
}

func TestDeleteTemplate(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notification_templates", "subscription_categories")

	ctx := context.Background()
	nt, _ := s.CreateTemplate(ctx, &models.NotificationTemplate{
		Slug:            "standalone.alert",
		Name:            "Standalone Alert",
		DefaultChannels: []string{},
	})

	if err := s.DeleteTemplate(ctx, nt.ID); err != nil {
		t.Fatalf("DeleteTemplate: %v", err)
	}

	_, err := s.GetTemplateByID(ctx, nt.ID)
	if err == nil {
		t.Fatal("expected error after delete")
	}
}
