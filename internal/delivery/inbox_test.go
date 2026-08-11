// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hermes-notifications/hermes/internal/centrifugo"
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
