// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hermes-notifications/hermes/pkg/client"
)

func TestAuthExchangeToken(t *testing.T) {
	tokenResp := client.TokenResponse{
		Token:     "eyJhbGciOiJIUzI1NiJ9.test",
		ExpiresAt: "2026-03-21T12:00:00Z",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/auth/token" {
			t.Errorf("expected /v1/auth/token, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json, got %s", r.Header.Get("Content-Type"))
		}

		var body client.TokenRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("failed to decode body: %v", err)
		}
		if body.OrganizationID != "organization1" || body.UserID != "user1" {
			t.Errorf("unexpected body: %+v", body)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(tokenResp)
	}))
	defer srv.Close()

	c := client.New(srv.URL, "test-key")
	result, err := c.Auth.ExchangeToken(context.Background(), client.TokenRequest{
		OrganizationID: "organization1",
		UserID:         "user1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Token != tokenResp.Token {
		t.Errorf("expected token %s, got %s", tokenResp.Token, result.Token)
	}
	if result.ExpiresAt != tokenResp.ExpiresAt {
		t.Errorf("expected expires_at %s, got %s", tokenResp.ExpiresAt, result.ExpiresAt)
	}
}
