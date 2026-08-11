// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package inbox_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func getUnreadCount(t *testing.T, srv interface{ Handler() http.Handler }, userID string) (int, int) {
	t.Helper()
	req := requestWithUser(httptest.NewRequest(http.MethodGet, "/v1/inbox/unread-count", nil), userID)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		return 0, rec.Code
	}
	var body struct {
		UnreadCount int `json:"unread_count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body.UnreadCount, rec.Code
}

func TestGetUnreadCount(t *testing.T) {
	srv, _ := newTestServer(t)

	count, code := getUnreadCount(t, srv, testUserID)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if count != 2 {
		t.Fatalf("unread_count = %d, want 2", count)
	}
}

// The point of the endpoint: a badge can have the number without pulling any rows.
func TestGetUnreadCount_ReturnsNoNotifications(t *testing.T) {
	srv, _ := newTestServer(t)

	req := requestWithUser(httptest.NewRequest(http.MethodGet, "/v1/inbox/unread-count", nil), testUserID)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, present := body["data"]; present {
		t.Fatal("the response carries notification data; the endpoint exists precisely to avoid that")
	}
}

// A warm cache must serve this without a database round trip -- that is the whole reason the
// endpoint is safe to call on every page load.
func TestGetUnreadCount_ServedFromCache(t *testing.T) {
	srv, store := newTestServer(t)
	srv.SetUnreadCache(&fakeUnreadCache{count: 17, ttl: 10 * time.Minute, found: true})

	count, _ := getUnreadCount(t, srv, testUserID)
	if count != 17 {
		t.Fatalf("unread_count = %d, want the cached 17", count)
	}
	if store.unreadCountCalls != 0 {
		t.Fatalf("the store was counted %d time(s); a warm cache must not reach it", store.unreadCountCalls)
	}
}

func TestGetUnreadCount_NoUser(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/inbox/unread-count", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// `unread-count` sits where a notification ID would. Routing prefers the static segment, so the
// two do not collide -- but only a test says so, and this is exactly the kind of thing a future
// `GET /v1/inbox/{id}` would break silently.
func TestGetUnreadCount_IsNotShadowedByTheIDRoute(t *testing.T) {
	srv, store := newTestServer(t)

	count, code := getUnreadCount(t, srv, testUserID)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 -- the literal path was matched as an id", code)
	}
	if count != 2 {
		t.Fatalf("unread_count = %d, want 2", count)
	}
	// Nothing may have been mutated: an id-route match would have marked something.
	for _, n := range store.notifications {
		if n.ReadAt != nil || n.ArchivedAt != nil || n.DeletedAt != nil {
			t.Fatalf("notification %s was mutated by a GET on the unread-count path", n.ID)
		}
	}
}
