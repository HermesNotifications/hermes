// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

//go:build integration

package e2e_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/hermes-notifications/hermes/internal/delivery"
	"github.com/hermes-notifications/hermes/internal/messaging"
	hermenats "github.com/hermes-notifications/hermes/internal/nats"
)

// alwaysFailingProvider stands in for an SMTP relay or webhook endpoint that is refusing
// connections.
type alwaysFailingProvider struct{ err error }

func (p *alwaysFailingProvider) Name() string { return "always-failing" }
func (p *alwaysFailingProvider) Send(context.Context, delivery.DeliveryRequest) (delivery.DeliveryResult, error) {
	return delivery.DeliveryResult{}, p.err
}

// Finding 9. The delivery workers returned nil on every failure, which the messaging
// layer reads as success — so the message was acked and dropped, and the DLQ machinery
// was unreachable from the delivery path. Nothing detected that, because no test ever
// drove a failing delivery to its conclusion. `docs/observability/runbooks/dead-letter-queue.md`
// even uses `dlq.delivery.email` as its worked example for a scenario that could not occur.
//
// This test exists so that cannot silently become true again: it asserts that a delivery
// the worker cannot process actually lands in the DLQ.
//
// It uses the PERMANENT path — an unparseable message — rather than exhausting
// maxDeliveries, because maxDeliveries is package-private to internal/messaging and
// draining ten attempts with backoff would make this test slow and flaky. What it proves
// is the part that was broken: the delivery worker now routes failures into the DLQ at
// all, rather than swallowing them.
func TestDLQ_UnprocessableDeliveryIsDeadLettered(t *testing.T) {
	natsURL := envOr("HERMES_NATS_URL", "nats://localhost:4222")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cleanupNATSConsumers(t, natsURL)

	client, err := messaging.Connect(natsURL)
	if err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	defer client.Close()
	if err := client.SetupStreams(ctx); err != nil {
		t.Fatalf("setup streams: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	worker := delivery.NewWorker(client, &alwaysFailingProvider{err: context.DeadlineExceeded},
		"email", "dlq-e2e-email-worker", logger)
	if err := worker.Start(ctx); err != nil {
		t.Fatalf("start worker: %v", err)
	}

	// A consumer on the DLQ, established before publishing so nothing is missed.
	raw, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatalf("dlq nats connect: %v", err)
	}
	defer raw.Close()
	js, err := jetstream.New(raw)
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	dlqConsumer, err := js.CreateOrUpdateConsumer(ctx, "DLQ", jetstream.ConsumerConfig{
		Name:          "dlq-e2e-reader",
		FilterSubject: "dlq.delivery.email",
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		t.Fatalf("create dlq consumer: %v", err)
	}

	// Unparseable on purpose: the worker classifies this as permanent, so it is
	// terminated straight to the DLQ instead of being retried.
	if err := client.Publish(ctx, "delivery.email", []byte("this is not a delivery message")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	msgs, err := dlqConsumer.Fetch(1, jetstream.FetchMaxWait(20*time.Second))
	if err != nil {
		t.Fatalf("fetch from dlq: %v", err)
	}

	var got *hermenats.DeadLetter
	for m := range msgs.Messages() {
		var dl hermenats.DeadLetter
		if err := json.Unmarshal(m.Data(), &dl); err != nil {
			t.Fatalf("unmarshal dead letter: %v", err)
		}
		got = &dl
		_ = m.Ack()
	}
	if err := msgs.Error(); err != nil {
		t.Fatalf("dlq fetch error: %v", err)
	}

	if got == nil {
		// Distinguish "nothing was ever published" from "published but this consumer
		// did not see it" — otherwise the failure is ambiguous and unhelpful.
		if s, serr := js.Stream(ctx, "DLQ"); serr == nil {
			if si, ierr := s.Info(ctx); ierr == nil {
				t.Logf("DLQ stream holds %d message(s), subjects=%v", si.State.Msgs, si.Config.Subjects)
			}
		}
		t.Fatal("nothing reached the DLQ — the delivery worker swallowed the failure, which is finding 9")
	}
	if got.Reason != hermenats.DeadLetterReasonTerminated {
		t.Errorf("dead-letter reason = %q, want %q — a permanent failure should terminate, not exhaust retries",
			got.Reason, hermenats.DeadLetterReasonTerminated)
	}
	if got.Subject != "delivery.email" {
		t.Errorf("dead letter records subject %q, want %q", got.Subject, "delivery.email")
	}
}
