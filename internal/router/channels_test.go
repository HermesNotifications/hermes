package router_test

import (
	"testing"

	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/hermes-notifications/hermes/internal/router"
)

func TestFilterChannelsForType_AllTemplates(t *testing.T) {
	nt := &models.NotificationType{
		EmailSubject: strPtr("subject"),
		SMSBody:      strPtr("body"),
		InboxTitle:   strPtr("title"),
	}
	got := router.FilterChannelsForType([]string{"email", "sms", "inbox"}, nt)
	if len(got) != 3 {
		t.Fatalf("expected 3 channels, got %d: %v", len(got), got)
	}
}

func TestFilterChannelsForType_NoEmailTemplate(t *testing.T) {
	nt := &models.NotificationType{
		InboxTitle: strPtr("title"),
	}
	got := router.FilterChannelsForType([]string{"email", "inbox"}, nt)
	if len(got) != 1 || got[0] != "inbox" {
		t.Fatalf("expected [inbox], got %v", got)
	}
}

func TestFilterChannelsForType_NilType(t *testing.T) {
	got := router.FilterChannelsForType([]string{"email", "inbox"}, nil)
	if len(got) != 2 {
		t.Fatalf("expected 2 channels for direct send, got %d", len(got))
	}
}
