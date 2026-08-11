// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

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
	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/cache"
	"github.com/hermes-notifications/hermes/internal/database"
	"github.com/hermes-notifications/hermes/internal/dispatch"
	"github.com/hermes-notifications/hermes/internal/eventwriter"
	"github.com/hermes-notifications/hermes/internal/messaging"
	"github.com/hermes-notifications/hermes/internal/send"
	"github.com/hermes-notifications/hermes/internal/store/postgres"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func TestPipeline_DispatchAndEventWriter(t *testing.T) {
	ctx := context.Background()
	dbURL := envOr("HERMES_DATABASE_URL", "postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable")
	natsURL := envOr("HERMES_NATS_URL", "nats://localhost:4222")
	redisURL := envOr("HERMES_REDIS_URL", "redis://localhost:6379/0")

	runID := uuid.New().String()[:8]
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// ── Clean up stale NATS consumers ──────────────────────────────────
	// WorkQueue streams only allow one consumer per filter subject.
	// Previous test runs may leave stale consumers that block new ones.
	cleanupNATSConsumers(t, natsURL)

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

	// ── Seed Data ───────────────────────────────────────────────────────
	organizationID := uuid.New().String()
	_, err = pool.Exec(ctx, "INSERT INTO organizations (id, name) VALUES ($1, $2)", organizationID, "Pipeline Test Organization")
	if err != nil {
		t.Fatalf("create organization: %v", err)
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
	// Permissions are explicit. Inserting a key with no permissions column at all
	// left these tests sending with a key that held nothing — which passed only
	// because /v1/send checked no permission (finding 3). Granting exactly what the
	// test exercises means a future gap fails here rather than passing silently.
	_, err = pool.Exec(ctx,
		"INSERT INTO api_keys (id, key_hash, name, permissions) VALUES ($1, $2, $3, $4)",
		keyID, keyHash, "Pipeline Test Key", []string{auth.PermNotificationsSend})
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	// Helper
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

	// Seed a standalone template with email+inbox content.
	templateSlug := "pipeline.template." + runID
	templateID := uuid.New().String()
	_, err = pool.Exec(ctx,
		`INSERT INTO notification_templates (id, slug, name, default_channels)
		 VALUES ($1, $2, $3, $4)`,
		templateID, templateSlug, "Pipeline Template", []string{"email", "inbox"},
	)
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	// Populate normalized content map so dispatch can filter and render channels.
	_, err = pool.Exec(ctx,
		`INSERT INTO template_channel_content (template_id, channel_slug, content) VALUES
		 ($1, 'email', '{"subject":"Hello {{.name}}"}'),
		 ($1, 'inbox', '{"title":"Hi {{.name}}","body":"Welcome {{.name}}"}')`,
		templateID,
	)
	if err != nil {
		t.Fatalf("create template content: %v", err)
	}

	// ── Start Dispatch + Event Writer BEFORE sending ───────────────────
	if err := rtr.Start(1, 0); err != nil {
		t.Fatalf("start dispatch: %v", err)
	}
	if err := ew.Start(ctx); err != nil {
		t.Fatalf("start event writer: %v", err)
	}
	defer ew.Stop()

	// ── Send notification via Send service ──────────────────────────────
	rec := doRequest("POST", "/v1/send", map[string]any{
		"to": map[string]any{
			"organization_id": organizationID,
			"user_id":   "pipeline-user-" + runID,
			"contacts":  map[string]any{"email": "pipeline-user-" + runID + "@example.com"},
		},
		"template": templateSlug,
		"data":     map[string]string{"name": "Alice"},
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("send: expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	var sendResp map[string]string
	json.NewDecoder(rec.Body).Decode(&sendResp)
	notifID := sendResp["notification_id"]
	if notifID == "" {
		t.Fatal("expected notification_id in response")
	}
	t.Logf("notification_id = %s", notifID)

	// ── Wait for async processing ──────────────────────────────────────
	// Dispatch picks up from notification.send, fans out to delivery.*,
	// and publishes routing events. Event Writer picks up events and writes to DB.
	// Give it up to 5 seconds with polling.
	var events []interface{}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		dbEvents, err := st.GetNotificationEvents(ctx, notifID)
		if err != nil {
			t.Logf("polling events: %v", err)
			continue
		}
		if len(dbEvents) > 0 {
			for _, e := range dbEvents {
				events = append(events, e)
			}
			break
		}
	}

	// ── Verify notification_events ─────────────────────────────────────
	if len(events) == 0 {
		t.Fatal("expected notification_events to be written, got none")
	}
	t.Logf("found %d notification events", len(events))

	// Verify that routing.dispatched events exist (one per channel: email, inbox)
	dbEvents, err := st.GetNotificationEvents(ctx, notifID)
	if err != nil {
		t.Fatalf("get events: %v", err)
	}

	dispatchedCount := 0
	for _, e := range dbEvents {
		t.Logf("event: channel=%s event=%s severity=%s", e.Channel, e.Event, e.Severity)
		if e.Event == "routing.dispatched" {
			dispatchedCount++
		}
	}
	if dispatchedCount < 2 {
		t.Fatalf("expected at least 2 routing.dispatched events (email+inbox), got %d", dispatchedCount)
	}

	// ── Verify notification channels were updated ──────────────────────
	notif, err := st.GetNotificationByID(ctx, notifID)
	if err != nil {
		t.Fatalf("get notification: %v", err)
	}
	if len(notif.Channels) == 0 {
		t.Fatal("expected notification channels to be set by dispatch")
	}
	t.Logf("notification channels: %v", notif.Channels)

	// The dispatch service publishes "routing.dispatched" events which don't trigger a status
	// update (only email.routed, inbox.routed etc. do). Without a delivery worker
	// running, status stays "pending". This is correct behavior — the pipeline
	// delivered the message to the delivery streams but no worker consumed them.
	t.Logf("notification status: %s (expected pending — no delivery worker running)", notif.Status)
	if notif.Status != "pending" {
		// If a delivery worker happens to be running, status could advance.
		// Either way, it should not be empty.
		if notif.Status == "" {
			t.Fatal("notification status is empty")
		}
	}

	t.Log("Pipeline integration test passed")
}

// cleanupNATSConsumers deletes all consumers on WorkQueue streams so the test
// can create fresh ones without "filtered consumer not unique" errors.
func cleanupNATSConsumers(t *testing.T, natsURL string) {
	t.Helper()
	nc, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatalf("cleanup nats connect: %v", err)
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("cleanup jetstream: %v", err)
	}

	ctx := context.Background()
	streams := []string{"NOTIFICATIONS", "DELIVERY", "EVENTS"}
	for _, streamName := range streams {
		stream, err := js.Stream(ctx, streamName)
		if err != nil {
			continue // stream may not exist yet
		}
		// Delete all consumers first
		consLister := stream.ListConsumers(ctx)
		for info := range consLister.Info() {
			t.Logf("deleting stale consumer %s/%s", streamName, info.Name)
			_ = js.DeleteConsumer(ctx, streamName, info.Name)
		}
		// Purge all old messages to avoid processing stale data
		if err := stream.Purge(ctx); err != nil {
			t.Logf("purge stream %s: %v", streamName, err)
		}
	}
}
