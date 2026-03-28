# Subscriptions & Templates Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace notification groups with subscription categories/subscriptions, rename notification types to templates, and simplify user preferences to boolean toggles.

**Architecture:** In-place migration that drops old tables (notification_groups, notification_types, user_preferences) and creates new ones (subscription_categories, subscriptions, notification_templates, user_subscriptions). All services update together. The Notification model's group_id→category_id and type_id→template_id renames propagate through NATS messages and every service.

**Tech Stack:** Go, PostgreSQL (golang-migrate), NATS JetStream, Redis, Huma (OpenAPI), pgx v5

---

### File Structure

**New files:**
- `migrations/000011_subscriptions_templates.up.sql` — migration up
- `migrations/000011_subscriptions_templates.down.sql` — migration down
- `internal/store/postgres/categories.go` — SubscriptionCategoryRepository impl
- `internal/store/postgres/subscriptions.go` — SubscriptionRepository impl
- `internal/store/postgres/templates.go` — TemplateRepository impl (replaces types.go)
- `internal/store/postgres/user_subscriptions.go` — UserSubscriptionRepository impl (replaces preferences.go)
- `internal/admin/handler_categories.go` — subscription category CRUD handlers
- `internal/admin/handler_subscriptions.go` — subscription CRUD handlers
- `internal/admin/handler_templates.go` — template CRUD handlers (replaces handler_types.go)

**Modified files:**
- `internal/models/models.go` — replace NotificationGroup/NotificationType/UserPreference with new structs
- `internal/store/interfaces.go` — replace GroupRepository/TypeRepository/PreferenceRepository
- `internal/store/postgres/store.go` — compile-time check stays (interface changes)
- `internal/store/postgres/notifications.go` — column renames (group_id→category_id, type_id→template_id)
- `internal/store/postgres/inbox.go` — column renames in queries/scans
- `internal/nats/messages.go` — SendMessage/DeliveryMessage field renames
- `internal/cache/redis.go` — rename type config → template config methods
- `internal/admin/server.go` — AdminStore interface, route registration
- `internal/admin/handler_send.go` — type→template, group→derived from subscription
- `internal/admin/testutil_test.go` — update mock store
- `internal/dispatch/dispatch.go` — use new resolver, field renames
- `internal/dispatch/channels.go` — rewrite ChannelResolver for subscription model
- `internal/dispatch/template.go` — TemplateResolver uses new store/model
- `internal/dispatch/channels_test.go` — update for new model types
- `internal/dispatch/template_test.go` — update for new model types
- `internal/userservice/server.go` — UserStore interface rewrite
- `internal/userservice/handler_preferences.go` — rewrite for boolean subscription model
- `internal/userservice/testutil_test.go` — update mock store
- `internal/userservice/handler_preferences_test.go` — update tests
- `internal/eventwriter/writer.go` — metadata key rename (type→template)
- `cmd/dispatch/main.go` — wire new resolver dependencies

**Deleted files:**
- `internal/store/postgres/groups.go` — replaced by categories.go
- `internal/store/postgres/types.go` — replaced by templates.go
- `internal/store/postgres/preferences.go` — replaced by user_subscriptions.go
- `internal/admin/handler_groups.go` — replaced by handler_categories.go
- `internal/admin/handler_types.go` — replaced by handler_templates.go

---

### Task 1: Database Migration

**Files:**
- Create: `migrations/000011_subscriptions_templates.up.sql`
- Create: `migrations/000011_subscriptions_templates.down.sql`

- [ ] **Step 1: Write the up migration**

```sql
-- migrations/000011_subscriptions_templates.up.sql

-- 1. Create subscription_categories
CREATE TABLE subscription_categories (
    id TEXT PRIMARY KEY,
    slug TEXT NOT NULL,
    name TEXT NOT NULL,
    default_channels TEXT[] NOT NULL DEFAULT '{}',
    default_state TEXT NOT NULL DEFAULT 'on',
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_subscription_categories_slug ON subscription_categories (slug);

-- 2. Create subscriptions
CREATE TABLE subscriptions (
    id TEXT PRIMARY KEY,
    category_id TEXT NOT NULL REFERENCES subscription_categories(id),
    slug TEXT NOT NULL,
    name TEXT NOT NULL,
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_subscriptions_category_slug ON subscriptions (category_id, slug);

-- 3. Seed default categories and subscriptions
INSERT INTO subscription_categories (id, slug, name, default_channels, default_state, sort_order)
VALUES
    ('sct_default_account', 'account', 'Account', '{email,inbox}', 'required', 0),
    ('sct_default_general', 'general', 'General', '{email,inbox}', 'on', 1),
    ('sct_default_marketing', 'marketing', 'Marketing', '{email}', 'off', 2);

INSERT INTO subscriptions (id, category_id, slug, name, sort_order)
VALUES
    ('sub_default_account', 'sct_default_account', 'account', 'Account', 0),
    ('sub_default_general', 'sct_default_general', 'general', 'General', 0),
    ('sub_default_marketing', 'sct_default_marketing', 'marketing', 'Marketing', 0);

-- 4. Create notification_templates from notification_types
CREATE TABLE notification_templates (
    id TEXT PRIMARY KEY,
    subscription_id TEXT REFERENCES subscriptions(id),
    slug TEXT NOT NULL,
    name TEXT NOT NULL,
    default_channels TEXT[] NOT NULL DEFAULT '{}',
    email_subject TEXT,
    email_body TEXT,
    sms_body TEXT,
    inbox_title TEXT,
    inbox_body TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_notification_templates_slug ON notification_templates (slug);

-- Migrate existing types → templates (assigned to General subscription)
INSERT INTO notification_templates (id, subscription_id, slug, name, default_channels, email_subject, email_body, sms_body, inbox_title, inbox_body, created_at)
SELECT
    t.id,
    'sub_default_general',
    t.slug,
    t.name,
    COALESCE(g.default_channels, '{}'),
    t.email_subject,
    t.email_body,
    t.sms_body,
    t.inbox_title,
    t.inbox_body,
    t.created_at
FROM notification_types t
LEFT JOIN notification_groups g ON t.group_id = g.id;

-- 5. Create user_subscriptions
CREATE TABLE user_subscriptions (
    user_id TEXT NOT NULL REFERENCES users(id),
    subscription_id TEXT NOT NULL REFERENCES subscriptions(id),
    opted_in BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, subscription_id)
);

-- 6. Alter notifications table: rename columns
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_type_id_fkey;
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_group_id_fkey;
ALTER TABLE notifications RENAME COLUMN type_id TO template_id;
ALTER TABLE notifications RENAME COLUMN group_id TO category_id;

-- Add new FK constraints (nullable template_id, required category_id)
ALTER TABLE notifications
    ADD CONSTRAINT notifications_template_id_fkey
    FOREIGN KEY (template_id) REFERENCES notification_templates(id);
ALTER TABLE notifications
    ADD CONSTRAINT notifications_category_id_fkey
    FOREIGN KEY (category_id) REFERENCES subscription_categories(id);

-- Migrate notifications.category_id to point to new categories
-- Map old group_id values to the General category (all existing groups go to General)
UPDATE notifications SET category_id = 'sct_default_general' WHERE category_id IS NOT NULL;

-- 7. Drop old tables (order matters for FK deps)
DROP TABLE IF EXISTS user_preferences;
DROP TABLE IF EXISTS notification_types;
DROP TABLE IF EXISTS notification_groups;
```

- [ ] **Step 2: Write the down migration**

```sql
-- migrations/000011_subscriptions_templates.down.sql

-- Recreate old tables
CREATE TABLE notification_groups (
    id TEXT PRIMARY KEY,
    slug TEXT NOT NULL,
    name TEXT NOT NULL,
    default_channels TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_notification_groups_slug ON notification_groups (slug);

CREATE TABLE notification_types (
    id TEXT PRIMARY KEY,
    group_id TEXT NOT NULL REFERENCES notification_groups(id),
    slug TEXT NOT NULL,
    name TEXT NOT NULL,
    email_subject TEXT,
    email_body TEXT,
    sms_body TEXT,
    inbox_title TEXT,
    inbox_body TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_notification_types_slug ON notification_types (slug);

CREATE TABLE user_preferences (
    user_id TEXT NOT NULL REFERENCES users(id),
    group_id TEXT NOT NULL REFERENCES notification_groups(id),
    channels TEXT[],
    PRIMARY KEY (user_id, group_id)
);

-- Revert notifications column renames
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_template_id_fkey;
ALTER TABLE notifications DROP CONSTRAINT IF EXISTS notifications_category_id_fkey;
ALTER TABLE notifications RENAME COLUMN template_id TO type_id;
ALTER TABLE notifications RENAME COLUMN category_id TO group_id;

-- Drop new tables
DROP TABLE IF EXISTS user_subscriptions;
DROP TABLE IF EXISTS notification_templates;
DROP TABLE IF EXISTS subscriptions;
DROP TABLE IF EXISTS subscription_categories;
```

- [ ] **Step 3: Verify migration syntax**

Run: `make migrate` (requires `make infra-up` first)

Expected: Migration 000011 applies successfully.

- [ ] **Step 4: Commit**

```bash
git add migrations/000011_subscriptions_templates.up.sql migrations/000011_subscriptions_templates.down.sql
git commit -m "feat: add migration for subscriptions and templates"
```

---

### Task 2: Models

**Files:**
- Modify: `internal/models/models.go`

- [ ] **Step 1: Replace NotificationGroup, NotificationType, UserPreference with new structs**

Replace `NotificationGroup` (lines 31-37) with:

```go
type SubscriptionCategory struct {
	ID              string    `json:"id"`
	Slug            string    `json:"slug"`
	Name            string    `json:"name"`
	DefaultChannels []string  `json:"default_channels"`
	DefaultState    string    `json:"default_state"`
	SortOrder       int       `json:"sort_order"`
	CreatedAt       time.Time `json:"created_at"`
}

type Subscription struct {
	ID         string    `json:"id"`
	CategoryID string    `json:"category_id"`
	Slug       string    `json:"slug"`
	Name       string    `json:"name"`
	SortOrder  int       `json:"sort_order"`
	CreatedAt  time.Time `json:"created_at"`
}
```

Replace `NotificationType` (lines 39-50) with:

```go
type NotificationTemplate struct {
	ID              string    `json:"id"`
	SubscriptionID  *string   `json:"subscription_id,omitempty"`
	Slug            string    `json:"slug"`
	Name            string    `json:"name"`
	DefaultChannels []string  `json:"default_channels"`
	EmailSubject    *string   `json:"email_subject,omitempty"`
	EmailBody       *string   `json:"email_body,omitempty"`
	SMSBody         *string   `json:"sms_body,omitempty"`
	InboxTitle      *string   `json:"inbox_title,omitempty"`
	InboxBody       *string   `json:"inbox_body,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}
