package auth_test

import (
	"context"
	"fmt"
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

func makeCustomJWT(t *testing.T, secret []byte, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(secret)
	if err != nil {
		t.Fatalf("makeCustomJWT: sign token: %v", err)
	}
	return signed
}

func internalKeyProvider(secret []byte) auth.JWTKeyProvider {
	return func() []auth.JWTSigningConfig {
		return []auth.JWTSigningConfig{
			{
				Name:          "hermes-internal",
				Secret:        secret,
				Algorithm:     "HS256",
				UserIDClaim:   "sub",
				TenantIDClaim: "tenant_id",
				Internal:      true,
			},
		}
	}
}

func TestJWTMiddleware_ValidToken(t *testing.T) {
	secret := []byte("test-secret")
	userID := "user-123"
	tenantID := "tenant-abc"

	tokenStr := makeJWT(t, secret, userID, tenantID)

	var capturedUserID, capturedTenantID string
	handler := auth.JWTMiddleware(internalKeyProvider(secret), nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	handler := auth.JWTMiddleware(internalKeyProvider(secret), nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	handler := auth.JWTMiddleware(internalKeyProvider(secret), nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	handler := auth.JWTMiddleware(internalKeyProvider(secret), nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func TestJWTMiddleware_MultiKey(t *testing.T) {
	hermesSecret := []byte("hermes-secret")
	providerSecret := []byte("provider-secret")

	keyProvider := func() []auth.JWTSigningConfig {
		return []auth.JWTSigningConfig{
			{
				Name:          "hermes-internal",
				Secret:        hermesSecret,
				Algorithm:     "HS256",
				UserIDClaim:   "sub",
				TenantIDClaim: "tenant_id",
				Internal:      true,
			},
			{
				Name:          "provider-key",
				Secret:        providerSecret,
				Algorithm:     "HS256",
				UserIDClaim:   "user_email",
				TenantIDClaim: "org_id",
				Internal:      false,
			},
		}
	}

	resolver := func(ctx context.Context, tenantID, externalID string) (string, error) {
		if externalID == "alice@example.com" && tenantID == "org-1" {
			return "internal-user-123", nil
		}
		return "", fmt.Errorf("user not found")
	}

	t.Run("hermes-issued token uses sub as internal ID", func(t *testing.T) {
		tokenStr := makeJWT(t, hermesSecret, "usr-ABC", "tenant-1")

		var capturedUserID string
		handler := auth.JWTMiddleware(keyProvider, resolver)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedUserID = auth.UserIDFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/api/inbox", nil)
		req.Header.Set("Authorization", "Bearer "+tokenStr)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		if capturedUserID != "usr-ABC" {
			t.Errorf("expected user_id usr-ABC, got %q", capturedUserID)
		}
	})

	t.Run("provider-issued token resolves external ID", func(t *testing.T) {
		tokenStr := makeCustomJWT(t, providerSecret, jwt.MapClaims{
			"user_email": "alice@example.com",
			"org_id":     "org-1",
			"exp":        float64(time.Now().Add(time.Hour).Unix()),
		})

		var capturedUserID, capturedTenantID string
		handler := auth.JWTMiddleware(keyProvider, resolver)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedUserID = auth.UserIDFromContext(r.Context())
			capturedTenantID = auth.TenantIDFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/api/inbox", nil)
		req.Header.Set("Authorization", "Bearer "+tokenStr)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		if capturedUserID != "internal-user-123" {
			t.Errorf("expected internal-user-123, got %q", capturedUserID)
		}
		if capturedTenantID != "org-1" {
			t.Errorf("expected org-1, got %q", capturedTenantID)
		}
	})

	t.Run("provider token with unknown user fails", func(t *testing.T) {
		tokenStr := makeCustomJWT(t, providerSecret, jwt.MapClaims{
			"user_email": "unknown@example.com",
			"org_id":     "org-1",
			"exp":        float64(time.Now().Add(time.Hour).Unix()),
		})

		handler := auth.JWTMiddleware(keyProvider, resolver)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/api/inbox", nil)
		req.Header.Set("Authorization", "Bearer "+tokenStr)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rr.Code)
		}
	})

	t.Run("wrong secret rejected", func(t *testing.T) {
		tokenStr := makeJWT(t, []byte("wrong-secret"), "usr-1", "tenant-1")

		handler := auth.JWTMiddleware(keyProvider, resolver)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/api/inbox", nil)
		req.Header.Set("Authorization", "Bearer "+tokenStr)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rr.Code)
		}
	})
}
