//go:build integration

// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

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

	subject, body, sms := "Hi {{.name}}", "<p>x</p>", "hi"
	created, err := s.CreateTemplate(ctx, &models.NotificationTemplate{
		Slug: "tc-dualwrite", Name: "T",
		DefaultChannels: []string{"email", "sms"},
		EmailSubject:    &subject,
		EmailBody:       &body,
		SMSBody:         &sms,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Content["email"]["subject"] != subject ||
		created.Content["email"]["body"] != body ||
		created.Content["sms"]["body"] != sms {
		t.Fatalf("create content: %+v", created.Content)
	}

	got, err := s.GetTemplateBySlug(ctx, "tc-dualwrite")
	if err != nil {
		t.Fatal(err)
	}
	if got.Content["email"]["subject"] != subject || got.Content["sms"]["body"] != sms {
		t.Fatalf("reload content: %+v", got.Content)
	}

	updatedSubject := "Updated"
	got.EmailSubject = &updatedSubject
	updated, err := s.UpdateTemplate(ctx, got)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Content["email"]["subject"] != updatedSubject {
		t.Fatalf("update content: %+v", updated.Content)
	}
	reload, err := s.GetTemplateContent(ctx, got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reload["email"]["subject"] != updatedSubject {
		t.Fatalf("reloaded content after update: %+v", reload)
	}
}
