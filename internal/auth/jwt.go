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
	Internal      bool // true for Hermes-issued keys (sub = internal user ID)
}

// JWTKeyProvider returns all active signing configs. Called on each request.
type JWTKeyProvider func() []JWTSigningConfig

// UserResolver resolves an external user ID + tenant ID to a Hermes internal user ID.
// Used for provider-issued tokens where the user_id_claim contains an external ID.
type UserResolver func(ctx context.Context, tenantID, externalID string) (internalID string, err error)

// JWTMiddleware validates JWTs against any of the provided signing configs.
// For internal keys (Hermes-issued), sub is treated as the internal user ID.
// For external keys (provider-issued), the configured claim is treated as an external ID
// and resolved to an internal user ID via the resolver.
func JWTMiddleware(keyProvider JWTKeyProvider, resolver UserResolver) func(http.Handler) http.Handler {
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
					switch cfg.Algorithm {
					case "HS256", "HS384", "HS512":
						if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
							return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
						}
					default:
						if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
							return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
						}
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

			// Extract user ID and tenant ID from claims using the config's claim mappings
			userIDRaw, ok := validClaims[matchedCfg.UserIDClaim]
			if !ok {
				// Fall back to "sub" from registered claims
				userIDRaw, ok = validClaims["sub"]
			}
			tenantIDRaw, tok := validClaims[matchedCfg.TenantIDClaim]

			userID, _ := claimToString(userIDRaw)
			tenantID, _ := claimToString(tenantIDRaw)

			if userID == "" || (!tok && tenantID == "") {
				http.Error(w, `{"error":"missing claims"}`, http.StatusUnauthorized)
				return
			}

			// For external (provider-issued) keys, resolve external ID to internal user ID
			if !matchedCfg.Internal && resolver != nil {
				internalID, err := resolver(r.Context(), tenantID, userID)
				if err != nil {
					http.Error(w, `{"error":"user resolution failed"}`, http.StatusUnauthorized)
					return
				}
				userID = internalID
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
