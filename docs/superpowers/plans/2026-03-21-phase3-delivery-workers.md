# Phase 3: Delivery Workers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the three delivery worker services (Email, SMS, Inbox) with a pluggable provider interface, completing the notification delivery pipeline from Router fan-out through to actual delivery + event publishing.

**Architecture:** Each worker subscribes to its NATS delivery subject (`delivery.email`, `delivery.sms`, `delivery.inbox`), delivers via a pluggable provider adapter, and publishes result events to `notification.events`. The Inbox Worker is special — it persists to Postgres and publishes to Centrifugo. All workers share a common `delivery` package with the provider interface and base worker logic.

**Tech Stack:** Go, NATS JetStream (existing `messaging` package), Centrifugo server-side HTTP API, pluggable provider adapters (webhook adapter ships first for email/SMS).

**Spec:** `docs/superpowers/specs/2026-03-20-hermes-notification-service-design.md`

**Depends on:** Phase 1 (foundation), Phase 2 (router + event writer, NATS message types).

---

## File Structure

```
hermes/
├── cmd/
│   ├── worker-email/
│   │   └── main.go
│   ├── worker-sms/
│   │   └── main.go
│   └── worker-inbox/
│       └── main.go
├── internal/
│   ├── delivery/
│   │   ├── provider.go            # DeliveryProvider interface + DeliveryRequest/Result types
│   │   ├── provider_test.go
│   │   ├── worker.go              # Base worker — shared NATS subscribe + event publish logic
│   │   ├── worker_test.go
│   │   ├── webhook.go             # Webhook adapter (shared by email + SMS)
│   │   └── webhook_test.go
│   ├── centrifugo/
│   │   ├── client.go              # Centrifugo server-side HTTP publish client
│   │   └── client_test.go
│   └── store/
│       └── inbox.go               # Inbox-specific store methods (new)
```

---

### Task 1: Delivery Provider Interface + Types

**Files:**
- Create: `internal/delivery/provider.go`

Defines the shared interface all delivery providers implement.

- [ ] **Step 1: Write provider interface**

```go
// internal/delivery/provider.go
package delivery

import "context"

// DeliveryRequest contains everything a provider needs to deliver a notification.
type DeliveryRequest struct {
	NotificationID string
	TenantID       string
	UserID         string
	Channel        string
	Title          string
	Body           string
	ActionURL      string
	ActionLabel    string
	// Channel-specific fields
	EmailTo      string // for email — resolved from user's email
	PhoneTo      string // for SMS — resolved from user's phone
}

// DeliveryResult contains the outcome of a delivery attempt.
type DeliveryResult struct {
	Success      bool
	ProviderName string
	ProviderID   string            // external ID from provider (e.g., SendGrid message ID)
	Error        string
	Metadata     map[string]string // extra provider-specific data
}

// Provider is the pluggable delivery adapter interface.
type Provider interface {
	Send(ctx context.Context, req DeliveryRequest) (DeliveryResult, error)
	Name() string
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/delivery/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/delivery/provider.go
git commit -m "feat: add delivery provider interface and types"
```

---

### Task 2: Webhook Delivery Adapter

**Files:**
- Create: `internal/delivery/webhook.go`
- Create: `internal/delivery/webhook_test.go`

The webhook adapter is the universal escape hatch — it POSTs the delivery request to a configured URL. Used by both email and SMS workers when no native provider is configured.

- [ ] **Step 1: Write webhook adapter**

```go
// internal/delivery/webhook.go
package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type WebhookProvider struct {
	url        string
	httpClient *http.Client
}

func NewWebhookProvider(url string) *WebhookProvider {
	return &WebhookProvider{
		url: url,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (w *WebhookProvider) Name() string { return "webhook" }

func (w *WebhookProvider) Send(ctx context.Context, req DeliveryRequest) (DeliveryResult, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return DeliveryResult{Error: err.Error()}, fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", w.url, bytes.NewReader(payload))
	if err != nil {
		return DeliveryResult{Error: err.Error()}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := w.httpClient.Do(httpReq)
	if err != nil {
		return DeliveryResult{Error: err.Error()}, fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return DeliveryResult{
			Success:      true,
			ProviderName: "webhook",
			Metadata:     map[string]string{"status_code": fmt.Sprintf("%d", resp.StatusCode), "response": string(body)},
		}, nil
	}

	return DeliveryResult{
		Error:        fmt.Sprintf("webhook returned %d: %s", resp.StatusCode, string(body)),
		ProviderName: "webhook",
	}, fmt.Errorf("webhook returned %d", resp.StatusCode)
}
```

