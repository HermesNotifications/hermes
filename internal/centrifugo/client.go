// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package centrifugo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	apiURL     string
	apiKey     string
	httpClient *http.Client
}

func NewClient(apiURL, apiKey string) *Client {
	return &Client{apiURL: apiURL, apiKey: apiKey, httpClient: &http.Client{Timeout: 5 * time.Second}}
}

func (c *Client) Publish(ctx context.Context, channel string, data any) error {
	payload := map[string]any{"channel": channel, "data": data}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.apiURL+"/api/publish", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "apikey "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("centrifugo returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}
