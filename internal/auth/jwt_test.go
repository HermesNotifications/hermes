// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hermes-notifications/hermes/internal/auth"
)

func makeJWT(t *testing.T, secret []byte, userID, organizationID string) string {
	t.Helper()
	claims := auth.HermesClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		OrganizationID: organizationID,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(secret)
	if err != nil {
		t.Fatalf("makeJWT: sign token: %v", err)
	}
	return signed
}

func internalKeyProvider(secret []byte) auth.JWTKeyProvider {
	return func() []auth.JWTSigningConfig {
		return []auth.JWTSigningConfig{
			{
				Name:                "hermes-internal",
				Secret:              secret,
				Algorithm:           "HS256",
				UserIDClaim:         "sub",
				OrganizationIDClaim: "organization_id",
			},
		}
	}
}

func TestJWTMiddleware_ValidToken(t *testing.T) {
	secret := []byte("test-secret")
	userID := "user-123"
	organizationID := "organization-abc"

	tokenStr := makeJWT(t, secret, userID, organizationID)

	var capturedUserID, capturedOrganizationID string
	handler := auth.JWTMiddleware(internalKeyProvider(secret))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUserID = auth.UserIDFromContext(r.Context())
		capturedOrganizationID = auth.OrganizationIDFromContext(r.Context())
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
	if capturedOrganizationID != organizationID {
		t.Errorf("expected organization_id %q, got %q", organizationID, capturedOrganizationID)
	}
}

func TestJWTMiddleware_MissingHeader(t *testing.T) {
	secret := []byte("test-secret")

	handler := auth.JWTMiddleware(internalKeyProvider(secret))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	handler := auth.JWTMiddleware(internalKeyProvider(secret))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	handler := auth.JWTMiddleware(internalKeyProvider(secret))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func TestJWTMiddleware_WrongSecret(t *testing.T) {
	secret := []byte("correct-secret")
	tokenStr := makeJWT(t, []byte("wrong-secret"), "usr-1", "organization-1")

	handler := auth.JWTMiddleware(internalKeyProvider(secret))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/inbox", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}