```

Replace `UserPreference` (lines 83-87) with:

```go
type UserSubscription struct {
	UserID         string    `json:"user_id"`
	SubscriptionID string    `json:"subscription_id"`
	OptedIn        bool      `json:"opted_in"`
	CreatedAt      time.Time `json:"created_at"`
}
```

Update `Notification` (lines 52-71): rename `TypeID` → `TemplateID` and `GroupID` → `CategoryID`:

```go
type Notification struct {
	ID             string             `json:"id"`
	TenantID       string             `json:"tenant_id"`
	UserID         string             `json:"user_id"`
	TemplateID     *string            `json:"template_id,omitempty"`
	CategoryID     string             `json:"category_id"`
	Title          string             `json:"title"`
	Body           string             `json:"body"`
	ActionURL      *string            `json:"action_url,omitempty"`
	ActionLabel    *string            `json:"action_label,omitempty"`
	IdempotencyKey *string            `json:"idempotency_key,omitempty"`
	Channels       []string           `json:"channels"`
	Status         NotificationStatus `json:"status"`
	CreatedAt      time.Time          `json:"created_at"`
	SentAt         *time.Time         `json:"sent_at,omitempty"`
	DeliveredAt    *time.Time         `json:"delivered_at,omitempty"`
	ReadAt         *time.Time         `json:"read_at,omitempty"`
	ArchivedAt     *time.Time         `json:"archived_at,omitempty"`
	DeletedAt      *time.Time         `json:"deleted_at,omitempty"`
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/models/...`

Expected: Compiles (models are self-contained). Other packages will break until updated.

- [ ] **Step 3: Commit**

```bash
git add internal/models/models.go
git commit -m "feat: replace group/type/preference models with subscription/template models"
```

---

### Task 3: Store Interfaces

**Files:**
- Modify: `internal/store/interfaces.go`

- [ ] **Step 1: Replace GroupRepository, TypeRepository, PreferenceRepository**

Replace `GroupRepository` (lines 16-23) with:

```go
// SubscriptionCategoryRepository defines operations for managing subscription categories.
type SubscriptionCategoryRepository interface {
	CreateCategory(ctx context.Context, slug, name string, defaultChannels []string, defaultState string, sortOrder int) (*models.SubscriptionCategory, error)
	GetCategoryByID(ctx context.Context, id string) (*models.SubscriptionCategory, error)
	GetCategoryBySlug(ctx context.Context, slug string) (*models.SubscriptionCategory, error)
	ListCategories(ctx context.Context) ([]models.SubscriptionCategory, error)
	UpdateCategory(ctx context.Context, id, name string, defaultChannels []string, defaultState string, sortOrder int) (*models.SubscriptionCategory, error)
	DeleteCategory(ctx context.Context, id string) error
}

// SubscriptionRepository defines operations for managing subscriptions.
type SubscriptionRepository interface {
	CreateSubscription(ctx context.Context, categoryID, slug, name string, sortOrder int) (*models.Subscription, error)
	GetSubscriptionByID(ctx context.Context, id string) (*models.Subscription, error)
	ListSubscriptionsByCategory(ctx context.Context, categoryID string) ([]models.Subscription, error)
	UpdateSubscription(ctx context.Context, id, name string, sortOrder int) (*models.Subscription, error)
	DeleteSubscription(ctx context.Context, id string) error
}
```

Replace `TypeRepository` (lines 25-33) with:

```go
// TemplateRepository defines operations for managing notification templates.
type TemplateRepository interface {
	CreateTemplate(ctx context.Context, input *models.NotificationTemplate) (*models.NotificationTemplate, error)
	GetTemplateByID(ctx context.Context, id string) (*models.NotificationTemplate, error)
	GetTemplateBySlug(ctx context.Context, slug string) (*models.NotificationTemplate, error)
	ListTemplates(ctx context.Context) ([]models.NotificationTemplate, error)
	UpdateTemplate(ctx context.Context, input *models.NotificationTemplate) (*models.NotificationTemplate, error)
	DeleteTemplate(ctx context.Context, id string) error
}
```

Replace `PreferenceRepository` (lines 42-48) with:

```go
// UserSubscriptionRepository defines operations for managing user subscription preferences.
type UserSubscriptionRepository interface {
	GetUserSubscription(ctx context.Context, userID, subscriptionID string) (*models.UserSubscription, error)
	GetUserSubscriptions(ctx context.Context, userID string) ([]models.UserSubscription, error)
	SetUserSubscription(ctx context.Context, userID, subscriptionID string, optedIn bool) (*models.UserSubscription, error)
	DeleteUserSubscription(ctx context.Context, userID, subscriptionID string) error
}
```

Update the `Notification` column references in `NotificationRepository` — no changes needed (it passes `*models.Notification` which now has the renamed fields).

Update the composite `Repository` interface (lines 88-99):

```go
type Repository interface {
	TenantRepository
	SubscriptionCategoryRepository
	SubscriptionRepository
	TemplateRepository
	UserRepository
	UserSubscriptionRepository
	NotificationRepository
	EventRepository
	InboxRepository
	AuthRepository
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/store/interfaces.go
git commit -m "feat: replace group/type/preference store interfaces with subscription/template"
```

---

### Task 4: Store Implementations — Categories and Subscriptions

**Files:**
- Create: `internal/store/postgres/categories.go`
- Create: `internal/store/postgres/subscriptions.go`
- Delete: `internal/store/postgres/groups.go`

- [ ] **Step 1: Write categories.go**

```go
package postgres

import (
	"context"
	"fmt"

	id "github.com/hermes-notifications/hermes/internal/id/v2"
	"github.com/hermes-notifications/hermes/internal/models"
)

var categoryIDGen = id.NewGenerator(id.Config{Prefix: "sct", TimeBits: 48, RandBits: 80})

func (s *Store) CreateCategory(ctx context.Context, slug, name string, defaultChannels []string, defaultState string, sortOrder int) (*models.SubscriptionCategory, error) {
	c := &models.SubscriptionCategory{
		ID:              categoryIDGen.New(),
		Slug:            slug,
		Name:            name,
		DefaultChannels: defaultChannels,
		DefaultState:    defaultState,
		SortOrder:       sortOrder,
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO subscription_categories (id, slug, name, default_channels, default_state, sort_order)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING created_at`,
		c.ID, c.Slug, c.Name, c.DefaultChannels, c.DefaultState, c.SortOrder,
	).Scan(&c.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create category: %w", err)
	}
	return c, nil
}

func (s *Store) GetCategoryByID(ctx context.Context, id string) (*models.SubscriptionCategory, error) {
	c := &models.SubscriptionCategory{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, slug, name, default_channels, default_state, sort_order, created_at
		 FROM subscription_categories WHERE id = $1`, id,
	).Scan(&c.ID, &c.Slug, &c.Name, &c.DefaultChannels, &c.DefaultState, &c.SortOrder, &c.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get category by id: %w", err)
	}
	return c, nil
}

func (s *Store) GetCategoryBySlug(ctx context.Context, slug string) (*models.SubscriptionCategory, error) {
	c := &models.SubscriptionCategory{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, slug, name, default_channels, default_state, sort_order, created_at
		 FROM subscription_categories WHERE slug = $1`, slug,
	).Scan(&c.ID, &c.Slug, &c.Name, &c.DefaultChannels, &c.DefaultState, &c.SortOrder, &c.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get category by slug: %w", err)
	}
	return c, nil
}

func (s *Store) ListCategories(ctx context.Context) ([]models.SubscriptionCategory, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, slug, name, default_channels, default_state, sort_order, created_at
		 FROM subscription_categories ORDER BY sort_order, created_at`)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	defer rows.Close()

	var categories []models.SubscriptionCategory
	for rows.Next() {
		var c models.SubscriptionCategory
		if err := rows.Scan(&c.ID, &c.Slug, &c.Name, &c.DefaultChannels, &c.DefaultState, &c.SortOrder, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan category: %w", err)
		}
		categories = append(categories, c)
	}
	return categories, rows.Err()
}

func (s *Store) UpdateCategory(ctx context.Context, id, name string, defaultChannels []string, defaultState string, sortOrder int) (*models.SubscriptionCategory, error) {
	c := &models.SubscriptionCategory{}
	err := s.pool.QueryRow(ctx,
		`UPDATE subscription_categories SET name = $2, default_channels = $3, default_state = $4, sort_order = $5
		 WHERE id = $1
		 RETURNING id, slug, name, default_channels, default_state, sort_order, created_at`,
		id, name, defaultChannels, defaultState, sortOrder,
	).Scan(&c.ID, &c.Slug, &c.Name, &c.DefaultChannels, &c.DefaultState, &c.SortOrder, &c.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("update category: %w", err)
	}
	return c, nil
}

func (s *Store) DeleteCategory(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM subscription_categories WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete category: %w", err)
	}
	return nil
}
```

- [ ] **Step 2: Write subscriptions.go**

```go
package postgres

import (
	"context"
	"fmt"

	id "github.com/hermes-notifications/hermes/internal/id/v2"
	"github.com/hermes-notifications/hermes/internal/models"
)

var subscriptionIDGen = id.NewGenerator(id.Config{Prefix: "sub", TimeBits: 48, RandBits: 80})

func (s *Store) CreateSubscription(ctx context.Context, categoryID, slug, name string, sortOrder int) (*models.Subscription, error) {
	sub := &models.Subscription{
		ID:         subscriptionIDGen.New(),
		CategoryID: categoryID,
		Slug:       slug,
		Name:       name,
		SortOrder:  sortOrder,
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO subscriptions (id, category_id, slug, name, sort_order)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING created_at`,
		sub.ID, sub.CategoryID, sub.Slug, sub.Name, sub.SortOrder,
	).Scan(&sub.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create subscription: %w", err)
	}
	return sub, nil
}

func (s *Store) GetSubscriptionByID(ctx context.Context, id string) (*models.Subscription, error) {
	sub := &models.Subscription{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, category_id, slug, name, sort_order, created_at
		 FROM subscriptions WHERE id = $1`, id,
	).Scan(&sub.ID, &sub.CategoryID, &sub.Slug, &sub.Name, &sub.SortOrder, &sub.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get subscription by id: %w", err)
	}
	return sub, nil
}

func (s *Store) ListSubscriptionsByCategory(ctx context.Context, categoryID string) ([]models.Subscription, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, category_id, slug, name, sort_order, created_at
		 FROM subscriptions WHERE category_id = $1 ORDER BY sort_order, created_at`, categoryID,
	)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}
	defer rows.Close()

	var subs []models.Subscription
	for rows.Next() {
		var sub models.Subscription
		if err := rows.Scan(&sub.ID, &sub.CategoryID, &sub.Slug, &sub.Name, &sub.SortOrder, &sub.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		subs = append(subs, sub)
	}
	return subs, rows.Err()
}

func (s *Store) UpdateSubscription(ctx context.Context, id, name string, sortOrder int) (*models.Subscription, error) {
	sub := &models.Subscription{}
	err := s.pool.QueryRow(ctx,
		`UPDATE subscriptions SET name = $2, sort_order = $3
		 WHERE id = $1
		 RETURNING id, category_id, slug, name, sort_order, created_at`,
		id, name, sortOrder,
	).Scan(&sub.ID, &sub.CategoryID, &sub.Slug, &sub.Name, &sub.SortOrder, &sub.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("update subscription: %w", err)
	}
	return sub, nil
}

func (s *Store) DeleteSubscription(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM subscriptions WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete subscription: %w", err)
	}
	return nil
}
```

- [ ] **Step 3: Delete groups.go**

```bash
rm internal/store/postgres/groups.go
```

- [ ] **Step 4: Commit**

```bash
git add internal/store/postgres/categories.go internal/store/postgres/subscriptions.go
git rm internal/store/postgres/groups.go
git commit -m "feat: add category and subscription store implementations, remove groups"
```

---

### Task 5: Store Implementations — Templates and User Subscriptions

**Files:**
- Create: `internal/store/postgres/templates.go`
- Create: `internal/store/postgres/user_subscriptions.go`
- Delete: `internal/store/postgres/types.go`
- Delete: `internal/store/postgres/preferences.go`

- [ ] **Step 1: Write templates.go**

```go
package postgres

import (
	"context"
	"fmt"

	id "github.com/hermes-notifications/hermes/internal/id/v2"
	"github.com/hermes-notifications/hermes/internal/models"
)

var templateIDGen = id.NewGenerator(id.Config{Prefix: "ntpl", TimeBits: 48, RandBits: 80})

func (s *Store) CreateTemplate(ctx context.Context, input *models.NotificationTemplate) (*models.NotificationTemplate, error) {
	input.ID = templateIDGen.New()
	err := s.pool.QueryRow(ctx,
		`INSERT INTO notification_templates (id, subscription_id, slug, name, default_channels, email_subject, email_body, sms_body, inbox_title, inbox_body)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 RETURNING created_at`,
		input.ID, input.SubscriptionID, input.Slug, input.Name, input.DefaultChannels,
		input.EmailSubject, input.EmailBody, input.SMSBody,
		input.InboxTitle, input.InboxBody,
	).Scan(&input.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create template: %w", err)
	}
	return input, nil
}

func (s *Store) GetTemplateByID(ctx context.Context, id string) (*models.NotificationTemplate, error) {
	t := &models.NotificationTemplate{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, subscription_id, slug, name, default_channels, email_subject, email_body, sms_body, inbox_title, inbox_body, created_at
		 FROM notification_templates WHERE id = $1`, id,
	).Scan(&t.ID, &t.SubscriptionID, &t.Slug, &t.Name, &t.DefaultChannels,
		&t.EmailSubject, &t.EmailBody, &t.SMSBody,
		&t.InboxTitle, &t.InboxBody, &t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get template by id: %w", err)
	}
	return t, nil
}

