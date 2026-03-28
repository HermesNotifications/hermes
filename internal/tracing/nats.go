package tracing

import (
	"context"

	"github.com/DataDog/dd-trace-go/v2/datastreams"
	"github.com/DataDog/dd-trace-go/v2/ddtrace/tracer"
	"github.com/nats-io/nats.go"
)

// NATSHeaderCarrier adapts nats.Header for Datadog propagation (APM + DSM).
type NATSHeaderCarrier nats.Header

// Set sets a header value.
func (c NATSHeaderCarrier) Set(key, val string) {
	nats.Header(c).Set(key, val)
}

// ForeachKey iterates over all header key/value pairs.
func (c NATSHeaderCarrier) ForeachKey(handler func(key, val string) error) error {
	for k, vals := range nats.Header(c) {
		for _, v := range vals {
			if err := handler(k, v); err != nil {
				return err
			}
		}
	}
	return nil
}

// InjectNATS creates a produce span, sets a DSM checkpoint,
// and injects both trace and pathway context into message headers.
// Returns the enriched context and span. Caller must call span.Finish().
func InjectNATS(ctx context.Context, msg *nats.Msg) (context.Context, *tracer.Span) {
	span, ctx := tracer.StartSpanFromContext(ctx, "nats.publish",
		tracer.ResourceName(msg.Subject),
		tracer.SpanType("queue"),
	)

	ctx, _ = tracer.SetDataStreamsCheckpoint(ctx,
		"direction:out", "type:nats", "topic:"+msg.Subject,
	)

	if msg.Header == nil {
		msg.Header = nats.Header{}
	}
	carrier := NATSHeaderCarrier(msg.Header)
	_ = tracer.Inject(span.Context(), carrier)
	datastreams.InjectToBase64Carrier(ctx, carrier)

	return ctx, span
}

// ExtractNATS extracts trace and pathway context from a received message,
// sets a DSM consume checkpoint, and returns a context with an active span.
// Caller must call span.Finish() when processing is done.
func ExtractNATS(ctx context.Context, headers nats.Header, subject string) (context.Context, *tracer.Span) {
	carrier := NATSHeaderCarrier(headers)

	opts := []tracer.StartSpanOption{
		tracer.ResourceName(subject),
		tracer.SpanType("queue"),
	}
	if sctx, err := tracer.Extract(carrier); err == nil {
		opts = append(opts, tracer.ChildOf(sctx)) //nolint:staticcheck // ChildOf is correct for remote parent contexts extracted from headers
	}
	span, ctx := tracer.StartSpanFromContext(ctx, "nats.consume", opts...)

	ctx = datastreams.ExtractFromBase64Carrier(ctx, carrier)
	ctx, _ = tracer.SetDataStreamsCheckpoint(ctx,
		"direction:in", "type:nats", "topic:"+subject,
	)

	return ctx, span
}
