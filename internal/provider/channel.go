// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package provider

import "github.com/hermes-notifications/hermes/internal/models"

// Built-in channel slugs.
const (
	ChannelEmail = "email"
	ChannelSMS   = "sms"
	ChannelInbox = "inbox"
)

// Rendered-content field keys. ChannelDescriptor.TitleField / BodyField
// reference these; dispatch's RenderedContent.Field resolves them against the
// (still fixed, until phase 2) rendered columns.
const (
	FieldEmailSubject = "email_subject"
	FieldEmailBody    = "email_body"
	FieldSMSBody      = "sms_body"
	FieldInboxTitle   = "inbox_title"
	FieldInboxBody    = "inbox_body"
)

// Render kinds for a content field.
const (
	RenderText = "text"
	RenderHTML = "html"
)

// ContentField declares one field in a channel's content schema. The Key is the
// channel-local field name stored in template_channel_content's JSONB (e.g.
// "subject", "body", "title"). Render selects text vs HTML rendering. MapsTo
// projects the rendered value onto the delivery MessageContent ("title" |
// "body" | "" for neither).
type ContentField struct {
	Key    string
	Render string // RenderText | RenderHTML
	MapsTo string // "title" | "body" | ""
}

// ChannelDescriptor declares everything dispatch needs to route a channel
// without hardcoding its name. It replaces the three `switch ch` blocks in
// internal/dispatch (FilterChannelsForTemplate, the contact-info filter, and
// contentForChannel).
type ChannelDescriptor struct {
	// Slug is the channel identifier (e.g. "email").
	Slug string

	// AddressKey names the contact point this channel delivers to: "email",
	// "phone", or "" when the channel needs no external address (e.g. inbox).
	AddressKey string

	// AddressLabel is the human phrase used in the "user has no X" skip event,
	// preserving today's exact event reason strings (e.g. "email address",
	// "phone number"). Empty when AddressKey is "".
	AddressLabel string

	// TitleField / BodyField name the rendered-content fields projected onto
	// the delivery message's Title / Body. Empty means the channel has no
	// title / no body.
	TitleField string
	BodyField  string

	// HasContent reports whether a template provides content for this channel.
	// Replaces the FilterChannelsForTemplate switch.
	HasContent func(t *models.NotificationTemplate) bool

	// Content is the channel's content schema: the ordered set of content
	// fields a template provides for this channel. Stored per channel in
	// template_channel_content. Supersedes TitleField/BodyField/HasContent
	// (removed in phase 2e).
	Content []ContentField
}

// ContentFieldByKey returns the content-field schema for a local field key.
func (d ChannelDescriptor) ContentFieldByKey(key string) (ContentField, bool) {
	for _, f := range d.Content {
		if f.Key == key {
			return f, true
		}
	}
	return ContentField{}, false
}
