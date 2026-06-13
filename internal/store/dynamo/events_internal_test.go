// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package dynamo

import (
	"strconv"
	"testing"
	"time"
)

// TestTTLSeconds verifies that ttlSeconds derives the expiry from Client.RetentionDays,
// and falls back to 90 days when the value is unset (≤0). This is a pure unit test — no
// DynamoDB infrastructure required.
func TestTTLSeconds(t *testing.T) {
	created := time.Unix(1_700_000_000, 0).UTC()

	cases := []struct {
		name      string
		retention int
		wantDays  int
	}{
		{"custom retention", 30, 30},
		{"default fallback when zero", 0, 90},
		{"default fallback when negative", -5, 90},
		{"large retention", 365, 365},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			es := &EventStore{client: &Client{RetentionDays: tc.retention}}
			got := es.ttlSeconds(created)

			want := strconv.FormatInt(created.Add(time.Duration(tc.wantDays)*24*time.Hour).Unix(), 10)
			if got != want {
				t.Errorf("ttlSeconds(retention=%d) = %s, want %s (created + %d days)",
					tc.retention, got, want, tc.wantDays)
			}
		})
	}
}
