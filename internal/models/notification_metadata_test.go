// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package models_test

import (
	"encoding/json"
	"testing"

	"github.com/hermesnotifications/hermes/internal/models"
)

func TestNotificationMetadata_Level(t *testing.T) {
	tests := []struct {
		name     string
		metadata models.NotificationMetadata
		want     string
		wantOK   bool
	}{
		{"info", models.NotificationMetadata{"level": "info"}, "info", true},
		{"success", models.NotificationMetadata{"level": "success"}, "success", true},
		{"warning", models.NotificationMetadata{"level": "warning"}, "warning", true},
		{"error", models.NotificationMetadata{"level": "error"}, "error", true},
		{"absent", models.NotificationMetadata{"toast": true}, "", false},
		{"nil map", nil, "", false},
		// Reported as absent, not passed through. A server may add levels, so a reader that
		// has not been updated must degrade rather than hand its caller a value it cannot use.
		{"unrecognised", models.NotificationMetadata{"level": "critical"}, "", false},
		{"wrong type", models.NotificationMetadata{"level": 3}, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tt.metadata.Level()
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("Level() = (%q, %v), want (%q, %v)", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestNotificationMetadata_Toast(t *testing.T) {
	tests := []struct {
		name     string
		metadata models.NotificationMetadata
		want     bool
	}{
		{"true", models.NotificationMetadata{"toast": true}, true},
		{"false", models.NotificationMetadata{"toast": false}, false},
		{"absent", models.NotificationMetadata{"level": "info"}, false},
		{"nil map", nil, false},
		// Strictly a boolean: the string "true" is not a request to interrupt someone.
		{"string true", models.NotificationMetadata{"toast": "true"}, false},
		{"number one", models.NotificationMetadata{"toast": 1}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.metadata.Toast(); got != tt.want {
				t.Errorf("Toast() = %v, want %v", got, tt.want)
			}
		})
	}
}

// The schema is what makes `level` validated at the edge and gives the generated TypeScript a
// literal union instead of `unknown`. A refactor that dropped it would break both silently, so
// it is asserted rather than assumed.
func TestNotificationMetadata_SchemaDocumentsTheReservedKeys(t *testing.T) {
	schema := models.NotificationMetadata{}.Schema(nil)

	if schema.Type != "object" {
		t.Errorf("type = %q, want object", schema.Type)
	}
	if schema.AdditionalProperties != true {
		t.Errorf("additionalProperties = %#v, want true — unknown keys must round-trip", schema.AdditionalProperties)
	}

	level, ok := schema.Properties["level"]
	if !ok {
		t.Fatal("no 'level' property in the schema")
	}
	if len(level.Enum) != len(models.ValidLevels) {
		t.Fatalf("enum has %d values, want %d", len(level.Enum), len(models.ValidLevels))
	}
	for i, want := range models.ValidLevels {
		if level.Enum[i] != want {
			t.Errorf("enum[%d] = %#v, want %q", i, level.Enum[i], want)
		}
	}

	toast, ok := schema.Properties["toast"]
	if !ok {
		t.Fatal("no 'toast' property in the schema")
	}
	if toast.Type != "boolean" {
		t.Errorf("toast type = %q, want boolean", toast.Type)
	}
}

func TestNotificationMetadata_RoundTripsThroughJSON(t *testing.T) {
	original := models.NotificationMetadata{
		"level":     "warning",
		"toast":     true,
		"invoiceId": "1041",
		"nested":    map[string]any{"tab": "billing"},
	}

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded models.NotificationMetadata
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if level, ok := decoded.Level(); !ok || level != "warning" {
		t.Errorf("level after round trip = (%q, %v)", level, ok)
	}
	if !decoded.Toast() {
		t.Error("toast did not survive the round trip")
	}
	if decoded["invoiceId"] != "1041" {
		t.Errorf("opaque key = %#v", decoded["invoiceId"])
	}
	nested, ok := decoded["nested"].(map[string]any)
	if !ok || nested["tab"] != "billing" {
		t.Errorf("nested object = %#v", decoded["nested"])
	}
}
