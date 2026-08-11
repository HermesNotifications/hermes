// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

//go:build integration

package messaging_test

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/internal/messaging"
	hermenats "github.com/hermes-notifications/hermes/internal/nats"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func testNATSUrl(t *testing.T) string {
	t.Helper()
	url := os.Getenv("HERMES_NATS_URL")
	if url == "" {
		url = "nats://localhost:4222"
	}
	return url
}

// cleanupConsumers deletes all consumers on the given streams so this test can
// create a fresh consumer without colliding with leftovers from other packages
// that share the same NATS instance (WorkQueue streams allow only one consumer
// per filter subject).
func cleanupConsumers(t *testing.T, url string, streams ...string) {
	t.Helper()
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("cleanup connect: %v", err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("cleanup jetstream: %v", err)
	}
	ctx := context.Background()
	for _, name := range streams {
		stream, err := js.Stream(ctx, name)
		if err != nil {
			continue // stream may not exist yet
		}
		for info := range stream.ListConsumers(ctx).Info() {
			_ = js.DeleteConsumer(ctx, name, info.Name)
		}
		// Purge retained messages so this test only sees its own publish.
		_ = stream.Purge(ctx)
	}
}

func TestConnect_And_SetupStreams(t *testing.T) {
	client, err := messaging.Connect(testNATSUrl(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Close()

	if err := client.SetupStreams(context.Background()); err != nil {
		t.Fatalf("SetupStreams: %v", err)
	}
}

func TestPublish_And_Subscribe(t *testing.T) {
	client, err := messaging.Connect(testNATSUrl(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Close()

	if err := client.SetupStreams(context.Background()); err != nil {
		t.Fatalf("SetupStreams: %v", err)
	}

	// Remove any consumers left on the shared NOTIFICATIONS stream by other
	// packages, so our subscribe on notification.send is unique.
	cleanupConsumers(t, testNATSUrl(t), "NOTIFICATIONS")

	payload := []byte(`{"test": true}`)
	if err := client.Publish(context.Background(), "notification.send", payload); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	received := make(chan []byte, 1)
	if err := client.Subscribe(messaging.SubscribeConfig{
		Subject: "notification.send", Consumer: "test-consumer", MaxAckPending: 256, Workers: 1,
	}, func(_ context.Context, data []byte, _ messaging.DeliveryInfo) error {
		received <- data
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	select {
	case msg := <-received:
		if string(msg) != string(payload) {
			t.Fatalf("expected %s, got %s", payload, msg)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for message")
	}
}

// TestSubscribe_PoolBoundsConcurrency guards the worker-pool semantics dispatch
// relies on: the single fetcher feeds exactly Workers parallel handlers — no fewer
// (parallelism works) and no more (concurrency is bounded even when far more
// messages are available than workers). Each handler holds its message in-flight on
// a release channel. We publish well more than Workers messages, confirm exactly
// Workers reach the barrier, and confirm no extra handler starts while they block.
func TestSubscribe_PoolBoundsConcurrency(t *testing.T) {
	client, err := messaging.Connect(testNATSUrl(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Close()
	if err := client.SetupStreams(context.Background()); err != nil {
		t.Fatalf("SetupStreams: %v", err)
	}
	cleanupConsumers(t, testNATSUrl(t), "NOTIFICATIONS")

	const workers = 4
	const total = 12
	for i := 0; i < total; i++ {
		if err := client.Publish(context.Background(), "notification.send", []byte(`{"n":1}`)); err != nil {
			t.Fatalf("Publish: %v", err)
		}
	}

	var inFlight atomic.Int32
	reached := make(chan struct{}, total)
	release := make(chan struct{})

	if err := client.Subscribe(messaging.SubscribeConfig{
		Subject: "notification.send", Consumer: "pool-test", MaxAckPending: 256, Workers: workers, Prefetch: 8,
	}, func(_ context.Context, _ []byte, _ messaging.DeliveryInfo) error {
		inFlight.Add(1)
		reached <- struct{}{}
		<-release // hold in-flight until the test releases every worker at once
		inFlight.Add(-1)
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// All `workers` handlers must reach the barrier (parallelism works).
	for i := 0; i < workers; i++ {
		select {
		case <-reached:
		case <-time.After(10 * time.Second):
			close(release)
			t.Fatalf("only %d/%d workers ran concurrently", inFlight.Load(), workers)
		}
	}

	// While all workers block, no additional handler may start, even though 8 more
	// messages are queued — the pool bounds concurrency to Workers (backpressure).
	time.Sleep(500 * time.Millisecond)
	if got := inFlight.Load(); got != workers {
		close(release)
		t.Fatalf("in-flight = %d, want exactly %d (pool not bounded)", got, workers)
	}
	select {
	case <-reached:
		close(release)
		t.Fatal("a 5th handler started while the pool was saturated; concurrency not bounded")
	default:
	}
	close(release)
}

// TestSubscribe_RecoversFromHandlerPanic verifies a panicking handler does not
// crash the worker pool: the panic is recovered and treated as a retryable failure,
// so the message is redelivered and (here, with maxDeliveries=2) eventually dead-
// lettered. If the pool died, no dead letter would ever appear.
func TestSubscribe_RecoversFromHandlerPanic(t *testing.T) {
	client, err := messaging.Connect(testNATSUrl(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Close()
	if err := client.SetupStreams(context.Background()); err != nil {
		t.Fatalf("SetupStreams: %v", err)
	}
	cleanupConsumers(t, testNATSUrl(t), "NOTIFICATIONS", "DLQ")

	restore := messaging.SetMaxDeliveriesForTest(2)
	defer restore()

	payload := []byte(`{"notification_id":"panic-test-1"}`)
	if err := client.Publish(context.Background(), "notification.send", payload); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if err := client.Subscribe(messaging.SubscribeConfig{
		Subject: "notification.send", Consumer: "panic-test", MaxAckPending: 256, Workers: 2,
	}, func(_ context.Context, _ []byte, _ messaging.DeliveryInfo) error {
		panic("boom")
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	dl := fetchDeadLetter(t, testNATSUrl(t), 15*time.Second)
	if dl.Reason != hermenats.DeadLetterReasonMaxDeliveries {
		t.Errorf("Reason = %q, want %q", dl.Reason, hermenats.DeadLetterReasonMaxDeliveries)
	}
	if dl.Attempts != 2 {
		t.Errorf("Attempts = %d, want 2", dl.Attempts)
	}
	waitForEmptyStream(t, testNATSUrl(t), "NOTIFICATIONS", 5*time.Second)
}

func TestSetupStreams_CreatesDLQ(t *testing.T) {
	client, err := messaging.Connect(testNATSUrl(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Close()

	if err := client.SetupStreams(context.Background()); err != nil {
		t.Fatalf("SetupStreams: %v", err)
	}

	nc, err := nats.Connect(testNATSUrl(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}

	stream, err := js.Stream(context.Background(), "DLQ")
	if err != nil {
		t.Fatalf("DLQ stream not found: %v", err)
	}
	info, err := stream.Info(context.Background())
	if err != nil {
		t.Fatalf("DLQ stream Info: %v", err)
	}
	cfg := info.Config
	if cfg.Retention != jetstream.LimitsPolicy {
		t.Errorf("Retention = %v, want LimitsPolicy", cfg.Retention)
	}
	if len(cfg.Subjects) != 1 || cfg.Subjects[0] != "dlq.>" {
		t.Errorf("Subjects = %v, want [dlq.>]", cfg.Subjects)
	}
	if cfg.MaxBytes != 1<<30 {
		t.Errorf("MaxBytes = %d, want %d", cfg.MaxBytes, 1<<30)
	}
	if cfg.MaxAge != 7*24*time.Hour {
		t.Errorf("MaxAge = %v, want 168h", cfg.MaxAge)
	}
	if cfg.Discard != jetstream.DiscardOld {
		t.Errorf("Discard = %v, want DiscardOld", cfg.Discard)
	}
	if cfg.Storage != jetstream.FileStorage {
		t.Errorf("Storage = %v, want FileStorage", cfg.Storage)
	}
}

// fetchDeadLetter reads the next dead letter off the DLQ stream.
func fetchDeadLetter(t *testing.T, url string, timeout time.Duration) *hermenats.DeadLetter {
	t.Helper()
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	cons, err := js.OrderedConsumer(context.Background(), "DLQ", jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{"dlq.>"},
	})
	if err != nil {
		t.Fatalf("ordered consumer: %v", err)
	}
	batch, err := cons.Fetch(1, jetstream.FetchMaxWait(timeout))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	msg := <-batch.Messages()
	if msg == nil {
		t.Fatalf("no dead letter within %v (batch error: %v)", timeout, batch.Error())
	}
	dl, err := hermenats.UnmarshalDeadLetter(msg.Data())
	if err != nil {
		t.Fatalf("unmarshal dead letter: %v", err)
	}
	return dl
}

// waitForEmptyStream polls until the stream has no messages (Term removes them).
func waitForEmptyStream(t *testing.T, url, stream string, timeout time.Duration) {
	t.Helper()
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s, err := js.Stream(context.Background(), stream)
		if err != nil {
			t.Fatalf("stream %s: %v", stream, err)
		}
		info, err := s.Info(context.Background())
		if err != nil {
			t.Fatalf("stream info: %v", err)
		}
		if info.State.Msgs == 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("stream %s not empty after %v", stream, timeout)
}

func TestSubscribe_DeadLettersAfterMaxDeliveries(t *testing.T) {
	client, err := messaging.Connect(testNATSUrl(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Close()
	if err := client.SetupStreams(context.Background()); err != nil {
		t.Fatalf("SetupStreams: %v", err)
	}
	cleanupConsumers(t, testNATSUrl(t), "NOTIFICATIONS", "DLQ")

	restore := messaging.SetMaxDeliveriesForTest(2)
	defer restore()

	payload := []byte(`{"notification_id":"dlq-test-1"}`)
	if err := client.Publish(context.Background(), "notification.send", payload); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if err := client.Subscribe(messaging.SubscribeConfig{
		Subject: "notification.send", Consumer: "dlq-exhaust-test", MaxAckPending: 256, Workers: 1,
	}, func(_ context.Context, _ []byte, _ messaging.DeliveryInfo) error {
		return errors.New("simulated transient failure")
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Attempt 1 fails → Nak (~0.5-1s backoff) → attempt 2 fails → dead letter.
	dl := fetchDeadLetter(t, testNATSUrl(t), 15*time.Second)
	if dl.Reason != hermenats.DeadLetterReasonMaxDeliveries {
		t.Errorf("Reason = %q, want %q", dl.Reason, hermenats.DeadLetterReasonMaxDeliveries)
	}
	if dl.Attempts != 2 {
		t.Errorf("Attempts = %d, want 2", dl.Attempts)
	}
	if dl.Subject != "notification.send" || dl.Stream != "NOTIFICATIONS" || dl.Consumer != "dlq-exhaust-test" {
		t.Errorf("identity fields = %s/%s/%s", dl.Subject, dl.Stream, dl.Consumer)
	}
	if dl.Error != "simulated transient failure" {
		t.Errorf("Error = %q", dl.Error)
	}
	if string(dl.Payload) != string(payload) {
		t.Errorf("Payload = %s, want %s", dl.Payload, payload)
	}
	// Term must remove the dead message from the WorkQueue stream.
	waitForEmptyStream(t, testNATSUrl(t), "NOTIFICATIONS", 5*time.Second)
}

type permanentTestError struct{}

func (permanentTestError) Error() string   { return "malformed payload" }
func (permanentTestError) Permanent() bool { return true }

func TestSubscribe_DeadLettersPermanentError(t *testing.T) {
	client, err := messaging.Connect(testNATSUrl(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Close()
	if err := client.SetupStreams(context.Background()); err != nil {
		t.Fatalf("SetupStreams: %v", err)
	}
	cleanupConsumers(t, testNATSUrl(t), "NOTIFICATIONS", "DLQ")

	payload := []byte(`{"not":"valid"}`)
	if err := client.Publish(context.Background(), "notification.send", payload); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if err := client.Subscribe(messaging.SubscribeConfig{
		Subject: "notification.send", Consumer: "dlq-perm-test", MaxAckPending: 256, Workers: 1,
	}, func(_ context.Context, _ []byte, _ messaging.DeliveryInfo) error {
		return permanentTestError{}
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	dl := fetchDeadLetter(t, testNATSUrl(t), 10*time.Second)
	if dl.Reason != hermenats.DeadLetterReasonTerminated {
		t.Errorf("Reason = %q, want %q", dl.Reason, hermenats.DeadLetterReasonTerminated)
	}
	if dl.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", dl.Attempts)
	}
	waitForEmptyStream(t, testNATSUrl(t), "NOTIFICATIONS", 5*time.Second)
}
