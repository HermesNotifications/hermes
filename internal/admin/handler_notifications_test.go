package admin_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/internal/models"
)

func TestHandleGetNotification(t *testing.T) {
	// Seed a notification directly via the mock store
	notificationID := "ntf-test-123"
	store := &mockStore{
		tenants: []models.Tenant{
			{ID: "test-tenant-id", Name: "Test Tenant", CreatedAt: time.Now()},
		},
		notifications: []models.Notification{
			{
				ID:       notificationID,
				TenantID: "test-tenant-id",
				UserID:   "usr-1",
				Title:    "Test Alert",
				Body:     "This is a test.",
				Channels: []string{"inbox"},
				Status:   models.StatusPending,
			},
		},
	}
	srv := newTestServerWithStore(t, store)

	// GET /v1/notifications/{id}
	req := httptest.NewRequest("GET", "/v1/notifications/"+notificationID, nil)
	rec := httptest.NewRecorder()
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
