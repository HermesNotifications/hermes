// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

//go:build integration

package messaging_test

import (
	"context"
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
