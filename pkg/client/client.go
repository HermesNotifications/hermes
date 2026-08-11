// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error (%d): %s", e.StatusCode, e.Message)
}

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client

	Categories    *CategoriesService
	Templates     *TemplatesService
	Notifications *NotificationsService
	Auth          *AuthService
	APIKeys       *APIKeysService
}

type Option func(*Client)

func WithHTTPClient(c *http.Client) Option {
	return func(cl *Client) {
		cl.httpClient = c
	}
}

func New(baseURL, apiKey string, opts ...Option) *Client {
	c := &Client{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: http.DefaultClient,
	}
	for _, opt := range opts {
		opt(c)
	}
	c.Categories = &CategoriesService{client: c}
	c.Templates = &TemplatesService{client: c}
	c.Notifications = &NotificationsService{client: c}
	c.Auth = &AuthService{client: c}
	c.APIKeys = &APIKeysService{client: c}
	return c
}

func (c *Client) newRequest(ctx context.Context, method, path string, body any) (*http.Request, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (c *Client) do(req *http.Request, v any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var errResp struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&errResp)
		return &APIError{StatusCode: resp.StatusCode, Message: errResp.Error}
	}

	if v != nil && resp.StatusCode != http.StatusNoContent {
		return json.NewDecoder(resp.Body).Decode(v)
	}
	return nil
}

