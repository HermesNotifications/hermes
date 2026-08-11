// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package provider

// Built-in channel slugs.
const (
	ChannelEmail = "email"
	ChannelSMS   = "sms"
	ChannelInbox = "inbox"
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

	// Content is the channel's content schema: the ordered set of content
	// fields a template provides for this channel. Stored per channel in
	// template_channel_content.
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
