package admin_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hermes-notifications/hermes/internal/models"
)

func TestHandleCreateSigningKey(t *testing.T) {
	srv := newTestServer(t)

	body := `{"name":"My SaaS Auth","algorithm":"HS256","secret":"my-signing-secret","user_id_claim":"sub","tenant_id_claim":"org_id"}`
	req := httptest.NewRequest("POST", "/v1/auth/keys", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var key models.JWTSigningKey
	json.NewDecoder(rec.Body).Decode(&key)
	if key.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if key.Name != "My SaaS Auth" {
		t.Errorf("expected name 'My SaaS Auth', got %q", key.Name)
	}
	if key.Secret != "" {
		t.Error("secret should not be in JSON response (json:\"-\" tag)")
	}
	if key.UserIDClaim != "sub" {
		t.Errorf("expected user_id_claim 'sub', got %q", key.UserIDClaim)
	}
	if key.TenantIDClaim != "org_id" {
		t.Errorf("expected tenant_id_claim 'org_id', got %q", key.TenantIDClaim)
	}
}

func TestHandleCreateSigningKey_Defaults(t *testing.T) {
	srv := newTestServer(t)

	body := `{"name":"Minimal Key","secret":"secret123"}`
	req := httptest.NewRequest("POST", "/v1/auth/keys", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var key models.JWTSigningKey
	json.NewDecoder(rec.Body).Decode(&key)
	if key.Algorithm != "HS256" {
		t.Errorf("expected default algorithm HS256, got %q", key.Algorithm)
	}
	if key.UserIDClaim != "sub" {
		t.Errorf("expected default user_id_claim 'sub', got %q", key.UserIDClaim)
	}
	if key.TenantIDClaim != "tenant_id" {
		t.Errorf("expected default tenant_id_claim 'tenant_id', got %q", key.TenantIDClaim)
	}
}

func TestHandleCreateSigningKey_MissingFields(t *testing.T) {
	srv := newTestServer(t)

	body := `{"name":"No Secret"}`
	req := httptest.NewRequest("POST", "/v1/auth/keys", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleCreateSigningKey_InvalidAlgorithm(t *testing.T) {
	srv := newTestServer(t)

	body := `{"name":"Bad Algo","secret":"s","algorithm":"RS256"}`
	req := httptest.NewRequest("POST", "/v1/auth/keys", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleListSigningKeys(t *testing.T) {
	srv := newTestServer(t)

	// Create a key first
	body := `{"name":"Test Key","secret":"secret"}`
	req := httptest.NewRequest("POST", "/v1/auth/keys", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	// List keys
	req = httptest.NewRequest("GET", "/v1/auth/keys", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var keys []models.JWTSigningKey
	json.NewDecoder(rec.Body).Decode(&keys)
	if len(keys) < 1 {
		t.Fatal("expected at least 1 key")
	}
}

func TestHandleDeleteSigningKey(t *testing.T) {
	srv := newTestServer(t)

	// Create a key first
	body := `{"name":"To Delete","secret":"secret"}`
	req := httptest.NewRequest("POST", "/v1/auth/keys", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	var created models.JWTSigningKey
	json.NewDecoder(rec.Body).Decode(&created)

	// Delete it
	req = httptest.NewRequest("DELETE", "/v1/auth/keys/"+created.ID, nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleDeleteSigningKey_NotFound(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest("DELETE", "/v1/auth/keys/nonexistent", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