func (s *Store) GetTemplateBySlug(ctx context.Context, slug string) (*models.NotificationTemplate, error) {
	t := &models.NotificationTemplate{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, subscription_id, slug, name, default_channels, email_subject, email_body, sms_body, inbox_title, inbox_body, created_at
		 FROM notification_templates WHERE slug = $1`, slug,
	).Scan(&t.ID, &t.SubscriptionID, &t.Slug, &t.Name, &t.DefaultChannels,
		&t.EmailSubject, &t.EmailBody, &t.SMSBody,
		&t.InboxTitle, &t.InboxBody, &t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get template by slug: %w", err)
	}
	return t, nil
}

func (s *Store) ListTemplates(ctx context.Context) ([]models.NotificationTemplate, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, subscription_id, slug, name, default_channels, email_subject, email_body, sms_body, inbox_title, inbox_body, created_at
		 FROM notification_templates ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	defer rows.Close()

	var templates []models.NotificationTemplate
	for rows.Next() {
		var t models.NotificationTemplate
		if err := rows.Scan(&t.ID, &t.SubscriptionID, &t.Slug, &t.Name, &t.DefaultChannels,
			&t.EmailSubject, &t.EmailBody, &t.SMSBody,
			&t.InboxTitle, &t.InboxBody, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan template: %w", err)
		}
		templates = append(templates, t)
	}
	return templates, rows.Err()
}

func (s *Store) UpdateTemplate(ctx context.Context, input *models.NotificationTemplate) (*models.NotificationTemplate, error) {
	err := s.pool.QueryRow(ctx,
		`UPDATE notification_templates
		 SET name = $2, subscription_id = $3, default_channels = $4, email_subject = $5, email_body = $6, sms_body = $7, inbox_title = $8, inbox_body = $9
		 WHERE id = $1
		 RETURNING id, subscription_id, slug, name, default_channels, email_subject, email_body, sms_body, inbox_title, inbox_body, created_at`,
		input.ID, input.Name, input.SubscriptionID, input.DefaultChannels,
		input.EmailSubject, input.EmailBody,
		input.SMSBody, input.InboxTitle, input.InboxBody,
	).Scan(&input.ID, &input.SubscriptionID, &input.Slug, &input.Name, &input.DefaultChannels,
		&input.EmailSubject, &input.EmailBody, &input.SMSBody,
		&input.InboxTitle, &input.InboxBody, &input.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("update template: %w", err)
	}
	return input, nil
}

func (s *Store) DeleteTemplate(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM notification_templates WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete template: %w", err)
	}
	return nil
}
```

- [ ] **Step 2: Write user_subscriptions.go**

```go
package postgres

import (
	"context"
	"fmt"

	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/jackc/pgx/v5"
)

func (s *Store) GetUserSubscription(ctx context.Context, userID, subscriptionID string) (*models.UserSubscription, error) {
	us := &models.UserSubscription{}
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, subscription_id, opted_in, created_at
		 FROM user_subscriptions
		 WHERE user_id = $1 AND subscription_id = $2`,
		userID, subscriptionID,
	).Scan(&us.UserID, &us.SubscriptionID, &us.OptedIn, &us.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get user subscription: %w", err)
	}
	return us, nil
}

func (s *Store) GetUserSubscriptions(ctx context.Context, userID string) ([]models.UserSubscription, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT user_id, subscription_id, opted_in, created_at
		 FROM user_subscriptions WHERE user_id = $1`, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("get user subscriptions: %w", err)
	}
	defer rows.Close()

	var subs []models.UserSubscription
	for rows.Next() {
		var us models.UserSubscription
		if err := rows.Scan(&us.UserID, &us.SubscriptionID, &us.OptedIn, &us.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan user subscription: %w", err)
		}
		subs = append(subs, us)
	}
	return subs, rows.Err()
}

func (s *Store) SetUserSubscription(ctx context.Context, userID, subscriptionID string, optedIn bool) (*models.UserSubscription, error) {
	us := &models.UserSubscription{}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO user_subscriptions (user_id, subscription_id, opted_in)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (user_id, subscription_id) DO UPDATE SET opted_in = EXCLUDED.opted_in
		 RETURNING user_id, subscription_id, opted_in, created_at`,
		userID, subscriptionID, optedIn,
	).Scan(&us.UserID, &us.SubscriptionID, &us.OptedIn, &us.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("set user subscription: %w", err)
	}
	return us, nil
}

func (s *Store) DeleteUserSubscription(ctx context.Context, userID, subscriptionID string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM user_subscriptions WHERE user_id = $1 AND subscription_id = $2`,
		userID, subscriptionID,
	)
	if err != nil {
		return fmt.Errorf("delete user subscription: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delete user subscription: %w", pgx.ErrNoRows)
	}
	return nil
}
```

- [ ] **Step 3: Delete old files**

```bash
rm internal/store/postgres/types.go internal/store/postgres/preferences.go
```

- [ ] **Step 4: Commit**

```bash
git add internal/store/postgres/templates.go internal/store/postgres/user_subscriptions.go
git rm internal/store/postgres/types.go internal/store/postgres/preferences.go
git commit -m "feat: add template and user subscription store implementations, remove types/preferences"
```

---

### Task 6: Store — Update Notifications and Inbox Queries

**Files:**
- Modify: `internal/store/postgres/notifications.go`
- Modify: `internal/store/postgres/inbox.go`

- [ ] **Step 1: Update notifications.go column references**

In `CreateNotification` (line 13), change `type_id, group_id` to `template_id, category_id`:

```go
func (s *Store) CreateNotification(ctx context.Context, n *models.Notification) (*models.Notification, error) {
	err := s.pool.QueryRow(ctx,
		`INSERT INTO notifications
			(id, tenant_id, user_id, template_id, category_id, title, body, action_url, action_label, idempotency_key, channels, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		 RETURNING created_at`,
		n.ID, n.TenantID, n.UserID, n.TemplateID, n.CategoryID,
		n.Title, n.Body, n.ActionURL, n.ActionLabel,
		n.IdempotencyKey, n.Channels, n.Status,
	).Scan(&n.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create notification: %w", err)
	}
	return n, nil
}
```

In `GetNotificationByID` (lines 29-38), change `type_id, group_id` to `template_id, category_id` and `.TypeID, .GroupID` to `.TemplateID, .CategoryID`:

```go
func (s *Store) GetNotificationByID(ctx context.Context, notifID string) (*models.Notification, error) {
	n := &models.Notification{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, user_id, template_id, category_id, title, body,
		        action_url, action_label, idempotency_key, channels, status,
		        created_at, sent_at, delivered_at, read_at, archived_at, deleted_at
		 FROM notifications WHERE id = $1`, notifID,
	).Scan(
		&n.ID, &n.TenantID, &n.UserID, &n.TemplateID, &n.CategoryID,
		&n.Title, &n.Body, &n.ActionURL, &n.ActionLabel,
		&n.IdempotencyKey, &n.Channels, &n.Status,
		&n.CreatedAt, &n.SentAt, &n.DeliveredAt, &n.ReadAt, &n.ArchivedAt, &n.DeletedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get notification by id: %w", err)
	}
	return n, nil
}
```

Same pattern for `GetNotificationByIdempotencyKey` — change column names and struct field references from `type_id/group_id` to `template_id/category_id` and `.TypeID/.GroupID` to `.TemplateID/.CategoryID`.

- [ ] **Step 2: Update inbox.go column references**

In `ListInbox` (line 51-52), change the SELECT to use `template_id, category_id`:

```go
query := fmt.Sprintf(`SELECT id, tenant_id, user_id, template_id, category_id, title, body,
        action_url, action_label, idempotency_key, channels, status,
        created_at, sent_at, delivered_at, read_at, archived_at, deleted_at
 FROM notifications
 WHERE user_id = $1 AND %s AND deleted_at IS NULL`, archiveFilter)
