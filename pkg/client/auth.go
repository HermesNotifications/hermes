// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package client

import (
	"context"
	"net/http"
)

type TokenRequest struct {
	UserID   string `json:"user_id"`
	TenantID string `json:"tenant_id"`
}

type TokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

type AuthService struct{ client *Client }

func (s *AuthService) ExchangeToken(ctx context.Context, body TokenRequest) (*TokenResponse, error) {
	req, err := s.client.newRequest(ctx, http.MethodPost, "/v1/auth/token", body)
	if err != nil {
		return nil, err
	}

	var resp TokenResponse
	if err := s.client.do(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
