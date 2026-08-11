// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package auth_test

import (
	"encoding/json"
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

// The rejection bodies were always JSON, but they went out through http.Error,
// which stamps Content-Type: text/plain. A client parsing on the header saw text
// and a client parsing on the body saw JSON, and the two disagreed on every 401
// the platform issues.
func TestAPIKeyMiddleware_RejectionsAreLabelledAsJSON(t *testing.T) {
	mw := auth.APIKeyMiddleware(validatorFor("secret"))

	for _, tc := range []struct{ name, header string }{
		{"missing key", ""},
		{"invalid key", "Bearer wrong"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Error("handler ran for a request that should have been rejected")
			}))
			req := httptest.NewRequest(http.MethodGet, "/v1/send", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d", rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			var body map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("body is not JSON: %v (%q)", err, rec.Body.String())
			}
			if body["error"] == "" {
				t.Errorf("body %v has no \"error\" field", body)
			}
		})
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
		// Named for what it actually sends: a different key, unprefixed. The
		// prefix itself is optional — see TestAPIKeyMiddleware_BearerPrefixIsOptional.
		{"rejects an unknown key sent without the Bearer prefix", "secret-but-mangled"},
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

// The "Bearer " prefix is optional: TrimPrefix is a no-op on a bare key, so both
// forms authenticate as the same credential.
//
// This is worth pinning rather than leaving implicit, because anything keying off
// the raw header instead of the validated key sees one credential as two. That is
// exactly how the rate limiter came to grant double its limit to anyone who sent
// the key both ways.
func TestAPIKeyMiddleware_BearerPrefixIsOptional(t *testing.T) {
	mw := auth.APIKeyMiddleware(validatorFor("secret", auth.PermNotificationsSend))

	for _, header := range []string{"secret", "Bearer secret"} {
		code, key := serveWith(mw, "/v1/send", header)
		if code != http.StatusOK {
			t.Errorf("Authorization %q: expected 200, got %d", header, code)
			continue
		}
		if key == nil {
			t.Errorf("Authorization %q: handler ran with no validated key", header)
			continue
		}
		if key.ID != "key_test" {
			t.Errorf("Authorization %q: validated key ID = %q, want key_test", header, key.ID)
		}
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
