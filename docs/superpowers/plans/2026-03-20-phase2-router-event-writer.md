# Phase 2: Router + Event Writer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Router service (template resolution, channel routing, fan-out) and Event Writer service (batch event persistence, status rollup) — completing the async processing pipeline from notification send through to delivery subjects.

**Architecture:** Both services are NATS consumers. The Router subscribes to `notification.send`, resolves templates (cached in Redis), determines target channels, and fans out delivery messages to `delivery.{channel}` subjects. The Event Writer subscribes to `notification.events`, batch-inserts event log entries, and updates notification status with out-of-order safety. Both publish events to `notification.events`.

**Tech Stack:** Go, NATS JetStream (existing `messaging` package), PostgreSQL (existing `store` package), Redis (existing `cache` package), Go `text/template` and `html/template` for template rendering.

**Spec:** `docs/superpowers/specs/2026-03-20-hermes-notification-service-design.md`

**Depends on:** Phase 1 (foundation + admin service) — all shared packages exist.

---

## File Structure

```
hermes/
├── cmd/
│   ├── router/
│   │   └── main.go                       # Router service entry point
│   └── worker-events/
│       └── main.go                       # Event Writer service entry point
├── internal/
│   ├── store/
│   │   ├── preferences.go                # User preferences queries (new)
│   │   ├── preferences_test.go           # Integration tests
│   │   ├── events.go                     # Event insert + status rollup (new)
│   │   └── events_test.go               # Integration tests
│   ├── router/
│   │   ├── router.go                     # Router service — NATS consumer, orchestration
│   │   ├── router_test.go               # Integration test
│   │   ├── template.go                   # Template resolution + rendering
│   │   ├── template_test.go
│   │   ├── channels.go                   # Channel resolution logic
│   │   └── channels_test.go
│   ├── eventwriter/
│   │   ├── writer.go                     # Batch event writer — NATS consumer, batch insert
│   │   ├── writer_test.go
│   │   └── batch.go                      # Batch buffer with size/time flush
│   └── nats/
│       └── messages.go                   # Shared NATS message types (new)
```

---

## Implementation Notes

- The NATS message published by the Admin service's send handler includes `data` (template variables) and `metadata.type` (slug). The Router needs these to resolve and render templates.
- The send handler currently publishes `content.title` and `content.body` even for type-based sends (they're empty strings). The Router should override these with rendered template output.
- For direct sends (no type), the Router skips template resolution. If `data` is provided with direct content, the Router renders the content fields as templates.
- Channel resolution order: explicit `channels` override → user preferences for the group → group's `default_channels`.
- The Event Writer uses application-side status rank logic (not a Postgres function) for simplicity.

---

### Task 1: Shared NATS Message Types

**Files:**
- Create: `internal/nats/messages.go`

Defines the typed structs for NATS messages shared between the Admin service, Router, and Workers.

- [ ] **Step 1: Create message types**

```go
// internal/nats/messages.go
package hermenats

import "encoding/json"

// SendMessage is published to notification.send by the Admin service.
type SendMessage struct {
	NotificationID string            `json:"notification_id"`
	TenantID       string            `json:"tenant_id"`
	UserID         string            `json:"user_id"`
	Content        MessageContent    `json:"content"`
	Metadata       MessageMetadata   `json:"metadata"`
	Data           map[string]any    `json:"data,omitempty"`
	Channels       []string          `json:"channels,omitempty"`
	Attempt        int               `json:"attempt"`
}

type MessageContent struct {
	Title       string  `json:"title"`
	Body        string  `json:"body"`
	ActionURL   *string `json:"action_url,omitempty"`
	ActionLabel *string `json:"action_label,omitempty"`
}

type MessageMetadata struct {
	Group string `json:"group"`
	Type  string `json:"type,omitempty"`
}

// DeliveryMessage is published to delivery.{channel} by the Router.
type DeliveryMessage struct {
	NotificationID string          `json:"notification_id"`
	TenantID       string          `json:"tenant_id"`
	UserID         string          `json:"user_id"`
	Channel        string          `json:"channel"`
	Content        MessageContent  `json:"content"`
	Metadata       MessageMetadata `json:"metadata"`
	Attempt        int             `json:"attempt"`
}

// EventMessage is published to notification.events by Router and Workers.
type EventMessage struct {
	NotificationID string         `json:"notification_id"`
	Channel        string         `json:"channel"`
	Event          string         `json:"event"`
	Severity       string         `json:"severity"` // info, warn, error
	Metadata       map[string]any `json:"metadata,omitempty"`
}

func (m *SendMessage) Marshal() ([]byte, error)     { return json.Marshal(m) }
func (m *DeliveryMessage) Marshal() ([]byte, error)  { return json.Marshal(m) }
func (m *EventMessage) Marshal() ([]byte, error)     { return json.Marshal(m) }

func UnmarshalSend(data []byte) (*SendMessage, error) {
	var m SendMessage
	return &m, json.Unmarshal(data, &m)
}

func UnmarshalDelivery(data []byte) (*DeliveryMessage, error) {
	var m DeliveryMessage
	return &m, json.Unmarshal(data, &m)
}

func UnmarshalEvent(data []byte) (*EventMessage, error) {
	var m EventMessage
	return &m, json.Unmarshal(data, &m)
}
```

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/nats/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/nats/
git commit -m "feat: add shared NATS message types"
```

---

### Task 2: Store — User Preferences

**Files:**
- Create: `internal/store/preferences.go`
- Create: `internal/store/preferences_test.go`

The Router needs to query user preferences to determine channels.

- [ ] **Step 1: Write preferences store**

```go
// internal/store/preferences.go
package store

import (
	"context"
	"fmt"

	"github.com/hermes-notifications/hermes/internal/models"
)

// GetUserPreference returns the user's channel preference for a group, or nil if using defaults.
func (s *Store) GetUserPreference(ctx context.Context, userID, groupID string) (*models.UserPreference, error) {
	p := &models.UserPreference{}
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, group_id, channels FROM user_preferences WHERE user_id = $1 AND group_id = $2`,
		userID, groupID,
	).Scan(&p.UserID, &p.GroupID, &p.Channels)
	if err != nil {
		return nil, fmt.Errorf("get user preference: %w", err)
	}
	return p, nil
}

// SetUserPreference creates or updates a user's channel preference for a group.
func (s *Store) SetUserPreference(ctx context.Context, userID, groupID string, channels []string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO user_preferences (user_id, group_id, channels) VALUES ($1, $2, $3)
		 ON CONFLICT (user_id, group_id) DO UPDATE SET channels = $3`,
		userID, groupID, channels,
	)
	if err != nil {
		return fmt.Errorf("set user preference: %w", err)
	}
	return nil
}

