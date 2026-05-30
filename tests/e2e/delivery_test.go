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
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/cache"
	"github.com/hermes-notifications/hermes/internal/centrifugo"
	"github.com/hermes-notifications/hermes/internal/database"
	"github.com/hermes-notifications/hermes/internal/delivery"
	"github.com/hermes-notifications/hermes/internal/dispatch"
	"github.com/hermes-notifications/hermes/internal/eventwriter"
	"github.com/hermes-notifications/hermes/internal/messaging"
	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/hermes-notifications/hermes/internal/send"
	"github.com/hermes-notifications/hermes/internal/store/postgres"
)

func TestDeliveryPipeline(t *testing.T) {
	ctx := context.Background()
	dbURL := envOr("HERMES_DATABASE_URL", "postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable")
	natsURL := envOr("HERMES_NATS_URL", "nats://localhost:4222")
	redisURL := envOr("HERMES_REDIS_URL", "redis://localhost:6379/0")

	runID := uuid.New().String()[:8]
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// ── Clean up stale NATS consumers ──────────────────────────────────
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

	// ── Mock Webhook Server (email provider) ────────────────────────────
	var emailDeliveryCount atomic.Int32
	emailDeliveryCh := make(chan struct{}, 1)
	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req delivery.DeliveryRequest
		json.NewDecoder(r.Body).Decode(&req)
		t.Logf("webhook received email delivery: notification_id=%s title=%q", req.NotificationID, req.Title)
		w.Header().Set("X-Provider-ID", "mock-email-"+req.NotificationID)
		w.WriteHeader(http.StatusOK)
		emailDeliveryCount.Add(1)
		select {
		case emailDeliveryCh <- struct{}{}:
		default:
		}
	}))
	defer webhookServer.Close()

	// ── Mock Centrifugo Server (inbox provider) ─────────────────────────
	var inboxDeliveryCount atomic.Int32
	inboxDeliveryCh := make(chan struct{}, 1)
	centrifugoServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		t.Logf("centrifugo received inbox delivery: channel=%v", payload["channel"])
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{})
		inboxDeliveryCount.Add(1)
		select {
		case inboxDeliveryCh <- struct{}{}:
		default:
		}
	}))
	defer centrifugoServer.Close()

	// ── Delivery Workers ────────────────────────────────────────────────
	emailProvider := delivery.NewWebhookProvider("mock-webhook", webhookServer.URL)
	emailWorker := delivery.NewWorker(natsClient, emailProvider, "email", "email-worker", logger)

	centrifugoClient := centrifugo.NewClient(centrifugoServer.URL, "test-api-key")
	inboxProvider := delivery.NewInboxProvider(centrifugoClient, nil)
	inboxWorker := delivery.NewWorker(natsClient, inboxProvider, "inbox", "inbox-worker", logger)

	// ── Seed Data ───────────────────────────────────────────────────────
	tenantID := uuid.New().String()
	_, err = pool.Exec(ctx, "INSERT INTO tenants (id, name) VALUES ($1, $2)", tenantID, "Delivery Test Tenant")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
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
		keyID, keyHash, "Delivery Test Key")
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

	// Seed a standalone template (no subscription) with email+inbox content.
	// Channels resolve from default_channels, filtered to those with content defined.
	templateSlug := "delivery.template." + runID
	_, err = pool.Exec(ctx,
		`INSERT INTO notification_templates (id, slug, name, default_channels, email_subject, inbox_title, inbox_body)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.New().String(), templateSlug, "Delivery Template", []string{"email", "inbox"},
		"Hello {{.name}}", "Hi {{.name}}", "Welcome {{.name}}",
	)
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	// ── Start all services BEFORE sending ───────────────────────────────
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
	if err := inboxWorker.Start(ctx); err != nil {
		t.Fatalf("start inbox worker: %v", err)
	}

	// ── Send notification via Send service ──────────────────────────────
	rec := doRequest("POST", "/v1/send", map[string]any{
		"to": map[string]any{
			"tenant_id": tenantID,
			"user_id":   "delivery-user-" + runID,
			"email":     "delivery-user-" + runID + "@example.com",
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

	// ── Wait for mock webhook to receive the email delivery ─────────────
	select {
	case <-emailDeliveryCh:
		t.Log("email delivery received by webhook")
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for email delivery")
	}

	// ── Wait for mock Centrifugo to receive the inbox delivery ──────────
	select {
	case <-inboxDeliveryCh:
		t.Log("inbox delivery received by centrifugo")
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for inbox delivery")
	}

	// ── Wait for events to appear in notification_events table ──────────
	// Poll the DB for up to 10 seconds since processing is async.
	var dbEvents []models.NotificationEvent
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		dbEvents, err = st.GetNotificationEvents(ctx, notifID)
		if err != nil {
			t.Logf("polling events: %v", err)
			continue
		}
		// We expect at least: routing.dispatched (email), routing.dispatched (inbox),
		// email.sent, inbox.sent — that's 4 events minimum.
		if len(dbEvents) >= 4 {
			break
		}
		t.Logf("polling: found %d events so far, waiting for >= 4", len(dbEvents))
	}

	if len(dbEvents) < 4 {
		t.Fatalf("expected at least 4 notification events, got %d", len(dbEvents))
	}
	t.Logf("found %d notification events", len(dbEvents))

	// Log all events for debugging
	hasEmailSent := false
	hasInboxSent := false
	dispatchedCount := 0
	for _, e := range dbEvents {
		t.Logf("event: channel=%s event=%s severity=%s", e.Channel, e.Event, e.Severity)
		if e.Event == "routing.dispatched" {
			dispatchedCount++
		}
		if e.Event == "email.sent" {
			hasEmailSent = true
		}
		if e.Event == "inbox.sent" {
			hasInboxSent = true
		}
	}

	if dispatchedCount < 2 {
		t.Fatalf("expected at least 2 routing.dispatched events (email+inbox), got %d", dispatchedCount)
	}
	if !hasEmailSent {
		t.Fatal("expected email.sent event, not found")
	}
	if !hasInboxSent {
		t.Fatal("expected inbox.sent event, not found")
	}

	// ── Verify notification status advanced to delivered ─────────────────
	// The email.sent event maps to StatusDelivered via eventToStatus.
	// Poll a bit more in case the status update is still processing.
	var notif *models.Notification
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		notif, err = st.GetNotificationByID(ctx, notifID)
		if err != nil {
			t.Fatalf("get notification: %v", err)
		}
		if notif.Status == models.StatusDelivered {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if notif.Status != models.StatusDelivered {
		t.Fatalf("expected notification status %q, got %q", models.StatusDelivered, notif.Status)
	}
	t.Logf("notification status: %s", notif.Status)

	// Verify delivery counts
	if emailDeliveryCount.Load() != 1 {
		t.Fatalf("expected 1 email delivery, got %d", emailDeliveryCount.Load())
	}
	if inboxDeliveryCount.Load() != 1 {
		t.Fatalf("expected 1 inbox delivery, got %d", inboxDeliveryCount.Load())
	}

	t.Log("Delivery pipeline integration test passed")
}
