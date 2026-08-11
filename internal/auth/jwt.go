// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const (
	ContextKeyUserID         contextKey = "user_id"
	ContextKeyOrganizationID contextKey = "organization_id"
)

// defaultSigningAlgorithm is assumed for a signing key that records no algorithm.
// No current writer produces one: the column is NOT NULL DEFAULT 'HS256'
// (migrations/000009_create_jwt_signing_keys.up.sql:4) and EnsureHermesSigningKey
// hardcodes HS256. NOT NULL still permits the empty string, though, so the case is
// handled deterministically rather than treated as unreachable. Assuming HS256 is
// deliberately tighter than falling back to "any HMAC method", which is the very
// hole this constant exists to close.
const defaultSigningAlgorithm = "HS256"

// HermesClaims is the JWT claims structure for Hermes-issued tokens.
type HermesClaims struct {
	jwt.RegisteredClaims
	OrganizationID string `json:"organization_id"`
}

// JWTSigningConfig represents one accepted signing key configuration.
type JWTSigningConfig struct {
	Name                string
	Secret              []byte
	Algorithm           string
	UserIDClaim         string
	OrganizationIDClaim string
}

// JWTKeyProvider returns all active signing configs. Called on each request.
type JWTKeyProvider func() []JWTSigningConfig

// JWTMiddleware validates JWTs against any of the provided signing configs.
// The sub claim is treated as the internal user ID (all tokens are Hermes-issued).
func JWTMiddleware(keyProvider JWTKeyProvider) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
				next.ServeHTTP(w, r)
				return
			}

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, `{"error":"missing authorization"}`, http.StatusUnauthorized)
				return
			}
			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

			configs := keyProvider()
			if len(configs) == 0 {
				http.Error(w, `{"error":"no signing keys configured"}`, http.StatusInternalServerError)
				return
			}

			var (
				validClaims jwt.MapClaims
				matchedCfg  *JWTSigningConfig
			)

			for i := range configs {
				cfg := &configs[i]
				claims := jwt.MapClaims{}
				algorithm := cfg.Algorithm
				if algorithm == "" {
					algorithm = defaultSigningAlgorithm
				}
				// WithValidMethods pins the token's alg header to the algorithm this
				// key was registered with. Without it the keyfunc below only checked
				// the HMAC *family*, so a key registered as HS512 accepted an HS256
				// token signed with the same secret. The family check is kept as
				// defence in depth: it turns a key misconfigured with an asymmetric
				// algorithm into a clear error rather than a []byte-as-RSA-key failure.
				token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
					if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
						return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
					}
					return cfg.Secret, nil
				}, jwt.WithValidMethods([]string{algorithm}))
				if err == nil && token.Valid {
					validClaims = claims
					matchedCfg = cfg
					break
				}
			}

			if validClaims == nil || matchedCfg == nil {
				http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
				return
			}

			// Extract user ID and organization ID from claims
			userIDRaw, ok := validClaims[matchedCfg.UserIDClaim]
			if !ok {
				userIDRaw = validClaims["sub"]
			}
			// Index without comma-ok: an absent claim yields nil, which claimToString
			// reports as unusable. The previous form took the map-presence bool and
			// tested `!present && organizationID == ""`, so a claim that was present
			// but not convertible — a bool, object, array, null, or an empty string —
			// satisfied the guard and the request proceeded with an empty
			// organization ID in context. Presence is not usability.
			organizationIDRaw := validClaims[matchedCfg.OrganizationIDClaim]

			userID, _ := claimToString(userIDRaw)
			organizationID, organizationOK := claimToString(organizationIDRaw)

			if userID == "" || !organizationOK {
				http.Error(w, `{"error":"missing claims"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), ContextKeyUserID, userID)
			ctx = context.WithValue(ctx, ContextKeyOrganizationID, organizationID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// claimToString converts a JWT claim value to a string.
func claimToString(v any) (string, bool) {
	switch val := v.(type) {
	case string:
		return val, val != ""
	case float64:
		return fmt.Sprintf("%.0f", val), true
	default:
		return "", false
	}
}

func UserIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ContextKeyUserID).(string)
	return v
}

func OrganizationIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ContextKeyOrganizationID).(string)
	return v
}
