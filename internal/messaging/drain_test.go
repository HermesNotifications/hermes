// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

//go:build integration

package messaging_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hermesnotifications/hermes/internal/messaging"
)

// The property that makes a rolling restart safe: a handler already running when shutdown
// begins is allowed to finish and ack.
//
// Before Drain existed, Close simply closed the connection and the process exited. The message
// was never acked, so JetStream redelivered it -- and whatever the handler had already done by
// then (sent the email, published to Centrifugo) happened a second time. Every rolling restart
// paid that cost for every message in flight.
func TestDrain_WaitsForInFlightHandlers(t *testing.T) {
	url := testNATSUrl(t)
	cleanupConsumers(t, url, "DELIVERY")

	client, err := messaging.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := client.SetupStreams(context.Background(), messaging.StreamOptions{}); err != nil {
		t.Fatalf("setup streams: %v", err)
	}

	var started, finished atomic.Bool
	release := make(chan struct{})

	err = client.Subscribe(messaging.SubscribeConfig{
		Subject:  "delivery.sms",
		Consumer: "drain-test",
		Workers:  1,
	}, func(ctx context.Context, _ []byte, _ messaging.DeliveryInfo) error {
		started.Store(true)
		<-release
		finished.Store(true)
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if err := client.Publish(context.Background(), "delivery.sms", []byte(`{}`)); err != nil {
		t.Fatalf("publish: %v", err)
	}

	waitUntil(t, "handler to start", func() bool { return started.Load() })

	// Drain concurrently: it must block on the handler rather than return immediately.
	drained := make(chan error, 1)
	go func() { drained <- client.Drain(10 * time.Second) }()

	select {
	case <-drained:
		t.Fatal("Drain returned while a handler was still running")
	case <-time.After(200 * time.Millisecond):
	}

	close(release)

	select {
	case err := <-drained:
		if err != nil {
			t.Fatalf("Drain: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Drain never returned after the handler finished")
	}

	if !finished.Load() {
		t.Fatal("the handler did not run to completion")
	}
}

// Drain must stop the fetcher before waiting, or a busy consumer keeps pulling new work for the
// whole shutdown window and never reaches zero in flight. Nothing used to stop it: the
// ConsumeContext that Consume returns was discarded.
func TestDrain_StopsPullingNewMessages(t *testing.T) {
	url := testNATSUrl(t)
	cleanupConsumers(t, url, "DELIVERY")

	client, err := messaging.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := client.SetupStreams(context.Background(), messaging.StreamOptions{}); err != nil {
		t.Fatalf("setup streams: %v", err)
	}

	var handled atomic.Int64
	firstStarted := make(chan struct{})
	release := make(chan struct{})
	var once bool

	err = client.Subscribe(messaging.SubscribeConfig{
		Subject:  "delivery.sms",
		Consumer: "drain-stop-test",
		Workers:  1,
		Prefetch: 1,
	}, func(ctx context.Context, _ []byte, _ messaging.DeliveryInfo) error {
		if !once {
			once = true
			close(firstStarted)
			<-release
		}
		handled.Add(1)
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// A backlog far larger than one message, so "stopped pulling" is observable.
	for i := 0; i < 20; i++ {
		if err := client.Publish(context.Background(), "delivery.sms", []byte(`{}`)); err != nil {
			t.Fatalf("publish %d: %v", i, err)
		}
	}

	<-firstStarted
	go func() {
		close(release)
	}()

	if err := client.Drain(10 * time.Second); err != nil {
		t.Fatalf("Drain: %v", err)
	}

	// The exact number depends on how many were already buffered when Stop landed; what must
	// not happen is the consumer working through the whole backlog regardless of the drain.
	if got := handled.Load(); got >= 20 {
		t.Fatalf("handled %d of 20 messages; Drain did not stop the fetcher", got)
	}
}

// Reported rather than swallowed: the messages are safe (unacked, so redelivered) but their
// side effects are about to be repeated, which is worth alerting on.
func TestDrain_ReportsTimeout(t *testing.T) {
	url := testNATSUrl(t)
	cleanupConsumers(t, url, "DELIVERY")

	client, err := messaging.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := client.SetupStreams(context.Background(), messaging.StreamOptions{}); err != nil {
		t.Fatalf("setup streams: %v", err)
	}

	started := make(chan struct{})
	block := make(chan struct{})
	defer close(block)

	err = client.Subscribe(messaging.SubscribeConfig{
		Subject:  "delivery.sms",
		Consumer: "drain-timeout-test",
		Workers:  1,
	}, func(ctx context.Context, _ []byte, _ messaging.DeliveryInfo) error {
		close(started)
		<-block
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if err := client.Publish(context.Background(), "delivery.sms", []byte(`{}`)); err != nil {
		t.Fatalf("publish: %v", err)
	}
	<-started

	if err := client.Drain(200 * time.Millisecond); !errors.Is(err, messaging.ErrDrainTimeout) {
		t.Fatalf("Drain error = %v, want ErrDrainTimeout", err)
	}
}

// A replica change migrates a whole file-backed stream between peers. It must not happen because
// a deploy rolled through with a different config — least of all in reverse, where pointing a
// single-node staging value at production would quietly strip every stream to one replica and
// report success.
func TestSetupStreams_RefusesAnUnattendedReplicaChange(t *testing.T) {
	url := testNATSUrl(t)
	client, err := messaging.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	if err := client.SetupStreams(ctx, messaging.StreamOptions{Replicas: 1}); err != nil {
		t.Fatalf("initial provision: %v", err)
	}

	err = client.SetupStreams(ctx, messaging.StreamOptions{Replicas: 3})
	if err == nil {
		t.Fatal("provisioning silently changed the replication factor")
	}
	if !strings.Contains(err.Error(), "HERMES_NATS_STREAM_REPLICAS_ALLOW_CHANGE") {
		t.Fatalf("error = %v, want it to name the override that makes the change deliberate", err)
	}
}

// Re-running with the same replica count is the normal case — every deploy does it — and must
// stay a clean no-op rather than tripping the guard.
func TestSetupStreams_IsIdempotentAtTheSameReplicaCount(t *testing.T) {
	url := testNATSUrl(t)
	client, err := messaging.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := client.SetupStreams(ctx, messaging.StreamOptions{Replicas: 1}); err != nil {
			t.Fatalf("provision %d: %v", i, err)
		}
	}
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