// DeleteUserPreference reverts a user to group defaults.
func (s *Store) DeleteUserPreference(ctx context.Context, userID, groupID string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM user_preferences WHERE user_id = $1 AND group_id = $2`,
		userID, groupID,
	)
	if err != nil {
		return fmt.Errorf("delete user preference: %w", err)
	}
	return nil
}
```

- [ ] **Step 2: Write integration test**

```go
//go:build integration

// internal/store/preferences_test.go
package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestSetAndGetUserPreference(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "user_preferences", "notifications", "users", "notification_types", "notification_groups", "tenants")

	ctx := context.Background()
	tenantID := uuid.New().String()
	s.CreateTenant(ctx, tenantID, "Test")
	u, _ := s.EnsureUser(ctx, tenantID, "ext-pref-1")
	g, _ := s.CreateGroup(ctx, "billing-pref", "Billing", []string{"email", "inbox"})

	// Set preference
	err := s.SetUserPreference(ctx, u.ID, g.ID, []string{"inbox"})
	if err != nil {
		t.Fatalf("SetUserPreference: %v", err)
	}

	// Get preference
	pref, err := s.GetUserPreference(ctx, u.ID, g.ID)
	if err != nil {
		t.Fatalf("GetUserPreference: %v", err)
	}
	if len(pref.Channels) != 1 || pref.Channels[0] != "inbox" {
		t.Fatalf("expected [inbox], got %v", pref.Channels)
	}

	// Delete preference
	err = s.DeleteUserPreference(ctx, u.ID, g.ID)
	if err != nil {
		t.Fatalf("DeleteUserPreference: %v", err)
	}

	// Verify it's gone
	_, err = s.GetUserPreference(ctx, u.ID, g.ID)
	if err == nil {
		t.Fatal("expected error after delete")
	}
}
```

Note: `GetUserPreference` returns an error when no row is found (pgx returns `pgx.ErrNoRows`). The Router should check for this and fall back to group defaults.

- [ ] **Step 3: Run tests**

```bash
go test ./internal/store/... -tags=integration -v
```

- [ ] **Step 4: Commit**

```bash
git add internal/store/preferences.go internal/store/preferences_test.go
git commit -m "feat: add user preferences store methods"
```

---

### Task 3: Store — Event Insert + Status Rollup

**Files:**
- Create: `internal/store/events.go`
- Create: `internal/store/events_test.go`

The Event Writer needs to batch-insert events and update notification status.

- [ ] **Step 1: Write events store**

```go
// internal/store/events.go
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/hermes-notifications/hermes/internal/id"
	"github.com/hermes-notifications/hermes/internal/models"
)

// InsertEvent inserts a notification event.
func (s *Store) InsertEvent(ctx context.Context, notificationID, channel, event, severity string, metadata []byte) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO notification_events (id, notification_id, channel, event, severity, metadata)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		id.New(), notificationID, channel, event, severity, metadata,
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

// InsertEvents batch-inserts multiple events in a single transaction.
func (s *Store) InsertEvents(ctx context.Context, events []models.NotificationEvent) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, e := range events {
		if e.ID == "" {
			e.ID = id.New()
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO notification_events (id, notification_id, channel, event, severity, metadata)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			e.ID, e.NotificationID, e.Channel, e.Event, e.Severity, e.Metadata,
		)
		if err != nil {
			return fmt.Errorf("insert event %s: %w", e.ID, err)
		}
	}

	return tx.Commit(ctx)
}

// UpdateNotificationStatus updates the notification status using the rollup rules.
// Status only advances, never regresses. Timestamps are set once (first writer wins).
func (s *Store) UpdateNotificationStatus(ctx context.Context, notificationID string, newStatus models.NotificationStatus, eventTime time.Time) error {
	rank := newStatus.Rank()

	_, err := s.pool.Exec(ctx, `
		UPDATE notifications
		SET status = CASE WHEN $2 > (
				CASE status
					WHEN 'pending' THEN 0
					WHEN 'sent' THEN 1
					WHEN 'delivered' THEN 2
					WHEN 'read' THEN 3
					WHEN 'archived' THEN 4
					ELSE 0
				END
			) THEN $3 ELSE status END,
		    sent_at = COALESCE(sent_at, CASE WHEN $2 >= 1 THEN $4 ELSE NULL END),
		    delivered_at = COALESCE(delivered_at, CASE WHEN $2 >= 2 THEN $4 ELSE NULL END)
		WHERE id = $1`,
		notificationID, rank, string(newStatus), eventTime,
	)
	if err != nil {
		return fmt.Errorf("update notification status: %w", err)
	}
	return nil
}
```

- [ ] **Step 2: Write integration test**

```go
//go:build integration

// internal/store/events_test.go
package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	hermesid "github.com/hermes-notifications/hermes/internal/id"
	"github.com/hermes-notifications/hermes/internal/models"
)

func TestInsertEvents_And_StatusRollup(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notification_events", "notifications", "users", "notification_types", "notification_groups", "tenants")

	ctx := context.Background()
	tenantID := uuid.New().String()
	s.CreateTenant(ctx, tenantID, "Test")
	u, _ := s.EnsureUser(ctx, tenantID, "ext-evt-1")
	g, _ := s.CreateGroup(ctx, "billing-evt", "Billing", []string{"email", "inbox"})

	notifID := hermesid.New()
	n := &models.Notification{
		ID: notifID, TenantID: tenantID, UserID: u.ID, GroupID: g.ID,
		Title: "Test", Body: "Body", Channels: []string{"email", "inbox"},
		Status: models.StatusPending,
	}
	s.CreateNotification(ctx, n)

	// Insert events
	events := []models.NotificationEvent{
		{NotificationID: notifID, Channel: "email", Event: "email.routed", Severity: "info"},
		{NotificationID: notifID, Channel: "inbox", Event: "inbox.routed", Severity: "info"},
	}
	if err := s.InsertEvents(ctx, events); err != nil {
		t.Fatalf("InsertEvents: %v", err)
	}

	// Verify events
	got, _ := s.GetNotificationEvents(ctx, notifID)
	if len(got) != 2 {
		t.Fatalf("expected 2 events, got %d", len(got))
	}

	// Update status to sent
	now := time.Now()
	if err := s.UpdateNotificationStatus(ctx, notifID, models.StatusSent, now); err != nil {
		t.Fatalf("UpdateStatus sent: %v", err)
	}

	notif, _ := s.GetNotificationByID(ctx, notifID)
	if notif.Status != models.StatusSent {
		t.Fatalf("expected sent, got %s", notif.Status)
	}
	if notif.SentAt == nil {
		t.Fatal("expected sent_at to be set")
	}

	// Update to delivered
	if err := s.UpdateNotificationStatus(ctx, notifID, models.StatusDelivered, now); err != nil {
		t.Fatalf("UpdateStatus delivered: %v", err)
	}
	notif, _ = s.GetNotificationByID(ctx, notifID)
	if notif.Status != models.StatusDelivered {
		t.Fatalf("expected delivered, got %s", notif.Status)
	}

	// Attempt regression to sent — should be ignored
	if err := s.UpdateNotificationStatus(ctx, notifID, models.StatusSent, now); err != nil {
		t.Fatalf("UpdateStatus regression: %v", err)
	}
	notif, _ = s.GetNotificationByID(ctx, notifID)
	if notif.Status != models.StatusDelivered {
		t.Fatalf("expected delivered (no regression), got %s", notif.Status)
	}
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/store/... -tags=integration -v
```

- [ ] **Step 4: Commit**

```bash
git add internal/store/events.go internal/store/events_test.go
git commit -m "feat: add event insert and status rollup store methods"
```

---

### Task 4: Template Resolution + Rendering

**Files:**
- Create: `internal/router/template.go`
- Create: `internal/router/template_test.go`

Resolves notification type config (from Redis cache or DB), renders templates with data.

- [ ] **Step 1: Write template resolver**

```go
// internal/router/template.go
package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	htmltemplate "html/template"
	"time"
	texttemplate "text/template"

	"github.com/hermes-notifications/hermes/internal/cache"
	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/hermes-notifications/hermes/internal/store"
)

type TemplateResolver struct {
	store *store.Store
	cache *cache.Client
}

func NewTemplateResolver(store *store.Store, cache *cache.Client) *TemplateResolver {
	return &TemplateResolver{store: store, cache: cache}
}

// Resolve fetches the notification type config, using Redis cache first.
func (tr *TemplateResolver) Resolve(ctx context.Context, slug string) (*models.NotificationType, error) {
	// Try cache
	if tr.cache != nil {
		data, err := tr.cache.GetTypeConfig(ctx, slug)
		if err == nil && data != nil {
			var nt models.NotificationType
			if err := json.Unmarshal(data, &nt); err == nil {
				return &nt, nil
			}
		}
	}

	// Cache miss — query DB
	nt, err := tr.store.GetTypeBySlug(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("resolve type %s: %w", slug, err)
	}

	// Populate cache
	if tr.cache != nil {
		if data, err := json.Marshal(nt); err == nil {
			tr.cache.SetTypeConfig(ctx, slug, data, 5*time.Minute)
		}
	}

	return nt, nil
}

// RenderContent renders all channel templates for a notification type with the given data.
// Returns a map of channel -> rendered content.
type RenderedContent struct {
	EmailSubject string
	EmailBody    string
	SMSBody      string
	InboxTitle   string
	InboxBody    string
}

func RenderTemplates(nt *models.NotificationType, data map[string]any) (*RenderedContent, error) {
	rc := &RenderedContent{}
	var err error

	if nt.EmailSubject != nil {
		rc.EmailSubject, err = renderText(*nt.EmailSubject, data)
		if err != nil {
			return nil, fmt.Errorf("render email_subject: %w", err)
		}
	}
	if nt.EmailBody != nil {
		rc.EmailBody, err = renderHTML(*nt.EmailBody, data)
		if err != nil {
			return nil, fmt.Errorf("render email_body: %w", err)
		}
	}
	if nt.SMSBody != nil {
		rc.SMSBody, err = renderText(*nt.SMSBody, data)
		if err != nil {
			return nil, fmt.Errorf("render sms_body: %w", err)
		}
	}
	if nt.InboxTitle != nil {
		rc.InboxTitle, err = renderText(*nt.InboxTitle, data)
		if err != nil {
			return nil, fmt.Errorf("render inbox_title: %w", err)
		}
	}
	if nt.InboxBody != nil {
		rc.InboxBody, err = renderText(*nt.InboxBody, data)
		if err != nil {
			return nil, fmt.Errorf("render inbox_body: %w", err)
		}
	}

	return rc, nil
}

// RenderDirectContent renders content fields as templates if data is provided.
func RenderDirectContent(title, body string, data map[string]any) (string, string, error) {
	if len(data) == 0 {
		return title, body, nil
	}
	renderedTitle, err := renderText(title, data)
	if err != nil {
		return "", "", fmt.Errorf("render title: %w", err)
	}
	renderedBody, err := renderText(body, data)
	if err != nil {
		return "", "", fmt.Errorf("render body: %w", err)
	}
	return renderedTitle, renderedBody, nil
}

func renderText(tmplStr string, data map[string]any) (string, error) {
	t, err := texttemplate.New("").Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func renderHTML(tmplStr string, data map[string]any) (string, error) {
	t, err := htmltemplate.New("").Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
```

- [ ] **Step 2: Write unit tests**

```go
// internal/router/template_test.go
package router_test

import (
	"testing"

	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/hermes-notifications/hermes/internal/router"
)

func strPtr(s string) *string { return &s }

func TestRenderTemplates(t *testing.T) {
	nt := &models.NotificationType{
		EmailSubject: strPtr("Invoice {{.number}} paid"),
		EmailBody:    strPtr("<p>Hi {{.name}}, invoice {{.number}} is paid.</p>"),
		InboxTitle:   strPtr("Invoice {{.number}} paid"),
		InboxBody:    strPtr("Your invoice {{.number}} has been paid."),
	}
	data := map[string]any{"number": "INV-001", "name": "Alice"}

	rc, err := router.RenderTemplates(nt, data)
	if err != nil {
		t.Fatalf("RenderTemplates: %v", err)
	}

	if rc.EmailSubject != "Invoice INV-001 paid" {
		t.Fatalf("email_subject: got %q", rc.EmailSubject)
	}
	if rc.InboxTitle != "Invoice INV-001 paid" {
		t.Fatalf("inbox_title: got %q", rc.InboxTitle)
	}
	if rc.InboxBody != "Your invoice INV-001 has been paid." {
		t.Fatalf("inbox_body: got %q", rc.InboxBody)
	}
}

func TestRenderTemplates_HTMLEscaping(t *testing.T) {
	nt := &models.NotificationType{
		EmailBody: strPtr("<p>Hello {{.name}}</p>"),
	}
	data := map[string]any{"name": "<script>alert('xss')</script>"}

	rc, err := router.RenderTemplates(nt, data)
	if err != nil {
		t.Fatalf("RenderTemplates: %v", err)
	}

	// html/template should escape the script tag
	if rc.EmailBody == "<p>Hello <script>alert('xss')</script></p>" {
		t.Fatal("expected HTML escaping but got raw script tag")
	}
}

func TestRenderDirectContent_WithData(t *testing.T) {
	title, body, err := router.RenderDirectContent(
		"Invoice {{.number}}",
		"Paid: {{.amount}}",
		map[string]any{"number": "123", "amount": "$99"},
	)
	if err != nil {
		t.Fatalf("RenderDirectContent: %v", err)
	}
	if title != "Invoice 123" {
		t.Fatalf("title: got %q", title)
	}
	if body != "Paid: $99" {
		t.Fatalf("body: got %q", body)
	}
}

func TestRenderDirectContent_NoData(t *testing.T) {
	title, body, err := router.RenderDirectContent("Hello", "World", nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if title != "Hello" || body != "World" {
		t.Fatalf("got %q %q", title, body)
	}
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/router/... -v
```

- [ ] **Step 4: Commit**

```bash
git add internal/router/template.go internal/router/template_test.go
git commit -m "feat: add template resolution and rendering with HTML escaping"
```

---

### Task 5: Channel Resolution

**Files:**
- Create: `internal/router/channels.go`
- Create: `internal/router/channels_test.go`

Determines which channels a notification should be delivered to.

- [ ] **Step 1: Write channel resolver**

```go
// internal/router/channels.go
package router

import (
	"context"

	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/hermes-notifications/hermes/internal/store"
)

type ChannelResolver struct {
	store *store.Store
}

func NewChannelResolver(store *store.Store) *ChannelResolver {
	return &ChannelResolver{store: store}
}

// ResolveChannels determines target channels for a notification.
// Priority: explicit override → user preferences → group defaults.
func (cr *ChannelResolver) ResolveChannels(ctx context.Context, explicitChannels []string, userID, groupID string) ([]string, error) {
	// 1. Explicit override from send request
	if len(explicitChannels) > 0 {
		return explicitChannels, nil
	}

	// 2. User preferences
	pref, err := cr.store.GetUserPreference(ctx, userID, groupID)
	if err == nil && pref != nil && len(pref.Channels) > 0 {
		return pref.Channels, nil
	}

	// 3. Group defaults
	group, err := cr.store.GetGroupByID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	return group.DefaultChannels, nil
}

// FilterChannelsForType filters channels to only those that have templates defined.
// For direct sends (no type), all channels are valid.
func FilterChannelsForType(channels []string, nt *models.NotificationType) []string {
	if nt == nil {
		return channels
	}
	var filtered []string
	for _, ch := range channels {
		switch ch {
		case "email":
			if nt.EmailSubject != nil || nt.EmailBody != nil {
				filtered = append(filtered, ch)
			}
		case "sms":
			if nt.SMSBody != nil {
				filtered = append(filtered, ch)
			}
		case "inbox":
			if nt.InboxTitle != nil || nt.InboxBody != nil {
				filtered = append(filtered, ch)
			}
		}
	}
	return filtered
}
```

- [ ] **Step 2: Write unit tests**

```go
// internal/router/channels_test.go
package router_test

import (
	"testing"

	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/hermes-notifications/hermes/internal/router"
)

func TestFilterChannelsForType_AllTemplates(t *testing.T) {
	nt := &models.NotificationType{
		EmailSubject: strPtr("subject"),
		SMSBody:      strPtr("body"),
		InboxTitle:   strPtr("title"),
	}
	got := router.FilterChannelsForType([]string{"email", "sms", "inbox"}, nt)
	if len(got) != 3 {
		t.Fatalf("expected 3 channels, got %d: %v", len(got), got)
	}
}

func TestFilterChannelsForType_NoEmailTemplate(t *testing.T) {
	nt := &models.NotificationType{
		InboxTitle: strPtr("title"),
	}
	got := router.FilterChannelsForType([]string{"email", "inbox"}, nt)
	if len(got) != 1 || got[0] != "inbox" {
		t.Fatalf("expected [inbox], got %v", got)
	}
}

func TestFilterChannelsForType_NilType(t *testing.T) {
	got := router.FilterChannelsForType([]string{"email", "inbox"}, nil)
	if len(got) != 2 {
		t.Fatalf("expected 2 channels for direct send, got %d", len(got))
	}
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/router/... -v
```

- [ ] **Step 4: Commit**

```bash
git add internal/router/channels.go internal/router/channels_test.go
git commit -m "feat: add channel resolution with preference override and type filtering"
```

---

### Task 6: Router Service — Core Logic

**Files:**
- Create: `internal/router/router.go`
- Create: `internal/router/router_test.go`
- Create: `cmd/router/main.go`

The Router subscribes to `notification.send`, processes each message, and fans out delivery messages.

- [ ] **Step 1: Write router**

```go
// internal/router/router.go
package router

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/hermes-notifications/hermes/internal/cache"
	hermenats "github.com/hermes-notifications/hermes/internal/nats"
	"github.com/hermes-notifications/hermes/internal/messaging"
	"github.com/hermes-notifications/hermes/internal/store"
)

type Router struct {
	nats      *messaging.Client
	templates *TemplateResolver
	channels  *ChannelResolver
	store     *store.Store
	logger    *slog.Logger
}

func New(nats *messaging.Client, st *store.Store, cache *cache.Client, logger *slog.Logger) *Router {
	return &Router{
		nats:      nats,
		templates: NewTemplateResolver(st, cache),
		channels:  NewChannelResolver(st),
		store:     st,
		logger:    logger,
	}
}

func (r *Router) Start(ctx context.Context) error {
	return r.nats.Subscribe("notification.send", "router", func(data []byte) error {
		return r.handleMessage(ctx, data)
	})
}

func (r *Router) handleMessage(ctx context.Context, data []byte) error {
	msg, err := hermenats.UnmarshalSend(data)
	if err != nil {
		r.logger.Error("unmarshal send message", "error", err)
		return nil // don't retry bad messages
	}

	r.logger.Info("routing notification",
		"notification_id", msg.NotificationID,
		"type", msg.Metadata.Type,
	)

	var content hermenats.MessageContent
	var notifType *models.NotificationType

	if msg.Metadata.Type != "" {
		// Template-based send — resolve and render
		nt, err := r.templates.Resolve(ctx, msg.Metadata.Type)
		if err != nil {
			r.logger.Error("resolve type", "error", err, "notification_id", msg.NotificationID)
			r.publishEvent(msg.NotificationID, "", "route.failed", "error", map[string]any{"error": err.Error()})
			return nil
		}
		notifType = nt

		rendered, err := RenderTemplates(nt, msg.Data)
		if err != nil {
			r.logger.Error("render templates", "error", err, "notification_id", msg.NotificationID)
			r.publishEvent(msg.NotificationID, "", "route.failed", "error", map[string]any{"error": err.Error()})
			return nil
		}

		content = hermenats.MessageContent{
			Title:       rendered.InboxTitle,
			Body:        rendered.InboxBody,
			ActionURL:   msg.Content.ActionURL,
			ActionLabel: msg.Content.ActionLabel,
		}
	} else {
		// Direct send — optionally render content with data
		title, body := msg.Content.Title, msg.Content.Body
		if len(msg.Data) > 0 {
			title, body, err = RenderDirectContent(title, body, msg.Data)
			if err != nil {
				r.logger.Error("render direct content", "error", err)
			}
		}
		content = hermenats.MessageContent{
			Title:       title,
			Body:        body,
			ActionURL:   msg.Content.ActionURL,
			ActionLabel: msg.Content.ActionLabel,
		}
	}

	// Resolve channels
	// Need the group_id — fetch notification from DB to get it
	notif, err := r.store.GetNotificationByID(ctx, msg.NotificationID)
	if err != nil {
		r.logger.Error("get notification", "error", err, "notification_id", msg.NotificationID)
		return err // retry
	}

	channels, err := r.channels.ResolveChannels(ctx, msg.Channels, msg.UserID, notif.GroupID)
	if err != nil {
		r.logger.Error("resolve channels", "error", err, "notification_id", msg.NotificationID)
		return err
	}

	// Filter channels by type templates (only for template-based sends)
	channels = FilterChannelsForType(channels, notifType)

	if len(channels) == 0 {
		r.logger.Warn("no channels resolved", "notification_id", msg.NotificationID)
		r.publishEvent(msg.NotificationID, "", "route.no_channels", "warn", nil)
		return nil
	}

	// Update notification channels in DB
	r.store.UpdateNotificationChannels(ctx, msg.NotificationID, channels)

	// Fan out delivery messages
	for _, ch := range channels {
		delivery := &hermenats.DeliveryMessage{
			NotificationID: msg.NotificationID,
			TenantID:       msg.TenantID,
			UserID:         msg.UserID,
			Channel:        ch,
			Content:        r.contentForChannel(content, notifType, ch, msg.Data),
			Metadata:       msg.Metadata,
			Attempt:        1,
		}

		deliveryBytes, _ := delivery.Marshal()
		if err := r.nats.Publish("delivery."+ch, deliveryBytes); err != nil {
			r.logger.Error("publish delivery", "error", err, "channel", ch, "notification_id", msg.NotificationID)
			r.publishEvent(msg.NotificationID, ch, ch+".route_failed", "error", map[string]any{"error": err.Error()})
			continue
		}

		r.publishEvent(msg.NotificationID, ch, ch+".routed", "info", nil)
	}

	// Publish sent event
	r.publishEvent(msg.NotificationID, "", "notification.sent", "info", map[string]any{"channels": channels})

	return nil
}

// contentForChannel returns channel-specific content from rendered templates.
func (r *Router) contentForChannel(defaultContent hermenats.MessageContent, nt *models.NotificationType, channel string, data map[string]any) hermenats.MessageContent {
	if nt == nil {
		return defaultContent
	}

	rendered, _ := RenderTemplates(nt, data)
	if rendered == nil {
		return defaultContent
	}

	switch channel {
	case "email":
		return hermenats.MessageContent{
			Title:       rendered.EmailSubject,
			Body:        rendered.EmailBody,
			ActionURL:   defaultContent.ActionURL,
			ActionLabel: defaultContent.ActionLabel,
		}
	case "sms":
		return hermenats.MessageContent{
			Body:        rendered.SMSBody,
			ActionURL:   defaultContent.ActionURL,
			ActionLabel: defaultContent.ActionLabel,
		}
	case "inbox":
		return hermenats.MessageContent{
			Title:       rendered.InboxTitle,
			Body:        rendered.InboxBody,
			ActionURL:   defaultContent.ActionURL,
			ActionLabel: defaultContent.ActionLabel,
		}
	default:
		return defaultContent
	}
}

func (r *Router) publishEvent(notificationID, channel, event, severity string, metadata map[string]any) {
	evt := &hermenats.EventMessage{
		NotificationID: notificationID,
		Channel:        channel,
		Event:          event,
		Severity:       severity,
		Metadata:       metadata,
	}
	evtBytes, _ := evt.Marshal()
	if err := r.nats.Publish("notification.events", evtBytes); err != nil {
		r.logger.Error("publish event", "error", err)
	}
}
```

This also requires a new store method — add `UpdateNotificationChannels`:

```go
// Add to internal/store/notifications.go
func (s *Store) UpdateNotificationChannels(ctx context.Context, notificationID string, channels []string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE notifications SET channels = $2 WHERE id = $1`,
		notificationID, channels,
	)
	return err
}
```

You also need to add an import for `models` in router.go — the `handleMessage` function uses `models.NotificationType`.

- [ ] **Step 2: Write cmd/router/main.go**

```go
// cmd/router/main.go
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hermes-notifications/hermes/internal/cache"
	"github.com/hermes-notifications/hermes/internal/config"
	"github.com/hermes-notifications/hermes/internal/database"
	"github.com/hermes-notifications/hermes/internal/messaging"
	"github.com/hermes-notifications/hermes/internal/router"
	"github.com/hermes-notifications/hermes/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	natsClient, err := messaging.Connect(cfg.NATSUrl)
	if err != nil {
		logger.Error("nats", "error", err)
		os.Exit(1)
	}
	defer natsClient.Close()
	natsClient.SetupStreams(ctx)

	redisClient, err := cache.Connect(cfg.RedisURL)
	if err != nil {
		logger.Error("redis", "error", err)
		os.Exit(1)
	}
	defer redisClient.Close()

	st := store.New(pool)
	r := router.New(natsClient, st, redisClient, logger)

	if err := r.Start(context.Background()); err != nil {
		logger.Error("start router", "error", err)
		os.Exit(1)
	}

	// Health checks
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok"))
	})
	go http.ListenAndServe(":8081", mux)

	logger.Info("router started")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("shutting down")
}
```

- [ ] **Step 3: Verify it compiles**

```bash
go build ./cmd/router/
```

- [ ] **Step 4: Commit**

```bash
git add internal/router/router.go cmd/router/ internal/store/notifications.go
git commit -m "feat: add router service with template resolution and channel fan-out"
```

---

### Task 7: Event Writer — Batch Buffer

**Files:**
- Create: `internal/eventwriter/batch.go`
- Create: `internal/eventwriter/batch_test.go`

A generic batch buffer that flushes on size or time, whichever comes first.

- [ ] **Step 1: Write batch buffer**

```go
// internal/eventwriter/batch.go
package eventwriter

