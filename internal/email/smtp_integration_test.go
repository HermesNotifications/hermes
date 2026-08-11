// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

//go:build integration

package email

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestSMTPProvider_SendToMailpit(t *testing.T) {
	smtpHost := envOr("HERMES_EMAIL_SMTP_HOST", "localhost")
	smtpPort := 1025
	mailpitAPI := envOr("MAILPIT_API_URL", "http://localhost:8025")

	// Skip if Mailpit is not running (not available in CI)
	resp, err := http.Get(mailpitAPI + "/api/v1/messages")
	if err != nil {
		t.Skip("Mailpit not available, skipping SMTP integration test")
	}
	resp.Body.Close()

	// Delete existing messages to start clean
	req, _ := http.NewRequest(http.MethodDelete, mailpitAPI+"/api/v1/messages", nil)
	http.DefaultClient.Do(req)

	provider := NewSMTPProvider(Config{
		SMTPHost: smtpHost,
		SMTPPort: smtpPort,
	})

	email := Email{
		From:     "test@example.com",
		To:       "user@example.com",
		Subject:  "Integration Test Email",
		HTMLBody: "<h1>Hello</h1><p>This is a test email.</p>",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err = provider.Send(ctx, email)
	if err != nil {
		t.Fatalf("send email: %v", err)
	}

	// Verify email arrived via Mailpit API
	time.Sleep(500 * time.Millisecond) // give Mailpit time to process

	resp, err = http.Get(mailpitAPI + "/api/v1/messages")
	if err != nil {
		t.Fatalf("query mailpit: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mailpit API returned %d", resp.StatusCode)
	}

	var result struct {
		Messages []struct {
			ID      string `json:"ID"`
			Subject string `json:"Subject"`
			From    struct {
				Address string `json:"Address"`
			} `json:"From"`
			To []struct {
				Address string `json:"Address"`
			} `json:"To"`
		} `json:"messages"`
		Total int `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode mailpit response: %v", err)
	}

	if result.Total == 0 {
		t.Fatal("expected at least one message in Mailpit, got 0")
	}

	msg := result.Messages[0]
	if msg.Subject != "Integration Test Email" {
		t.Errorf("expected subject 'Integration Test Email', got %q", msg.Subject)
	}
	if msg.From.Address != "test@example.com" {
		t.Errorf("expected from 'test@example.com', got %q", msg.From.Address)
	}
	if len(msg.To) == 0 || msg.To[0].Address != "user@example.com" {
		t.Errorf("expected to 'user@example.com', got %v", msg.To)
	}

	// Verify HTML body via message detail
	detailResp, err := http.Get(fmt.Sprintf("%s/api/v1/message/%s", mailpitAPI, msg.ID))
	if err != nil {
		t.Fatalf("get message detail: %v", err)
	}
	defer detailResp.Body.Close()

	var detail struct {
		HTML string `json:"HTML"`
	}
	if err := json.NewDecoder(detailResp.Body).Decode(&detail); err != nil {
		t.Fatalf("decode message detail: %v", err)
	}
	if detail.HTML == "" {
		t.Error("expected HTML body, got empty")
	}

	t.Log("SMTP integration test passed — email delivered to Mailpit")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
