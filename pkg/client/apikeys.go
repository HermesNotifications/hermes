// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package client

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type APIKey struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"created_at"`
}

type APIKeyCreated struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	RawKey      string    `json:"raw_key"`
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"created_at"`
}

type CreateAPIKeyRequest struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions,omitempty"`
}

type APIKeysService struct {
	client *Client
}

func (s *APIKeysService) List(ctx context.Context) ([]APIKey, error) {
	req, err := s.client.newRequest(ctx, http.MethodGet, "/v1/apikeys", nil)
	if err != nil {
		return nil, err
	}
	var keys []APIKey
	if err := s.client.do(req, &keys); err != nil {
		return nil, err
	}
	return keys, nil
}

func (s *APIKeysService) Create(ctx context.Context, body CreateAPIKeyRequest) (*APIKeyCreated, error) {
	req, err := s.client.newRequest(ctx, http.MethodPost, "/v1/apikeys", body)
	if err != nil {
		return nil, err
	}
	var created APIKeyCreated
	if err := s.client.do(req, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

func (s *APIKeysService) Delete(ctx context.Context, id string) error {
	req, err := s.client.newRequest(ctx, http.MethodDelete, fmt.Sprintf("/v1/apikeys/%s", id), nil)
	if err != nil {
		return err
	}
	return s.client.do(req, nil)
}
