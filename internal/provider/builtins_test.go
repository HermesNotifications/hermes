// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package provider

import (
	"testing"
)

func TestBuiltins_Channels(t *testing.T) {
	for _, slug := range []string{ChannelEmail, ChannelSMS, ChannelInbox} {
		if _, ok := Builtins.Channel(slug); !ok {
			t.Errorf("built-in channel %q not registered", slug)
		}
	}
}

func TestBuiltins_AddressKeys(t *testing.T) {
	cases := map[string]struct{ key, label string }{
		ChannelEmail: {"email", "email address"},
		ChannelSMS:   {"phone", "phone number"},
		ChannelInbox: {"", ""},
	}
	for slug, want := range cases {
		desc, _ := Builtins.Channel(slug)
		if desc.AddressKey != want.key || desc.AddressLabel != want.label {
			t.Errorf("%s: got (%q,%q), want (%q,%q)", slug, desc.AddressKey, desc.AddressLabel, want.key, want.label)
		}
	}
}

func TestBuiltins_Providers(t *testing.T) {
	if got := Builtins.ProvidersForChannel(ChannelEmail); len(got) != 2 || got[0] != "smtp" || got[1] != "ses" {
		t.Errorf("email providers: got %v, want [smtp ses]", got)
	}
	if got := Builtins.ProvidersForChannel(ChannelSMS); len(got) != 1 || got[0] != "sms" {
		t.Errorf("sms providers: got %v, want [sms]", got)
	}
	if got := Builtins.ProvidersForChannel(ChannelInbox); len(got) != 1 || got[0] != "inbox" {
		t.Errorf("inbox providers: got %v, want [inbox]", got)
	}
}
