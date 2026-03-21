package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hermes-notifications/hermes/internal/auth"
)

func makeJWT(t *testing.T, secret []byte, userID, tenantID string) string {
	t.Helper()
	claims := auth.HermesClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		TenantID: tenantID,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(secret)
	if err != nil {
		t.Fatalf("makeJWT: sign token: %v", err)
	}
	return signed
}

func TestJWTMiddleware_ValidToken(t *testing.T) {
	secret := []byte("test-secret")
	userID := "user-123"
	tenantID := "tenant-abc"

	tokenStr := makeJWT(t, secret, userID, tenantID)

	var capturedUserID, capturedTenantID string
	handler := auth.JWTMiddleware(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUserID = auth.UserIDFromContext(r.Context())
		capturedTenantID = auth.TenantIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/inbox", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if capturedUserID != userID {
		t.Errorf("expected user_id %q, got %q", userID, capturedUserID)
	}
	if capturedTenantID != tenantID {
		t.Errorf("expected tenant_id %q, got %q", tenantID, capturedTenantID)
	}
}

func TestJWTMiddleware_MissingHeader(t *testing.T) {
	secret := []byte("test-secret")

	handler := auth.JWTMiddleware(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/inbox", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestJWTMiddleware_InvalidToken(t *testing.T) {
	secret := []byte("test-secret")

	handler := auth.JWTMiddleware(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/inbox", nil)
	req.Header.Set("Authorization", "Bearer this.is.garbage")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestJWTMiddleware_SkipsHealthChecks(t *testing.T) {
	secret := []byte("test-secret")

	handler := auth.JWTMiddleware(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, path := range []string{"/healthz", "/readyz"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("path %s: expected 200, got %d", path, rr.Code)
		}
	}
}
