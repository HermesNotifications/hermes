// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package admin_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hermes-notifications/hermes/internal/auth"
)

func TestHandleAuthToken(t *testing.T) {
	srv := newTestServer(t)

	body := `{"user_id":"ext-user-1","organization_id":"test-organization-id"}`
	req := httptest.NewRequest("POST", "/v1/auth/token", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
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
	if claims.OrganizationID != "test-organization-id" {
		t.Fatalf("expected organization_id test-organization-id, got %s", claims.OrganizationID)
	}
}

func TestHandleAuthToken_NewOrganizationAutoCreated(t *testing.T) {
	srv := newTestServer(t)

	body := `{"user_id":"ext-user-1","organization_id":"brand-new-organization"}`
	req := httptest.NewRequest("POST", "/v1/auth/token", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (auto-create organization), got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)

	claims := &auth.HermesClaims{}
	token, err := jwt.ParseWithClaims(resp["token"], claims, func(t *jwt.Token) (any, error) {
		return []byte("test-jwt-secret"), nil
	})
	if err != nil || !token.Valid {
		t.Fatalf("token invalid: %v", err)
	}
	if claims.OrganizationID != "brand-new-organization" {
		t.Fatalf("expected organization_id brand-new-organization, got %s", claims.OrganizationID)
	}
}

func TestHandleAuthToken_CustomExpiry(t *testing.T) {
	srv := newTestServer(t)

	body := `{"user_id":"ext-user-1","organization_id":"test-organization-id","expires_in":86400}`
	req := httptest.NewRequest("POST", "/v1/auth/token", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	claims := &auth.HermesClaims{}
	var resp map[string]string
	json.NewDecoder(rec.Body).Decode(&resp)
	token, err := jwt.ParseWithClaims(resp["token"], claims, func(t *jwt.Token) (any, error) {
		return []byte("test-jwt-secret"), nil
	})
	if err != nil || !token.Valid {
		t.Fatalf("token invalid: %v", err)
	}

	exp, _ := claims.GetExpirationTime()
	ttl := time.Until(exp.Time)
	// 86400s = 24h, with ±10% jitter expect 21.6h–26.4h
	if ttl < 21*time.Hour+36*time.Minute || ttl > 26*time.Hour+24*time.Minute {
		t.Fatalf("expected TTL ~24h (±10%%), got %v", ttl)
	}
}

func TestHandleAuthToken_ExpiryTooShort(t *testing.T) {
	srv := newTestServer(t)

	body := `{"user_id":"ext-user-1","organization_id":"test-organization-id","expires_in":1800}`
	req := httptest.NewRequest("POST", "/v1/auth/token", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAuthToken_ExpiryTooLong(t *testing.T) {
	srv := newTestServer(t)

	body := `{"user_id":"ext-user-1","organization_id":"test-organization-id","expires_in":700000}`
	req := httptest.NewRequest("POST", "/v1/auth/token", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleAuthToken_MissingFields(t *testing.T) {
	srv := newTestServer(t)

	body := `{"user_id":"ext-user-1"}`
	req := httptest.NewRequest("POST", "/v1/auth/token", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", rec.Code)
	}
}
