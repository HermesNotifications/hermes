// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package messaging_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/hermesnotifications/hermes/internal/messaging"
	"github.com/nats-io/nats-server/v2/server"
)

// stall_test.go proves the state machine. This proves the wiring around it against a real
// JetStream server: that the poll reads the numbers it thinks it reads, that a consumer whose
// handlers never return counts as holding work (it is ack-pending, not pending), and that
// ConsumersProgressing is fed by the monitor goroutine Subscribe starts.
//
// The server is embedded, so `make test` still needs no infrastructure — the same trick
// permissions_test.go uses, without the accounts file.

func startJetStreamServer(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "hermes-stall-js")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	srv, err := server.NewServer(&server.Options{
		Port:      -1,
		JetStream: true,
		StoreDir:  dir,
		NoLog:     true,
		NoSigs:    true,
	})
	if err != nil {
		t.Fatalf("embedded nats: %v", err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(20 * time.Second) {
		t.Fatal("embedded nats did not become ready")
	}
	t.Cleanup(func() {
		srv.Shutdown()
		_ = os.RemoveAll(dir)
	})
	return srv.ClientURL()
}

// connectWithStallTimeout dials the embedded server with stall detection wound down to something
// a test can wait out. A second is 240 times shorter than production, and every property being
// asserted is a ratio between the window and the events inside it, not an absolute duration.
func connectWithStallTimeout(t *testing.T, url string, timeout time.Duration) *messaging.Client {
	t.Helper()
	client, err := messaging.Connect(url, messaging.WithConsumerStallTimeout(timeout))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(client.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := client.SetupStreams(ctx, messaging.StreamOptions{}); err != nil {
		t.Fatalf("setup streams: %v", err)
	}
	return client
}

// A consumer with nothing to do, polled repeatedly over several windows, must never report
// stalled. This is the case that decides whether the probe can be attached at all: dispatch is
// idle most nights.
func TestStall_IdleConsumerOnARealServerStaysHealthy(t *testing.T) {
	url := startJetStreamServer(t)
	client := connectWithStallTimeout(t, url, 500*time.Millisecond)

	if err := client.Subscribe(messaging.SubscribeConfig{
		Subject:  "notification.send",
		Consumer: "dispatch",
	}, func(context.Context, []byte, messaging.DeliveryInfo) error { return nil }); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := client.ConsumersProgressing(); err != nil {
			t.Fatalf("an idle consumer was reported stalled: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// The incident, reproduced: messages waiting, handlers that never return, and a consumer that
// therefore settles nothing. On the wire this shows up as NumAckPending rather than NumPending,
// which is why the poll sums the two.
func TestStall_WedgedHandlersAreReportedAndRecover(t *testing.T) {
	url := startJetStreamServer(t)
	client := connectWithStallTimeout(t, url, 500*time.Millisecond)

	release := make(chan struct{})
	entered := make(chan struct{}, 16)
	if err := client.Subscribe(messaging.SubscribeConfig{
		Subject:  "notification.send",
		Consumer: "dispatch",
		Workers:  2,
		// Long enough that the handler timeout cannot rescue this test by cancelling the wedge
		// for it — the point is a consumer that stays wedged.
		HandlerTimeout: time.Minute,
		AckWait:        2 * time.Minute,
	}, func(ctx context.Context, _ []byte, _ messaging.DeliveryInfo) error {
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for range 8 {
		if err := client.Publish(ctx, "notification.send", []byte(`{}`)); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}

	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("no message reached a handler")
	}

	if !eventually(t, 10*time.Second, func() bool { return client.ConsumersProgressing() != nil }) {
		t.Fatal("a consumer whose handlers never return was reported healthy")
	}

	// Recovery without a restart: the moment the handlers settle their messages, the check goes
	// green again on its own. A probe that latched would restart a pod that had already fixed
	// itself.
	close(release)
	if !eventually(t, 10*time.Second, func() bool { return client.ConsumersProgressing() == nil }) {
		t.Fatalf("still stalled after the handlers returned: %v", client.ConsumersProgressing())
	}
}

// Draining is not stalling, even when the drain leaves work behind: Drain stops the fetcher while
// messages are still queued, which is precisely the shape of a stall.
func TestStall_DrainingClientIsNotStalled(t *testing.T) {
	url := startJetStreamServer(t)
	client := connectWithStallTimeout(t, url, 300*time.Millisecond)

	if err := client.Subscribe(messaging.SubscribeConfig{
		Subject:  "notification.send",
		Consumer: "dispatch",
	}, func(context.Context, []byte, messaging.DeliveryInfo) error { return nil }); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for range 4 {
		if err := client.Publish(ctx, "notification.send", []byte(`{}`)); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}

	if err := client.Drain(5 * time.Second); err != nil {
		t.Fatalf("drain: %v", err)
	}

	time.Sleep(time.Second) // several windows
	if err := client.ConsumersProgressing(); err != nil {
		t.Fatalf("a drained client reported stalled: %v", err)
	}
}

func eventually(t *testing.T, within time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return false
}
