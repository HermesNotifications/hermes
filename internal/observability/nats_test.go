// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package observability_test

import (
	"context"
	"testing"

	"github.com/hermesnotifications/hermes/internal/observability"
	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// withRecorder installs a recording TracerProvider and the W3C propagator as the
// process globals, restoring both afterwards.
//
// The globals are the point rather than an inconvenience: InjectNATS and
// ExtractNATS deliberately read otel.GetTextMapPropagator() so that services get
// whatever Init configured, and a test that swapped in its own propagator would
// stop testing that wiring. Tests using this must not call t.Parallel().
func withRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()

	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))

	prevTracer := otel.GetTracerProvider()
	prevProp := otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTracer)
		otel.SetTextMapPropagator(prevProp)
	})
	return sr
}

func TestInjectNATS_WritesTraceparent(t *testing.T) {
	withRecorder(t)

	msg := &nats.Msg{Subject: "notification.send"}
	_, span := observability.InjectNATS(context.Background(), msg)
	span.End()

	got := msg.Header.Get("traceparent")
	if got == "" {
		t.Fatal("no traceparent header written; the message would start a new trace downstream")
	}
	// The header must name the span we just created, not merely be well-formed.
	sc := span.SpanContext()
	if want := sc.TraceID().String(); !contains(got, want) {
		t.Errorf("traceparent %q does not carry trace ID %s", got, want)
	}
	if want := sc.SpanID().String(); !contains(got, want) {
		t.Errorf("traceparent %q does not carry span ID %s", got, want)
	}
}

// TestExtractNATS_JoinsTheProducerTrace is the load-bearing one: it is the
// property the whole pipeline's cross-service tracing rests on.
func TestExtractNATS_JoinsTheProducerTrace(t *testing.T) {
	withRecorder(t)

	msg := &nats.Msg{Subject: "notification.send"}
	_, producer := observability.InjectNATS(context.Background(), msg)
	producer.End()

	_, consumer := observability.ExtractNATS(context.Background(), msg.Header, msg.Subject, 1)
	consumer.End()

	psc, csc := producer.SpanContext(), consumer.SpanContext()
	if psc.TraceID() != csc.TraceID() {
		t.Fatalf("consumer started a new trace: producer %s, consumer %s", psc.TraceID(), csc.TraceID())
	}

	// Same trace is necessary but not sufficient -- the consumer must hang off
	// the producer specifically, or the trace is a flat list rather than a tree.
	ro, ok := consumer.(sdktrace.ReadOnlySpan)
	if !ok {
		t.Fatal("consumer span is not a ReadOnlySpan")
	}
	if parent := ro.Parent().SpanID(); parent != psc.SpanID() {
		t.Errorf("consumer parent = %s, want the producer span %s", parent, psc.SpanID())
	}
	if ro.SpanKind() != trace.SpanKindConsumer {
		t.Errorf("consumer span kind = %v, want Consumer", ro.SpanKind())
	}
}

// A redelivery is a new unit of work, minutes after the publish it came from. Parenting
// it there would claim the original span was still open — with retryDelay capped at 240s
// and maxDeliveries at 10, by a quarter of an hour. It gets its own trace and a link.
func TestExtractNATS_RedeliveryStartsANewTraceLinkedToTheProducer(t *testing.T) {
	withRecorder(t)

	msg := &nats.Msg{Subject: "delivery.email"}
	_, producer := observability.InjectNATS(context.Background(), msg)
	producer.End()

	_, retry := observability.ExtractNATS(context.Background(), msg.Header, msg.Subject, 3)
	retry.End()

	psc, rsc := producer.SpanContext(), retry.SpanContext()
	if psc.TraceID() == rsc.TraceID() {
		t.Error("redelivery joined the producer's trace; it should start its own")
	}

	ro, ok := retry.(sdktrace.ReadOnlySpan)
	if !ok {
		t.Fatal("retry span is not a ReadOnlySpan")
	}
	if ro.Parent().IsValid() {
		t.Errorf("redelivery has parent %s, want a root span", ro.Parent().SpanID())
	}

	links := ro.Links()
	if len(links) != 1 {
		t.Fatalf("redelivery has %d links, want 1 back to the producer", len(links))
	}
	if links[0].SpanContext.SpanID() != psc.SpanID() {
		t.Errorf("link points at %s, want the producer span %s",
			links[0].SpanContext.SpanID(), psc.SpanID())
	}
}

