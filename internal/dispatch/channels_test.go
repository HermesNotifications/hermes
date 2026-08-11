// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package dispatch_test

import (
	"testing"

	"github.com/hermes-notifications/hermes/internal/dispatch"
	"github.com/hermes-notifications/hermes/internal/models"
)

func TestFilterChannelsForTemplate_AllTemplates(t *testing.T) {
	nt := &models.NotificationTemplate{
		Content: map[string]map[string]string{
			"email": {"subject": "subject"},
			"sms":   {"body": "body"},
			"inbox": {"title": "title"},
		},
	}
	got := dispatch.FilterChannelsForTemplate([]string{"email", "sms", "inbox"}, nt)
	if len(got) != 3 {
		t.Fatalf("expected 3 channels, got %d: %v", len(got), got)
	}
}

func TestFilterChannelsForTemplate_NoEmailTemplate(t *testing.T) {
	nt := &models.NotificationTemplate{
		Content: map[string]map[string]string{"inbox": {"title": "title"}},
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
