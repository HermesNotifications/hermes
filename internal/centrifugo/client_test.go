package centrifugo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_Publish_Success(t *testing.T) {
	var capturedChannel string
	var capturedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/publish" {
			t.Errorf("expected path /api/publish, got %s", r.URL.Path)
		}
		capturedAuth = r.Header.Get("Authorization")

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode body: %v", err)
		}
		capturedChannel, _ = payload["channel"].(string)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-api-key")
	err := client.Publish(context.Background(), "user#123", map[string]string{"title": "Hello"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if capturedChannel != "user#123" {
		t.Errorf("expected channel 'user#123', got %q", capturedChannel)
	}
	if capturedAuth != "apikey test-api-key" {
		t.Errorf("expected Authorization 'apikey test-api-key', got %q", capturedAuth)
	}
}

func TestClient_Publish_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-api-key")
	err := client.Publish(context.Background(), "user#123", map[string]string{"title": "Hello"})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}
