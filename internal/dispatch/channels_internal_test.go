// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package dispatch

import (
	"testing"

	"github.com/hermes-notifications/hermes/internal/models"
	hermenats "github.com/hermes-notifications/hermes/internal/nats"
)

func TestFilterChannelsForTemplate(t *testing.T) {
	nt := &models.NotificationTemplate{
		Content: map[string]map[string]string{
			"email": {"body": "e"},
			"inbox": {"title": "i"},
			// sms absent -> filtered out
		},
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

	all := []string{"email", "sms", "anything"}
	if got := FilterChannelsForTemplate(all, nil); len(got) != 3 {
		t.Fatalf("nil template: got %v, want passthrough", got)
	}
}

func TestContentForChannel(t *testing.T) {
	url := "https://x"
	original := hermenats.MessageContent{ActionURL: &url}
	rc := RenderedContent{
		"email": {"subject": "es", "body": "eb"},
		"sms":   {"body": "sb"},
		"inbox": {"title": "it", "body": "ib"},
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
	bogus := contentForChannel("bogus", original, rc)
	if bogus.Title != "" || bogus.Body != "" || bogus.ActionURL != &url {
		t.Fatalf("bogus: got %+v", bogus)
	}
}

func TestFilterChannelsByContact(t *testing.T) {
	// email + sms required; inbox always kept. Recipient has email only.
	rec := hermenats.Recipient{"email": "a@b.c"}
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

	// Unknown channel has no address requirement — must be kept, matching the
	// original switch which had no default case.
	keptBogus, skippedBogus := filterChannelsByContact([]string{"bogus"}, hermenats.Recipient{})
	if len(keptBogus) != 1 || keptBogus[0] != "bogus" {
		t.Fatalf("unknown channel: got kept=%v, want [bogus]", keptBogus)
	}
	if len(skippedBogus) != 0 {
		t.Fatalf("unknown channel: got skipped=%v, want none", skippedBogus)
	}
}
