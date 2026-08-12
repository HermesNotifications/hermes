// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hermesnotifications/hermes/internal/centrifugo"
	"github.com/hermesnotifications/hermes/internal/models"
)

func TestInboxProvider_Send_Success(t *testing.T) {
	var capturedChannel string
	var capturedData map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode body: %v", err)
		}
		capturedChannel, _ = payload["channel"].(string)
		capturedData, _ = payload["data"].(map[string]any)
		w.WriteHeader(http.StatusOK)
		// What real Centrifugo answers on success. A double that returns a bare 200 still
		// passes — the client accepts a stripped envelope — but it cannot tell a working
		// client from one that ignores the body, which is how the logical-error bug survived.
		w.Write([]byte(`{"result":{}}`))
	}))
	defer server.Close()

	centrifugoClient := centrifugo.NewClient(server.URL, "test-key")
	provider := NewInboxProvider(centrifugoClient, nil, nil)

	req := DeliveryRequest{
		NotificationID: "notif-1",
		UserID:         "user-42",
		Title:          "Hello",
		Body:           "World",
	}

	result, err := provider.Send(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !result.Success {
		t.Error("expected success to be true")
	}
	if result.ProviderName != "inbox" {
		t.Errorf("expected provider name 'inbox', got %q", result.ProviderName)
	}

	expectedChannel := "user#user-42"
	if capturedChannel != expectedChannel {
		t.Errorf("expected channel %q, got %q", expectedChannel, capturedChannel)
	}

	if capturedData == nil {
		t.Fatal("expected data payload, got nil")
	}
	if capturedData["title"] != "Hello" {
		t.Errorf("expected title 'Hello', got %v", capturedData["title"])
	}
	if capturedData["id"] != "notif-1" {
		t.Errorf("expected id 'notif-1', got %v", capturedData["id"])
	}
}

// capturePublish stands up a Centrifugo double and returns the data payload it received.
func capturePublish(t *testing.T, req DeliveryRequest) map[string]any {
	t.Helper()
	var captured map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode body: %v", err)
		}
		captured, _ = payload["data"].(map[string]any)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":{}}`))
	}))
	defer server.Close()

	provider := NewInboxProvider(centrifugo.NewClient(server.URL, "test-key"), nil, nil)
	if _, err := provider.Send(context.Background(), req); err != nil {
		t.Fatalf("send: %v", err)
	}
	return captured
}

// The inbox worker has no database (cmd/worker-inbox wires NATS, Redis and Centrifugo and
// nothing else), so whatever a client is to receive has to arrive on the DeliveryRequest. This
// is the test that holds that path open.
func TestInboxProvider_Send_CarriesMetadataVerbatim(t *testing.T) {
	data := capturePublish(t, DeliveryRequest{
		NotificationID: "notif-1",
		UserID:         "user-42",
		Title:          "Hello",
		Body:           "World",
		Metadata: models.NotificationMetadata{
			"level":     "warning",
			"toast":     true,
			"invoiceId": "1041",
			"nested":    map[string]any{"a": float64(1)},
		},
	})

	metadata, ok := data["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("expected metadata on the published frame, got %#v", data["metadata"])
	}
	if metadata["level"] != "warning" {
		t.Errorf("level: got %#v", metadata["level"])
	}
	if metadata["toast"] != true {
		t.Errorf("toast: got %#v", metadata["toast"])
	}
	// The keys Hermes does not read must survive too, or "passthrough" is not true.
	if metadata["invoiceId"] != "1041" {
		t.Errorf("opaque key: got %#v", metadata["invoiceId"])
	}
	if _, ok := metadata["nested"].(map[string]any); !ok {
		t.Errorf("nested object: got %#v", metadata["nested"])
	}
}

func TestInboxProvider_Send_OmitsMetadataWhenThereIsNone(t *testing.T) {
	// Absent rather than null, so a send carrying no metadata produces the frame existing
	// clients already handle.
	data := capturePublish(t, DeliveryRequest{
		NotificationID: "notif-1",
		UserID:         "user-42",
		Title:          "Hello",
		Body:           "World",
	})

	if _, present := data["metadata"]; present {
		t.Errorf("metadata key present on a notification that had none: %#v", data["metadata"])
	}
}

func TestInboxProvider_Send_CentrifugoError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("centrifugo error"))
	}))
	defer server.Close()

	centrifugoClient := centrifugo.NewClient(server.URL, "test-key")
	provider := NewInboxProvider(centrifugoClient, nil, nil)

	req := DeliveryRequest{
		NotificationID: "notif-1",
		UserID:         "user-42",
		Title:          "Hello",
		Body:           "World",
	}

	result, err := provider.Send(context.Background(), req)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if result.Success {
		t.Error("expected success to be false")
	}
	if result.ProviderName != "inbox" {
		t.Errorf("expected provider name 'inbox', got %q", result.ProviderName)
	}
	if result.Error == "" {
		t.Error("expected non-empty error in result")
	}
}
