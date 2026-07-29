// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package admin_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/hermes-notifications/hermes/internal/admin"
	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/models"
)

const adminHMACSecret = "test-hmac-secret"

// authenticatedAdmin builds a server on the REAL authentication path — no SetSkipAuth —
// holding one API key with exactly the permissions given, and returns the raw key.
//
// Finding 3: permissions were checked on three API-key routes and nowhere else, so these
// tests have to go through the middleware chain rather than injecting a context.
func authenticatedAdmin(t *testing.T, permissions ...string) (http.Handler, string) {
	t.Helper()

	raw, keyID, err := auth.GenerateAPIKey("")
	if err != nil {
		t.Fatalf("generate api key: %v", err)
	}
	_, secret, err := auth.ParseAPIKey(raw)
	if err != nil {
		t.Fatalf("parse api key: %v", err)
	}

	store := &mockStore{
		apiKeys: []models.APIKey{{
			ID:          keyID,
			KeyHash:     auth.HMACHashAPIKey(secret, adminHMACSecret),
			Permissions: permissions,
		}},
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	srv := admin.NewServer(store, store, nil, nil, []byte("test-jwt-secret"), adminHMACSecret, logger)
	return srv.Handler(), raw
}

func get(t *testing.T, handler http.Handler, path, bearer string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Code
}

// Each route group is gated on its own permission, so a key scoped to one must not reach
// another. Before this change, only the API-key routes checked anything at all.
func TestHandler_EnforcesPerRoutePermissions(t *testing.T) {
	cases := []struct {
		name  string
		path  string
		grant string
		other string
	}{
		{
			name:  "templates require templates:manage",
			path:  "/v1/templates",
			grant: auth.PermTemplatesManage,
			other: auth.PermNotificationsSend,
		},
		{
			name:  "organizations require organizations:manage",
			path:  "/v1/organizations",
			grant: auth.PermOrganizationsManage,
			other: auth.PermNotificationsSend,
		},
		{
			name:  "api keys require apikeys:manage",
			path:  "/v1/apikeys",
			grant: auth.PermAPIKeysManage,
			other: auth.PermNotificationsSend,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			denied, key := authenticatedAdmin(t, tc.other)
			if code := get(t, denied, tc.path, key); code != http.StatusForbidden {
				t.Errorf("a key holding only %s reached %s: got %d, want 403", tc.other, tc.path, code)
			}

			allowed, key2 := authenticatedAdmin(t, tc.grant)
			if code := get(t, allowed, tc.path, key2); code == http.StatusForbidden {
				t.Errorf("a key holding %s was refused at %s", tc.grant, tc.path)
			}
		})
	}
}

// The three pre-existing checks were written `if key != nil && !HasPermission(...)`,
// which passes when the key is nil. Unreachable in production because the middleware
// 401s first — but this pins that the route is refused rather than served, so the
// fail-open form cannot come back unnoticed.
func TestHandler_RejectsUnauthenticatedRequests(t *testing.T) {
	handler, _ := authenticatedAdmin(t, auth.AllPermissions...)

	for _, path := range []string{"/v1/templates", "/v1/organizations", "/v1/apikeys"} {
		if code := get(t, handler, path, ""); code != http.StatusUnauthorized {
			t.Errorf("%s: expected 401 with no bearer token, got %d", path, code)
		}
	}
}

// Skip-auth must grant full permissions rather than bypass the checks — otherwise every
// other test in this package would pass for the wrong reason.
func TestHandler_SkipAuthGrantsFullPermissions(t *testing.T) {
	srv := newTestServer(t)

	for _, path := range []string{"/v1/templates", "/v1/organizations", "/v1/apikeys"} {
		code := get(t, srv.Handler(), path, "")
		if code == http.StatusForbidden || code == http.StatusUnauthorized {
			t.Errorf("%s: skip-auth should behave as a fully privileged caller, got %d", path, code)
		}
	}
}
