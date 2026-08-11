//go:build integration

// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package postgres_test

import (
	"context"
	"testing"

	"github.com/hermes-notifications/hermes/internal/models"
)

func TestTemplateContent_DualWriteAndLoad(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notification_templates", "subscription_categories")
	ctx := context.Background()

	created, err := s.CreateTemplate(ctx, &models.NotificationTemplate{
		Slug: "tc-dualwrite", Name: "T",
		DefaultChannels: []string{"email", "sms"},
		Content: map[string]map[string]string{
			"email": {"subject": "Hi {{.name}}", "body": "<p>x</p>"},
			"sms":   {"body": "hi"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Content["email"]["subject"] != "Hi {{.name}}" ||
		created.Content["email"]["body"] != "<p>x</p>" ||
		created.Content["sms"]["body"] != "hi" {
		t.Fatalf("create content: %+v", created.Content)
	}

	got, err := s.GetTemplateBySlug(ctx, "tc-dualwrite")
	if err != nil {
		t.Fatal(err)
	}
	if got.Content["email"]["subject"] != "Hi {{.name}}" || got.Content["sms"]["body"] != "hi" {
		t.Fatalf("reload content: %+v", got.Content)
	}

	got.Content["email"]["subject"] = "Updated"
	updated, err := s.UpdateTemplate(ctx, got)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Content["email"]["subject"] != "Updated" {
		t.Fatalf("update content: %+v", updated.Content)
	}
	reload, err := s.GetTemplateContent(ctx, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reload["email"]["subject"] != "Updated" {
		t.Fatalf("reloaded content after update: %+v", reload)
	}
}