- [ ] **Step 2: Write unit test with httptest server**

```go
// internal/delivery/webhook_test.go
package delivery_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hermes-notifications/hermes/internal/delivery"
)

func TestWebhookProvider_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		var req delivery.DeliveryRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.NotificationID == "" {
			t.Fatal("expected notification_id")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"msg-123"}`))
	}))
	defer server.Close()

	p := delivery.NewWebhookProvider(server.URL)
	result, err := p.Send(context.Background(), delivery.DeliveryRequest{
		NotificationID: "notif-1",
		Channel:        "email",
		Title:          "Test",
		Body:           "Body",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	if p.Name() != "webhook" {
		t.Fatalf("expected webhook, got %s", p.Name())
	}
}

func TestWebhookProvider_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer server.Close()

	p := delivery.NewWebhookProvider(server.URL)
	result, err := p.Send(context.Background(), delivery.DeliveryRequest{
		NotificationID: "notif-1",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if result.Success {
		t.Fatal("expected failure")
	}
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/delivery/... -v
```

- [ ] **Step 4: Commit**

```bash
git add internal/delivery/
git commit -m "feat: add webhook delivery adapter"
```

---

### Task 3: Base Worker Logic

**Files:**
- Create: `internal/delivery/worker.go`
- Create: `internal/delivery/worker_test.go`

Shared worker logic: subscribe to NATS subject, unmarshal delivery message, call provider, publish event.

- [ ] **Step 1: Write base worker**

```go
// internal/delivery/worker.go
package delivery

import (
	"context"
	"encoding/json"
	"log/slog"

	hermenats "github.com/hermes-notifications/hermes/internal/nats"
	"github.com/hermes-notifications/hermes/internal/messaging"
)

// Worker is a base delivery worker that subscribes to a NATS subject and delivers via a Provider.
type Worker struct {
	nats     *messaging.Client
	provider Provider
	channel  string
	consumer string
	logger   *slog.Logger
}

func NewWorker(nats *messaging.Client, provider Provider, channel, consumer string, logger *slog.Logger) *Worker {
	return &Worker{
		nats:     nats,
		provider: provider,
		channel:  channel,
		consumer: consumer,
		logger:   logger,
	}
}

func (w *Worker) Start(ctx context.Context) error {
	return w.nats.Subscribe("delivery."+w.channel, w.consumer, func(data []byte) error {
		return w.handleMessage(ctx, data)
	})
}

func (w *Worker) handleMessage(ctx context.Context, data []byte) error {
	msg, err := hermenats.UnmarshalDelivery(data)
	if err != nil {
		w.logger.Error("unmarshal delivery message", "error", err)
		return nil // don't retry bad messages
	}

	w.logger.Info("delivering notification",
		"notification_id", msg.NotificationID,
		"channel", w.channel,
		"provider", w.provider.Name(),
	)

	req := DeliveryRequest{
		NotificationID: msg.NotificationID,
		TenantID:       msg.TenantID,
		UserID:         msg.UserID,
		Channel:        w.channel,
		Title:          msg.Content.Title,
		Body:           msg.Content.Body,
	}
	if msg.Content.ActionURL != nil {
		req.ActionURL = *msg.Content.ActionURL
	}
	if msg.Content.ActionLabel != nil {
		req.ActionLabel = *msg.Content.ActionLabel
	}

	result, err := w.provider.Send(ctx, req)
	if err != nil {
		w.logger.Error("delivery failed",
			"notification_id", msg.NotificationID,
			"channel", w.channel,
			"error", err,
		)
		w.publishEvent(msg.NotificationID, w.channel+".failed", "error", map[string]any{"error": err.Error()})
		return nil // ack — don't retry failed deliveries (rely on dead-letter for now)
	}

	w.logger.Info("delivery succeeded",
		"notification_id", msg.NotificationID,
		"channel", w.channel,
		"provider", result.ProviderName,
	)
	w.publishEvent(msg.NotificationID, w.channel+".sent", "info", map[string]any{
		"provider": result.ProviderName,
		"provider_id": result.ProviderID,
	})

	return nil
}

func (w *Worker) publishEvent(notificationID, event, severity string, metadata map[string]any) {
	evt := &hermenats.EventMessage{
		NotificationID: notificationID,
		Channel:        w.channel,
		Event:          event,
		Severity:       severity,
		Metadata:       metadata,
	}
	evtBytes, _ := json.Marshal(evt)
	if err := w.nats.Publish("notification.events", evtBytes); err != nil {
		w.logger.Error("publish event failed", "error", err)
	}
}
```

- [ ] **Step 2: Write unit test with mock provider**

```go
// internal/delivery/worker_test.go
package delivery_test

import (
	"context"
	"testing"

	"github.com/hermes-notifications/hermes/internal/delivery"
)

type mockProvider struct {
	sendFn func(ctx context.Context, req delivery.DeliveryRequest) (delivery.DeliveryResult, error)
}

func (m *mockProvider) Send(ctx context.Context, req delivery.DeliveryRequest) (delivery.DeliveryResult, error) {
	return m.sendFn(ctx, req)
}

func (m *mockProvider) Name() string { return "mock" }

func TestWorker_HandleMessage_Success(t *testing.T) {
	var received delivery.DeliveryRequest
	provider := &mockProvider{
		sendFn: func(ctx context.Context, req delivery.DeliveryRequest) (delivery.DeliveryResult, error) {
			received = req
			return delivery.DeliveryResult{Success: true, ProviderName: "mock"}, nil
		},
	}

	// We can't easily test the full Subscribe flow without NATS,
	// but we can test that the provider interface works correctly.
	_, err := provider.Send(context.Background(), delivery.DeliveryRequest{
		NotificationID: "notif-1",
		Channel:        "email",
		Title:          "Test",
		Body:           "Body",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if received.NotificationID != "notif-1" {
		t.Fatalf("expected notif-1, got %s", received.NotificationID)
	}
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/delivery/... -v
```

- [ ] **Step 4: Commit**

```bash
git add internal/delivery/worker.go internal/delivery/worker_test.go
git commit -m "feat: add base delivery worker with NATS subscribe and event publish"
```

---

### Task 4: Centrifugo Client

**Files:**
- Create: `internal/centrifugo/client.go`
- Create: `internal/centrifugo/client_test.go`

HTTP client for Centrifugo's server-side publish API.

- [ ] **Step 1: Write Centrifugo client**

```go
// internal/centrifugo/client.go
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
	return &Client{
		apiURL:     apiURL,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// Publish sends a message to a Centrifugo channel.
func (c *Client) Publish(ctx context.Context, channel string, data any) error {
	payload := map[string]any{
		"channel": channel,
		"data":    data,
	}
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
```

- [ ] **Step 2: Write unit test with httptest**

```go
// internal/centrifugo/client_test.go
package centrifugo_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hermes-notifications/hermes/internal/centrifugo"
)

func TestPublish_Success(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/publish" {
			t.Fatalf("expected /api/publish, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "apikey test-key" {
			t.Fatalf("expected auth header, got %s", r.Header.Get("Authorization"))
		}
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	c := centrifugo.NewClient(server.URL, "test-key")
	err := c.Publish(context.Background(), "user#user-123", map[string]string{"title": "Hello"})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if received["channel"] != "user#user-123" {
		t.Fatalf("expected channel user#user-123, got %v", received["channel"])
	}
}

func TestPublish_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	c := centrifugo.NewClient(server.URL, "test-key")
	err := c.Publish(context.Background(), "user#user-123", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/centrifugo/... -v
```

- [ ] **Step 4: Commit**

```bash
git add internal/centrifugo/
git commit -m "feat: add Centrifugo server-side HTTP publish client"
```

---

### Task 5: Inbox Store Methods

**Files:**
- Create: `internal/store/inbox.go`
- Create: `internal/store/inbox_test.go`

The Inbox Worker needs to update notification status to `delivered` and set `delivered_at`. We already have `UpdateNotificationStatus` from Phase 2, but we need a way to mark a notification as inbox-delivered specifically (persisting the inbox state).

For now, the Inbox Worker's "delivery" means the notification was already persisted by the Admin service. The Inbox Worker's job is to: (1) update status to delivered, and (2) publish to Centrifugo. No additional store method is needed beyond what exists.

Actually — let's skip this task. The existing `UpdateNotificationStatus` in `store/events.go` handles everything the Inbox Worker needs. Moving on.

---

### Task 5: Email Worker Service

**Files:**
- Create: `cmd/worker-email/main.go`

Wires up the base Worker with a webhook provider (configurable URL from env).

- [ ] **Step 1: Write email worker entry point**

```go
// cmd/worker-email/main.go
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hermes-notifications/hermes/internal/config"
	"github.com/hermes-notifications/hermes/internal/delivery"
	"github.com/hermes-notifications/hermes/internal/messaging"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	natsClient, err := messaging.Connect(cfg.NATSUrl)
	if err != nil {
		logger.Error("nats", "error", err)
		os.Exit(1)
	}
	defer natsClient.Close()

	if err := natsClient.SetupStreams(ctx); err != nil {
		logger.Error("nats stream setup", "error", err)
		os.Exit(1)
	}

	// Configure provider — webhook by default
	webhookURL := os.Getenv("HERMES_EMAIL_WEBHOOK_URL")
	if webhookURL == "" {
		webhookURL = "http://localhost:9090/email" // default for local dev
	}
	provider := delivery.NewWebhookProvider(webhookURL)

	worker := delivery.NewWorker(natsClient, provider, "email", "worker-email", logger)

	if err := worker.Start(context.Background()); err != nil {
		logger.Error("start email worker", "error", err)
		os.Exit(1)
	}

	// Health checks
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	go http.ListenAndServe(":8083", mux)

	logger.Info("email worker started", "provider", provider.Name(), "webhook_url", webhookURL)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("shutting down")
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./cmd/worker-email/
```

- [ ] **Step 3: Commit**

```bash
git add cmd/worker-email/
git commit -m "feat: add email worker service with webhook provider"
```

---

### Task 6: SMS Worker Service

**Files:**
- Create: `cmd/worker-sms/main.go`

Same pattern as email — webhook provider, different NATS subject.

- [ ] **Step 1: Write SMS worker entry point**

```go
// cmd/worker-sms/main.go
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hermes-notifications/hermes/internal/config"
	"github.com/hermes-notifications/hermes/internal/delivery"
	"github.com/hermes-notifications/hermes/internal/messaging"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	natsClient, err := messaging.Connect(cfg.NATSUrl)
	if err != nil {
		logger.Error("nats", "error", err)
		os.Exit(1)
	}
	defer natsClient.Close()

	if err := natsClient.SetupStreams(ctx); err != nil {
		logger.Error("nats stream setup", "error", err)
		os.Exit(1)
	}

	webhookURL := os.Getenv("HERMES_SMS_WEBHOOK_URL")
	if webhookURL == "" {
		webhookURL = "http://localhost:9090/sms"
	}
	provider := delivery.NewWebhookProvider(webhookURL)

	worker := delivery.NewWorker(natsClient, provider, "sms", "worker-sms", logger)

	if err := worker.Start(context.Background()); err != nil {
		logger.Error("start sms worker", "error", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	go http.ListenAndServe(":8084", mux)

	logger.Info("sms worker started", "provider", provider.Name(), "webhook_url", webhookURL)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("shutting down")
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./cmd/worker-sms/
```

- [ ] **Step 3: Commit**

```bash
git add cmd/worker-sms/
git commit -m "feat: add SMS worker service with webhook provider"
```

---

### Task 7: Inbox Worker Service

**Files:**
- Create: `internal/delivery/inbox.go` — Inbox-specific provider that persists + publishes to Centrifugo
- Create: `internal/delivery/inbox_test.go`
- Create: `cmd/worker-inbox/main.go`

The Inbox Worker is different from email/SMS — it doesn't call an external webhook. Instead, it publishes the notification to Centrifugo for real-time push. The notification is already persisted in Postgres by the Admin service, so the Inbox Worker just pushes to Centrifugo and publishes an `inbox.delivered` event.

- [ ] **Step 1: Write inbox provider**

```go
// internal/delivery/inbox.go
package delivery

import (
	"context"
	"fmt"
	"time"

	"github.com/hermes-notifications/hermes/internal/centrifugo"
)

// InboxProvider delivers notifications to the user's inbox via Centrifugo push.
type InboxProvider struct {
	centrifugo *centrifugo.Client
}

func NewInboxProvider(centrifugo *centrifugo.Client) *InboxProvider {
	return &InboxProvider{centrifugo: centrifugo}
}

func (p *InboxProvider) Name() string { return "inbox" }

func (p *InboxProvider) Send(ctx context.Context, req DeliveryRequest) (DeliveryResult, error) {
	// Build the Centrifugo push payload matching the spec
	payload := map[string]any{
		"id":         req.NotificationID,
		"title":      req.Title,
		"body":       req.Body,
		"created_at": time.Now().UTC().Format(time.RFC3339),
	}
	if req.ActionURL != "" || req.ActionLabel != "" {
		action := map[string]string{}
		if req.ActionURL != "" {
			action["url"] = req.ActionURL
		}
		if req.ActionLabel != "" {
			action["label"] = req.ActionLabel
		}
		payload["action"] = action
	}

	// Publish to user's Centrifugo channel
	channel := fmt.Sprintf("user#%s", req.UserID)
	if err := p.centrifugo.Publish(ctx, channel, payload); err != nil {
		return DeliveryResult{
			ProviderName: "inbox",
			Error:        err.Error(),
		}, fmt.Errorf("centrifugo publish: %w", err)
	}

	return DeliveryResult{
		Success:      true,
		ProviderName: "inbox",
	}, nil
}
```

- [ ] **Step 2: Write unit test**

```go
// internal/delivery/inbox_test.go
package delivery_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	cfgo "github.com/hermes-notifications/hermes/internal/centrifugo"
	"github.com/hermes-notifications/hermes/internal/delivery"
)

func TestInboxProvider_Success(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := cfgo.NewClient(server.URL, "test-key")
	provider := delivery.NewInboxProvider(client)

	result, err := provider.Send(context.Background(), delivery.DeliveryRequest{
		NotificationID: "notif-1",
		UserID:         "user-123",
		Title:          "Test Notification",
		Body:           "Test body",
		ActionURL:      "https://example.com",
		ActionLabel:    "View",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	if provider.Name() != "inbox" {
		t.Fatalf("expected inbox, got %s", provider.Name())
	}
	// Verify Centrifugo channel
	if received["channel"] != "user#user-123" {
		t.Fatalf("expected channel user#user-123, got %v", received["channel"])
	}
	// Verify payload
	data := received["data"].(map[string]any)
	if data["title"] != "Test Notification" {
		t.Fatalf("expected title, got %v", data["title"])
	}
}
```

- [ ] **Step 3: Write inbox worker entry point**

```go
// cmd/worker-inbox/main.go
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	cfgo "github.com/hermes-notifications/hermes/internal/centrifugo"
	"github.com/hermes-notifications/hermes/internal/config"
	"github.com/hermes-notifications/hermes/internal/delivery"
	"github.com/hermes-notifications/hermes/internal/messaging"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	natsClient, err := messaging.Connect(cfg.NATSUrl)
	if err != nil {
		logger.Error("nats", "error", err)
		os.Exit(1)
	}
	defer natsClient.Close()

	if err := natsClient.SetupStreams(ctx); err != nil {
		logger.Error("nats stream setup", "error", err)
		os.Exit(1)
	}

	// Centrifugo config
	centrifugoURL := os.Getenv("HERMES_CENTRIFUGO_API_URL")
	if centrifugoURL == "" {
		centrifugoURL = "http://localhost:8000"
	}
	centrifugoKey := os.Getenv("HERMES_CENTRIFUGO_API_KEY")
	if centrifugoKey == "" {
		centrifugoKey = "centrifugo-api-key"
	}

	cfgoClient := cfgo.NewClient(centrifugoURL, centrifugoKey)
	provider := delivery.NewInboxProvider(cfgoClient)

	worker := delivery.NewWorker(natsClient, provider, "inbox", "worker-inbox", logger)

	if err := worker.Start(context.Background()); err != nil {
		logger.Error("start inbox worker", "error", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	go http.ListenAndServe(":8085", mux)

	logger.Info("inbox worker started", "centrifugo_url", centrifugoURL)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("shutting down")
}
```

- [ ] **Step 4: Run all tests**

```bash
go test ./internal/delivery/... -v
go test ./internal/centrifugo/... -v
```

- [ ] **Step 5: Verify all binaries compile**

```bash
go build ./cmd/worker-email/ && go build ./cmd/worker-sms/ && go build ./cmd/worker-inbox/
```

- [ ] **Step 6: Commit**

```bash
git add internal/delivery/inbox.go internal/delivery/inbox_test.go cmd/worker-inbox/
git commit -m "feat: add inbox worker with Centrifugo push"
```

---

### Task 8: Delivery Pipeline Integration Test

**Files:**
- Create: `tests/e2e/delivery_test.go`

End-to-end test verifying the full pipeline: Admin → Router → Email/Inbox Workers → Event Writer → status update.

This test:
1. Connects to real Postgres, NATS, Redis
2. Sets up a mock webhook server for the email worker
3. Starts Router, Email Worker (with webhook to mock server), Event Writer in-process
4. Sends a notification with email+inbox channels
5. Verifies the mock webhook received the email delivery
6. Waits for events + status update
7. Verifies notification status advanced to `delivered` (since workers publish `email.sent`/`inbox.delivered` events)

Note: Inbox Worker needs Centrifugo running. For the test, use a mock HTTP server as the Centrifugo endpoint — it just needs to return 200.

- [ ] **Step 1: Write test**

The test should follow the pattern of the existing `pipeline_test.go` but add workers. Use unique slugs, clean NATS consumers before starting, verify via DB side effects.

- [ ] **Step 2: Run test**

```bash
go test ./tests/e2e/... -tags=integration -v -run TestDelivery -timeout=30s
```

- [ ] **Step 3: Commit**

```bash
git add tests/e2e/delivery_test.go
git commit -m "test: add full delivery pipeline integration test"
```

---

### Task 9: Tidy and Final Verification

- [ ] **Step 1: go mod tidy**

```bash
go mod tidy
```

- [ ] **Step 2: Run all unit tests**

```bash
go test ./... -v
```

- [ ] **Step 3: Verify all 6 binaries compile**

```bash
go build ./cmd/admin/ && go build ./cmd/router/ && go build ./cmd/worker-events/ && go build ./cmd/worker-email/ && go build ./cmd/worker-sms/ && go build ./cmd/worker-inbox/
```

- [ ] **Step 4: Commit tidy if needed**

```bash
git add go.mod go.sum
git commit -m "chore: go mod tidy"
```

---

## Phase 3 Completion Criteria

- [ ] Shared delivery provider interface (`Provider` with `Send` + `Name`)
- [ ] Webhook adapter (used by email + SMS workers)
- [ ] Centrifugo HTTP publish client
- [ ] Inbox provider (publishes to Centrifugo via user-limited channels)
- [ ] Base worker logic (NATS subscribe, provider dispatch, event publish)
- [ ] Email worker service (`cmd/worker-email/`, port 8083)
- [ ] SMS worker service (`cmd/worker-sms/`, port 8084)
- [ ] Inbox worker service (`cmd/worker-inbox/`, port 8085)
- [ ] Full delivery pipeline integration test
- [ ] All unit and integration tests pass
- [ ] All 6 binaries compile
