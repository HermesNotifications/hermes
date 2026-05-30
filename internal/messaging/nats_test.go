//go:build integration

package messaging_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/internal/messaging"
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
	if err := client.Subscribe("notification.send", "test-consumer", 256, 1, func(_ context.Context, data []byte, _ messaging.DeliveryInfo) error {
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
