// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package messaging

import (
	"errors"
	"fmt"
	"testing"

	hermenats "github.com/hermes-notifications/hermes/internal/nats"
)

type permErr struct{ msg string }

func (e *permErr) Error() string   { return e.msg }
func (e *permErr) Permanent() bool { return true }

type notPermErr struct{}

func (notPermErr) Error() string   { return "transient despite interface" }
func (notPermErr) Permanent() bool { return false }

func TestClassify(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		attempt    uint64
		wantDead   bool
		wantReason string
	}{
		{"transient mid-flight", errors.New("boom"), 3, false, ""},
		{"transient first attempt", errors.New("boom"), 1, false, ""},
		{"transient exhausted", errors.New("boom"), 10, true, hermenats.DeadLetterReasonMaxDeliveries},
		{"transient past limit", errors.New("boom"), 11, true, hermenats.DeadLetterReasonMaxDeliveries},
		{"permanent first attempt", &permErr{"invalid tenant uuid"}, 1, true, hermenats.DeadLetterReasonTerminated},
		{"permanent on last attempt wins over exhaustion", &permErr{"bad"}, 10, true, hermenats.DeadLetterReasonTerminated},
		{"wrapped permanent", fmt.Errorf("handle: %w", &permErr{"bad"}), 2, true, hermenats.DeadLetterReasonTerminated},
		{"PermanentError reporting false is transient", notPermErr{}, 2, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dead, reason := classify(tc.err, tc.attempt)
			if dead != tc.wantDead || reason != tc.wantReason {
				t.Errorf("classify(%v, %d) = (%v, %q), want (%v, %q)",
					tc.err, tc.attempt, dead, reason, tc.wantDead, tc.wantReason)
			}
		})
	}
}
