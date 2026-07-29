// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hermes-notifications/hermes/internal/auth"
)

// serveWith runs one request through mw and reports the status plus the key the handler
// observed. A nil key means the handler ran without one, which must never happen on an
// authenticated path.
func serveWith(mw func(http.Handler) http.Handler, path, authHeader string) (int, *auth.ValidatedKey) {
	var seen *auth.ValidatedKey
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = auth.GetValidatedKey(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Code, seen
}

// validatorFor accepts exactly one raw key and rejects everything else.
func validatorFor(rawKey string, perms ...string) auth.APIKeyValidator {
	return func(got string) *auth.ValidatedKey {
		if got != rawKey {
			return nil
		}
		return &auth.ValidatedKey{ID: "key_test", Permissions: perms}
	}
}

func TestAPIKeyMiddleware_PutsTheValidatedKeyInContext(t *testing.T) {
	mw := auth.APIKeyMiddleware(validatorFor("secret", auth.PermNotificationsSend))

	code, key := serveWith(mw, "/v1/send", "Bearer secret")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if key == nil {
		t.Fatal("handler ran with no validated key in context")
	}
	if !auth.HasPermission(key, auth.PermNotificationsSend) {
		t.Error("the key reaching the handler lost its permissions")
	}
}

func TestAPIKeyMiddleware_RejectsMissingAndInvalidKeys(t *testing.T) {
	mw := auth.APIKeyMiddleware(validatorFor("secret"))

	cases := []struct {
		name   string
		header string
	}{
		{"rejects a request with no Authorization header", ""},
		{"rejects an unknown key", "Bearer wrong"},
		{"rejects a key sent without the Bearer prefix", "secret-but-mangled"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, key := serveWith(mw, "/v1/send", tc.header)
			if code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", code)
			}
			if key != nil {
				t.Error("a rejected request must not reach the handler at all")
			}
		})
	}
}

// This is what makes CheckPermission's fail-closed behaviour safe in production: an
// unauthenticated request never reaches a handler, so a nil key there is genuinely
// exceptional rather than routine.
func TestAPIKeyMiddleware_HealthEndpointsBypassAuthAndCarryNoKey(t *testing.T) {
	mw := auth.APIKeyMiddleware(validatorFor("secret"))

	for _, path := range []string{"/healthz", "/readyz"} {
		code, key := serveWith(mw, path, "")
		if code != http.StatusOK {
			t.Errorf("%s: expected 200 without a key, got %d", path, code)
		}
		if key != nil {
			t.Errorf("%s: expected no validated key on an unauthenticated path", path)
		}
	}
}

// Finding 3. SkipAuthMiddleware is how tests keep working now that CheckPermission fails
// closed. Injecting a fully-privileged synthetic key is what lets the production path
// drop its nil-check entirely, rather than carrying a fail-open branch for tests' benefit.
func TestSkipAuthMiddleware_InjectsAFullyPrivilegedKey(t *testing.T) {
	code, key := serveWith(auth.SkipAuthMiddleware(), "/v1/send", "")

	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if key == nil {
		t.Fatal("expected a synthetic key in context")
	}
	for _, perm := range auth.AllPermissions {
		if !auth.HasPermission(key, perm) {
			t.Errorf("synthetic key is missing %q, so skip-auth would not behave like full access", perm)
		}
	}
}
