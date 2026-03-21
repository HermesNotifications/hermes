package delivery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebhookProvider_Send_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}
		w.Header().Set("X-Provider-ID", "test-provider-123")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	provider := NewWebhookProvider("test-webhook", server.URL)

	req := DeliveryRequest{
		NotificationID: "notif-1",
		TenantID:       "tenant-1",
		UserID:         "user-1",
		Channel:        "email",
		Title:          "Test Title",
		Body:           "Test Body",
	}

	result, err := provider.Send(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !result.Success {
		t.Error("expected success to be true")
	}
	if result.ProviderName != "test-webhook" {
		t.Errorf("expected provider name 'test-webhook', got %q", result.ProviderName)
	}
	if result.ProviderID != "test-provider-123" {
		t.Errorf("expected provider ID 'test-provider-123', got %q", result.ProviderID)
	}
}

func TestWebhookProvider_Send_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	provider := NewWebhookProvider("test-webhook", server.URL)

	req := DeliveryRequest{
		NotificationID: "notif-1",
		Channel:        "email",
		Title:          "Test Title",
		Body:           "Test Body",
	}

	result, err := provider.Send(context.Background(), req)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if result.Success {
		t.Error("expected success to be false")
	}
}

func TestWebhookProvider_Name(t *testing.T) {
	provider := NewWebhookProvider("my-webhook", "http://example.com")
	if provider.Name() != "my-webhook" {
		t.Errorf("expected name 'my-webhook', got %q", provider.Name())
	}
}
