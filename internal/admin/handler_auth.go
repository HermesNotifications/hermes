package admin

import (
	"context"
	"crypto/rand"
	"math/big"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/hermes-notifications/hermes/internal/auth"
)

type tokenInput struct {
	Body struct {
		UserID   string `json:"user_id" required:"true" minLength:"1" doc:"External user identifier"`
		TenantID string `json:"tenant_id" required:"true" minLength:"1" doc:"Tenant identifier"`
	}
}

type tokenOutput struct {
	Body struct {
		Token     string `json:"token" doc:"JWT token for user-facing API access"`
		ExpiresAt string `json:"expires_at" doc:"Token expiration time in RFC3339 format"`
	}
}

func (s *Server) registerAuthRoutes() {
	huma.Register(s.api, huma.Operation{
		OperationID: "exchange-token",
		Method:      http.MethodPost,
		Path:        "/v1/auth/token",
		Summary:     "Exchange credentials for a user JWT token",
		Tags:        []string{"Auth"},
	}, func(ctx context.Context, input *tokenInput) (*tokenOutput, error) {
		// Validate tenant exists
		if _, err := s.store.GetTenantByID(ctx, input.Body.TenantID); err != nil {
			return nil, huma.Error400BadRequest("unknown tenant_id")
		}

		// Ensure user exists (auto-create on first token request)
		user, err := s.store.EnsureUser(ctx, input.Body.TenantID, input.Body.UserID)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		// 1h base TTL with ±10% jitter (54-66 minutes)
		baseTTL := time.Hour
		jitterRange := big.NewInt(int64(baseTTL / 5)) // 12 minutes range
		jitterBig, _ := rand.Int(rand.Reader, jitterRange)
		jitter := time.Duration(jitterBig.Int64()) - baseTTL/10
		exp := time.Now().Add(baseTTL + jitter)

		claims := &auth.HermesClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   user.ID,
				ExpiresAt: jwt.NewNumericDate(exp),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
			TenantID: input.Body.TenantID,
		}

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenStr, err := token.SignedString(s.jwtSecret)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		resp := &tokenOutput{}
		resp.Body.Token = tokenStr
		resp.Body.ExpiresAt = exp.UTC().Format(time.RFC3339)
		return resp, nil
	})
}
