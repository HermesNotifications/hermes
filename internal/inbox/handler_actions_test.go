// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package inbox_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleMarkRead(t *testing.T) {
	srv, store := newTestServer(t)

	req := httptest.NewRequest(http.MethodPut, "/v1/inbox/notif-1/read", nil)
	req = requestWithUser(req, testUserID)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if store.notifications[0].ReadAt == nil {
		t.Fatal("expected ReadAt to be set")
	}
}

func TestHandleMarkUnread(t *testing.T) {
	srv, store := newTestServer(t)

	// First mark as read
	req := httptest.NewRequest(http.MethodPut, "/v1/inbox/notif-1/read", nil)
	req = requestWithUser(req, testUserID)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	// Then mark as unread
	req = httptest.NewRequest(http.MethodDelete, "/v1/inbox/notif-1/read", nil)
	req = requestWithUser(req, testUserID)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if store.notifications[0].ReadAt != nil {
		t.Fatal("expected ReadAt to be nil after unread")
	}
}

func TestHandleArchive(t *testing.T) {
	srv, store := newTestServer(t)

	req := httptest.NewRequest(http.MethodPut, "/v1/inbox/notif-1/archive", nil)
	req = requestWithUser(req, testUserID)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if store.notifications[0].ArchivedAt == nil {
		t.Fatal("expected ArchivedAt to be set")
	}
}

func TestHandleUnarchive(t *testing.T) {
	srv, store := newTestServer(t)

	// First archive
	req := httptest.NewRequest(http.MethodPut, "/v1/inbox/notif-1/archive", nil)
	req = requestWithUser(req, testUserID)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	// Then unarchive
	req = httptest.NewRequest(http.MethodDelete, "/v1/inbox/notif-1/archive", nil)
	req = requestWithUser(req, testUserID)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if store.notifications[0].ArchivedAt != nil {
		t.Fatal("expected ArchivedAt to be nil after unarchive")
	}
}

func TestHandleSoftDelete(t *testing.T) {
	srv, store := newTestServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/v1/inbox/notif-1", nil)
	req = requestWithUser(req, testUserID)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if store.notifications[0].DeletedAt == nil {
		t.Fatal("expected DeletedAt to be set")
	}
}

func TestHandleMarkAllRead(t *testing.T) {
	srv, store := newTestServer(t)

	req := httptest.NewRequest(http.MethodPut, "/v1/inbox/read-all", nil)
	req = requestWithUser(req, testUserID)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	for _, n := range store.notifications {
		if n.UserID == testUserID && n.ReadAt == nil {
			t.Fatalf("expected all notifications to be read, but %s is unread", n.ID)
		}
	}
}

func TestHandleMarkRead_NoUser(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodPut, "/v1/inbox/notif-1/read", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleMarkRead_NotFound(t *testing.T) {
	srv, _ := newTestServer(t)

	// Marking a nonexistent notification is a no-op, not an error (idempotent)
	req := httptest.NewRequest(http.MethodPut, "/v1/inbox/nonexistent/read", nil)
	req = requestWithUser(req, testUserID)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
