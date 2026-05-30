// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package dispatch_test

import (
	"testing"

	"github.com/hermes-notifications/hermes/internal/dispatch"
	"github.com/hermes-notifications/hermes/internal/models"
)

func TestFilterChannelsForTemplate_AllTemplates(t *testing.T) {
	nt := &models.NotificationTemplate{
		EmailSubject: strPtr("subject"),
		SMSBody:      strPtr("body"),
		InboxTitle:   strPtr("title"),
	}
	got := dispatch.FilterChannelsForTemplate([]string{"email", "sms", "inbox"}, nt)
	if len(got) != 3 {
		t.Fatalf("expected 3 channels, got %d: %v", len(got), got)
	}
}

func TestFilterChannelsForTemplate_NoEmailTemplate(t *testing.T) {
	nt := &models.NotificationTemplate{
		InboxTitle: strPtr("title"),
	}
	got := dispatch.FilterChannelsForTemplate([]string{"email", "inbox"}, nt)
	if len(got) != 1 || got[0] != "inbox" {
		t.Fatalf("expected [inbox], got %v", got)
	}
}

func TestFilterChannelsForTemplate_NilTemplate(t *testing.T) {
	got := dispatch.FilterChannelsForTemplate([]string{"email", "inbox"}, nil)
	if len(got) != 2 {
		t.Fatalf("expected 2 channels for direct send, got %d", len(got))
	}
}
