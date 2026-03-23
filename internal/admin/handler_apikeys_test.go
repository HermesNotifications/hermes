package admin_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleListAPIKeys(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/apikeys", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleCreateAPIKey(t *testing.T) {
	srv := newTestServer(t)

	body := `{"name":"Test Key","permissions":["notifications:send"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/apikeys", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		RawKey      string   `json:"raw_key"`
		Permissions []string `json:"permissions"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.Name != "Test Key" {
		t.Fatalf("expected name 'Test Key', got %s", resp.Name)
	}
	if resp.RawKey == "" {
		t.Fatal("expected raw_key to be set")
	}
	if len(resp.Permissions) != 1 || resp.Permissions[0] != "notifications:send" {
		t.Fatalf("expected permissions [notifications:send], got %v", resp.Permissions)
	}
}

func TestHandleCreateAPIKey_InvalidPermission(t *testing.T) {
	srv := newTestServer(t)

	body := `{"name":"Bad Key","permissions":["foo:bar"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/apikeys", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleCreateAPIKey_DefaultPermissions(t *testing.T) {
	srv := newTestServer(t)

	body := `{"name":"Default Key"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/apikeys", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Permissions []string `json:"permissions"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)

	if len(resp.Permissions) != 3 {
		t.Fatalf("expected 3 default permissions, got %d: %v", len(resp.Permissions), resp.Permissions)
	}
}

func TestHandleDeleteAPIKey(t *testing.T) {
	srv := newTestServer(t)

	// Create a key first
	body := `{"name":"To Delete","permissions":["notifications:send"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/apikeys", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	var createResp struct {
		ID string `json:"id"`
	}
	json.NewDecoder(rec.Body).Decode(&createResp)
	keyID := createResp.ID

	// Delete it
	req = httptest.NewRequest(http.MethodDelete, "/v1/apikeys/"+keyID, nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleDeleteAPIKey_NotFound(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/v1/apikeys/key_nonexistent", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}
