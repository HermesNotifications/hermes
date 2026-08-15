// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package observability

import (
	"context"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// NATSHeaderCarrier adapts nats.Header to OTel's propagation.TextMapCarrier.
// Use with otel.GetTextMapPropagator().Inject / Extract to ferry W3C
// TraceContext across NATS subjects.
type NATSHeaderCarrier nats.Header

// Get returns the first value for key or the empty string.
func (c NATSHeaderCarrier) Get(key string) string {
	return nats.Header(c).Get(key)
}

// Set sets key to value, replacing any existing values.
func (c NATSHeaderCarrier) Set(key, val string) {
	nats.Header(c).Set(key, val)
}

// Keys returns all header names.
func (c NATSHeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// InjectNATS starts a producer span, injects the trace context into the
// message headers, and returns the context + span. Caller MUST call
// span.End() when the publish call returns.
func InjectNATS(ctx context.Context, msg *nats.Msg) (context.Context, trace.Span) {
	tracer := otel.Tracer("github.com/hermesnotifications/hermes/internal/observability")
	ctx, span := tracer.Start(ctx, "nats.publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			semconv.MessagingSystemKey.String("nats"),
			semconv.MessagingDestinationName(msg.Subject),
			semconv.MessagingOperationTypePublish,
		),
	)

	if msg.Header == nil {
		msg.Header = nats.Header{}
	}
	otel.GetTextMapPropagator().Inject(ctx, NATSHeaderCarrier(msg.Header))

	return ctx, span
}

// ExtractNATS extracts the remote trace context from message headers and starts a
// consumer span for it. Caller MUST call span.End() when processing is done.
//
// attempt is the 1-based delivery count, and it decides how the span joins the
// producer's trace:
//
//   - First delivery: a child of the publish span, which is the ordinary messaging
//     shape and what makes the pipeline read as one trace.
//
//   - Redelivery: a new root, linked back to the publish. A retry is not the same
//     unit of work as the original publish, and parenting it there says something
//     false about time — JetStream keeps the headers, so attempt 5 would otherwise
//     graft onto a span that ended long before. With retryDelay capped at 240s and
//     maxDeliveries at 10, "long before" is minutes, and the trace would show a root
//     that finished in milliseconds with a child starting a quarter of an hour later.
//
// The tradeoff is stated plainly because it is real: a linked retry is harder to find
// from the original trace than a child would be, since not every backend walks links
// (see docs/observability/adr/004). That is worth accepting only where the parent-child
// claim is actually false, which is why the first delivery keeps its parent.
func ExtractNATS(ctx context.Context, headers nats.Header, subject string, attempt uint64) (context.Context, trace.Span) {
	ctx = otel.GetTextMapPropagator().Extract(ctx, NATSHeaderCarrier(headers))
	tracer := otel.Tracer("github.com/hermesnotifications/hermes/internal/observability")

	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			semconv.MessagingSystemKey.String("nats"),
			semconv.MessagingDestinationName(subject),
			semconv.MessagingOperationTypeReceive,
			// Named to match the delivery.send span in internal/delivery, so one
			// attribute answers "which attempt was this" across both.
			attribute.Int64("messaging.attempt", int64(attempt)),
		),
	}

	// Only when there is a producer context to link to. A message published before
	// this instrumentation existed has none, and WithNewRoot on a span that has no
	// parent anyway would just drop the link and change nothing.
	if attempt > 1 {
		if producer := trace.SpanContextFromContext(ctx); producer.IsValid() {
			opts = append(opts,
				trace.WithNewRoot(),
				trace.WithLinks(trace.Link{SpanContext: producer}),
			)
		}
	}

	ctx, span := tracer.Start(ctx, "nats.consume", opts...)
	return ctx, span
}

// RecordError is a small convenience for the common "set status + record" pattern.
func RecordError(span trace.Span, err error) {
	if err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}
