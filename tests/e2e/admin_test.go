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
	"github.com/hermes-notifications/hermes/internal/messaging"
	"github.com/hermes-notifications/hermes/internal/store"
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

	// Create store and server
	st := store.New(pool)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	srv := admin.NewServer(st, natsClient, redisClient, pool, logger)
	srv.SetSkipAuth(false) // Test with auth enabled

	handler := srv.Handler()

	// 1. Create a tenant directly in DB
	tenantID := uuid.New().String()
	_, err = pool.Exec(ctx, "INSERT INTO tenants (id, name) VALUES ($1, $2)", tenantID, "E2E Test Tenant")
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	// 2. Create an API key
	rawKey := "hms_e2e_test_key_" + uuid.New().String()
	keyHash, err := auth.HashAPIKey(rawKey)
	if err != nil {
		t.Fatalf("hash key: %v", err)
	}
	_, err = pool.Exec(ctx, "INSERT INTO api_keys (id, key_hash, name) VALUES ($1, $2, $3)", uuid.New().String(), keyHash, "E2E Test Key")
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	// Helper to make authenticated requests
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

	// 3. POST /v1/groups — create "billing" group
	rec := doRequest("POST", "/v1/groups", map[string]any{
		"slug": "billing-" + runID, "name": "Billing", "default_channels": []string{"email", "inbox"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create group: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	// 4. POST /v1/types — create "invoice.paid" type
	var group map[string]any
	json.NewDecoder(bytes.NewReader(rec.Body.Bytes())).Decode(&group) // re-read from bytes
	// Re-fetch the response properly
	rec = doRequest("POST", "/v1/groups", map[string]any{
		"slug": "billing-e2e-" + runID, "name": "Billing E2E", "default_channels": []string{"email", "inbox"},
	})
	json.NewDecoder(rec.Body).Decode(&group)
	groupID := group["id"].(string)

	rec = doRequest("POST", "/v1/types", map[string]any{
		"group_id": groupID, "slug": "invoice.paid.e2e." + runID, "name": "Invoice Paid",
		"email_subject": "Invoice {{.number}} paid",
		"inbox_title":   "Invoice paid",
		"inbox_body":    "Your invoice {{.number}} has been paid",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create type: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	// 5. POST /v1/send — send notification with type
	rec = doRequest("POST", "/v1/send", map[string]any{
		"tenant_id": tenantID,
		"user_id":   "ext-user-e2e-1",
		"type":      "invoice.paid.e2e." + runID,
		"data":      map[string]string{"number": "INV-001"},
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

	// 6. GET /v1/notifications/:id — verify status is "pending"
	rec = doRequest("GET", "/v1/notifications/"+notifID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get notification: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var statusResp map[string]any
	json.NewDecoder(rec.Body).Decode(&statusResp)
	notif := statusResp["notification"].(map[string]any)
	if notif["status"] != "pending" {
		t.Fatalf("expected status pending, got %s", notif["status"])
	}

	// 7. Verify NATS message was published
	received := make(chan []byte, 1)
	err = natsClient.Subscribe("notification.send", "test-consumer", func(data []byte) error {
		select {
		case received <- data:
		default:
		}
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// Send another notification to trigger NATS message (first one may already be consumed)
	rec = doRequest("POST", "/v1/send", map[string]any{
		"tenant_id": tenantID,
		"user_id":   "ext-user-e2e-2",
		"group":     "billing-e2e-" + runID,
		"content":   map[string]string{"title": "Test", "body": "Test body"},
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("send 2: expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	select {
	case msg := <-received:
		var parsed map[string]any
		json.Unmarshal(msg, &parsed)
		if parsed["notification_id"] == nil {
			t.Fatal("NATS message missing notification_id")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for NATS message")
	}

	// 8. Test idempotency
	rec1 := doRequest("POST", "/v1/send", map[string]any{
		"tenant_id": tenantID,
		"user_id":   "ext-user-e2e-3",
		"group":     "billing-e2e-" + runID,
		"content":   map[string]string{"title": "Idem Test", "body": "Body"},
	})
	// Add idempotency header for this one
	idemKey := "test-idem-key-" + runID
	req := httptest.NewRequest("POST", "/v1/send", bytes.NewBufferString(`{
		"tenant_id": "`+tenantID+`",
		"user_id": "ext-user-e2e-4",
		"group": "billing-e2e-`+runID+`",
		"content": {"title": "Idem", "body": "Body"}
	}`))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("X-Idempotency-Key", idemKey)
	rec1 = httptest.NewRecorder()
	handler.ServeHTTP(rec1, req)

	// Same request again
	req2 := httptest.NewRequest("POST", "/v1/send", bytes.NewBufferString(`{
		"tenant_id": "`+tenantID+`",
		"user_id": "ext-user-e2e-4",
		"group": "billing-e2e-`+runID+`",
		"content": {"title": "Idem", "body": "Body"}
	}`))
	req2.Header.Set("Authorization", "Bearer "+rawKey)
	req2.Header.Set("X-Idempotency-Key", idemKey)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	var resp1, resp2 map[string]string
	json.NewDecoder(rec1.Body).Decode(&resp1)
	json.NewDecoder(rec2.Body).Decode(&resp2)
	if resp1["notification_id"] != resp2["notification_id"] {
		t.Fatalf("idempotency failed: got %s and %s", resp1["notification_id"], resp2["notification_id"])
	}

	t.Logf("E2E test passed! notification_id=%s", notifID)
}
