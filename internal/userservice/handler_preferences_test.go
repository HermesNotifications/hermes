package userservice_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hermes-notifications/hermes/internal/models"
)

func TestHandleListPreferences(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/users/me/preferences", nil)
	req = requestWithUser(req, testUserID)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Data []models.UserPreference `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 preference, got %d", len(resp.Data))
	}
	if resp.Data[0].GroupID != "group-1" {
		t.Fatalf("expected group_id group-1, got %q", resp.Data[0].GroupID)
	}
}

func TestHandleListPreferences_NoUser(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/users/me/preferences", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSetPreference(t *testing.T) {
	srv, store := newTestServer(t)

	body := `{"channels":["sms","inbox"]}`
	req := httptest.NewRequest(http.MethodPut, "/v1/users/me/preferences/group-1", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = requestWithUser(req, testUserID)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var pref models.UserPreference
	if err := json.NewDecoder(rec.Body).Decode(&pref); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(pref.Channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(pref.Channels))
	}

	// Verify store was updated
	if len(store.preferences[0].Channels) != 2 || store.preferences[0].Channels[0] != "sms" {
		t.Fatalf("store preference not updated: %v", store.preferences[0].Channels)
	}
}

func TestHandleSetPreference_NewGroup(t *testing.T) {
	srv, store := newTestServer(t)

	body := `{"channels":["email"]}`
	req := httptest.NewRequest(http.MethodPut, "/v1/users/me/preferences/group-2", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = requestWithUser(req, testUserID)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if len(store.preferences) != 2 {
		t.Fatalf("expected 2 preferences, got %d", len(store.preferences))
	}
}

func TestHandleSetPreference_EmptyChannels(t *testing.T) {
	srv, _ := newTestServer(t)

	body := `{"channels":[]}`
	req := httptest.NewRequest(http.MethodPut, "/v1/users/me/preferences/group-1", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = requestWithUser(req, testUserID)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleDeletePreference(t *testing.T) {
	srv, store := newTestServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/v1/users/me/preferences/group-1", nil)
	req = requestWithUser(req, testUserID)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if len(store.preferences) != 0 {
		t.Fatalf("expected 0 preferences after delete, got %d", len(store.preferences))
	}
}

func TestHandleDeletePreference_NotFound(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/v1/users/me/preferences/nonexistent", nil)
	req = requestWithUser(req, testUserID)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}