import (
	"sync"
	"time"
)

type Batch[T any] struct {
	items     []T
	maxSize   int
	flushFn   func([]T)
	mu        sync.Mutex
	timer     *time.Timer
	interval  time.Duration
}

func NewBatch[T any](maxSize int, interval time.Duration, flushFn func([]T)) *Batch[T] {
	return &Batch[T]{
		maxSize:  maxSize,
		interval: interval,
		flushFn:  flushFn,
	}
}

func (b *Batch[T]) Add(item T) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.items = append(b.items, item)

	if len(b.items) >= b.maxSize {
		b.flushLocked()
		return
	}

	// Start timer on first item
	if len(b.items) == 1 && b.interval > 0 {
		b.timer = time.AfterFunc(b.interval, func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if len(b.items) > 0 {
				b.flushLocked()
			}
		})
	}
}

func (b *Batch[T]) Flush() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.flushLocked()
}

func (b *Batch[T]) flushLocked() {
	if len(b.items) == 0 {
		return
	}
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	items := b.items
	b.items = nil
	b.flushFn(items)
}
```

- [ ] **Step 2: Write unit test**

```go
// internal/eventwriter/batch_test.go
package eventwriter_test

import (
	"sync"
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/internal/eventwriter"
)

func TestBatch_FlushOnSize(t *testing.T) {
	var mu sync.Mutex
	var flushed [][]int

	batch := eventwriter.NewBatch[int](3, time.Minute, func(items []int) {
		mu.Lock()
		flushed = append(flushed, items)
		mu.Unlock()
	})

	batch.Add(1)
	batch.Add(2)
	batch.Add(3) // should trigger flush

	mu.Lock()
	if len(flushed) != 1 {
		t.Fatalf("expected 1 flush, got %d", len(flushed))
	}
	if len(flushed[0]) != 3 {
		t.Fatalf("expected 3 items, got %d", len(flushed[0]))
	}
	mu.Unlock()
}