```

And the Scan (lines 83-88):

```go
if err := rows.Scan(
	&n.ID, &n.TenantID, &n.UserID, &n.TemplateID, &n.CategoryID,
	&n.Title, &n.Body, &n.ActionURL, &n.ActionLabel,
	&n.IdempotencyKey, &n.Channels, &n.Status,
	&n.CreatedAt, &n.SentAt, &n.DeliveredAt, &n.ReadAt, &n.ArchivedAt, &n.DeletedAt,
); err != nil {
```

- [ ] **Step 3: Commit**

```bash
git add internal/store/postgres/notifications.go internal/store/postgres/inbox.go
git commit -m "feat: rename type_id/group_id columns to template_id/category_id in store queries"
```

---

### Task 7: NATS Messages

**Files:**
- Modify: `internal/nats/messages.go`

- [ ] **Step 1: Update SendMessage, MessageMetadata, DeliveryMessage**

Replace `GroupID` with `CategoryID` and `SubscriptionID` in `SendMessage`. Replace `MessageMetadata` fields:

```go
type SendMessage struct {
	NotificationID string          `json:"notification_id"`
	TenantID       string          `json:"tenant_id"`
	UserID         string          `json:"user_id"`
	CategoryID     string          `json:"category_id"`
	SubscriptionID string          `json:"subscription_id,omitempty"`
	Content        MessageContent  `json:"content"`
	Metadata       MessageMetadata `json:"metadata"`
	Data           map[string]any  `json:"data,omitempty"`
	Channels       []string        `json:"channels,omitempty"`
	Attempt        int             `json:"attempt"`
}

type MessageMetadata struct {
	Template string `json:"template,omitempty"`
}
```

No changes to `DeliveryMessage` struct fields except `Metadata` now has the updated `MessageMetadata` type (which already changed above).

- [ ] **Step 2: Commit**

```bash
git add internal/nats/messages.go
git commit -m "feat: update NATS messages for subscription/template model"
```

---

### Task 8: Cache Layer

**Files:**
- Modify: `internal/cache/redis.go`

- [ ] **Step 1: Rename type config methods to template config**

Rename the three methods and their cache key prefix from `type:` to `template:`:

```go
func (c *Client) GetTemplateConfig(ctx context.Context, slug string) ([]byte, error) {
	val, err := c.rdb.Get(ctx, "template:"+slug).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get template config: %w", err)
	}
	return val, nil
}

func (c *Client) SetTemplateConfig(ctx context.Context, slug string, data []byte, ttl time.Duration) error {
	return c.rdb.Set(ctx, "template:"+slug, data, ttl).Err()
}

