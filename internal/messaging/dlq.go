// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package messaging

import (
	"errors"

	hermenats "github.com/hermes-notifications/hermes/internal/nats"
)

// dlqStreamName is the stream that captures terminally failed messages.
// It uses Limits retention (not WorkQueue) so dead letters survive
// inspection reads during an incident.
const dlqStreamName = "DLQ"

// classify decides whether a failed delivery is terminal and, if so, why.
// Permanent rejection takes precedence over retry exhaustion so the
// dead-letter reason reflects the actual defect.
func classify(err error, attempt uint64) (deadLetter bool, reason string) {
	var pe PermanentError
	if errors.As(err, &pe) && pe.Permanent() {
		return true, hermenats.DeadLetterReasonTerminated
	}
	if attempt >= uint64(maxDeliveries) {
		return true, hermenats.DeadLetterReasonMaxDeliveries
	}
	return false, ""
}