func TestBatch_FlushOnInterval(t *testing.T) {
	var mu sync.Mutex
	var flushed [][]int

	batch := eventwriter.NewBatch[int](100, 50*time.Millisecond, func(items []int) {
		mu.Lock()
		flushed = append(flushed, items)
		mu.Unlock()
	})

	batch.Add(1)
	batch.Add(2)

	// Wait for timer flush
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if len(flushed) != 1 {
		t.Fatalf("expected 1 flush, got %d", len(flushed))
	}
	if len(flushed[0]) != 2 {
		t.Fatalf("expected 2 items, got %d", len(flushed[0]))
	}
	mu.Unlock()
}

func TestBatch_ManualFlush(t *testing.T) {
	var flushed [][]int

	batch := eventwriter.NewBatch[int](100, time.Minute, func(items []int) {
		flushed = append(flushed, items)
	})

	batch.Add(1)
	batch.Flush()

	if len(flushed) != 1 || len(flushed[0]) != 1 {
		t.Fatalf("expected 1 flush with 1 item, got %v", flushed)
	}
}
```

- [ ] **Step 3: Run tests**

```bash
go test ./internal/eventwriter/... -v
```

- [ ] **Step 4: Commit**

```bash
git add internal/eventwriter/
git commit -m "feat: add generic batch buffer with size and time flush"
```

---

### Task 8: Event Writer Service

**Files:**
- Create: `internal/eventwriter/writer.go`
- Create: `cmd/worker-events/main.go`

The Event Writer subscribes to `notification.events`, batch-inserts events, and updates notification status.

- [ ] **Step 1: Write event writer**

```go
// internal/eventwriter/writer.go
package eventwriter

