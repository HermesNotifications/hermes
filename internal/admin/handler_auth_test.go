package admin_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hermes-notifications/hermes/internal/auth"
)

func TestHandleAuthToken(t *testing.T) {
	srv := newTestServer(t)

	body := `{"user_id":"ext-user-1","tenant_id":"test-tenant-id"}`
	req := httptest.NewRequest("POST", "/v1/auth/token", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["token"] == "" {
		t.Fatal("expected token in response")
	}
	if resp["expires_at"] == "" {
		t.Fatal("expected expires_at in response")
	}

	// Verify the token is valid and has the right claims
	claims := &auth.HermesClaims{}
	token, err := jwt.ParseWithClaims(resp["token"], claims, func(t *jwt.Token) (any, error) {
		return []byte("test-jwt-secret"), nil
	})
	if err != nil || !token.Valid {
		t.Fatalf("token invalid: %v", err)
	}

	sub, _ := claims.GetSubject()
	if sub == "" {
		t.Fatal("expected subject (user ID) in token")
	}
	if claims.TenantID != "test-tenant-id" {
		t.Fatalf("expected tenant_id test-tenant-id, got %s", claims.TenantID)
	}
}

func TestHandleAuthToken_UnknownTenant(t *testing.T) {
	srv := newTestServer(t)

	body := `{"user_id":"ext-user-1","tenant_id":"unknown-tenant"}`
	req := httptest.NewRequest("POST", "/v1/auth/token", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAuthToken_MissingFields(t *testing.T) {
	srv := newTestServer(t)

	body := `{"user_id":"ext-user-1"}`
	req := httptest.NewRequest("POST", "/v1/auth/token", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