// The first delivery must keep its parent. That is the ordinary messaging shape and the
// thing that makes the pipeline read as one trace end to end.
func TestExtractNATS_FirstDeliveryIsNotLinked(t *testing.T) {
	withRecorder(t)

	msg := &nats.Msg{Subject: "delivery.email"}
	_, producer := observability.InjectNATS(context.Background(), msg)
	producer.End()

	_, consumer := observability.ExtractNATS(context.Background(), msg.Header, msg.Subject, 1)
	consumer.End()

	ro, ok := consumer.(sdktrace.ReadOnlySpan)
	if !ok {
		t.Fatal("consumer span is not a ReadOnlySpan")
	}
	if n := len(ro.Links()); n != 0 {
		t.Errorf("first delivery carries %d links, want 0 — it has a real parent", n)
	}
	if ro.Parent().SpanID() != producer.SpanContext().SpanID() {
		t.Errorf("first delivery parent = %s, want the producer %s",
			ro.Parent().SpanID(), producer.SpanContext().SpanID())
	}
}

// A message published before this instrumentation existed carries no producer context,
// so there is nothing to link to and nothing to detach from.
func TestExtractNATS_RedeliveryWithoutAProducerContextIsJustARoot(t *testing.T) {
	withRecorder(t)

	_, span := observability.ExtractNATS(context.Background(), nil, "delivery.email", 4)
	span.End()

	ro, ok := span.(sdktrace.ReadOnlySpan)
	if !ok {
		t.Fatal("span is not a ReadOnlySpan")
	}
	if n := len(ro.Links()); n != 0 {
		t.Errorf("got %d links with no producer context, want 0", n)
	}
	if !span.SpanContext().IsValid() {
		t.Error("span context is invalid")
	}
}

// The attempt is on the span either way, so "which try was this" is answerable without
// inferring it from the trace shape.
func TestExtractNATS_RecordsTheAttempt(t *testing.T) {
	withRecorder(t)

	_, span := observability.ExtractNATS(context.Background(), nil, "delivery.email", 7)
	span.End()

	ro, ok := span.(sdktrace.ReadOnlySpan)
	if !ok {
		t.Fatal("span is not a ReadOnlySpan")
	}
	var got int64
	for _, attr := range ro.Attributes() {
		if attr.Key == "messaging.attempt" {
			got = attr.Value.AsInt64()
		}
	}
	if got != 7 {
		t.Errorf("messaging.attempt = %d, want 7", got)
	}
}

func TestExtractNATS_WithoutHeadersStartsARootSpan(t *testing.T) {
	withRecorder(t)

	// A message published before this instrumentation existed, or by a producer
	// that does not inject. It must yield a usable root span, not an error and
	// not an invalid context.
	_, span := observability.ExtractNATS(context.Background(), nil, "notification.send", 1)
	span.End()

	if !span.SpanContext().IsValid() {
		t.Fatal("headerless message produced an invalid span context")
	}
	ro, ok := span.(sdktrace.ReadOnlySpan)
	if !ok {
		t.Fatal("span is not a ReadOnlySpan")
	}
	if ro.Parent().IsValid() {
		t.Errorf("headerless message got parent %s, want a root span", ro.Parent().SpanID())
	}
}

func TestInjectNATS_AllocatesANilHeader(t *testing.T) {
	withRecorder(t)

	// nats.Msg literals built without a Header are the common case -- see
	// messaging.Client.Publish -- so injection has to allocate rather than panic.
	msg := &nats.Msg{Subject: "delivery.email"}
	if msg.Header != nil {
		t.Fatal("precondition: header should start nil")
	}
	_, span := observability.InjectNATS(context.Background(), msg)
	span.End()

	if msg.Header == nil {
		t.Fatal("header still nil after inject")
	}
}

func TestNATSHeaderCarrier_RoundTrip(t *testing.T) {
	c := observability.NATSHeaderCarrier(nats.Header{})
	c.Set("traceparent", "00-abc-def-01")
	c.Set("tracestate", "vendor=1")

	if got := c.Get("traceparent"); got != "00-abc-def-01" {
		t.Errorf("Get(traceparent) = %q", got)
	}
	if got := c.Get("absent"); got != "" {
		t.Errorf("Get(absent) = %q, want empty", got)
	}
	if got := len(c.Keys()); got != 2 {
		t.Errorf("Keys() returned %d entries, want 2", got)
	}
}

func TestRecordError_NilIsANoop(t *testing.T) {
	withRecorder(t)

	_, span := otel.Tracer("test").Start(context.Background(), "s")
	observability.RecordError(span, nil)
	span.End()

	ro, ok := span.(sdktrace.ReadOnlySpan)
	if !ok {
		t.Fatal("span is not a ReadOnlySpan")
	}
	if n := len(ro.Events()); n != 0 {
		t.Errorf("RecordError(nil) recorded %d events, want 0", n)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