import (
	"context"
	"log/slog"
	"time"

	hermenats "github.com/hermes-notifications/hermes/internal/nats"
	"github.com/hermes-notifications/hermes/internal/messaging"
	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/hermes-notifications/hermes/internal/store"
)

type Writer struct {
	nats   *messaging.Client
	store  *store.Store
	logger *slog.Logger
	batch  *Batch[*hermenats.EventMessage]
}

func New(nats *messaging.Client, st *store.Store, logger *slog.Logger) *Writer {
	w := &Writer{
		nats:   nats,
		store:  st,
		logger: logger,
	}
	w.batch = NewBatch[*hermenats.EventMessage](100, 500*time.Millisecond, w.flush)
	return w
}

func (w *Writer) Start(ctx context.Context) error {
	return w.nats.Subscribe("notification.events", "event-writer", func(data []byte) error {
		msg, err := hermenats.UnmarshalEvent(data)
		if err != nil {
			w.logger.Error("unmarshal event", "error", err)
			return nil // don't retry bad messages
		}
		w.batch.Add(msg)
		return nil
	})
}

func (w *Writer) Stop() {
	w.batch.Flush()
}

func (w *Writer) flush(events []*hermenats.EventMessage) {
	ctx := context.Background()

	// Convert to models
	dbEvents := make([]models.NotificationEvent, len(events))
	for i, e := range events {
		var metadata []byte
		if e.Metadata != nil {
			metadata, _ = json.Marshal(e.Metadata)
		}
		dbEvents[i] = models.NotificationEvent{
			NotificationID: e.NotificationID,
			Channel:        e.Channel,
			Event:          e.Event,
			Severity:       e.Severity,
			Metadata:       metadata,
		}
	}

	// Batch insert events
	if err := w.store.InsertEvents(ctx, dbEvents); err != nil {
		w.logger.Error("batch insert events", "error", err, "count", len(events))
		return
	}

	// Update notification statuses based on events
	for _, e := range events {
		status := eventToStatus(e.Event)
		if status != "" {
			if err := w.store.UpdateNotificationStatus(ctx, e.NotificationID, status, time.Now()); err != nil {
				w.logger.Error("update status", "error", err, "notification_id", e.NotificationID)
			}
		}
	}

	w.logger.Info("flushed events", "count", len(events))
}

