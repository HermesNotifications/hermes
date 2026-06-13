// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package messaging

import (
	"context"
	"errors"
	"time"

	"github.com/hermes-notifications/hermes/internal/observability"
	hermenats "github.com/hermes-notifications/hermes/internal/nats"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// dlqStreamName is the stream that captures terminally failed messages.
// It uses Limits retention (not WorkQueue) so dead letters survive
// inspection reads during an incident.
const dlqStreamName = "DLQ"

var meter = observability.Meter("github.com/hermes-notifications/hermes/internal/messaging")

var deadLetterCounter, _ = meter.Int64Counter(
	"hermes.messaging.dead_letters",
	metric.WithDescription("Messages captured to the DLQ stream after terminal failure."),
	metric.WithUnit("1"),
)

var dlqPublishFailureCounter, _ = meter.Int64Counter(
	"hermes.messaging.dlq_publish_failures",
	metric.WithDescription("Failed attempts to publish a dead letter; the source message was left in its stream."),
	metric.WithUnit("1"),
)

// publishDeadLetter captures a terminally failed message to the DLQ stream.
// The caller must only Term() the original message if this returns nil —
// never destroy a message that wasn't preserved.
func (c *Client) publishDeadLetter(ctx context.Context, stream, consumer, subject, reason string, attempt uint64, handlerErr error, payload []byte) error {
	dl := &hermenats.DeadLetter{
		Subject:  subject,
		Stream:   stream,
		Consumer: consumer,
		Reason:   reason,
		Attempts: attempt,
		Error:    handlerErr.Error(),
		FailedAt: time.Now().UTC(),
		Payload:  payload,
	}
	data, err := dl.Marshal()
	if err == nil {
		err = c.Publish(ctx, "dlq."+subject, data)
	}
	if err != nil {
		dlqPublishFailureCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("stream", stream),
			attribute.String("consumer", consumer),
		))
		return err
	}
	deadLetterCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("stream", stream),
		attribute.String("consumer", consumer),
		attribute.String("reason", reason),
	))
	return nil
}

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
