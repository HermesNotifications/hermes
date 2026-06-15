// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

//go:build integration

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"text/template"
	"time"

	"github.com/google/uuid"
	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/cache"
	"github.com/hermes-notifications/hermes/internal/database"
	"github.com/hermes-notifications/hermes/internal/delivery"
	"github.com/hermes-notifications/hermes/internal/dispatch"
	"github.com/hermes-notifications/hermes/internal/email"
	"github.com/hermes-notifications/hermes/internal/eventwriter"
	"github.com/hermes-notifications/hermes/internal/messaging"
	"github.com/hermes-notifications/hermes/internal/send"
	"github.com/hermes-notifications/hermes/internal/store/postgres"
)

func TestPipeline_EmailDeliveryToMailpit(t *testing.T) {
	ctx := context.Background()
	dbURL := envOr("HERMES_DATABASE_URL", "postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable")
	natsURL := envOr("HERMES_NATS_URL", "nats://localhost:4222")
	redisURL := envOr("HERMES_REDIS_URL", "redis://localhost:6379/0")
	mailpitAPI := envOr("MAILPIT_API_URL", "http://localhost:8025")

	runID := uuid.New().String()[:8]
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Clean up stale NATS consumers
	cleanupNATSConsumers(t, natsURL)

	// Clean Mailpit inbox
	req, _ := http.NewRequest(http.MethodDelete, mailpitAPI+"/api/v1/messages", nil)
	http.DefaultClient.Do(req)

	// ── Infrastructure ──────────────────────────────────────────────────
	if err := database.RunMigrations(dbURL, "../../migrations"); err != nil {
		t.Fatalf("migrations: %v", err)
	}

	pool, err := database.NewPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("database: %v", err)
	}
	defer pool.Close()

	natsClient, err := messaging.Connect(natsURL)
	if err != nil {
		t.Fatalf("nats: %v", err)
	}
	defer natsClient.Close()
	if err := natsClient.SetupStreams(ctx); err != nil {
		t.Fatalf("setup streams: %v", err)
	}

	redisClient, err := cache.Connect(redisURL)
	if err != nil {
		t.Fatalf("redis: %v", err)
	}
	defer redisClient.Close()

	// ── Store + Services ────────────────────────────────────────────────
	st := postgres.New(pool)

	// Send server (with API key auth)
	srv := send.NewServer(st, natsClient, redisClient, pool, "test-hmac-secret", logger)
	srv.SetSkipAuth(false)
	handler := srv.Handler()

	// Dispatch
	templateResolver := dispatch.NewTemplateResolver(st, redisClient)
	channelResolver := dispatch.NewChannelResolver(st, nil)
	rtr := dispatch.NewDispatch(natsClient, st, st, st, templateResolver, channelResolver, logger)

	// Event Writer
	ew := eventwriter.New(natsClient, st, logger)

	// Email Worker — SMTP to Mailpit
	emailProvider := email.NewSMTPProvider(email.Config{
		SMTPHost: "localhost",
		SMTPPort: 1025,
	})
	layout := template.Must(template.New("layout").Parse(`{{.Content}}`))
	adapter := email.NewDeliveryAdapter(emailProvider, "noreply@hermes-test.com", layout)
	emailWorker := delivery.NewWorker(natsClient, adapter, "email", "worker-email-e2e", logger)

	// ── Seed Data ───────────────────────────────────────────────────────
	tenantID := uuid.New().String()
	_, err = pool.Exec(ctx, "INSERT INTO tenants (id, name) VALUES ($1, $2)", tenantID, "Email E2E Tenant")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	// Create user with email address
	userExternalID := "email-user-" + runID
	user, err := st.EnsureUser(ctx, tenantID, userExternalID)
	if err != nil {
		t.Fatalf("ensure user: %v", err)
	}
	testEmail := "testuser-" + runID + "@example.com"
	_, err = st.UpdateUserContacts(ctx, user.ID, &testEmail, nil)
	if err != nil {
		t.Fatalf("update user contacts: %v", err)
	}

	// API key
	rawKey, keyID, err := auth.GenerateAPIKey("")
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	_, secret, err := auth.ParseAPIKey(rawKey)
	if err != nil {
		t.Fatalf("parse key: %v", err)
	}
	keyHash := auth.HMACHashAPIKey(secret, "test-hmac-secret")
	_, err = pool.Exec(ctx, "INSERT INTO api_keys (id, key_hash, name) VALUES ($1, $2, $3)",
		keyID, keyHash, "Email E2E Key")
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	doRequest := func(method, path string, body any) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		if body != nil {
			json.NewEncoder(&buf).Encode(body)
		}
		req := httptest.NewRequest(method, path, &buf)
		req.Header.Set("Authorization", "Bearer "+rawKey)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	// Seed a standalone template with email content (default channel: email).
	templateSlug := "email.template." + runID
	templateID := uuid.New().String()
	_, err = pool.Exec(ctx,
		`INSERT INTO notification_templates (id, slug, name, default_channels, email_subject, email_body)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		templateID, templateSlug, "Email Test Template", []string{"email"},
		"Welcome {{.name}}!", "<h1>Hello {{.name}}</h1><p>Your order #{{.order_id}} has been confirmed.</p>",
	)
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	// Populate normalized content map so dispatch can filter and render channels.
	_, err = pool.Exec(ctx,
		`INSERT INTO template_channel_content (template_id, channel_slug, content) VALUES
		 ($1, 'email', '{"subject":"Welcome {{.name}}!","body":"<h1>Hello {{.name}}</h1><p>Your order #{{.order_id}} has been confirmed.</p>"}')`,
		templateID,
	)
	if err != nil {
		t.Fatalf("create template content: %v", err)
	}

	// ── Start services ──────────────────────────────────────────────────
	if err := rtr.Start(); err != nil {
		t.Fatalf("start dispatch: %v", err)
	}
	if err := ew.Start(ctx); err != nil {
		t.Fatalf("start event writer: %v", err)
	}
	defer ew.Stop()
	if err := emailWorker.Start(ctx); err != nil {
		t.Fatalf("start email worker: %v", err)
	}

	// ── Send notification ───────────────────────────────────────────────
	rec := doRequest("POST", "/v1/send", map[string]any{
		"to": map[string]any{
			"tenant_id": tenantID,
			"user_id":   userExternalID,
		},
		"template": templateSlug,
		"data":     map[string]string{"name": "Alice", "order_id": "12345"},
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("send: expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	var sendResp map[string]string
	json.NewDecoder(rec.Body).Decode(&sendResp)
	notifID := sendResp["notification_id"]
	t.Logf("notification_id = %s", notifID)

	// ── Wait for email to arrive in Mailpit ─────────────────────────────
	var mailpitMsg struct {
		ID      string `json:"ID"`
		Subject string `json:"Subject"`
		From    struct {
			Address string `json:"Address"`
		} `json:"From"`
		To []struct {
			Address string `json:"Address"`
		} `json:"To"`
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)

		resp, err := http.Get(mailpitAPI + "/api/v1/messages")
		if err != nil {
			continue
		}

		var result struct {
			Messages []json.RawMessage `json:"messages"`
			Total    int               `json:"total"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		if result.Total > 0 {
			json.Unmarshal(result.Messages[0], &mailpitMsg)
			break
		}
	}

	if mailpitMsg.ID == "" {
		t.Fatal("no email arrived in Mailpit within timeout")
	}

	// ── Verify email content ────────────────────────────────────────────
	if mailpitMsg.Subject != "Welcome Alice!" {
		t.Errorf("expected subject 'Welcome Alice!', got %q", mailpitMsg.Subject)
	}
	if mailpitMsg.From.Address != "noreply@hermes-test.com" {
		t.Errorf("expected from 'noreply@hermes-test.com', got %q", mailpitMsg.From.Address)
	}
	if len(mailpitMsg.To) == 0 || mailpitMsg.To[0].Address != testEmail {
		t.Errorf("expected to %q, got %v", testEmail, mailpitMsg.To)
	}

	// Verify HTML body
	detailResp, err := http.Get(fmt.Sprintf("%s/api/v1/message/%s", mailpitAPI, mailpitMsg.ID))
	if err != nil {
		t.Fatalf("get message detail: %v", err)
	}
	defer detailResp.Body.Close()

	var detail struct {
		HTML string `json:"HTML"`
	}
	json.NewDecoder(detailResp.Body).Decode(&detail)

	if detail.HTML == "" {
		t.Error("expected HTML body, got empty")
	}
	t.Logf("HTML body length: %d bytes", len(detail.HTML))

	t.Log("Email E2E test passed — email delivered through full pipeline to Mailpit")
}