// eventToStatus maps event names to notification statuses.
func eventToStatus(event string) models.NotificationStatus {
	switch event {
	case "notification.sent", "email.routed", "sms.routed", "inbox.routed":
		return models.StatusSent
	case "email.sent", "sms.sent", "inbox.delivered":
		return models.StatusDelivered
	default:
		return "" // no status update for this event
	}
}
```

You need to add `"encoding/json"` to the import in writer.go.

- [ ] **Step 2: Write cmd/worker-events/main.go**

```go
// cmd/worker-events/main.go
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
	"github.com/hermes-notifications/hermes/internal/database"
	"github.com/hermes-notifications/hermes/internal/eventwriter"
	"github.com/hermes-notifications/hermes/internal/messaging"
	"github.com/hermes-notifications/hermes/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg := config.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	natsClient, err := messaging.Connect(cfg.NATSUrl)
	if err != nil {
		logger.Error("nats", "error", err)
		os.Exit(1)
	}
	defer natsClient.Close()
	natsClient.SetupStreams(ctx)

	st := store.New(pool)
	w := eventwriter.New(natsClient, st, logger)

	if err := w.Start(context.Background()); err != nil {
		logger.Error("start event writer", "error", err)
		os.Exit(1)
	}

	// Health checks
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok"))
	})
	go http.ListenAndServe(":8082", mux)

	logger.Info("event writer started")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down")
	w.Stop()
}
```

- [ ] **Step 3: Verify both compile**

```bash
go build ./cmd/router/ && go build ./cmd/worker-events/
```

- [ ] **Step 4: Commit**

```bash
git add internal/eventwriter/writer.go cmd/worker-events/
git commit -m "feat: add event writer service with batch insert and status rollup"
```

---

### Task 9: Integration Test — Router + Event Writer Pipeline

**Files:**
- Create: `tests/e2e/pipeline_test.go`

End-to-end test: send a notification through the Admin service, run the Router, verify delivery messages appear on NATS and events are written.

- [ ] **Step 1: Write pipeline integration test**

This test:
1. Connects to real Postgres, NATS, Redis
2. Creates tenant, group, type, API key
3. Starts the Router and Event Writer in-process
4. Sends a notification via the Admin service handler
5. Subscribes to `delivery.email` and `delivery.inbox` to verify fan-out
6. Waits for the Event Writer to process events
7. Verifies notification status is updated to `sent`

```go
//go:build integration

