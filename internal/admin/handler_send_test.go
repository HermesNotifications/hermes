package admin_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleSend_DirectContent(t *testing.T) {
	srv := newTestServer(t)

	// Send notification with direct content and explicit channels
	body := `{
		"tenant_id": "test-tenant-id",
		"user_id": "ext-user-1",
		"content": {
			"title": "Invoice Paid",
			"body": "Your invoice has been paid."
		},
		"channels": ["email"]
	}`
	req := httptest.NewRequest("POST", "/v1/send", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["notification_id"] == "" {
		t.Fatal("expected notification_id in response")
	}
}

func TestHandleSend_MissingTemplateAndContent(t *testing.T) {
	srv := newTestServer(t)
	body := `{"tenant_id": "test-tenant-id", "user_id": "ext-1"}`
	req := httptest.NewRequest("POST", "/v1/send", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleSend_BothTemplateAndContent(t *testing.T) {
	srv := newTestServer(t)
	body := `{"tenant_id": "test-tenant-id", "user_id": "ext-1", "template": "foo", "content": {"title": "x", "body": "y"}}`
	req := httptest.NewRequest("POST", "/v1/send", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleSend_UnknownTenant(t *testing.T) {
	srv := newTestServer(t)
	body := `{"tenant_id": "unknown-tenant", "user_id": "ext-1", "content": {"title": "x", "body": "y"}, "channels": ["email"]}`
	req := httptest.NewRequest("POST", "/v1/send", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSend_MissingChannelsForDirectSend(t *testing.T) {
	srv := newTestServer(t)
	body := `{"tenant_id": "test-tenant-id", "user_id": "ext-1", "content": {"title": "x", "body": "y"}}`
	req := httptest.NewRequest("POST", "/v1/send", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
