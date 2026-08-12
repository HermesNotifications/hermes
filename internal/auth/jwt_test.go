// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hermesnotifications/hermes/internal/auth"
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

// makeRawJWT signs arbitrary claims with an explicit method, which HermesClaims
// cannot express: OrganizationID is typed as string there, so a claim carrying a
// bool, an object or null is unrepresentable through makeJWT.
func makeRawJWT(t *testing.T, method jwt.SigningMethod, secret []byte, claims jwt.MapClaims) string {
	t.Helper()
	if _, ok := claims["exp"]; !ok {
		claims["exp"] = jwt.NewNumericDate(time.Now().Add(time.Hour))
	}
	signed, err := jwt.NewWithClaims(method, claims).SignedString(secret)
	if err != nil {
		t.Fatalf("makeRawJWT: sign token: %v", err)
	}
	return signed
}

// keyProviderWithAlgorithm is internalKeyProvider with the registered algorithm
// under test, so a token's alg can be varied independently of the key's.
func keyProviderWithAlgorithm(secret []byte, algorithm string) auth.JWTKeyProvider {
	return func() []auth.JWTSigningConfig {
		return []auth.JWTSigningConfig{
			{
				Name:                "hermes-internal",
				Secret:              secret,
				Algorithm:           algorithm,
				UserIDClaim:         "sub",
				OrganizationIDClaim: "organization_id",
			},
		}
	}
}

// serve runs one request through the middleware and reports the status plus the
// organization ID the handler observed.
func serve(t *testing.T, provider auth.JWTKeyProvider, tokenStr string) (int, string) {
	t.Helper()
	var capturedOrganizationID string
	handler := auth.JWTMiddleware(provider)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedOrganizationID = auth.OrganizationIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/inbox", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr.Code, capturedOrganizationID
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

// Finding 20, gap 1. The per-key Algorithm was stored and never read, so the only
// check was that the token used *some* HMAC method — a key registered as HS512
// accepted an HS256 token signed with the same secret.
func TestJWTMiddleware_EnforcesConfiguredAlgorithm(t *testing.T) {
	secret := []byte("shared-secret")
	claims := jwt.MapClaims{"sub": "usr-1", "organization_id": "org-1"}

	t.Run("rejects a weaker HMAC algorithm than the key registers", func(t *testing.T) {
		token := makeRawJWT(t, jwt.SigningMethodHS256, secret, claims)
		if code, _ := serve(t, keyProviderWithAlgorithm(secret, "HS512"), token); code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for HS256 token against an HS512 key, got %d", code)
		}
	})

	t.Run("rejects a stronger HMAC algorithm than the key registers", func(t *testing.T) {
		token := makeRawJWT(t, jwt.SigningMethodHS512, secret, claims)
		if code, _ := serve(t, keyProviderWithAlgorithm(secret, "HS256"), token); code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for HS512 token against an HS256 key, got %d", code)
		}
	})

	t.Run("accepts the algorithm the key registers", func(t *testing.T) {
		token := makeRawJWT(t, jwt.SigningMethodHS512, secret, claims)
		if code, _ := serve(t, keyProviderWithAlgorithm(secret, "HS512"), token); code != http.StatusOK {
			t.Fatalf("expected 200 for HS512 token against an HS512 key, got %d", code)
		}
	})
}

// An empty Algorithm is not produced by any current writer — the column is
// `NOT NULL DEFAULT 'HS256'` (migrations/000009_create_jwt_signing_keys.up.sql:4)
// and EnsureHermesSigningKey hardcodes HS256 — but NOT NULL still permits "",
// so the behaviour is pinned rather than assumed unreachable.
func TestJWTMiddleware_EmptyAlgorithmDefaultsToHS256(t *testing.T) {
	secret := []byte("shared-secret")
	claims := jwt.MapClaims{"sub": "usr-1", "organization_id": "org-1"}

	if code, _ := serve(t, keyProviderWithAlgorithm(secret, ""), makeRawJWT(t, jwt.SigningMethodHS256, secret, claims)); code != http.StatusOK {
		t.Errorf("expected 200 for an HS256 token against a key with no algorithm, got %d", code)
	}
	if code, _ := serve(t, keyProviderWithAlgorithm(secret, ""), makeRawJWT(t, jwt.SigningMethodHS512, secret, claims)); code != http.StatusUnauthorized {
		t.Errorf("expected 401 for an HS512 token against a key with no algorithm, got %d", code)
	}
}

// Finding 20, gap 2. The missing-claims guard tested raw map presence, not whether
// the claim converted to a usable string — so any present-but-unusable value passed
// and the request proceeded with an empty organization ID in context.
func TestJWTMiddleware_RejectsUnusableOrganizationClaim(t *testing.T) {
	secret := []byte("shared-secret")

	cases := []struct {
		name  string
		value any
	}{
		{name: "rejects a boolean organization claim", value: true},
		{name: "rejects an object organization claim", value: map[string]any{"id": "org-1"}},
		{name: "rejects an array organization claim", value: []any{"org-1"}},
		{name: "rejects a null organization claim", value: nil},
		{name: "rejects an empty-string organization claim", value: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token := makeRawJWT(t, jwt.SigningMethodHS256, secret, jwt.MapClaims{
				"sub":             "usr-1",
				"organization_id": tc.value,
			})
			code, org := serve(t, internalKeyProvider(secret), token)
			if code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d (organization ID in context: %q)", code, org)
			}
		})
	}
}

func TestJWTMiddleware_AcceptsNumericOrganizationClaim(t *testing.T) {
	secret := []byte("shared-secret")
	token := makeRawJWT(t, jwt.SigningMethodHS256, secret, jwt.MapClaims{
		"sub":             "usr-1",
		"organization_id": float64(4321),
	})

	code, org := serve(t, internalKeyProvider(secret), token)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if org != "4321" {
		t.Errorf("expected organization ID %q, got %q", "4321", org)
	}
}

func TestJWTMiddleware_RejectsAbsentOrganizationClaim(t *testing.T) {
	secret := []byte("shared-secret")
	token := makeRawJWT(t, jwt.SigningMethodHS256, secret, jwt.MapClaims{"sub": "usr-1"})

	if code, _ := serve(t, internalKeyProvider(secret), token); code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", code)
	}
}
