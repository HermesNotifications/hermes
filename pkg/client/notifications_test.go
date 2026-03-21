package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hermes-notifications/hermes/pkg/client"
)

func TestNotificationsSend(t *testing.T) {
	sendResp := client.SendResponse{NotificationID: "notif_01"}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/send" {
			t.Errorf("expected /v1/send, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json, got %s", r.Header.Get("Content-Type"))
		}

		var body client.SendRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode body: %v", err)
		}
		if body.TenantID != "tenant1" || body.UserID != "user1" {
			t.Errorf("unexpected body: %+v", body)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(sendResp)
	}))
	defer srv.Close()

	c := client.New(srv.URL, "test-key")
	result, err := c.Notifications.Send(context.Background(), client.SendRequest{
		TenantID: "tenant1",
		UserID:   "user1",
		Type:     "welcome",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NotificationID != "notif_01" {
		t.Errorf("expected notification_id notif_01, got %s", result.NotificationID)
	}
}

func TestNotificationsSendWithIdempotencyKey(t *testing.T) {
	sendResp := client.SendResponse{NotificationID: "notif_02"}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ikey := r.Header.Get("X-Idempotency-Key")
		if ikey != "my-idempotency-key" {
			t.Errorf("expected X-Idempotency-Key my-idempotency-key, got %q", ikey)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(sendResp)
	}))
	defer srv.Close()

	c := client.New(srv.URL, "test-key")
	result, err := c.Notifications.Send(context.Background(), client.SendRequest{
		TenantID: "tenant1",
		UserID:   "user1",
	}, client.WithIdempotencyKey("my-idempotency-key"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.NotificationID != "notif_02" {
		t.Errorf("expected notification_id notif_02, got %s", result.NotificationID)
	}
}

func TestNotificationsGetStatus(t *testing.T) {
	rawNotif := json.RawMessage(`{"id":"n1","status":"delivered"}`)
	rawEvents := json.RawMessage(`[{"type":"delivered","created_at":"2024-01-01T00:00:00Z"}]`)
	statusResp := client.NotificationStatus{
		Notification: rawNotif,
		Events:       rawEvents,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/v1/notifications/n1" {
			t.Errorf("expected /v1/notifications/n1, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(statusResp)
	}))
	defer srv.Close()

	c := client.New(srv.URL, "test-key")
	result, err := c.Notifications.GetStatus(context.Background(), "n1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(result.Notification) != string(rawNotif) {
		t.Errorf("unexpected notification: %s", result.Notification)
	}
	if string(result.Events) != string(rawEvents) {
		t.Errorf("unexpected events: %s", result.Events)
	}
}
