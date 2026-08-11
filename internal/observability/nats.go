// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package observability

import (
	"context"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
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
	tracer := otel.Tracer("github.com/hermes-notifications/hermes/internal/observability")
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

// ExtractNATS extracts the remote trace context from message headers and
// starts a consumer span as a child of that context. Caller MUST call
// span.End() when processing is done.
func ExtractNATS(ctx context.Context, headers nats.Header, subject string) (context.Context, trace.Span) {
	ctx = otel.GetTextMapPropagator().Extract(ctx, NATSHeaderCarrier(headers))
	tracer := otel.Tracer("github.com/hermes-notifications/hermes/internal/observability")
	ctx, span := tracer.Start(ctx, "nats.consume",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			semconv.MessagingSystemKey.String("nats"),
			semconv.MessagingDestinationName(subject),
			semconv.MessagingOperationTypeReceive,
		),
	)
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
