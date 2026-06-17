// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package models_test

import (
	"encoding/json"
	"testing"

	"github.com/hermes-notifications/hermes/internal/models"
)

func TestNotificationTemplate_ContentJSON(t *testing.T) {
	tpl := models.NotificationTemplate{
		Content: map[string]map[string]string{
			"email": {"subject": "Hi {{.name}}", "body": "<p>x</p>"},
			"sms":   {"body": "hi"},
		},
	}
	b, err := json.Marshal(tpl)
	if err != nil {
		t.Fatal(err)
	}
	var back models.NotificationTemplate
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Content["email"]["subject"] != "Hi {{.name}}" || back.Content["sms"]["body"] != "hi" {
		t.Fatalf("round-trip mismatch: %+v", back.Content)
	}
}

func TestUser_ContactsJSON(t *testing.T) {
	u := models.User{Contacts: map[string]string{"email": "a@b.c", "phone": "+1555"}}
	b, _ := json.Marshal(u)
	var back models.User
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Contacts["email"] != "a@b.c" || back.Contacts["phone"] != "+1555" {
		t.Fatalf("round-trip mismatch: %+v", back.Contacts)
	}
}
