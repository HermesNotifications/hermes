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
	ContextKeyUserID   contextKey = "user_id"
	ContextKeyTenantID contextKey = "tenant_id"
)

// HermesClaims is the JWT claims structure for Hermes-issued tokens.
type HermesClaims struct {
	jwt.RegisteredClaims
	TenantID string `json:"tenant_id"`
}

// JWTSigningConfig represents one accepted signing key configuration.
type JWTSigningConfig struct {
	Name          string
	Secret        []byte
	Algorithm     string
	UserIDClaim   string
	TenantIDClaim string
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
				token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
					if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
						return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
					}
					return cfg.Secret, nil
				})
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

			// Extract user ID and tenant ID from claims
			userIDRaw, ok := validClaims[matchedCfg.UserIDClaim]
			if !ok {
				userIDRaw = validClaims["sub"]
			}
			tenantIDRaw, tok := validClaims[matchedCfg.TenantIDClaim]

			userID, _ := claimToString(userIDRaw)
			tenantID, _ := claimToString(tenantIDRaw)

			if userID == "" || (!tok && tenantID == "") {
				http.Error(w, `{"error":"missing claims"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), ContextKeyUserID, userID)
			ctx = context.WithValue(ctx, ContextKeyTenantID, tenantID)
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

func TenantIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ContextKeyTenantID).(string)
	return v
}