func (c *Client) InvalidateTemplateConfig(ctx context.Context, slug string) error {
	return c.rdb.Del(ctx, "template:"+slug).Err()
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/cache/redis.go
git commit -m "feat: rename type config cache methods to template config"
```

---

### Task 9: Dispatch Service — Template Resolver

**Files:**
- Modify: `internal/dispatch/template.go`

- [ ] **Step 1: Update TemplateResolver to use new store interface and model**

```go
type TemplateResolver struct {
	store store.TemplateRepository
	cache *cache.Client
}

func NewTemplateResolver(store store.TemplateRepository, cache *cache.Client) *TemplateResolver {
	return &TemplateResolver{store: store, cache: cache}
}

func (tr *TemplateResolver) Resolve(ctx context.Context, slug string) (*models.NotificationTemplate, error) {
	if tr.cache != nil {
		data, err := tr.cache.GetTemplateConfig(ctx, slug)
		if err == nil && data != nil {
			var nt models.NotificationTemplate
			if err := json.Unmarshal(data, &nt); err == nil {
				return &nt, nil
			}
		}
	}
	nt, err := tr.store.GetTemplateBySlug(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("resolve template %s: %w", slug, err)
	}
	if tr.cache != nil {
		if data, err := json.Marshal(nt); err == nil {
			tr.cache.SetTemplateConfig(ctx, slug, data, 5*time.Minute)
		}
	}
	return nt, nil
}
```

Update `RenderTemplates` to accept `*models.NotificationTemplate`:

```go
func RenderTemplates(nt *models.NotificationTemplate, data map[string]any) (*RenderedContent, error) {
```

The body stays identical — it accesses the same field names (`EmailSubject`, `EmailBody`, `SMSBody`, `InboxTitle`, `InboxBody`).

- [ ] **Step 2: Commit**

```bash
git add internal/dispatch/template.go
git commit -m "feat: update template resolver for new model and cache methods"
```

---

### Task 10: Dispatch Service — Channel Resolver Rewrite

**Files:**
- Modify: `internal/dispatch/channels.go`

- [ ] **Step 1: Rewrite ChannelResolver for the subscription model**

```go
package dispatch

import (
	"context"

	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/hermes-notifications/hermes/internal/store"
)

// channelStore composes the repository interfaces needed for channel resolution.
type channelStore interface {
	store.UserSubscriptionRepository
	store.SubscriptionRepository
	store.SubscriptionCategoryRepository
}

type ChannelResolver struct {
	store channelStore
}

func NewChannelResolver(store channelStore) *ChannelResolver {
	return &ChannelResolver{store: store}
}

// ResolveChannels determines target channels for a template-based send.
// For templates with a subscription: required check → user pref → category default.
// For standalone templates: explicit channels → template default_channels.
func (cr *ChannelResolver) ResolveChannels(ctx context.Context, explicitChannels []string, userID string, template *models.NotificationTemplate) ([]string, error) {
	// Standalone template (no subscription)
	if template.SubscriptionID == nil {
		if len(explicitChannels) > 0 {
			return explicitChannels, nil
		}
		if len(template.DefaultChannels) > 0 {
			return template.DefaultChannels, nil
		}
		return nil, nil
	}

	// Template with subscription — resolve category
	sub, err := cr.store.GetSubscriptionByID(ctx, *template.SubscriptionID)
	if err != nil {
		return nil, err
	}
	cat, err := cr.store.GetCategoryByID(ctx, sub.CategoryID)
	if err != nil {
		return nil, err
	}

	// Required category: always send
	if cat.DefaultState == "required" {
		if len(explicitChannels) > 0 {
			return explicitChannels, nil
		}
		return cat.DefaultChannels, nil
	}

	// Check explicit channel override — but respect opt-out
	channels := cat.DefaultChannels
	if len(explicitChannels) > 0 {
		channels = explicitChannels
	}

	// Check user subscription preference
	us, err := cr.store.GetUserSubscription(ctx, userID, sub.ID)
	if err == nil && us != nil {
		if !us.OptedIn {
			return nil, nil // user opted out
		}
		return channels, nil
	}

	// No explicit user preference — use category default state
	if cat.DefaultState == "off" {
		return nil, nil // default opt-out
	}

	// default state is "on"
	return channels, nil
}

// FilterChannelsForTemplate filters channels to only those with templates defined.
// For direct sends (nil template), all channels pass through.
func FilterChannelsForTemplate(channels []string, nt *models.NotificationTemplate) []string {
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

- [ ] **Step 2: Commit**

```bash
git add internal/dispatch/channels.go
git commit -m "feat: rewrite channel resolver for subscription-based preference model"
```

---

### Task 11: Dispatch Service — Main Handler

**Files:**
- Modify: `internal/dispatch/dispatch.go`

- [ ] **Step 1: Update Dispatch struct and constructor**

```go
type Dispatch struct {
	nats             *messaging.Client
	store            store.NotificationRepository
	users            store.UserRepository
	templateResolver *TemplateResolver
	channelResolver  *ChannelResolver
	logger           *slog.Logger
}

func NewDispatch(nats *messaging.Client, store store.NotificationRepository, users store.UserRepository, templateResolver *TemplateResolver, channelResolver *ChannelResolver, logger *slog.Logger) *Dispatch {
	return &Dispatch{
		nats:             nats,
		store:            store,
		users:            users,
		templateResolver: templateResolver,
		channelResolver:  channelResolver,
		logger:           logger,
	}
}
```

No changes to the struct or constructor — they're already correct.

- [ ] **Step 2: Update handleSend to use new models and resolver signature**

Key changes in `handleSend`:
- `msg.Metadata.Type` → `msg.Metadata.Template`
- `*models.NotificationType` → `*models.NotificationTemplate`
- `d.channelResolver.ResolveChannels(ctx, msg.Channels, msg.UserID, msg.GroupID)` → `d.channelResolver.ResolveChannels(ctx, msg.Channels, msg.UserID, nt)` for template-based sends
- For direct sends (no template), handle channel resolution inline
- `FilterChannelsForType` → `FilterChannelsForTemplate`
- `msg.Metadata` passed through to `DeliveryMessage` (already uses updated struct)

```go
func (d *Dispatch) handleSend(ctx context.Context, data []byte) error {
	msg, err := hermenats.UnmarshalSend(data)
	if err != nil {
		d.logger.Error("unmarshal send message", "error", err)
		return fmt.Errorf("unmarshal send: %w", err)
	}

	log := d.logger.With("notification_id", msg.NotificationID)

	var nt *models.NotificationTemplate
	var rendered *RenderedContent
	content := msg.Content

	if msg.Metadata.Template != "" {
		// Template-based send: resolve template and render
		nt, err = d.templateResolver.Resolve(ctx, msg.Metadata.Template)
		if err != nil {
			log.Error("resolve template", "error", err, "template", msg.Metadata.Template)
			d.publishEvent(ctx, msg.NotificationID, "", "routing.failed", "error", map[string]any{
				"error": err.Error(),
			})
			return fmt.Errorf("resolve template: %w", err)
		}

		rendered, err = RenderTemplates(nt, msg.Data)
		if err != nil {
			log.Error("render templates", "error", err)
			d.publishEvent(ctx, msg.NotificationID, "", "routing.failed", "error", map[string]any{
				"error": err.Error(),
			})
			return fmt.Errorf("render templates: %w", err)
		}
	} else {
		// Direct send: optionally render content with data
		title, body, err := RenderDirectContent(content.Title, content.Body, msg.Data)
		if err != nil {
			log.Error("render direct content", "error", err)
			d.publishEvent(ctx, msg.NotificationID, "", "routing.failed", "error", map[string]any{
				"error": err.Error(),
			})
			return fmt.Errorf("render direct content: %w", err)
		}
		content.Title = title
		content.Body = body
	}

	// Resolve channels
	var channels []string
	if nt != nil {
		channels, err = d.channelResolver.ResolveChannels(ctx, msg.Channels, msg.UserID, nt)
	} else {
		// Direct send: use explicit channels
		channels = msg.Channels
	}
	if err != nil {
		log.Error("resolve channels", "error", err)
		d.publishEvent(ctx, msg.NotificationID, "", "routing.failed", "error", map[string]any{
			"error": err.Error(),
		})
		return fmt.Errorf("resolve channels: %w", err)
	}

	// Filter channels by template content
	channels = FilterChannelsForTemplate(channels, nt)

	if len(channels) == 0 {
		log.Warn("no channels after filtering")
		d.publishEvent(ctx, msg.NotificationID, "", "routing.no_channels", "warn", nil)
		return nil
	}

	// Resolve user contact info for recipient fields
	user, err := d.users.GetUserByID(ctx, msg.UserID)
	if err != nil {
		log.Error("resolve user", "error", err)
		return fmt.Errorf("resolve user: %w", err)
	}

	recipient := hermenats.Recipient{}
	if user.Email != nil {
		recipient.Email = *user.Email
	}
	if user.Phone != nil {
		recipient.Phone = *user.Phone
	}

	// Filter channels that require contact info the user doesn't have
	var filteredChannels []string
	for _, ch := range channels {
		switch ch {
		case "email":
			if recipient.Email == "" {
				log.Warn("skipping email channel: user has no email", "user_id", msg.UserID)
				d.publishEvent(ctx, msg.NotificationID, ch, "routing.no_contact", "warn", map[string]any{
					"reason": "user has no email address",
				})
				continue
			}
		case "sms":
			if recipient.Phone == "" {
				log.Warn("skipping sms channel: user has no phone", "user_id", msg.UserID)
				d.publishEvent(ctx, msg.NotificationID, ch, "routing.no_contact", "warn", map[string]any{
					"reason": "user has no phone number",
				})
				continue
			}
		}
		filteredChannels = append(filteredChannels, ch)
	}
	channels = filteredChannels

	if len(channels) == 0 {
		log.Warn("no channels after contact filtering")
		d.publishEvent(ctx, msg.NotificationID, "", "routing.no_channels", "warn", nil)
		return nil
	}

	// Update notification channels in DB
	if err := d.store.UpdateNotificationChannels(ctx, msg.NotificationID, channels); err != nil {
		log.Error("update notification channels", "error", err)
		return fmt.Errorf("update notification channels: %w", err)
	}

	// Fan out to delivery channels
	for _, ch := range channels {
		deliveryContent := contentForChannel(ch, content, rendered)

		dm := &hermenats.DeliveryMessage{
			NotificationID: msg.NotificationID,
			TenantID:       msg.TenantID,
			UserID:         msg.UserID,
			Channel:        ch,
			Content:        deliveryContent,
			Metadata:       msg.Metadata,
			Recipient:      recipient,
			Attempt:        msg.Attempt,
		}

		dmBytes, err := dm.Marshal()
		if err != nil {
			log.Error("marshal delivery message", "error", err, "channel", ch)
			continue
		}

		subject := "delivery." + ch
		if err := d.nats.Publish(ctx, subject, dmBytes); err != nil {
			log.Error("publish delivery", "error", err, "channel", ch)
			d.publishEvent(ctx, msg.NotificationID, ch, "delivery.publish_failed", "error", map[string]any{
				"error": err.Error(),
			})
			continue
		}

		log.Info("published to delivery", "channel", ch)
		d.publishEvent(ctx, msg.NotificationID, ch, "routing.dispatched", "info", nil)
	}

	return nil
}
```

- [ ] **Step 3: Commit**

```bash
git add internal/dispatch/dispatch.go
git commit -m "feat: update dispatch handler for subscription/template model"
```

---

### Task 12: Admin Service — Category and Subscription Handlers

**Files:**
- Create: `internal/admin/handler_categories.go`
- Create: `internal/admin/handler_subscriptions.go`
- Delete: `internal/admin/handler_groups.go`

- [ ] **Step 1: Write handler_categories.go**

```go
package admin

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hermes-notifications/hermes/internal/models"
)

type createCategoryInput struct {
	Body struct {
		Slug            string   `json:"slug" required:"true" minLength:"1" doc:"URL-friendly identifier"`
		Name            string   `json:"name" required:"true" minLength:"1" doc:"Human-readable name"`
		DefaultChannels []string `json:"default_channels,omitempty" doc:"Default delivery channels"`
		DefaultState    string   `json:"default_state" required:"true" enum:"on,off,required" doc:"Default subscription state"`
		SortOrder       int      `json:"sort_order,omitempty" doc:"Display order"`
	}
}

type updateCategoryInput struct {
	ID string `path:"id" doc:"Category ID"`
	Body struct {
		Name            string   `json:"name" required:"true" doc:"Human-readable name"`
		DefaultChannels []string `json:"default_channels" doc:"Default delivery channels"`
		DefaultState    string   `json:"default_state" required:"true" enum:"on,off,required" doc:"Default subscription state"`
		SortOrder       int      `json:"sort_order" doc:"Display order"`
	}
}

type categoryIDInput struct {
	ID string `path:"id" doc:"Category ID"`
}

type categoryOutput struct {
	Body models.SubscriptionCategory
}

type categoryListOutput struct {
	Body []models.SubscriptionCategory
}

func (s *Server) registerCategoryRoutes() {
	huma.Register(s.api, huma.Operation{
		OperationID: "list-subscription-categories",
		Method:      http.MethodGet,
		Path:        "/v1/subscriptions/categories",
		Summary:     "List subscription categories",
		Tags:        []string{"Subscriptions"},
	}, func(ctx context.Context, input *struct{}) (*categoryListOutput, error) {
		categories, err := s.store.ListCategories(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}
		return &categoryListOutput{Body: categories}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID:   "create-subscription-category",
		Method:        http.MethodPost,
		Path:          "/v1/subscriptions/categories",
		Summary:       "Create a subscription category",
		Tags:          []string{"Subscriptions"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, input *createCategoryInput) (*categoryOutput, error) {
		c, err := s.store.CreateCategory(ctx, input.Body.Slug, input.Body.Name, input.Body.DefaultChannels, input.Body.DefaultState, input.Body.SortOrder)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}
		return &categoryOutput{Body: *c}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID: "update-subscription-category",
		Method:      http.MethodPut,
		Path:        "/v1/subscriptions/categories/{id}",
		Summary:     "Update a subscription category",
		Tags:        []string{"Subscriptions"},
	}, func(ctx context.Context, input *updateCategoryInput) (*categoryOutput, error) {
		c, err := s.store.UpdateCategory(ctx, input.ID, input.Body.Name, input.Body.DefaultChannels, input.Body.DefaultState, input.Body.SortOrder)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}
		return &categoryOutput{Body: *c}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID:   "delete-subscription-category",
		Method:        http.MethodDelete,
		Path:          "/v1/subscriptions/categories/{id}",
		Summary:       "Delete a subscription category",
		Tags:          []string{"Subscriptions"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, input *categoryIDInput) (*struct{}, error) {
		if err := s.store.DeleteCategory(ctx, input.ID); err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}
		return nil, nil
	})
}
```

- [ ] **Step 2: Write handler_subscriptions.go**

```go
package admin

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hermes-notifications/hermes/internal/models"
)

type createSubscriptionInput struct {
	CategoryID string `path:"category_id" doc:"Category ID"`
	Body       struct {
		Slug      string `json:"slug" required:"true" minLength:"1" doc:"URL-friendly identifier"`
		Name      string `json:"name" required:"true" minLength:"1" doc:"Human-readable name"`
		SortOrder int    `json:"sort_order,omitempty" doc:"Display order within category"`
	}
}

type updateSubscriptionInput struct {
	ID   string `path:"id" doc:"Subscription ID"`
	Body struct {
		Name      string `json:"name" required:"true" doc:"Human-readable name"`
		SortOrder int    `json:"sort_order" doc:"Display order within category"`
	}
}

type subscriptionIDInput struct {
	ID string `path:"id" doc:"Subscription ID"`
}

type listSubscriptionsInput struct {
	CategoryID string `path:"category_id" doc:"Category ID"`
}

type subscriptionOutput struct {
	Body models.Subscription
}

type subscriptionListOutput struct {
	Body []models.Subscription
}

func (s *Server) registerSubscriptionRoutes() {
	huma.Register(s.api, huma.Operation{
		OperationID: "list-subscriptions",
		Method:      http.MethodGet,
		Path:        "/v1/subscriptions/categories/{category_id}/subscriptions",
		Summary:     "List subscriptions in a category",
		Tags:        []string{"Subscriptions"},
	}, func(ctx context.Context, input *listSubscriptionsInput) (*subscriptionListOutput, error) {
		subs, err := s.store.ListSubscriptionsByCategory(ctx, input.CategoryID)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}
		return &subscriptionListOutput{Body: subs}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID:   "create-subscription",
		Method:        http.MethodPost,
		Path:          "/v1/subscriptions/categories/{category_id}/subscriptions",
		Summary:       "Create a subscription",
		Tags:          []string{"Subscriptions"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, input *createSubscriptionInput) (*subscriptionOutput, error) {
		sub, err := s.store.CreateSubscription(ctx, input.CategoryID, input.Body.Slug, input.Body.Name, input.Body.SortOrder)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}
		return &subscriptionOutput{Body: *sub}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID: "update-subscription",
		Method:      http.MethodPut,
		Path:        "/v1/subscriptions/{id}",
		Summary:     "Update a subscription",
		Tags:        []string{"Subscriptions"},
	}, func(ctx context.Context, input *updateSubscriptionInput) (*subscriptionOutput, error) {
		sub, err := s.store.UpdateSubscription(ctx, input.ID, input.Body.Name, input.Body.SortOrder)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}
		return &subscriptionOutput{Body: *sub}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID:   "delete-subscription",
		Method:        http.MethodDelete,
		Path:          "/v1/subscriptions/{id}",
		Summary:       "Delete a subscription",
		Tags:          []string{"Subscriptions"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, input *subscriptionIDInput) (*struct{}, error) {
		if err := s.store.DeleteSubscription(ctx, input.ID); err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}
		return nil, nil
	})
}
```

- [ ] **Step 3: Delete handler_groups.go**

```bash
rm internal/admin/handler_groups.go
```

- [ ] **Step 4: Commit**

```bash
git add internal/admin/handler_categories.go internal/admin/handler_subscriptions.go
git rm internal/admin/handler_groups.go
git commit -m "feat: add subscription category and subscription admin handlers, remove groups"
```

---

### Task 13: Admin Service — Template Handler and Send Handler

**Files:**
- Create: `internal/admin/handler_templates.go`
- Delete: `internal/admin/handler_types.go`
- Modify: `internal/admin/handler_send.go`

- [ ] **Step 1: Write handler_templates.go**

```go
package admin

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hermes-notifications/hermes/internal/models"
)

type createTemplateInput struct {
	Body struct {
		Slug            string   `json:"slug" required:"true" minLength:"1" doc:"URL-friendly identifier"`
		Name            string   `json:"name" required:"true" minLength:"1" doc:"Human-readable name"`
		SubscriptionID  *string  `json:"subscription_id,omitempty" doc:"Subscription ID (null for standalone)"`
		DefaultChannels []string `json:"default_channels,omitempty" doc:"Default channels (used when no subscription)"`
		EmailSubject    *string  `json:"email_subject,omitempty" doc:"Email subject template"`
		EmailBody       *string  `json:"email_body,omitempty" doc:"Email body template (HTML)"`
		SMSBody         *string  `json:"sms_body,omitempty" doc:"SMS body template"`
		InboxTitle      *string  `json:"inbox_title,omitempty" doc:"Inbox title template"`
		InboxBody       *string  `json:"inbox_body,omitempty" doc:"Inbox body template"`
	}
}

type updateTemplateInput struct {
	ID string `path:"id" doc:"Template ID"`
	Body struct {
		Name            string   `json:"name" doc:"Human-readable name"`
		SubscriptionID  *string  `json:"subscription_id,omitempty" doc:"Subscription ID (null for standalone)"`
		DefaultChannels []string `json:"default_channels,omitempty" doc:"Default channels (used when no subscription)"`
		EmailSubject    *string  `json:"email_subject,omitempty" doc:"Email subject template"`
		EmailBody       *string  `json:"email_body,omitempty" doc:"Email body template (HTML)"`
		SMSBody         *string  `json:"sms_body,omitempty" doc:"SMS body template"`
		InboxTitle      *string  `json:"inbox_title,omitempty" doc:"Inbox title template"`
		InboxBody       *string  `json:"inbox_body,omitempty" doc:"Inbox body template"`
	}
}

type deleteTemplateInput struct {
	ID string `path:"id" doc:"Template ID"`
}

type templateOutput struct {
	Body models.NotificationTemplate
}

type templateListOutput struct {
	Body []models.NotificationTemplate
}

func (s *Server) registerTemplateRoutes() {
	huma.Register(s.api, huma.Operation{
		OperationID: "list-templates",
		Method:      http.MethodGet,
		Path:        "/v1/templates",
		Summary:     "List notification templates",
		Tags:        []string{"Templates"},
	}, func(ctx context.Context, input *struct{}) (*templateListOutput, error) {
		templates, err := s.store.ListTemplates(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}
		return &templateListOutput{Body: templates}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID:   "create-template",
		Method:        http.MethodPost,
		Path:          "/v1/templates",
		Summary:       "Create a notification template",
		Tags:          []string{"Templates"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, input *createTemplateInput) (*templateOutput, error) {
		nt := &models.NotificationTemplate{
			Slug: input.Body.Slug, Name: input.Body.Name,
			SubscriptionID: input.Body.SubscriptionID, DefaultChannels: input.Body.DefaultChannels,
			EmailSubject: input.Body.EmailSubject, EmailBody: input.Body.EmailBody,
			SMSBody: input.Body.SMSBody, InboxTitle: input.Body.InboxTitle, InboxBody: input.Body.InboxBody,
		}
		nt, err := s.store.CreateTemplate(ctx, nt)
		if err != nil {
			s.logger.Error("failed to create template", "error", err, "slug", input.Body.Slug)
			return nil, huma.Error500InternalServerError("internal server error")
		}
		return &templateOutput{Body: *nt}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID: "update-template",
		Method:      http.MethodPut,
		Path:        "/v1/templates/{id}",
		Summary:     "Update a notification template",
		Tags:        []string{"Templates"},
	}, func(ctx context.Context, input *updateTemplateInput) (*templateOutput, error) {
		nt := &models.NotificationTemplate{
			ID: input.ID, Name: input.Body.Name,
			SubscriptionID: input.Body.SubscriptionID, DefaultChannels: input.Body.DefaultChannels,
			EmailSubject: input.Body.EmailSubject, EmailBody: input.Body.EmailBody,
			SMSBody: input.Body.SMSBody, InboxTitle: input.Body.InboxTitle, InboxBody: input.Body.InboxBody,
		}
		nt, err := s.store.UpdateTemplate(ctx, nt)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		// Invalidate cache
		if s.cache != nil {
			s.cache.InvalidateTemplateConfig(ctx, nt.Slug)
		}
		return &templateOutput{Body: *nt}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID:   "delete-template",
		Method:        http.MethodDelete,
		Path:          "/v1/templates/{id}",
		Summary:       "Delete a notification template",
		Tags:          []string{"Templates"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, input *deleteTemplateInput) (*struct{}, error) {
		// Get slug for cache invalidation before deleting
		existing, err := s.store.GetTemplateByID(ctx, input.ID)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}
		if err := s.store.DeleteTemplate(ctx, input.ID); err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}
		if s.cache != nil {
			s.cache.InvalidateTemplateConfig(ctx, existing.Slug)
		}
		return nil, nil
	})
}
```

- [ ] **Step 2: Update handler_send.go**

Replace the full `sendInput` struct and handler logic:

- `Type` field → `Template`
- Remove `Group` field
- Resolve `categoryID` and `subscriptionID` from template's subscription chain
- Build `models.Notification` with `TemplateID` and `CategoryID`
- NATS message uses new field names

```go
type sendInput struct {
	IdempotencyKey string `header:"X-Idempotency-Key" required:"false" doc:"Idempotency key for deduplication"`
	Body           struct {
		TenantID string         `json:"tenant_id" required:"true" minLength:"1" doc:"Tenant identifier"`
		UserID   string         `json:"user_id" required:"true" minLength:"1" doc:"External user identifier"`
		Template string         `json:"template,omitempty" doc:"Notification template slug (mutually exclusive with content)"`
		Content  *sendContent   `json:"content,omitempty" doc:"Direct content (mutually exclusive with template)"`
		Data     map[string]any `json:"data,omitempty" doc:"Template data for rendering"`
		Channels []string       `json:"channels,omitempty" doc:"Explicit delivery channels"`
	}
}
```

In the handler, replace the group resolution block (lines 86-105) with template resolution:

```go
		// Resolve category from template's subscription, or require channels for direct sends
		var categoryID string
		var templateID *string
		var subscriptionID string
		if req.Template != "" {
			nt, err := s.store.GetTemplateBySlug(ctx, req.Template)
			if err != nil {
				return nil, huma.Error400BadRequest("unknown notification template")
			}
			templateID = &nt.ID
			if nt.SubscriptionID != nil {
				sub, err := s.store.GetSubscriptionByID(ctx, *nt.SubscriptionID)
				if err != nil {
					return nil, huma.Error500InternalServerError("internal server error")
				}
				categoryID = sub.CategoryID
				subscriptionID = sub.ID
			}
		} else {
			if len(req.Channels) == 0 {
				return nil, huma.Error400BadRequest("channels required for direct content sends")
			}
		}
```

Build the notification:

```go
		n := &models.Notification{
			ID:         notifID,
			TenantID:   req.TenantID,
			UserID:     user.ID,
			TemplateID: templateID,
			CategoryID: categoryID,
			Channels:   channels,
			Status:     models.StatusPending,
		}
```

Build the NATS message:

```go
		msg := map[string]any{
			"notification_id": notifID,
			"tenant_id":       req.TenantID,
			"user_id":         user.ID,
			"content": map[string]any{
				"title":        n.Title,
				"body":         n.Body,
				"action_url":   n.ActionURL,
				"action_label": n.ActionLabel,
			},
			"metadata": map[string]any{
				"template": req.Template,
			},
			"category_id":    categoryID,
			"subscription_id": subscriptionID,
			"data":           req.Data,
			"attempt":        1,
		}
		if len(req.Channels) > 0 {
			msg["channels"] = req.Channels
		}
```

- [ ] **Step 3: Delete handler_types.go**

```bash
rm internal/admin/handler_types.go
```

- [ ] **Step 4: Commit**

```bash
git add internal/admin/handler_templates.go internal/admin/handler_send.go
git rm internal/admin/handler_types.go
git commit -m "feat: add template admin handler, update send for subscription model"
```

---

### Task 14: Admin Service — Server and Store Interface Update

**Files:**
- Modify: `internal/admin/server.go`

- [ ] **Step 1: Update AdminStore interface**

Replace the Groups and Types sections with:

```go
type AdminStore interface {
	// Tenants
	GetTenantByID(ctx context.Context, id string) (*models.Tenant, error)

	// Subscription Categories
	CreateCategory(ctx context.Context, slug, name string, defaultChannels []string, defaultState string, sortOrder int) (*models.SubscriptionCategory, error)
	GetCategoryByID(ctx context.Context, id string) (*models.SubscriptionCategory, error)
	ListCategories(ctx context.Context) ([]models.SubscriptionCategory, error)
	UpdateCategory(ctx context.Context, id, name string, defaultChannels []string, defaultState string, sortOrder int) (*models.SubscriptionCategory, error)
	DeleteCategory(ctx context.Context, id string) error

	// Subscriptions
	CreateSubscription(ctx context.Context, categoryID, slug, name string, sortOrder int) (*models.Subscription, error)
	GetSubscriptionByID(ctx context.Context, id string) (*models.Subscription, error)
	ListSubscriptionsByCategory(ctx context.Context, categoryID string) ([]models.Subscription, error)
	UpdateSubscription(ctx context.Context, id, name string, sortOrder int) (*models.Subscription, error)
	DeleteSubscription(ctx context.Context, id string) error

	// Templates
	CreateTemplate(ctx context.Context, input *models.NotificationTemplate) (*models.NotificationTemplate, error)
	GetTemplateByID(ctx context.Context, id string) (*models.NotificationTemplate, error)
	GetTemplateBySlug(ctx context.Context, slug string) (*models.NotificationTemplate, error)
	ListTemplates(ctx context.Context) ([]models.NotificationTemplate, error)
	UpdateTemplate(ctx context.Context, input *models.NotificationTemplate) (*models.NotificationTemplate, error)
	DeleteTemplate(ctx context.Context, id string) error

	// Users
	EnsureUser(ctx context.Context, tenantID, externalID string) (*models.User, error)

	// Notifications
	CreateNotification(ctx context.Context, n *models.Notification) (*models.Notification, error)
	GetNotificationByID(ctx context.Context, id string) (*models.Notification, error)
	GetNotificationByIdempotencyKey(ctx context.Context, tenantID, key string) (*models.Notification, error)
	GetNotificationEvents(ctx context.Context, notificationID string) ([]models.NotificationEvent, error)

	// API Keys
	CreateAPIKey(ctx context.Context, id, keyHash, name string, permissions []string) (*models.APIKey, error)
	ListAPIKeys(ctx context.Context) ([]models.APIKey, error)
	GetAPIKeyByID(ctx context.Context, id string) (*models.APIKey, error)
	DeleteAPIKey(ctx context.Context, id string) error

	// JWT Signing Keys
	EnsureHermesSigningKey(ctx context.Context, secret string) error
}
```

- [ ] **Step 2: Update routes() registration**

Replace `s.registerGroupRoutes()` and `s.registerTypeRoutes()` with:

```go
func (s *Server) routes() {
	s.router.Get("/healthz", httputil.HealthzHandler())
	if s.pool != nil {
		s.router.Get("/readyz", httputil.ReadyzHandler(s.pool.Ping))
	} else {
		s.router.Get("/readyz", httputil.ReadyzHandler())
	}

	s.registerCategoryRoutes()
	s.registerSubscriptionRoutes()
	s.registerTemplateRoutes()
	s.registerSendRoutes()
	s.registerNotificationRoutes()
	s.registerAuthRoutes()
	s.registerAPIKeyRoutes()
}
```

Update the API description:

```go
config.Info.Description = "Server-to-server API for managing subscription categories, templates, and sending notifications."
```

- [ ] **Step 3: Commit**

```bash
git add internal/admin/server.go
git commit -m "feat: update admin server interface and routes for subscriptions/templates"
```

---

### Task 15: Admin Service — Update Mock Store and Tests

**Files:**
- Modify: `internal/admin/testutil_test.go`

- [ ] **Step 1: Rewrite mockStore for new interfaces**

Replace the `groups` and `types` fields and methods with `categories`, `subscriptions`, and `templates`:

```go
type mockStore struct {
	tenants       []models.Tenant
	categories    []models.SubscriptionCategory
	subscriptions []models.Subscription
	templates     []models.NotificationTemplate
	users         []models.User
	notifications []models.Notification
	events        []models.NotificationEvent
	apiKeys       []models.APIKey
}

// --- Subscription Categories ---

func (m *mockStore) CreateCategory(ctx context.Context, slug, name string, defaultChannels []string, defaultState string, sortOrder int) (*models.SubscriptionCategory, error) {
	c := models.SubscriptionCategory{
		ID: fmt.Sprintf("sct-%d", len(m.categories)+1), Slug: slug, Name: name,
		DefaultChannels: defaultChannels, DefaultState: defaultState, SortOrder: sortOrder,
		CreatedAt: time.Now(),
	}
	m.categories = append(m.categories, c)
	return &c, nil
}

func (m *mockStore) GetCategoryByID(ctx context.Context, id string) (*models.SubscriptionCategory, error) {
	for _, c := range m.categories {
		if c.ID == id {
			return &c, nil
		}
	}
	return nil, fmt.Errorf("category not found: %s", id)
}

func (m *mockStore) ListCategories(ctx context.Context) ([]models.SubscriptionCategory, error) {
	return m.categories, nil
}

func (m *mockStore) UpdateCategory(ctx context.Context, id, name string, defaultChannels []string, defaultState string, sortOrder int) (*models.SubscriptionCategory, error) {
	for i, c := range m.categories {
		if c.ID == id {
			m.categories[i].Name = name
			m.categories[i].DefaultChannels = defaultChannels
			m.categories[i].DefaultState = defaultState
			m.categories[i].SortOrder = sortOrder
			updated := m.categories[i]
			return &updated, nil
		}
	}
	return nil, fmt.Errorf("category not found: %s", id)
}

func (m *mockStore) DeleteCategory(ctx context.Context, id string) error {
	for i, c := range m.categories {
		if c.ID == id {
			m.categories = append(m.categories[:i], m.categories[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("category not found: %s", id)
}

// --- Subscriptions ---

func (m *mockStore) CreateSubscription(ctx context.Context, categoryID, slug, name string, sortOrder int) (*models.Subscription, error) {
	s := models.Subscription{
		ID: fmt.Sprintf("sub-%d", len(m.subscriptions)+1), CategoryID: categoryID,
		Slug: slug, Name: name, SortOrder: sortOrder, CreatedAt: time.Now(),
	}
	m.subscriptions = append(m.subscriptions, s)
	return &s, nil
}

func (m *mockStore) GetSubscriptionByID(ctx context.Context, id string) (*models.Subscription, error) {
	for _, s := range m.subscriptions {
		if s.ID == id {
			return &s, nil
		}
	}
	return nil, fmt.Errorf("subscription not found: %s", id)
}

func (m *mockStore) ListSubscriptionsByCategory(ctx context.Context, categoryID string) ([]models.Subscription, error) {
	var result []models.Subscription
	for _, s := range m.subscriptions {
		if s.CategoryID == categoryID {
			result = append(result, s)
		}
	}
	return result, nil
}

func (m *mockStore) UpdateSubscription(ctx context.Context, id, name string, sortOrder int) (*models.Subscription, error) {
	for i, s := range m.subscriptions {
		if s.ID == id {
			m.subscriptions[i].Name = name
			m.subscriptions[i].SortOrder = sortOrder
			updated := m.subscriptions[i]
			return &updated, nil
		}
	}
	return nil, fmt.Errorf("subscription not found: %s", id)
}

func (m *mockStore) DeleteSubscription(ctx context.Context, id string) error {
	for i, s := range m.subscriptions {
		if s.ID == id {
			m.subscriptions = append(m.subscriptions[:i], m.subscriptions[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("subscription not found: %s", id)
}

// --- Templates ---

func (m *mockStore) CreateTemplate(ctx context.Context, input *models.NotificationTemplate) (*models.NotificationTemplate, error) {
	t := *input
	t.ID = fmt.Sprintf("ntpl-%d", len(m.templates)+1)
	t.CreatedAt = time.Now()
	m.templates = append(m.templates, t)
	return &t, nil
}

func (m *mockStore) GetTemplateByID(ctx context.Context, id string) (*models.NotificationTemplate, error) {
	for _, t := range m.templates {
		if t.ID == id {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("template not found: %s", id)
}

func (m *mockStore) GetTemplateBySlug(ctx context.Context, slug string) (*models.NotificationTemplate, error) {
	for _, t := range m.templates {
		if t.Slug == slug {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("template not found: %s", slug)
}

func (m *mockStore) ListTemplates(ctx context.Context) ([]models.NotificationTemplate, error) {
	return m.templates, nil
}

func (m *mockStore) UpdateTemplate(ctx context.Context, input *models.NotificationTemplate) (*models.NotificationTemplate, error) {
	for i, t := range m.templates {
		if t.ID == input.ID {
			m.templates[i] = *input
			updated := m.templates[i]
			return &updated, nil
		}
	}
	return nil, fmt.Errorf("template not found: %s", input.ID)
}

func (m *mockStore) DeleteTemplate(ctx context.Context, id string) error {
	for i, t := range m.templates {
		if t.ID == id {
			m.templates = append(m.templates[:i], m.templates[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("template not found: %s", id)
}
```

Update `CreateNotification` mock to use `TemplateID`/`CategoryID` (these are just field names on the struct, so no code change needed — the struct already uses the new model).

- [ ] **Step 2: Commit**

```bash
git add internal/admin/testutil_test.go
git commit -m "feat: update admin mock store for subscription/template model"
```

---

### Task 16: User Service — Preference Handler Rewrite

**Files:**
- Modify: `internal/userservice/server.go`
- Modify: `internal/userservice/handler_preferences.go`
- Modify: `internal/userservice/testutil_test.go`

- [ ] **Step 1: Update UserStore interface in server.go**

```go
type UserStore interface {
	GetUserByID(ctx context.Context, userID string) (*models.User, error)
	UpdateUserContacts(ctx context.Context, userID string, email, phone *string) (*models.User, error)
	GetUserSubscriptions(ctx context.Context, userID string) ([]models.UserSubscription, error)
	SetUserSubscription(ctx context.Context, userID, subscriptionID string, optedIn bool) (*models.UserSubscription, error)
	DeleteUserSubscription(ctx context.Context, userID, subscriptionID string) error
	ListCategories(ctx context.Context) ([]models.SubscriptionCategory, error)
	ListSubscriptionsByCategory(ctx context.Context, categoryID string) ([]models.Subscription, error)
}
```

- [ ] **Step 2: Rewrite handler_preferences.go**

```go
package userservice

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hermes-notifications/hermes/internal/auth"
)

type subscriptionIDInput struct {
	SubscriptionID string `path:"subscription_id" doc:"Subscription ID"`
}

type setPreferenceInput struct {
	SubscriptionID string `path:"subscription_id" doc:"Subscription ID"`
	Body           struct {
		OptedIn bool `json:"opted_in" required:"true" doc:"Whether the user is subscribed"`
	}
}

type preferenceSubscription struct {
	ID         string `json:"id"`
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	OptedIn    bool   `json:"opted_in"`
	Toggleable bool   `json:"toggleable"`
}

type preferenceCategory struct {
	ID              string                   `json:"id"`
	Slug            string                   `json:"slug"`
	Name            string                   `json:"name"`
	DefaultChannels []string                 `json:"default_channels"`
	DefaultState    string                   `json:"default_state"`
	Subscriptions   []preferenceSubscription `json:"subscriptions"`
}

type preferenceCenterOutput struct {
	Body struct {
		Categories []preferenceCategory `json:"categories"`
	}
}

type statusOutput struct {
	Body struct {
		Status string `json:"status" example:"ok" doc:"Operation result"`
	}
}

func (s *Server) registerPreferenceRoutes() {
	huma.Register(s.api, huma.Operation{
		OperationID: "get-preference-center",
		Method:      http.MethodGet,
		Path:        "/v1/users/me/preferences",
		Summary:     "Get notification preference center",
		Tags:        []string{"Preferences"},
	}, func(ctx context.Context, input *struct{}) (*preferenceCenterOutput, error) {
		userID := auth.UserIDFromContext(ctx)
		if userID == "" {
			return nil, huma.Error401Unauthorized("missing user")
		}

		categories, err := s.store.ListCategories(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		userSubs, err := s.store.GetUserSubscriptions(ctx, userID)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		// Build lookup of user's explicit preferences
		userSubMap := make(map[string]bool)
		for _, us := range userSubs {
			userSubMap[us.SubscriptionID] = us.OptedIn
		}

		var result []preferenceCategory
		for _, cat := range categories {
			subs, err := s.store.ListSubscriptionsByCategory(ctx, cat.ID)
			if err != nil {
				return nil, huma.Error500InternalServerError("internal server error")
			}

			var prefSubs []preferenceSubscription
			for _, sub := range subs {
				optedIn := cat.DefaultState == "on" || cat.DefaultState == "required"
				if explicit, ok := userSubMap[sub.ID]; ok {
					optedIn = explicit
				}
				if cat.DefaultState == "required" {
					optedIn = true // required always on
				}

				prefSubs = append(prefSubs, preferenceSubscription{
					ID:         sub.ID,
					Slug:       sub.Slug,
					Name:       sub.Name,
					OptedIn:    optedIn,
					Toggleable: cat.DefaultState != "required",
				})
			}

			result = append(result, preferenceCategory{
				ID:              cat.ID,
				Slug:            cat.Slug,
				Name:            cat.Name,
				DefaultChannels: cat.DefaultChannels,
				DefaultState:    cat.DefaultState,
				Subscriptions:   prefSubs,
			})
		}

		resp := &preferenceCenterOutput{}
		resp.Body.Categories = result
		return resp, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID: "set-preference",
		Method:      http.MethodPut,
		Path:        "/v1/users/me/preferences/{subscription_id}",
		Summary:     "Set subscription preference",
		Tags:        []string{"Preferences"},
	}, func(ctx context.Context, input *setPreferenceInput) (*statusOutput, error) {
		userID := auth.UserIDFromContext(ctx)
		if userID == "" {
			return nil, huma.Error401Unauthorized("missing user")
		}

		if _, err := s.store.SetUserSubscription(ctx, userID, input.SubscriptionID, input.Body.OptedIn); err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		resp := &statusOutput{}
		resp.Body.Status = "ok"
		return resp, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID: "delete-preference",
		Method:      http.MethodDelete,
		Path:        "/v1/users/me/preferences/{subscription_id}",
		Summary:     "Delete subscription preference (revert to default)",
		Tags:        []string{"Preferences"},
	}, func(ctx context.Context, input *subscriptionIDInput) (*statusOutput, error) {
		userID := auth.UserIDFromContext(ctx)
		if userID == "" {
			return nil, huma.Error401Unauthorized("missing user")
		}

		if err := s.store.DeleteUserSubscription(ctx, userID, input.SubscriptionID); err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}

		resp := &statusOutput{}
		resp.Body.Status = "ok"
		return resp, nil
	})
}
```

- [ ] **Step 3: Update testutil_test.go mock store**

```go
type mockUserStore struct {
	users             []models.User
	userSubscriptions []models.UserSubscription
	categories        []models.SubscriptionCategory
	subscriptions     []models.Subscription
}

func (m *mockUserStore) GetUserSubscriptions(ctx context.Context, userID string) ([]models.UserSubscription, error) {
	var result []models.UserSubscription
	for _, us := range m.userSubscriptions {
		if us.UserID == userID {
			result = append(result, us)
		}
	}
	return result, nil
}

func (m *mockUserStore) SetUserSubscription(ctx context.Context, userID, subscriptionID string, optedIn bool) (*models.UserSubscription, error) {
	for i, us := range m.userSubscriptions {
		if us.UserID == userID && us.SubscriptionID == subscriptionID {
			m.userSubscriptions[i].OptedIn = optedIn
			updated := m.userSubscriptions[i]
			return &updated, nil
		}
	}
	us := models.UserSubscription{
		UserID: userID, SubscriptionID: subscriptionID, OptedIn: optedIn,
	}
	m.userSubscriptions = append(m.userSubscriptions, us)
	return &us, nil
}

func (m *mockUserStore) DeleteUserSubscription(ctx context.Context, userID, subscriptionID string) error {
	for i, us := range m.userSubscriptions {
		if us.UserID == userID && us.SubscriptionID == subscriptionID {
			m.userSubscriptions = append(m.userSubscriptions[:i], m.userSubscriptions[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("not found")
}

func (m *mockUserStore) ListCategories(ctx context.Context) ([]models.SubscriptionCategory, error) {
	return m.categories, nil
}

func (m *mockUserStore) ListSubscriptionsByCategory(ctx context.Context, categoryID string) ([]models.Subscription, error) {
	var result []models.Subscription
	for _, s := range m.subscriptions {
		if s.CategoryID == categoryID {
			result = append(result, s)
		}
	}
	return result, nil
}
```

Update `newTestServer` to use new seed data:

```go
func newTestServer(t *testing.T) (*userservice.Server, *mockUserStore) {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	email := "user@example.com"
	store := &mockUserStore{
		users: []models.User{
			{ID: testUserID, TenantID: "tenant-1", ExternalID: "ext-1", Email: &email, CreatedAt: time.Now()},
		},
		categories: []models.SubscriptionCategory{
			{ID: "sct-1", Slug: "general", Name: "General", DefaultChannels: []string{"email", "inbox"}, DefaultState: "on"},
			{ID: "sct-2", Slug: "marketing", Name: "Marketing", DefaultChannels: []string{"email"}, DefaultState: "off"},
		},
		subscriptions: []models.Subscription{
			{ID: "sub-1", CategoryID: "sct-1", Slug: "general", Name: "General"},
			{ID: "sub-2", CategoryID: "sct-2", Slug: "marketing", Name: "Marketing"},
		},
		userSubscriptions: []models.UserSubscription{
			{UserID: testUserID, SubscriptionID: "sub-2", OptedIn: true},
		},
	}
	srv := userservice.NewServer(store, nil, logger)
	srv.SetSkipAuth(true)
	return srv, store
}
```

- [ ] **Step 4: Commit**

```bash
git add internal/userservice/server.go internal/userservice/handler_preferences.go internal/userservice/testutil_test.go
git commit -m "feat: rewrite user service preferences for subscription model"
```

---

### Task 17: Dispatch Tests and Wiring

**Files:**
- Modify: `internal/dispatch/channels_test.go`
- Modify: `cmd/dispatch/main.go`

- [ ] **Step 1: Update channels_test.go**

Replace `models.NotificationType` with `models.NotificationTemplate`:

```go
package dispatch_test

import (
	"testing"

	"github.com/hermes-notifications/hermes/internal/dispatch"
	"github.com/hermes-notifications/hermes/internal/models"
)

func TestFilterChannelsForTemplate_AllTemplates(t *testing.T) {
	nt := &models.NotificationTemplate{
		EmailSubject: strPtr("subject"),
		SMSBody:      strPtr("body"),
		InboxTitle:   strPtr("title"),
	}
	got := dispatch.FilterChannelsForTemplate([]string{"email", "sms", "inbox"}, nt)
	if len(got) != 3 {
		t.Fatalf("expected 3 channels, got %d: %v", len(got), got)
	}
}

func TestFilterChannelsForTemplate_NoEmailTemplate(t *testing.T) {
	nt := &models.NotificationTemplate{
		InboxTitle: strPtr("title"),
	}
	got := dispatch.FilterChannelsForTemplate([]string{"email", "inbox"}, nt)
	if len(got) != 1 || got[0] != "inbox" {
		t.Fatalf("expected [inbox], got %v", got)
	}
}

func TestFilterChannelsForTemplate_NilTemplate(t *testing.T) {
	got := dispatch.FilterChannelsForTemplate([]string{"email", "inbox"}, nil)
	if len(got) != 2 {
		t.Fatalf("expected 2 channels for direct send, got %d", len(got))
	}
}
```

- [ ] **Step 2: Update cmd/dispatch/main.go wiring**

The `NewChannelResolver` now just takes `st` which satisfies all needed interfaces:

```go
	st := postgres.New(pool)
	templateResolver := dispatch.NewTemplateResolver(st, redisClient)
	channelResolver := dispatch.NewChannelResolver(st)
	d := dispatch.NewDispatch(natsClient, st, st, templateResolver, channelResolver, logger)
```

This stays the same — `st` already satisfies `channelStore` since `*postgres.Store` implements the composite `Repository` interface. No code change needed, but verify it compiles.

- [ ] **Step 3: Commit**

```bash
git add internal/dispatch/channels_test.go cmd/dispatch/main.go
git commit -m "feat: update dispatch tests and wiring for template model"
```

---

### Task 18: Verify Notification Handler (Admin)

**Files:**
- Check: `internal/admin/handler_notifications.go`

- [ ] **Step 1: Verify no changes needed**

This handler passes `models.Notification` directly to JSON output. The `TemplateID`/`CategoryID` renames in the model struct automatically change the JSON field names. No code change is needed — just verify the file compiles as part of the build step in Task 20.

---

### Task 19: Update User Service Preference Tests

**Files:**
- Modify: `internal/userservice/handler_preferences_test.go`

- [ ] **Step 1: Update test cases for new API**

The preference tests need to use the new endpoints:
- `GET /v1/users/me/preferences` now returns the preference center format with categories
- `PUT /v1/users/me/preferences/{subscription_id}` accepts `{"opted_in": true}`
- `DELETE /v1/users/me/preferences/{subscription_id}`

Read the existing test file and update to match the new API contract. Tests should verify:
1. Preference center lists categories with subscriptions and correct opted_in state
2. Setting a preference updates opted_in
3. Deleting a preference reverts to default

- [ ] **Step 2: Run tests**

Run: `go test ./internal/userservice/... -v`

Expected: All tests pass.

- [ ] **Step 3: Commit**

```bash
git add internal/userservice/handler_preferences_test.go
git commit -m "test: update user service preference tests for subscription model"
```

---

### Task 20: Build and Test

- [ ] **Step 1: Build all services**

Run: `make build`

Expected: All services compile successfully.

- [ ] **Step 2: Run unit tests**

Run: `make test`

Expected: All tests pass.

- [ ] **Step 3: Run lint**

Run: `make lint`

Expected: No lint errors.

- [ ] **Step 4: Fix any compilation or test issues**

Address any remaining issues from the build/test/lint steps.

- [ ] **Step 5: Final commit**

```bash
git add -A
git commit -m "fix: resolve remaining compilation and test issues"
```

---

### Task 21: Update Dispatch Template Test

**Files:**
- Modify: `internal/dispatch/template_test.go`

- [ ] **Step 1: Read and update template_test.go**

Read the file and update any references to `models.NotificationType` → `models.NotificationTemplate`, `GetTypeBySlug` → `GetTemplateBySlug`, and cache method names `GetTypeConfig`/`SetTypeConfig` → `GetTemplateConfig`/`SetTemplateConfig`.

- [ ] **Step 2: Run dispatch tests**

Run: `go test ./internal/dispatch/... -v`

Expected: All tests pass.

- [ ] **Step 3: Commit**

```bash
git add internal/dispatch/template_test.go
git commit -m "test: update dispatch template tests for new model"
```
