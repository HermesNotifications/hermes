package auth

import (
	"net/http"
	"strings"
)

// APIKeyValidator validates a raw API key and returns the validated key on success.
type APIKeyValidator func(rawKey string) *ValidatedKey

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
				http.Error(w, `{"error":"missing api key"}`, http.StatusUnauthorized)
				return
			}
			key = strings.TrimPrefix(key, "Bearer ")

			validated := validate(key)
			if validated == nil {
				http.Error(w, `{"error":"invalid api key"}`, http.StatusUnauthorized)
				return
			}

			ctx := WithValidatedKey(r.Context(), validated)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
