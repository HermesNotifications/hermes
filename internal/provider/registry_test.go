// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package provider

import "testing"

func TestRegistry_RegisterAndLookupChannel(t *testing.T) {
	r := NewRegistry()
	r.RegisterChannel(ChannelDescriptor{Slug: "email", AddressKey: "email"})

	desc, ok := r.Channel("email")
	if !ok {
		t.Fatal("expected channel 'email' to be registered")
	}
	if desc.AddressKey != "email" {
		t.Fatalf("AddressKey: got %q, want %q", desc.AddressKey, "email")
	}
	if _, ok := r.Channel("nope"); ok {
		t.Fatal("expected unknown channel to report ok=false")
	}
}

func TestRegistry_DuplicateChannelPanics(t *testing.T) {
	r := NewRegistry()
	r.RegisterChannel(ChannelDescriptor{Slug: "email"})
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate channel registration")
		}
	}()
	r.RegisterChannel(ChannelDescriptor{Slug: "email"})
}

func TestRegistry_RegisterProviderAndOrder(t *testing.T) {
	r := NewRegistry()
	r.RegisterChannel(ChannelDescriptor{Slug: "email"})
	r.RegisterProvider(Manifest{ID: "smtp", Channel: "email"})
	r.RegisterProvider(Manifest{ID: "ses", Channel: "email"})

	got := r.ProvidersForChannel("email")
	if len(got) != 2 || got[0] != "smtp" || got[1] != "ses" {
		t.Fatalf("ProvidersForChannel: got %v, want [smtp ses]", got)
	}
	if got := r.ProvidersForChannel("sms"); got != nil {
		t.Fatalf("ProvidersForChannel(sms): got %v, want nil", got)
	}
}

func TestRegistry_ProviderForUnknownChannelPanics(t *testing.T) {
	r := NewRegistry()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic when registering provider for unknown channel")
		}
	}()
	r.RegisterProvider(Manifest{ID: "smtp", Channel: "email"})
}
