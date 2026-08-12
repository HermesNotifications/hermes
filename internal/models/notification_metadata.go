// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package models

import "github.com/danielgtaylor/huma/v2"

// Levels a notification may declare. Deliberately *not* the same vocabulary as
// NotificationEvent.Severity ("info"/"warn"/"error"), which is the operational delivery log:
// two different concepts, and reusing the word would guarantee they were conflated.
const (
	LevelInfo    = "info"
	LevelSuccess = "success"
	LevelWarning = "warning"
	LevelError   = "error"
)

// MetadataKeyLevel and MetadataKeyToast are the only keys Hermes interprets.
const (
	MetadataKeyLevel = "level"
	MetadataKeyToast = "toast"
)

// ValidLevels is the accepted set for MetadataKeyLevel, in presentation order.
var ValidLevels = []string{LevelInfo, LevelSuccess, LevelWarning, LevelError}

// MaxMetadataBytes bounds the serialized metadata object accepted on a send.
//
// Not configurable, on purpose. A per-deployment cap turns a client-contract limit into a
// deployment variable no SDK can discover, so every caller would have to code for the smallest
// value it might meet — which is the same as having the smallest value everywhere.
//
// 4 KiB is ~13x the size of the existing notification.new frame, ~0.4% of the NATS 1 MiB default
// max_payload, and ~1% of the DynamoDB 400 KB item limit, so it cannot be the reason any of
// those reject a write.
const MaxMetadataBytes = 4096

// NotificationMetadata is an opaque object supplied by the sender, stored verbatim, and echoed
// back on the inbox row and on the realtime event.
//
// Hermes reads exactly two keys from it -- "level" and "toast" -- and never interprets anything
// else. That is a commitment: reserving a further bare top-level name later would break any
// caller already using it for their own purposes.
//
// "Verbatim" means a semantic round trip, not a byte-for-byte one, and the difference is worth
// stating because both hops are lossy in ways that surprise people. jsonb does not preserve key
// order, strips insignificant whitespace, and collapses duplicate keys to the last. Decoding
// into map[string]any turns every number into a float64, so an integer above 2^53 loses
// precision. If exact value fidelity is ever needed, map[string]json.RawMessage is a drop-in
// with an identical emitted schema.
type NotificationMetadata map[string]any

// Schema describes the object to huma, so that the two reserved keys are documented and
// validated at the edge while everything else passes through untouched.
//
// Implementing huma.SchemaProvider is what buys the enum: without it, a map[string]any renders
// as a bare `additionalProperties: {}`, which validates nothing and generates a TypeScript type
// with no knowledge of the levels. Note this type is a named *map*, not a struct, so huma inlines
// the schema rather than emitting a $ref -- the same shape therefore appears in the send request
// body and on the Notification in both the inbox and admin specs.
//
// Deliberately not json.RawMessage: huma emits a bare empty schema for that, which
// openapi-typescript renders as `unknown`, on which reading `.level` is a compile error.
func (NotificationMetadata) Schema(huma.Registry) *huma.Schema {
	levels := make([]any, 0, len(ValidLevels))
	for _, level := range ValidLevels {
		levels = append(levels, level)
	}

	return &huma.Schema{
		Type:        huma.TypeObject,
		Description: "Opaque metadata stored with the notification and echoed back. Hermes reads only 'level' and 'toast'; every other key round-trips untouched.",
		Properties: map[string]*huma.Schema{
			MetadataKeyLevel: {
				Type:        huma.TypeString,
				Enum:        levels,
				Description: "How a client should present this notification.",
			},
			MetadataKeyToast: {
				Type:        huma.TypeBoolean,
				Description: "Whether a client should surface this transiently rather than waiting for the user to open their inbox.",
			},
		},
		// The whole point: unknown keys are allowed and preserved.
		AdditionalProperties: true,
	}
}

// Level returns the declared level, and whether one was present and recognised.
//
// An unrecognised value is reported as absent rather than passed through. Servers are free to
// add levels, so a reader that has not been updated must degrade to "no level" rather than hand
// its caller a string it cannot handle.
func (m NotificationMetadata) Level() (string, bool) {
	raw, ok := m[MetadataKeyLevel].(string)
	if !ok {
		return "", false
	}
	for _, level := range ValidLevels {
		if raw == level {
			return raw, true
		}
	}
	return "", false
}

// Toast reports whether the sender asked for this to be surfaced transiently.
//
// Strictly a boolean true: the string "true" is not a request to interrupt someone.
func (m NotificationMetadata) Toast() bool {
	toast, _ := m[MetadataKeyToast].(bool)
	return toast
}