package e2e_test
// ... full implementation in the test file
```

The test should use unique slugs (timestamp or UUID prefix) to avoid conflicts.

- [ ] **Step 2: Run test**

```bash
go test ./tests/e2e/... -tags=integration -v -run TestPipeline -timeout=30s
```

- [ ] **Step 3: Commit**

```bash
git add tests/e2e/pipeline_test.go
git commit -m "test: add integration test for router + event writer pipeline"
```

---

### Task 10: Tidy and Final Verification

- [ ] **Step 1: go mod tidy**

```bash
go mod tidy
```

- [ ] **Step 2: Run all unit tests**

```bash
go test ./... -v
```

- [ ] **Step 3: Run integration tests individually**

```bash
go test ./internal/store/... -tags=integration -v
go test ./internal/router/... -v
go test ./internal/eventwriter/... -v
go test ./tests/e2e/... -tags=integration -v -run TestPipeline -timeout=30s
```

- [ ] **Step 4: Verify all binaries build**

```bash
go build ./cmd/admin/ && go build ./cmd/router/ && go build ./cmd/worker-events/
```

- [ ] **Step 5: Commit tidy**

```bash
git add go.mod go.sum
git commit -m "chore: go mod tidy"
```

---

## Phase 2 Completion Criteria

- [ ] Shared NATS message types for all services
- [ ] User preferences store methods
- [ ] Event insert with batch support and status rollup with out-of-order safety
- [ ] Template resolution with Redis cache + DB fallback
- [ ] Template rendering: html/template for email, text/template for SMS/inbox
- [ ] Channel resolution: explicit override → user preferences → group defaults
- [ ] Channel filtering by type templates
- [ ] Router service subscribes to `notification.send`, fans out to `delivery.{channel}`
- [ ] Event Writer service subscribes to `notification.events`, batch-inserts, updates status
- [ ] Both services compile and have health check endpoints
- [ ] Integration test verifying the full pipeline
- [ ] All unit and integration tests pass
