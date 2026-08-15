// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

//go:build integration

package messaging_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hermesnotifications/hermes/internal/messaging"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// TestPublishSubscribe_CarriesTraceContextAcrossTheBroker is the end-to-end
// version of the unit tests in internal/observability: it goes through the real
// Client.Publish and Client.Subscribe, a real JetStream broker, and a real
// durable consumer.
//
// It exists because everything the unit tests prove is about the helpers in
// isolation. The property that actually matters -- that a handler runs inside
// the publisher's trace -- depends on Publish attaching headers, JetStream
// preserving them, and processMessage extracting them before invoking the
// handler. Only a round trip through the broker covers all three.
func TestPublishSubscribe_CarriesTraceContextAcrossTheBroker(t *testing.T) {
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

	url := testNATSUrl(t)
	cleanupConsumers(t, url, "NOTIFICATIONS")

	client, err := messaging.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	if err := client.SetupStreams(ctx, messaging.StreamOptions{}); err != nil {
		t.Fatalf("setup streams: %v", err)
	}

	// Buffered: the handler must never block on a test that has already failed.
	got := make(chan trace.SpanContext, 1)
	err = client.Subscribe(messaging.SubscribeConfig{
		Subject:  "notification.send",
		Consumer: "trace-propagation-test",
		Workers:  1,
	}, func(ctx context.Context, _ []byte, _ messaging.DeliveryInfo) error {
		select {
		case got <- trace.SpanContextFromContext(ctx):
		default:
		}
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// The publisher's span stands in for the HTTP server span that wraps a real
	// POST /v1/send.
	pubCtx, parent := tp.Tracer("test").Start(ctx, "test.publish")
	if err := client.Publish(pubCtx, "notification.send", []byte(`{"test":true}`)); err != nil {
		t.Fatalf("publish: %v", err)
	}
	parent.End()

	select {
	case sc := <-got:
		if !sc.IsValid() {
			t.Fatal("handler ran with no span context: the trace stops at the broker")
		}
		if sc.TraceID() != parent.SpanContext().TraceID() {
			t.Errorf("handler trace = %s, want the publisher's %s",
				sc.TraceID(), parent.SpanContext().TraceID())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the message")
	}
}

// A redelivery must land on its own trace, linked back rather than parented.
//
// The unit tests in internal/observability cover the branch; this covers the premise
// underneath it — that JetStream really does hand the original headers back on a
// redelivery, so attempt 2 would otherwise graft onto the first attempt's trace. If that
// stopped being true the branch would still be exercised and would still be pointless.
func TestRedelivery_StartsItsOwnTraceLinkedToThePublisher(t *testing.T) {
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

	// Two attempts is enough to see the second one, and stops the message being
	// redelivered underneath the assertions afterwards.
	defer messaging.SetMaxDeliveriesForTest(2)()

	url := testNATSUrl(t)
	cleanupConsumers(t, url, "NOTIFICATIONS")

	client, err := messaging.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	if err := client.SetupStreams(ctx, messaging.StreamOptions{}); err != nil {
		t.Fatalf("setup streams: %v", err)
	}

	traces := make(chan trace.TraceID, 4)
	err = client.Subscribe(messaging.SubscribeConfig{
		Subject:  "notification.send",
		Consumer: "redelivery-trace-test",
		Workers:  1,
	}, func(ctx context.Context, _ []byte, info messaging.DeliveryInfo) error {
		select {
		case traces <- trace.SpanContextFromContext(ctx).TraceID():
		default:
		}
		// Fail the first attempt so the message comes back; succeed on the second so
		// it settles instead of dead-lettering.
		if info.Attempt == 1 {
			return errors.New("forced failure to trigger a redelivery")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	pubCtx, parent := tp.Tracer("test").Start(ctx, "test.publish")
	if err := client.Publish(pubCtx, "notification.send", []byte(`{"test":true}`)); err != nil {
		t.Fatalf("publish: %v", err)
	}
	parent.End()
	published := parent.SpanContext().TraceID()

	var first, second trace.TraceID
	// The nak backoff is retryDelay(1), which is at most one second.
	deadline := time.After(20 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case id := <-traces:
			if i == 0 {
				first = id
			} else {
				second = id
			}
		case <-deadline:
			t.Fatalf("timed out after %d deliveries", i)
		}
	}

	if first != published {
		t.Errorf("first delivery trace = %s, want the publisher's %s", first, published)
	}
	if second == published {
		t.Error("redelivery reused the publisher's trace; it should start its own")
	}
	if !second.IsValid() {
		t.Error("redelivery had no trace at all")
	}
}
