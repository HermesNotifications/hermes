// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package dispatch

import (
	"testing"

	hermenats "github.com/hermes-notifications/hermes/internal/nats"
	"github.com/hermes-notifications/hermes/internal/provider"
)

func TestRenderedContent_Field(t *testing.T) {
	rc := &RenderedContent{
		EmailSubject: "subj",
		EmailBody:    "ebody",
		SMSBody:      "sbody",
		InboxTitle:   "ititle",
		InboxBody:    "ibody",
	}
	cases := map[string]string{
		provider.FieldEmailSubject: "subj",
		provider.FieldEmailBody:    "ebody",
		provider.FieldSMSBody:      "sbody",
		provider.FieldInboxTitle:   "ititle",
		provider.FieldInboxBody:    "ibody",
		"":                         "",
		"unknown":                  "",
	}
	for key, want := range cases {
		if got := rc.Field(key); got != want {
			t.Errorf("Field(%q): got %q, want %q", key, got, want)
		}
	}
}

func TestRecipient_AddressFor(t *testing.T) {
	r := hermenats.Recipient{Email: "a@b.c", Phone: "+15551234"}
	if got := r.AddressFor("email"); got != "a@b.c" {
		t.Errorf("AddressFor(email): got %q", got)
	}
	if got := r.AddressFor("phone"); got != "+15551234" {
		t.Errorf("AddressFor(phone): got %q", got)
	}
	if got := r.AddressFor(""); got != "" {
		t.Errorf("AddressFor(\"\"): got %q, want empty", got)
	}
}
