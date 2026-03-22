package admin_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleGetNotification(t *testing.T) {
	srv := newTestServer(t)

	// Create a group first
	groupBody := `{"slug":"alerts","name":"Alerts","default_channels":["inbox"]}`
	req := httptest.NewRequest("POST", "/v1/groups", bytes.NewBufferString(groupBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create group: %d %s", rec.Code, rec.Body.String())
	}

	// Send a notification
	sendBody := `{
		"tenant_id": "test-tenant-id",
		"user_id": "ext-user-1",
		"group": "alerts",
		"content": {
			"title": "Test Alert",
			"body": "This is a test."
		}
	}`
	req = httptest.NewRequest("POST", "/v1/send", bytes.NewBufferString(sendBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("send notification: %d %s", rec.Code, rec.Body.String())
	}

	var sendResp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&sendResp); err != nil {
		t.Fatalf("decode send response: %v", err)
	}
	notificationID := sendResp["notification_id"]
	if notificationID == "" {
		t.Fatal("expected notification_id in send response")
	}

	// GET /v1/notifications/{id}
	req = httptest.NewRequest("GET", "/v1/notifications/"+notificationID, nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["notification"] == nil {
		t.Fatal("expected notification in response")
	}
	if _, ok := resp["events"]; !ok {
		t.Fatal("expected events key in response")
	}
}

func TestHandleGetNotification_NotFound(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest("GET", "/v1/notifications/nonexistent", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}
