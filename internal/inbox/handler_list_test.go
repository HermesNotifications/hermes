// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package inbox_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/models"
)

func requestWithUser(r *http.Request, userID string) *http.Request {
	ctx := context.WithValue(r.Context(), auth.ContextKeyUserID, userID)
	ctx = context.WithValue(ctx, auth.ContextKeyOrganizationID, "organization-1")
	return r.WithContext(ctx)
}

func TestHandleListInbox(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/inbox", nil)
	req = requestWithUser(req, testUserID)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Data        []models.Notification `json:"data"`
		UnreadCount int                   `json:"unread_count"`
		Cursor      string                `json:"cursor"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 notifications, got %d", len(resp.Data))
	}
	if resp.UnreadCount != 2 {
		t.Fatalf("expected unread_count=2, got %d", resp.UnreadCount)
	}
}

func TestHandleListInbox_NoUser(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/inbox", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleListInbox_WithLimit(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/inbox?limit=1", nil)
	req = requestWithUser(req, testUserID)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Data        []models.Notification `json:"data"`
		UnreadCount int                   `json:"unread_count"`
		Cursor      string                `json:"cursor"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(resp.Data))
	}
}
