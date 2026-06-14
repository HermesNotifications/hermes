// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package dispatch

import (
	"testing"

	"github.com/hermes-notifications/hermes/internal/models"
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
	if got := r.AddressFor("unknown"); got != "" {
		t.Errorf("AddressFor(unknown): got %q, want empty", got)
	}
}

func TestFilterChannelsForTemplate(t *testing.T) {
	nt := &models.NotificationTemplate{
		EmailBody:  sp("e"),
		InboxTitle: sp("i"),
		// SMSBody nil -> sms filtered out
	}
	got := FilterChannelsForTemplate([]string{"email", "sms", "inbox", "bogus"}, nt)
	want := []string{"email", "inbox"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}

	// nil template passes everything through unchanged.
	all := []string{"email", "sms", "anything"}
	if got := FilterChannelsForTemplate(all, nil); len(got) != 3 {
		t.Fatalf("nil template: got %v, want passthrough", got)
	}
}

func sp(s string) *string { return &s }

func TestContentForChannel(t *testing.T) {
	url := "https://x"
	original := hermenats.MessageContent{ActionURL: &url}
	rc := &RenderedContent{
		EmailSubject: "es", EmailBody: "eb",
		SMSBody:    "sb",
		InboxTitle: "it", InboxBody: "ib",
	}

	// rendered == nil -> passthrough of original.
	if got := contentForChannel("email", original, nil); got.ActionURL != &url {
		t.Fatal("nil rendered: expected original passthrough")
	}

	email := contentForChannel("email", original, rc)
	if email.Title != "es" || email.Body != "eb" || email.ActionURL != &url {
		t.Fatalf("email: got %+v", email)
	}
	sms := contentForChannel("sms", original, rc)
	if sms.Title != "" || sms.Body != "sb" {
		t.Fatalf("sms: got title=%q body=%q, want title empty", sms.Title, sms.Body)
	}
	inbox := contentForChannel("inbox", original, rc)
	if inbox.Title != "it" || inbox.Body != "ib" {
		t.Fatalf("inbox: got %+v", inbox)
	}
	// unknown channel -> empty title/body (ActionURL still carried).
	bogus := contentForChannel("bogus", original, rc)
	if bogus.Title != "" || bogus.Body != "" || bogus.ActionURL != &url {
		t.Fatalf("bogus: got %+v", bogus)
	}
}

func TestFilterChannelsByContact(t *testing.T) {
	// email + sms required; inbox always kept. Recipient has email only.
	rec := hermenats.Recipient{Email: "a@b.c"}
	kept, skipped := filterChannelsByContact([]string{"email", "sms", "inbox"}, rec)

	wantKept := []string{"email", "inbox"}
	if len(kept) != len(wantKept) || kept[0] != "email" || kept[1] != "inbox" {
		t.Fatalf("kept: got %v, want %v", kept, wantKept)
	}
	if len(skipped) != 1 || skipped[0].Channel != "sms" {
		t.Fatalf("skipped: got %v, want one sms skip", skipped)
	}
	// Exact strings the call site formats from the skip:
	if got := "skipping " + skipped[0].Channel + " channel: user has no " + skipped[0].AddressKey; got != "skipping sms channel: user has no phone" {
		t.Fatalf("log string: got %q", got)
	}
	if got := "user has no " + skipped[0].AddressLabel; got != "user has no phone number" {
		t.Fatalf("event reason: got %q", got)
	}
}
