package userservice_test

import (
	"bytes"
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
	ctx = context.WithValue(ctx, auth.ContextKeyTenantID, "tenant-1")
	return r.WithContext(ctx)
}

func TestHandleGetProfile(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/users/me", nil)
	req = requestWithUser(req, testUserID)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var user models.User
	if err := json.NewDecoder(rec.Body).Decode(&user); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if user.ID != testUserID {
		t.Fatalf("expected user ID %q, got %q", testUserID, user.ID)
	}
	if user.Email == nil || *user.Email != "user@example.com" {
		t.Fatalf("expected email user@example.com, got %v", user.Email)
	}
}

func TestHandleGetProfile_NoUser(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/users/me", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleUpdateContacts(t *testing.T) {
	srv, store := newTestServer(t)

	body := `{"email":"new@example.com","phone":"+15551234567"}`
	req := httptest.NewRequest(http.MethodPut, "/v1/users/me/contacts", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = requestWithUser(req, testUserID)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var user models.User
	if err := json.NewDecoder(rec.Body).Decode(&user); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if user.Email == nil || *user.Email != "new@example.com" {
		t.Fatalf("expected email new@example.com, got %v", user.Email)
	}
	if user.Phone == nil || *user.Phone != "+15551234567" {
		t.Fatalf("expected phone +15551234567, got %v", user.Phone)
	}

	// Verify store was updated
	if store.users[0].Email == nil || *store.users[0].Email != "new@example.com" {
		t.Fatal("store was not updated")
	}
}

func TestHandleUpdateContacts_MissingFields(t *testing.T) {
	srv, _ := newTestServer(t)

	body := `{}`
	req := httptest.NewRequest(http.MethodPut, "/v1/users/me/contacts", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = requestWithUser(req, testUserID)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleUpdateContacts_InvalidJSON(t *testing.T) {
	srv, _ := newTestServer(t)

	body := `{invalid`
	req := httptest.NewRequest(http.MethodPut, "/v1/users/me/contacts", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = requestWithUser(req, testUserID)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
