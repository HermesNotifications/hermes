// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

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
		organizations: []models.Organization{
			{ID: "test-organization-id", Name: "Test Organization", CreatedAt: time.Now()},
		},
		notifications: []models.Notification{
			{
				ID:             notificationID,
				OrganizationID: "test-organization-id",
				UserID:         "usr-1",
				Title:          "Test Alert",
				Body:           "This is a test.",
				Channels:       []string{"inbox"},
				Status:         models.StatusPending,
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

func TestListNotifications(t *testing.T) {
	store := &mockStore{
		organizations: []models.Organization{
			{ID: "organization-1", Name: "Test", CreatedAt: time.Now()},
		},
		notifications: []models.Notification{
			{ID: "notif-1", OrganizationID: "organization-1", UserID: "usr-1", Title: "Hello", Status: "sent", CreatedAt: time.Now()},
			{ID: "notif-2", OrganizationID: "organization-1", UserID: "usr-2", Title: "World", Status: "pending", CreatedAt: time.Now()},
			{ID: "notif-3", OrganizationID: "organization-1", UserID: "usr-1", Title: "Test", Status: "delivered", CreatedAt: time.Now()},
		},
	}
	srv := newTestServerWithStore(t, store)

	req := httptest.NewRequest("GET", "/v1/notifications", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp []models.Notification
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(resp) != 3 {
		t.Fatalf("expected 3 notifications, got %d", len(resp))
	}
}

func TestListNotifications_WithLimit(t *testing.T) {
	store := &mockStore{
		organizations: []models.Organization{
			{ID: "organization-1", Name: "Test", CreatedAt: time.Now()},
		},
		notifications: []models.Notification{
			{ID: "notif-1", OrganizationID: "organization-1", UserID: "usr-1", Title: "A", Status: "sent", CreatedAt: time.Now()},
			{ID: "notif-2", OrganizationID: "organization-1", UserID: "usr-2", Title: "B", Status: "sent", CreatedAt: time.Now()},
			{ID: "notif-3", OrganizationID: "organization-1", UserID: "usr-1", Title: "C", Status: "sent", CreatedAt: time.Now()},
		},
	}
	srv := newTestServerWithStore(t, store)

	req := httptest.NewRequest("GET", "/v1/notifications?limit=2", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp []models.Notification
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(resp) != 2 {
		t.Fatalf("expected 2 notifications, got %d", len(resp))
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
