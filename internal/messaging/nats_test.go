//go:build integration

package messaging_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/internal/messaging"
)

func testNATSUrl(t *testing.T) string {
	t.Helper()
	url := os.Getenv("HERMES_NATS_URL")
	if url == "" {
		url = "nats://localhost:4222"
	}
	return url
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

	payload := []byte(`{"test": true}`)
	if err := client.Publish(context.Background(), "notification.send", payload); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	received := make(chan []byte, 1)
	if err := client.Subscribe("notification.send", "test-consumer", 256, 1, func(_ context.Context, data []byte) error {
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
