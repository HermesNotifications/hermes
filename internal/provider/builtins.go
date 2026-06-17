// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package provider

// Builtins is the process-wide registry of first-party channels and providers.
// It is constructed once at package-init time and read-only thereafter, so the
// existing dispatch call sites can consult it without a signature change.
// (Phase 3 will layer a DB-backed view over this for third-party providers.)
var Builtins = newBuiltinRegistry()

func newBuiltinRegistry() *Registry {
	r := NewRegistry()

	r.RegisterChannel(ChannelDescriptor{
		Slug:         ChannelEmail,
		AddressKey:   "email",
		AddressLabel: "email address",
		Content: []ContentField{
			{Key: "subject", Render: RenderText, MapsTo: "title"},
			{Key: "body", Render: RenderHTML, MapsTo: "body"},
		},
	})
	r.RegisterChannel(ChannelDescriptor{
		Slug:         ChannelSMS,
		AddressKey:   "phone",
		AddressLabel: "phone number",
		Content: []ContentField{
			{Key: "body", Render: RenderText, MapsTo: "body"},
		},
	})
	r.RegisterChannel(ChannelDescriptor{
		Slug:       ChannelInbox,
		AddressKey: "", // inbox needs no external contact point
		Content: []ContentField{
			{Key: "title", Render: RenderText, MapsTo: "title"},
			{Key: "body", Render: RenderText, MapsTo: "body"},
		},
	})

	// Built-in providers, matching what the workers run today. Provider-level
	// selection lands in phase 3; registered now so the registry reflects the
	// deployed providers (email worker: smtp/ses, sms worker: webhook named
	// "sms", inbox worker: "inbox").
	r.RegisterProvider(Manifest{ID: "smtp", Channel: ChannelEmail})
	r.RegisterProvider(Manifest{ID: "ses", Channel: ChannelEmail})
	r.RegisterProvider(Manifest{ID: "sms", Channel: ChannelSMS})
	r.RegisterProvider(Manifest{ID: "inbox", Channel: ChannelInbox})

	return r
}
