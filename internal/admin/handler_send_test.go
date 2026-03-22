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

	// Create a group first
	groupBody := `{"slug":"billing","name":"Billing","default_channels":["email","inbox"]}`
	req := httptest.NewRequest("POST", "/v1/groups", bytes.NewBufferString(groupBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create group: %d %s", rec.Code, rec.Body.String())
	}

	// Send notification with direct content
	body := `{
		"tenant_id": "test-tenant-id",
		"user_id": "ext-user-1",
		"group": "billing",
		"content": {
			"title": "Invoice Paid",
			"body": "Your invoice has been paid."
		}
	}`
	req = httptest.NewRequest("POST", "/v1/send", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
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

func TestHandleSend_MissingTypeAndContent(t *testing.T) {
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

func TestHandleSend_BothTypeAndContent(t *testing.T) {
	srv := newTestServer(t)
	body := `{"tenant_id": "test-tenant-id", "user_id": "ext-1", "type": "foo", "content": {"title": "x", "body": "y"}}`
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
	body := `{"tenant_id": "unknown-tenant", "user_id": "ext-1", "content": {"title": "x", "body": "y"}, "group": "billing"}`
	req := httptest.NewRequest("POST", "/v1/send", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSend_MissingGroupForDirectSend(t *testing.T) {
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
