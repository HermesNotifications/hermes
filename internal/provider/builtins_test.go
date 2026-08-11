// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

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

func TestBuiltins_IsAddressKey(t *testing.T) {
	for _, key := range []string{"email", "phone"} {
		if !Builtins.IsAddressKey(key) {
			t.Errorf("expected %q to be a valid address key", key)
		}
	}
	for _, key := range []string{"", "inbox", "slack", "unknown"} {
		if Builtins.IsAddressKey(key) {
			t.Errorf("expected %q to NOT be a valid address key", key)
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
