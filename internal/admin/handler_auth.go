// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

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
		UserID         string `json:"user_id" required:"true" minLength:"1" doc:"External user identifier"`
		OrganizationID string `json:"organization_id" required:"true" minLength:"1" doc:"Organization identifier"`
		ExpiresIn      *int   `json:"expires_in,omitempty" minimum:"3600" maximum:"604800" doc:"Requested token lifetime in seconds (min 3600 = 1h, max 604800 = 7d, default 14400 = 4h). The actual expiry includes ±10% random jitter to prevent thundering-herd token refreshes."`
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
		if err := requirePermission(ctx, auth.PermOrganizationsManage); err != nil {
			return nil, err
		}

		// Auto-create organization on first sight
		if _, err := s.organizations.EnsureOrganization(ctx, input.Body.OrganizationID); err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		// Ensure user exists (auto-create on first token request)
		user, err := s.store.EnsureUser(ctx, input.Body.OrganizationID, input.Body.UserID)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		// Default 4h base TTL, overridable via expires_in
		baseTTL := 4 * time.Hour
		if input.Body.ExpiresIn != nil {
			baseTTL = time.Duration(*input.Body.ExpiresIn) * time.Second
		}
		// ±10% jitter to prevent thundering-herd token refreshes
		jitterRange := big.NewInt(int64(baseTTL / 5))
		jitterBig, _ := rand.Int(rand.Reader, jitterRange)
		jitter := time.Duration(jitterBig.Int64()) - baseTTL/10
		exp := time.Now().Add(baseTTL + jitter)

		claims := &auth.HermesClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   user.ID,
				ExpiresAt: jwt.NewNumericDate(exp),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
			},
			OrganizationID: input.Body.OrganizationID,
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
