// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package auth

import (
	"net/http"
	"strings"

	"github.com/hermesnotifications/hermes/internal/httputil"
)

// APIKeyValidator validates a raw API key and returns the validated key on success.
type APIKeyValidator func(rawKey string) *ValidatedKey

// SkipAuthMiddleware injects a synthetic key holding every permission, for servers
// constructed with SetSkipAuth(true). Intended for tests only.
//
// It exists so CheckPermission can fail closed unconditionally. The alternative — letting
// handlers treat a missing key as permitted — is what made the previous inline checks
// fail open, and a security control weakened for the convenience of tests protects
// nothing. Skipping authentication now means "act as a fully privileged caller", which is
// what tests actually want, rather than "skip authorization too", which is what they got.
func SkipAuthMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := WithValidatedKey(r.Context(), &ValidatedKey{
				ID:          "key_skipauth",
				Permissions: AllPermissions,
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// APIKeyMiddleware returns HTTP middleware that validates API keys from the Authorization header.
func APIKeyMiddleware(validate APIKeyValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
				next.ServeHTTP(w, r)
				return
			}

			key := r.Header.Get("Authorization")
			if key == "" {
				recordAuthResult(r.Context(), schemeAPIKey, reasonMissing)
				httputil.ClientError(w, http.StatusUnauthorized, "missing api key")
				return
			}
			key = strings.TrimPrefix(key, "Bearer ")

			validated := validate(key)
			if validated == nil {
				recordAuthResult(r.Context(), schemeAPIKey, reasonInvalidKey)
				httputil.ClientError(w, http.StatusUnauthorized, "invalid api key")
				return
			}

			recordAuthResult(r.Context(), schemeAPIKey, reasonOK)
			ctx := WithValidatedKey(r.Context(), validated)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
