// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

//go:build integration

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hermes-notifications/hermes/internal/admin"
	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/cache"
	"github.com/hermes-notifications/hermes/internal/database"
	"github.com/hermes-notifications/hermes/internal/dispatch"
	"github.com/hermes-notifications/hermes/internal/eventwriter"
	"github.com/hermes-notifications/hermes/internal/messaging"
	"github.com/hermes-notifications/hermes/internal/send"
	"github.com/hermes-notifications/hermes/internal/store/postgres"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func TestSendNotification_E2E(t *testing.T) {
	ctx := context.Background()
	dbURL := envOr("HERMES_DATABASE_URL", "postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable")
	natsURL := envOr("HERMES_NATS_URL", "nats://localhost:4222")
	redisURL := envOr("HERMES_REDIS_URL", "redis://localhost:6379/0")

	// Use a unique run ID to avoid slug collisions across test runs
	runID := uuid.New().String()[:8]

	// Run migrations
	if err := database.RunMigrations(dbURL, "../../migrations"); err != nil {
		t.Fatalf("migrations: %v", err)
	}

	// Connect to all infra
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
	natsClient.SetupStreams(ctx)

	redisClient, err := cache.Connect(redisURL)
	if err != nil {
		t.Fatalf("redis: %v", err)
	}
	defer redisClient.Close()

	// Clean up stale NATS consumers (dispatch uses WorkQueue streams)
	cleanupNATSConsumers(t, natsURL)

	// Create store and services
	st := postgres.New(pool)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// Send service handles POST /v1/send (publishes to NATS)
	sendSrv := send.NewServer(st, natsClient, redisClient, pool, "test-hmac-secret", logger)
	sendSrv.SetSkipAuth(false)
	sendHandler := sendSrv.Handler()

	// Admin service handles notification reads
	adminSrv := admin.NewServer(st, st, redisClient, pool, []byte("test-jwt-secret"), "test-hmac-secret", logger)
	adminSrv.SetSkipAuth(false) // Test with auth enabled
	adminHandler := adminSrv.Handler()

	// Dispatch persists notifications and routes them; Event Writer records events.
	templateResolver := dispatch.NewTemplateResolver(st, redisClient)
	channelResolver := dispatch.NewChannelResolver(st, nil)
	rtr := dispatch.NewDispatch(natsClient, st, st, st, templateResolver, channelResolver, logger)
	ew := eventwriter.New(natsClient, st, logger)

	// 1. Create a tenant directly in DB
	tenantID := uuid.New().String()
	_, err = pool.Exec(ctx, "INSERT INTO tenants (id, name) VALUES ($1, $2)", tenantID, "E2E Test Tenant")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	// 2. Create an API key
	hmacSecret := "test-hmac-secret"
	rawKey, keyID, err := auth.GenerateAPIKey("")
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	_, secret, err := auth.ParseAPIKey(rawKey)
	if err != nil {
		t.Fatalf("parse key: %v", err)
	}
	keyHash := auth.HMACHashAPIKey(secret, hmacSecret)
	allPerms := []string{"apikeys:manage", "notifications:send", "templates:manage", "tenants:manage"}
	_, err = pool.Exec(ctx, "INSERT INTO api_keys (id, key_hash, name, permissions) VALUES ($1, $2, $3, $4)", keyID, keyHash, "E2E Test Key", allPerms)
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	// Helpers to make authenticated requests against each service.
	doReq := func(handler http.Handler, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		if body != nil {
			json.NewEncoder(&buf).Encode(body)
		}
		req := httptest.NewRequest(method, path, &buf)
		req.Header.Set("Authorization", "Bearer "+rawKey)
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	// 3. Seed a standalone template (email+inbox) directly in the DB.
	templateSlug := "invoice.paid.e2e." + runID
	templateID := uuid.New().String()
	_, err = pool.Exec(ctx,
		`INSERT INTO notification_templates (id, slug, name, default_channels, email_subject, inbox_title, inbox_body)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		templateID, templateSlug, "Invoice Paid", []string{"email", "inbox"},
		"Invoice {{.number}} paid", "Invoice paid", "Your invoice {{.number}} has been paid",
	)
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	// Seed normalized per-channel content (matches the fixed columns above) so
	// dispatch's content-gated routing fans out to email + inbox.
	_, err = pool.Exec(ctx,
		`INSERT INTO template_channel_content (template_id, channel_slug, content) VALUES
		 ($1, 'email', $2), ($1, 'inbox', $3)`,
		templateID,
		`{"subject":"Invoice {{.number}} paid"}`,
		`{"title":"Invoice paid","body":"Your invoice {{.number}} has been paid"}`,
	)
	if err != nil {
		t.Fatalf("create template content: %v", err)
	}

	// Start dispatch + event writer so notifications get persisted and routed.
	if err := rtr.Start(); err != nil {
		t.Fatalf("start dispatch: %v", err)
	}
	if err := ew.Start(ctx); err != nil {
		t.Fatalf("start event writer: %v", err)
	}
	defer ew.Stop()

	// 4. POST /v1/send — template-based send via the Send service.
	rec := doReq(sendHandler, "POST", "/v1/send", map[string]any{
		"to":       map[string]any{"tenant_id": tenantID, "user_id": "ext-user-e2e-1", "email": "ext-user-e2e-1@example.com"},
		"template": templateSlug,
		"data":     map[string]string{"number": "INV-001"},
	}, nil)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("send: expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	var sendResp map[string]string
	json.NewDecoder(rec.Body).Decode(&sendResp)
	notifID := sendResp["notification_id"]
	if notifID == "" {
		t.Fatal("expected notification_id in response")
	}

	// 5. Poll the Admin read API until dispatch has persisted the notification.
	var notif map[string]any
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rec = doReq(adminHandler, "GET", "/v1/notifications/"+notifID, nil, nil)
		if rec.Code == http.StatusOK {
			var statusResp map[string]any
			json.NewDecoder(rec.Body).Decode(&statusResp)
			notif = statusResp["notification"].(map[string]any)
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if notif == nil {
		t.Fatal("notification was not persisted by dispatch within timeout")
	}
	if notif["status"] == nil || notif["status"] == "" {
		t.Fatalf("expected a notification status, got %v", notif["status"])
	}
	t.Logf("notification status: %v", notif["status"])

	// 6. Idempotency — two direct-content sends with the same key return the same ID.
	idemKey := "test-idem-key-" + runID
	idemBody := map[string]any{
		"to":       map[string]any{"tenant_id": tenantID, "user_id": "ext-user-e2e-4", "email": "ext-user-e2e-4@example.com"},
		"content":  map[string]string{"title": "Idem", "body": "Body"},
		"channels": []string{"inbox"},
	}
	rec1 := doReq(sendHandler, "POST", "/v1/send", idemBody, map[string]string{"X-Idempotency-Key": idemKey})
	if rec1.Code != http.StatusAccepted {
		t.Fatalf("idempotent send 1: expected 202, got %d: %s", rec1.Code, rec1.Body.String())
	}
	rec2 := doReq(sendHandler, "POST", "/v1/send", idemBody, map[string]string{"X-Idempotency-Key": idemKey})
	if rec2.Code != http.StatusAccepted {
		t.Fatalf("idempotent send 2: expected 202, got %d: %s", rec2.Code, rec2.Body.String())
	}

	var resp1, resp2 map[string]string
	json.NewDecoder(rec1.Body).Decode(&resp1)
	json.NewDecoder(rec2.Body).Decode(&resp2)
	if resp1["notification_id"] != resp2["notification_id"] {
		t.Fatalf("idempotency failed: got %s and %s", resp1["notification_id"], resp2["notification_id"])
	}

	t.Logf("E2E test passed! notification_id=%s", notifID)
}
