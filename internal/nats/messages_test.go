// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package hermenats

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDeadLetter_RoundTrip(t *testing.T) {
	in := &DeadLetter{
		Subject:  "delivery.email",
		Stream:   "DELIVERY",
		Consumer: "worker-email",
		Reason:   DeadLetterReasonMaxDeliveries,
		Attempts: 10,
		Error:    "smtp: connection refused",
		FailedAt: time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC),
		Payload:  json.RawMessage(`{"notification_id":"abc123"}`),
	}

	data, err := in.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	out, err := UnmarshalDeadLetter(data)
	if err != nil {
		t.Fatalf("UnmarshalDeadLetter: %v", err)
	}

	if out.Subject != in.Subject || out.Stream != in.Stream || out.Consumer != in.Consumer {
		t.Errorf("identity fields mismatch: got %+v", out)
	}
	if out.Reason != DeadLetterReasonMaxDeliveries {
		t.Errorf("Reason = %q, want %q", out.Reason, DeadLetterReasonMaxDeliveries)
	}
	if out.Attempts != 10 {
		t.Errorf("Attempts = %d, want 10", out.Attempts)
	}
	if out.Error != in.Error {
		t.Errorf("Error = %q, want %q", out.Error, in.Error)
	}
	if !out.FailedAt.Equal(in.FailedAt) {
		t.Errorf("FailedAt = %v, want %v", out.FailedAt, in.FailedAt)
	}
	// Payload must pass through verbatim so operators can replay it.
	if string(out.Payload) != string(in.Payload) {
		t.Errorf("Payload = %s, want %s", out.Payload, in.Payload)
	}
}

// The case the DLQ exists for. Payload was json.RawMessage, whose MarshalJSON validates
// its contents — so a payload that is not valid JSON made DeadLetter.Marshal fail, the
// dead letter was never published, and internal/messaging fell back to nacking the
// message until MaxAge. A malformed message is precisely what cannot be processed and
// most needs preserving, so the DLQ failed exactly where it was most needed, silently.
func TestDeadLetter_PreservesAPayloadThatIsNotValidJSON(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
	}{
		{"plain text", []byte("this is not a delivery message")},
		{"truncated JSON", []byte(`{"notification_id":`)},
		{"binary", []byte{0x00, 0x01, 0x02, 0xff, 0xfe}},
		{"empty", []byte{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := &DeadLetter{
				Subject: "delivery.email",
				Stream:  "DELIVERY",
				Reason:  DeadLetterReasonTerminated,
				Payload: tc.payload,
			}

			data, err := in.Marshal()
			if err != nil {
				t.Fatalf("Marshal failed, so this message could never reach the DLQ: %v", err)
			}

			out, err := UnmarshalDeadLetter(data)
			if err != nil {
				t.Fatalf("UnmarshalDeadLetter: %v", err)
			}
			// Verbatim round-trip is the point: an operator replays what actually failed.
			if string(out.Payload) != string(tc.payload) {
				t.Errorf("Payload = %q, want %q", out.Payload, tc.payload)
			}
		})
	}
}

func TestDeadLetterReasons(t *testing.T) {
	if DeadLetterReasonMaxDeliveries != "max_deliveries" {
		t.Errorf("DeadLetterReasonMaxDeliveries = %q", DeadLetterReasonMaxDeliveries)
	}
	if DeadLetterReasonTerminated != "terminated" {
		t.Errorf("DeadLetterReasonTerminated = %q", DeadLetterReasonTerminated)
	}
}
