# Namespaces Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add namespace-level organizational boundaries to Hermes so notification types, API keys, and notifications can be scoped to a product/app.

**Architecture:** Namespaces are a new first-class entity orthogonal to tenants. API keys are optionally scoped to a namespace (NULL = global). Notification types and notifications gain a `namespace_id` column. No FK constraints — application-level validation for datastore portability. Existing data migrates to an auto-created `default` namespace.

**Tech Stack:** Go, PostgreSQL, NATS JetStream, Huma (OpenAPI), slog structured logging

**Spec:** `docs/superpowers/specs/2026-03-28-namespaces-design.md`

---

## File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Create | `migrations/000011_add_namespaces.up.sql` | Schema: namespaces table, add namespace_id columns |
| Create | `migrations/000011_add_namespaces.down.sql` | Rollback migration |
| Modify | `internal/models/models.go` | Add Namespace model; add NamespaceID to APIKey, NotificationType, Notification |
| Modify | `internal/store/interfaces.go` | Add NamespaceRepository; widen TypeRepository + AuthRepository signatures |
| Create | `internal/store/postgres/namespaces.go` | Namespace CRUD store implementation |
| Modify | `internal/store/postgres/types.go` | Namespace-scoped type queries |
| Modify | `internal/store/postgres/auth.go` | namespace_id in API key queries |
| Modify | `internal/store/postgres/notifications.go` | namespace_id in notification INSERT/SELECT |
| Modify | `internal/store/postgres/inbox.go` | namespace_id in SELECT columns, optional namespace filter |
| Modify | `internal/auth/permissions.go` | Add PermNamespacesManage; add NamespaceID to ValidatedKey |
| Create | `internal/admin/handler_namespaces.go` | Namespace CRUD endpoints |
| Modify | `internal/admin/handler_apikeys.go` | namespace_id on create, scoping on list |
| Modify | `internal/admin/handler_types.go` | Namespace-scoped type operations |
| Modify | `internal/admin/handler_send.go` | Infer namespace from auth context, tag notification |
| Modify | `internal/admin/server.go` | AdminStore interface + validateAPIKey + route registration |
| Modify | `internal/admin/testutil_test.go` | Mock store: namespace methods + updated signatures |
| Modify | `internal/nats/messages.go` | Add NamespaceID to SendMessage and DeliveryMessage |
| Modify | `internal/dispatch/dispatch.go` | Pass NamespaceID through, add to log context |
| Modify | `internal/dispatch/template.go` | Namespace-scoped type cache key |
| Modify | `internal/delivery/worker.go` | Add namespace_id to log context |
| Modify | `internal/inbox/handler_list.go` | Optional namespace query filter |
| Modify | `internal/inbox/server.go` | InboxStore interface update |

---

### Task 1: Database Migration

**Files:**
- Create: `migrations/000011_add_namespaces.up.sql`
- Create: `migrations/000011_add_namespaces.down.sql`

- [ ] **Step 1: Write the up migration**

```sql
-- migrations/000011_add_namespaces.up.sql

-- Namespace entity
CREATE TABLE namespaces (
    id         TEXT PRIMARY KEY,
    slug       TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    settings   JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed the default namespace
INSERT INTO namespaces (id, slug, name) VALUES ('ns_default', 'default', 'Default');

-- Add namespace_id to api_keys (NULL = global key)
ALTER TABLE api_keys ADD COLUMN namespace_id TEXT;

-- Add namespace_id to notification_types (NOT NULL, default to 'ns_default')
ALTER TABLE notification_types ADD COLUMN namespace_id TEXT NOT NULL DEFAULT 'ns_default';

-- Uniqueness is now (namespace_id, slug) instead of just (slug)
DROP INDEX IF EXISTS idx_notification_types_slug;
CREATE UNIQUE INDEX idx_notification_types_ns_slug ON notification_types (namespace_id, slug);

-- Add namespace_id to notifications
ALTER TABLE notifications ADD COLUMN namespace_id TEXT NOT NULL DEFAULT 'ns_default';
CREATE INDEX idx_notifications_namespace ON notifications (namespace_id);
```

- [ ] **Step 2: Write the down migration**

```sql
-- migrations/000011_add_namespaces.down.sql

DROP INDEX IF EXISTS idx_notifications_namespace;
ALTER TABLE notifications DROP COLUMN IF EXISTS namespace_id;

DROP INDEX IF EXISTS idx_notification_types_ns_slug;
ALTER TABLE notification_types DROP COLUMN IF EXISTS namespace_id;
CREATE UNIQUE INDEX idx_notification_types_slug ON notification_types (slug);

ALTER TABLE api_keys DROP COLUMN IF EXISTS namespace_id;

DROP TABLE IF EXISTS namespaces;
```

- [ ] **Step 3: Verify migration compiles**

Run: `make migrate`
Expected: Migration 000011 applies successfully. Verify `namespaces` table exists and default row is inserted.

- [ ] **Step 4: Commit**

```bash
git add migrations/000011_add_namespaces.up.sql migrations/000011_add_namespaces.down.sql
git commit -m "feat: add namespaces migration with default namespace seed"
```

---

### Task 2: Models and Namespace ID Generator

**Files:**
- Modify: `internal/models/models.go`

- [ ] **Step 1: Add Namespace model and NamespaceID fields**

Add the `Namespace` struct and update `APIKey`, `NotificationType`, and `Notification`:

```go
// Add to models.go

type Namespace struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Settings  []byte    `json:"settings,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
```

Add `NamespaceID` fields:

```go
// In APIKey struct, add after Permissions:
NamespaceID *string `json:"namespace_id,omitempty"`

// In NotificationType struct, add after GroupID:
NamespaceID string `json:"namespace_id"`

// In Notification struct, add after TenantID:
NamespaceID string `json:"namespace_id"`
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/models/...`
Expected: PASS (models has no external dependencies that break)

- [ ] **Step 3: Commit**

```bash
git add internal/models/models.go
git commit -m "feat: add Namespace model and NamespaceID fields to existing models"
```

---

### Task 3: Store Interfaces and Namespace Store Implementation

This task must be committed atomically — the interface and implementation must land together to keep the compile-time check in `store.go` passing.

**Files:**
- Modify: `internal/store/interfaces.go`
- Create: `internal/store/postgres/namespaces.go`
- Modify: `internal/store/postgres/types.go`
- Modify: `internal/store/postgres/auth.go`
- Modify: `internal/store/postgres/notifications.go`
- Modify: `internal/store/postgres/inbox.go`

- [ ] **Step 1: Add NamespaceRepository and update interfaces**

In `internal/store/interfaces.go`, add:

```go
// NamespaceRepository defines operations for managing namespaces.
type NamespaceRepository interface {
	CreateNamespace(ctx context.Context, id, slug, name string) (*models.Namespace, error)
	GetNamespaceByID(ctx context.Context, id string) (*models.Namespace, error)
	GetNamespaceBySlug(ctx context.Context, slug string) (*models.Namespace, error)
	ListNamespaces(ctx context.Context) ([]models.Namespace, error)
	UpdateNamespace(ctx context.Context, id, name string, settings []byte) (*models.Namespace, error)
}
```

Update `TypeRepository`:
```go
// Change GetTypeBySlug signature:
GetTypeBySlug(ctx context.Context, namespaceID, slug string) (*models.NotificationType, error)

// Change ListTypes signature:
ListTypes(ctx context.Context, namespaceID string) ([]models.NotificationType, error)
```

Update `AuthRepository`:
```go
// Change CreateAPIKey signature:
CreateAPIKey(ctx context.Context, id, keyHash, name string, permissions []string, namespaceID *string) (*models.APIKey, error)

// Change ListAPIKeys signature:
ListAPIKeys(ctx context.Context, namespaceID *string) ([]models.APIKey, error)
```

Add `NamespaceRepository` to the `Repository` composite:
```go
type Repository interface {
	TenantRepository
	NamespaceRepository  // NEW
	GroupRepository
	TypeRepository
	// ... rest unchanged
}
```

- [ ] **Step 2: Implement namespace store**

Create `internal/store/postgres/namespaces.go`:

```go
package postgres

import (
	"context"
	"fmt"

	"github.com/hermes-notifications/hermes/internal/models"
)

func (s *Store) CreateNamespace(ctx context.Context, id, slug, name string) (*models.Namespace, error) {
	ns := &models.Namespace{ID: id, Slug: slug, Name: name}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO namespaces (id, slug, name) VALUES ($1, $2, $3) RETURNING settings, created_at`,
		ns.ID, ns.Slug, ns.Name,
	).Scan(&ns.Settings, &ns.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create namespace: %w", err)
	}
	return ns, nil
}

func (s *Store) GetNamespaceByID(ctx context.Context, id string) (*models.Namespace, error) {
	ns := &models.Namespace{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, slug, name, settings, created_at FROM namespaces WHERE id = $1`, id,
	).Scan(&ns.ID, &ns.Slug, &ns.Name, &ns.Settings, &ns.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get namespace by id: %w", err)
	}
	return ns, nil
}

func (s *Store) GetNamespaceBySlug(ctx context.Context, slug string) (*models.Namespace, error) {
	ns := &models.Namespace{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, slug, name, settings, created_at FROM namespaces WHERE slug = $1`, slug,
	).Scan(&ns.ID, &ns.Slug, &ns.Name, &ns.Settings, &ns.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get namespace by slug: %w", err)
	}
	return ns, nil
}

func (s *Store) ListNamespaces(ctx context.Context) ([]models.Namespace, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, slug, name, settings, created_at FROM namespaces ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}
	defer rows.Close()

	var namespaces []models.Namespace
	for rows.Next() {
		var ns models.Namespace
		if err := rows.Scan(&ns.ID, &ns.Slug, &ns.Name, &ns.Settings, &ns.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan namespace: %w", err)
		}
		namespaces = append(namespaces, ns)
	}
	return namespaces, rows.Err()
}

func (s *Store) UpdateNamespace(ctx context.Context, id, name string, settings []byte) (*models.Namespace, error) {
	ns := &models.Namespace{}
	err := s.pool.QueryRow(ctx,
		`UPDATE namespaces SET name = $2, settings = $3
		 WHERE id = $1
		 RETURNING id, slug, name, settings, created_at`,
		id, name, settings,
	).Scan(&ns.ID, &ns.Slug, &ns.Name, &ns.Settings, &ns.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("update namespace: %w", err)
	}
	return ns, nil
}
```

- [ ] **Step 3: Update types.go for namespace_id**

In `internal/store/postgres/types.go`:

Update `CreateType` — add `namespace_id` to INSERT:
```go
func (s *Store) CreateType(ctx context.Context, input *models.NotificationType) (*models.NotificationType, error) {
	input.ID = id.New()
	err := s.pool.QueryRow(ctx,
		`INSERT INTO notification_types (id, namespace_id, group_id, slug, name, email_subject, email_body, sms_body, inbox_title, inbox_body)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 RETURNING created_at`,
		input.ID, input.NamespaceID, input.GroupID, input.Slug, input.Name,
		input.EmailSubject, input.EmailBody, input.SMSBody,
		input.InboxTitle, input.InboxBody,
	).Scan(&input.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create type: %w", err)
	}
	return input, nil
}
```

Update `GetTypeBySlug` — accept `namespaceID` param, filter by it:
```go
func (s *Store) GetTypeBySlug(ctx context.Context, namespaceID, slug string) (*models.NotificationType, error) {
	t := &models.NotificationType{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, namespace_id, group_id, slug, name, email_subject, email_body, sms_body, inbox_title, inbox_body, created_at
		 FROM notification_types WHERE namespace_id = $1 AND slug = $2`, namespaceID, slug,
	).Scan(&t.ID, &t.NamespaceID, &t.GroupID, &t.Slug, &t.Name,
		&t.EmailSubject, &t.EmailBody, &t.SMSBody,
		&t.InboxTitle, &t.InboxBody, &t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get type by slug: %w", err)
	}
	return t, nil
}
```

Update `GetTypeByID` — add namespace_id to SELECT/Scan:
```go
func (s *Store) GetTypeByID(ctx context.Context, id string) (*models.NotificationType, error) {
	t := &models.NotificationType{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, namespace_id, group_id, slug, name, email_subject, email_body, sms_body, inbox_title, inbox_body, created_at
		 FROM notification_types WHERE id = $1`, id,
	).Scan(&t.ID, &t.NamespaceID, &t.GroupID, &t.Slug, &t.Name,
		&t.EmailSubject, &t.EmailBody, &t.SMSBody,
		&t.InboxTitle, &t.InboxBody, &t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get type by id: %w", err)
	}
	return t, nil
}
```

Update `ListTypes` — accept `namespaceID` param:
```go
func (s *Store) ListTypes(ctx context.Context, namespaceID string) ([]models.NotificationType, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, namespace_id, group_id, slug, name, email_subject, email_body, sms_body, inbox_title, inbox_body, created_at
		 FROM notification_types WHERE namespace_id = $1 ORDER BY created_at`, namespaceID)
	if err != nil {
		return nil, fmt.Errorf("list types: %w", err)
	}
	defer rows.Close()

	var types []models.NotificationType
	for rows.Next() {
		var t models.NotificationType
		if err := rows.Scan(&t.ID, &t.NamespaceID, &t.GroupID, &t.Slug, &t.Name,
			&t.EmailSubject, &t.EmailBody, &t.SMSBody,
			&t.InboxTitle, &t.InboxBody, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan type: %w", err)
		}
		types = append(types, t)
	}
	return types, rows.Err()
}
```

Update `UpdateType` — add namespace_id to RETURNING Scan:
```go
func (s *Store) UpdateType(ctx context.Context, input *models.NotificationType) (*models.NotificationType, error) {
	err := s.pool.QueryRow(ctx,
		`UPDATE notification_types
		 SET name = $2, email_subject = $3, email_body = $4, sms_body = $5, inbox_title = $6, inbox_body = $7
		 WHERE id = $1
		 RETURNING id, namespace_id, group_id, slug, name, email_subject, email_body, sms_body, inbox_title, inbox_body, created_at`,
		input.ID, input.Name, input.EmailSubject, input.EmailBody,
		input.SMSBody, input.InboxTitle, input.InboxBody,
	).Scan(&input.ID, &input.NamespaceID, &input.GroupID, &input.Slug, &input.Name,
		&input.EmailSubject, &input.EmailBody, &input.SMSBody,
		&input.InboxTitle, &input.InboxBody, &input.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("update type: %w", err)
	}
	return input, nil
}
```

- [ ] **Step 4: Update auth.go for namespace_id**

In `internal/store/postgres/auth.go`:

Update `CreateAPIKey`:
```go
func (s *Store) CreateAPIKey(ctx context.Context, id, keyHash, name string, permissions []string, namespaceID *string) (*models.APIKey, error) {
	k := &models.APIKey{ID: id, KeyHash: keyHash, Name: name, Permissions: permissions, NamespaceID: namespaceID}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO api_keys (id, key_hash, name, permissions, namespace_id) VALUES ($1, $2, $3, $4, $5) RETURNING created_at`,
		k.ID, k.KeyHash, k.Name, k.Permissions, k.NamespaceID,
	).Scan(&k.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create api key: %w", err)
	}
	return k, nil
}
```

Update `ListAPIKeys` — accept optional namespace filter:
```go
func (s *Store) ListAPIKeys(ctx context.Context, namespaceID *string) ([]models.APIKey, error) {
	var query string
	var args []any
	if namespaceID != nil {
		query = `SELECT id, key_hash, name, permissions, namespace_id, created_at FROM api_keys WHERE namespace_id = $1`
		args = []any{*namespaceID}
	} else {
		query = `SELECT id, key_hash, name, permissions, namespace_id, created_at FROM api_keys`
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	var keys []models.APIKey
	for rows.Next() {
		var k models.APIKey
		if err := rows.Scan(&k.ID, &k.KeyHash, &k.Name, &k.Permissions, &k.NamespaceID, &k.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}
```

Update `GetAPIKeyByID` — add namespace_id to SELECT/Scan:
```go
func (s *Store) GetAPIKeyByID(ctx context.Context, id string) (*models.APIKey, error) {
	var k models.APIKey
	err := s.pool.QueryRow(ctx,
		`SELECT id, key_hash, name, permissions, namespace_id, created_at FROM api_keys WHERE id = $1`,
		id,
	).Scan(&k.ID, &k.KeyHash, &k.Name, &k.Permissions, &k.NamespaceID, &k.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get api key: %w", err)
	}
	return &k, nil
}
```

- [ ] **Step 5: Update notifications.go for namespace_id**

In `internal/store/postgres/notifications.go`:

Update `CreateNotification` — add `namespace_id` to INSERT:
```go
func (s *Store) CreateNotification(ctx context.Context, n *models.Notification) (*models.Notification, error) {
	err := s.pool.QueryRow(ctx,
		`INSERT INTO notifications
			(id, namespace_id, tenant_id, user_id, type_id, group_id, title, body, action_url, action_label, idempotency_key, channels, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		 RETURNING created_at`,
		n.ID, n.NamespaceID, n.TenantID, n.UserID, n.TypeID, n.GroupID,
		n.Title, n.Body, n.ActionURL, n.ActionLabel,
		n.IdempotencyKey, n.Channels, n.Status,
	).Scan(&n.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create notification: %w", err)
	}
	return n, nil
}
```

Update all SELECT/Scan calls in `GetNotificationByID` and `GetNotificationByIdempotencyKey` to include `namespace_id` after `id` in the column list and scan into `&n.NamespaceID` after `&n.ID`.

- [ ] **Step 6: Update inbox.go for namespace_id**

In `internal/store/postgres/inbox.go`:

Update `ListInbox` SELECT columns to include `namespace_id` after `id`, and scan into `&n.NamespaceID` after `&n.ID`. Same for all other queries that SELECT from notifications.

- [ ] **Step 7: Verify it compiles**

Run: `go build ./...`
Expected: FAIL — callers in `internal/admin/`, `internal/dispatch/`, `internal/inbox/` have not been updated yet. That's expected; we fix them in subsequent tasks.

Actually, this is the compilation coupling problem. We need to update all callers in the same commit. Let's proceed to update the admin and dispatch callers minimally.

- [ ] **Step 8: Update admin callers to match new signatures**

In `internal/admin/handler_types.go`:
- `s.store.ListTypes(ctx)` → `s.store.ListTypes(ctx, "ns_default")` (temporary hardcode, replaced in Task 6)
- `s.store.GetTypeBySlug(ctx, req.Type)` → needs namespace; temporarily pass `"ns_default"`

In `internal/admin/handler_send.go`:
- `s.store.GetTypeBySlug(ctx, req.Type)` → `s.store.GetTypeBySlug(ctx, "ns_default", req.Type)` (temporary, replaced in Task 7)

In `internal/admin/handler_apikeys.go`:
- `s.store.CreateAPIKey(ctx, keyID, keyHash, input.Body.Name, permissions)` → add `, nil` for namespaceID
- `s.store.ListAPIKeys(ctx)` → `s.store.ListAPIKeys(ctx, nil)`

In `internal/admin/server.go` `AdminStore` interface — update signatures to match new store interfaces.

In `internal/admin/testutil_test.go` mockStore — update method signatures and add namespace methods (stubs that return the default namespace).

In `internal/dispatch/template.go`:
- `tr.store.GetTypeBySlug(ctx, slug)` → `tr.store.GetTypeBySlug(ctx, "ns_default", slug)` (temporary, replaced in Task 9)

- [ ] **Step 9: Verify it compiles and tests pass**

Run: `go build ./...`
Expected: PASS

Run: `make test`
Expected: All unit tests pass

- [ ] **Step 10: Commit**

```bash
git add internal/store/ internal/models/models.go internal/admin/ internal/dispatch/template.go
git commit -m "feat: add namespace store layer — models, interfaces, postgres implementation

Adds Namespace entity, NamespaceID fields to APIKey/NotificationType/Notification.
All callers temporarily use ns_default; proper namespace resolution follows."
```

---

### Task 4: Auth Layer — ValidatedKey and Permission Changes

**Files:**
- Modify: `internal/auth/permissions.go`

- [ ] **Step 1: Write tests for new auth behavior**

Create or update `internal/auth/permissions_test.go`:

```go
func TestHasPermission_NamespacesManage(t *testing.T) {
	key := &auth.ValidatedKey{
		ID:          "key_test",
		Permissions: []string{auth.PermNamespacesManage},
	}
	if !auth.HasPermission(key, auth.PermNamespacesManage) {
		t.Error("expected namespaces:manage permission")
	}
}

func TestValidatePermissions_NamespacesManage(t *testing.T) {
	err := auth.ValidatePermissions([]string{auth.PermNamespacesManage})
	if err != nil {
		t.Errorf("expected namespaces:manage to be valid, got: %v", err)
	}
}

func TestIsGlobalKey(t *testing.T) {
	global := &auth.ValidatedKey{ID: "key_1", NamespaceID: nil}
	if !auth.IsGlobalKey(global) {
		t.Error("nil namespace should be global")
	}

	ns := "ns_billing"
	scoped := &auth.ValidatedKey{ID: "key_2", NamespaceID: &ns}
	if auth.IsGlobalKey(scoped) {
		t.Error("non-nil namespace should not be global")
	}
}

func TestEffectiveNamespaceID(t *testing.T) {
	ns := "ns_billing"
	scoped := &auth.ValidatedKey{ID: "key_1", NamespaceID: &ns}
	if got := auth.EffectiveNamespaceID(scoped, "ns_other"); got != "ns_billing" {
		t.Errorf("scoped key should use key namespace, got %s", got)
	}

	global := &auth.ValidatedKey{ID: "key_2", NamespaceID: nil}
	if got := auth.EffectiveNamespaceID(global, "ns_explicit"); got != "ns_explicit" {
		t.Errorf("global key should use explicit namespace, got %s", got)
	}

	if got := auth.EffectiveNamespaceID(global, ""); got != "ns_default" {
		t.Errorf("global key with no explicit should default, got %s", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/auth/... -run "TestHasPermission_NamespacesManage|TestIsGlobalKey|TestEffectiveNamespaceID" -v`
Expected: FAIL — functions don't exist yet

- [ ] **Step 3: Implement auth changes**

In `internal/auth/permissions.go`:

Add the new permission constant:
```go
const (
	PermAPIKeysManage     = "apikeys:manage"
	PermNamespacesManage  = "namespaces:manage"  // NEW
	PermNotificationsSend = "notifications:send"
	PermTemplatesManage   = "templates:manage"
	PermTenantsManage     = "tenants:manage"
)

var AllPermissions = []string{
	PermAPIKeysManage,
	PermNamespacesManage,  // NEW
	PermNotificationsSend,
	PermTemplatesManage,
	PermTenantsManage,
}
```

Add `NamespaceID` to `ValidatedKey`:
```go
type ValidatedKey struct {
	ID          string
	Permissions []string
	NamespaceID *string  // nil = global key
}
```

Add namespace helper constants and functions:
```go
const DefaultNamespaceID = "ns_default"

// IsGlobalKey returns true if the key is not scoped to any namespace.
func IsGlobalKey(key *ValidatedKey) bool {
	return key == nil || key.NamespaceID == nil
}

// EffectiveNamespaceID returns the namespace to use for an operation.
// Scoped keys always use their namespace. Global keys use the explicit value, falling back to default.
func EffectiveNamespaceID(key *ValidatedKey, explicit string) string {
	if key != nil && key.NamespaceID != nil {
		return *key.NamespaceID
	}
	if explicit != "" {
		return explicit
	}
	return DefaultNamespaceID
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/auth/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/auth/permissions.go internal/auth/permissions_test.go
git commit -m "feat: add namespace permission, IsGlobalKey, and EffectiveNamespaceID helpers"
```

---

### Task 5: Update validateAPIKey to Populate NamespaceID

**Files:**
- Modify: `internal/admin/server.go`

- [ ] **Step 1: Update the cache struct and validateAPIKey**

In `internal/admin/server.go`, update the `validateAPIKey` method:

The cached JSON struct and the store return now include `NamespaceID`. Update both the cache read/write and the `ValidatedKey` construction:

```go
// In the cache hit branch:
var entry struct {
	KeyHash     string   `json:"key_hash"`
	Permissions []string `json:"permissions"`
	NamespaceID *string  `json:"namespace_id,omitempty"`  // NEW
}

// In the cache write branch:
entry, _ := json.Marshal(struct {
	KeyHash     string   `json:"key_hash"`
	Permissions []string `json:"permissions"`
	NamespaceID *string  `json:"namespace_id,omitempty"`  // NEW
}{keyHash, permissions, namespaceID})

// Return ValidatedKey with NamespaceID:
return &auth.ValidatedKey{ID: keyID, Permissions: permissions, NamespaceID: namespaceID}
```

You'll need a local `namespaceID *string` variable populated from either cache or store:

```go
var keyHash string
var permissions []string
var namespaceID *string  // NEW

// Cache hit path:
if cached != nil {
    // ... unmarshal ...
    keyHash = entry.KeyHash
    permissions = entry.Permissions
    namespaceID = entry.NamespaceID  // NEW
}

// Store path:
if keyHash == "" {
    k, err := s.store.GetAPIKeyByID(context.Background(), keyID)
    // ...
    keyHash = k.KeyHash
    permissions = k.Permissions
    namespaceID = k.NamespaceID  // NEW
    // cache write includes namespaceID
}
```

- [ ] **Step 2: Verify tests pass**

Run: `make test`
Expected: PASS (existing tests use skipAuth, so validateAPIKey changes don't break them)

- [ ] **Step 3: Commit**

```bash
git add internal/admin/server.go
git commit -m "feat: populate NamespaceID on ValidatedKey from API key lookup"
```

---

### Task 6: Namespace CRUD Admin Handler

**Files:**
- Create: `internal/admin/handler_namespaces.go`
- Modify: `internal/admin/server.go` (add to AdminStore, register routes)
- Modify: `internal/admin/testutil_test.go` (mock namespace methods)
- Create: `internal/admin/handler_namespaces_test.go`

- [ ] **Step 1: Add namespace methods to AdminStore interface**

In `internal/admin/server.go`, add to the `AdminStore` interface:

```go
// Namespaces
CreateNamespace(ctx context.Context, id, slug, name string) (*models.Namespace, error)
GetNamespaceByID(ctx context.Context, id string) (*models.Namespace, error)
GetNamespaceBySlug(ctx context.Context, slug string) (*models.Namespace, error)
ListNamespaces(ctx context.Context) ([]models.Namespace, error)
UpdateNamespace(ctx context.Context, id, name string, settings []byte) (*models.Namespace, error)
```

- [ ] **Step 2: Add mock namespace methods to testutil_test.go**

In `internal/admin/testutil_test.go`, add to `mockStore`:

```go
// Add field:
namespaces []models.Namespace

// Add methods:
func (m *mockStore) CreateNamespace(ctx context.Context, id, slug, name string) (*models.Namespace, error) {
	for _, ns := range m.namespaces {
		if ns.Slug == slug {
			return nil, fmt.Errorf("namespace slug already exists: %s", slug)
		}
	}
	ns := models.Namespace{
		ID: id, Slug: slug, Name: name,
		Settings:  []byte("{}"),
		CreatedAt: time.Now(),
	}
	m.namespaces = append(m.namespaces, ns)
	return &ns, nil
}

func (m *mockStore) GetNamespaceByID(ctx context.Context, id string) (*models.Namespace, error) {
	for _, ns := range m.namespaces {
		if ns.ID == id {
			return &ns, nil
		}
	}
	return nil, fmt.Errorf("namespace not found: %s", id)
}

func (m *mockStore) GetNamespaceBySlug(ctx context.Context, slug string) (*models.Namespace, error) {
	for _, ns := range m.namespaces {
		if ns.Slug == slug {
			return &ns, nil
		}
	}
	return nil, fmt.Errorf("namespace not found: %s", slug)
}

func (m *mockStore) ListNamespaces(ctx context.Context) ([]models.Namespace, error) {
	return m.namespaces, nil
}

func (m *mockStore) UpdateNamespace(ctx context.Context, id, name string, settings []byte) (*models.Namespace, error) {
	for i, ns := range m.namespaces {
		if ns.ID == id {
			m.namespaces[i].Name = name
			if settings != nil {
				m.namespaces[i].Settings = settings
			}
			updated := m.namespaces[i]
			return &updated, nil
		}
	}
	return nil, fmt.Errorf("namespace not found: %s", id)
}
```

Also seed the default namespace in `newTestServer`:
```go
store := &mockStore{
	tenants: []models.Tenant{
		{ID: "test-tenant-id", Name: "Test Tenant", CreatedAt: time.Now()},
	},
	namespaces: []models.Namespace{
		{ID: "ns_default", Slug: "default", Name: "Default", Settings: []byte("{}"), CreatedAt: time.Now()},
	},
}
```

- [ ] **Step 3: Write handler_namespaces_test.go**

```go
package admin_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hermes-notifications/hermes/internal/models"
)

func TestHandleCreateNamespace(t *testing.T) {
	srv := newTestServer(t)
	body := `{"slug":"billing","name":"Billing App"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/namespaces", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var ns models.Namespace
	json.NewDecoder(rec.Body).Decode(&ns)
	if ns.Slug != "billing" {
		t.Errorf("expected slug 'billing', got %q", ns.Slug)
	}
	if ns.ID == "" {
		t.Error("expected non-empty ID")
	}
}

func TestHandleListNamespaces(t *testing.T) {
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/namespaces", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var namespaces []models.Namespace
	json.NewDecoder(rec.Body).Decode(&namespaces)
	if len(namespaces) < 1 {
		t.Fatal("expected at least 1 namespace (default)")
	}
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test ./internal/admin/... -run "TestHandleCreateNamespace|TestHandleListNamespaces" -v`
Expected: FAIL — handler not registered yet

- [ ] **Step 5: Create handler_namespaces.go**

```go
package admin

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hermes-notifications/hermes/internal/auth"
	id "github.com/hermes-notifications/hermes/internal/id/v2"
	"github.com/hermes-notifications/hermes/internal/models"
)

var namespaceIDGen = id.NewGenerator(id.Config{Prefix: "ns", RandBits: 80})

type createNamespaceInput struct {
	Body struct {
		Slug string `json:"slug" required:"true" minLength:"1" doc:"URL-friendly identifier (immutable)"`
		Name string `json:"name" required:"true" minLength:"1" doc:"Human-readable name"`
	}
}

type updateNamespaceInput struct {
	ID string `path:"id" doc:"Namespace ID"`
	Body struct {
		Name     string `json:"name" doc:"Human-readable name"`
		Settings []byte `json:"settings,omitempty" doc:"Namespace settings (JSON)"`
	}
}

type namespaceOutput struct {
	Body models.Namespace
}

type namespaceListOutput struct {
	Body []models.Namespace
}

func (s *Server) registerNamespaceRoutes() {
	huma.Register(s.api, huma.Operation{
		OperationID: "list-namespaces",
		Method:      http.MethodGet,
		Path:        "/v1/namespaces",
		Summary:     "List all namespaces",
		Tags:        []string{"Namespaces"},
	}, func(ctx context.Context, input *struct{}) (*namespaceListOutput, error) {
		key := auth.GetValidatedKey(ctx)
		if key != nil && !auth.HasPermission(key, auth.PermNamespacesManage) {
			return nil, huma.Error403Forbidden("insufficient permissions")
		}
		namespaces, err := s.store.ListNamespaces(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal server error")
		}
		return &namespaceListOutput{Body: namespaces}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID:   "create-namespace",
		Method:        http.MethodPost,
		Path:          "/v1/namespaces",
		Summary:       "Create a namespace",
		Tags:          []string{"Namespaces"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, input *createNamespaceInput) (*namespaceOutput, error) {
		key := auth.GetValidatedKey(ctx)
		if key != nil {
			if !auth.HasPermission(key, auth.PermNamespacesManage) {
				return nil, huma.Error403Forbidden("insufficient permissions")
			}
			if !auth.IsGlobalKey(key) {
				return nil, huma.Error403Forbidden("only global API keys can manage namespaces")
			}
		}
		ns, err := s.store.CreateNamespace(ctx, namespaceIDGen.New(), input.Body.Slug, input.Body.Name)
		if err != nil {
			s.logger.Error("failed to create namespace", "error", err, "slug", input.Body.Slug)
			return nil, huma.Error500InternalServerError("internal server error")
		}
		return &namespaceOutput{Body: *ns}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID: "get-namespace",
		Method:      http.MethodGet,
		Path:        "/v1/namespaces/{id}",
		Summary:     "Get a namespace by ID",
		Tags:        []string{"Namespaces"},
	}, func(ctx context.Context, input *struct {
		ID string `path:"id" doc:"Namespace ID"`
	}) (*namespaceOutput, error) {
		key := auth.GetValidatedKey(ctx)
		if key != nil && !auth.HasPermission(key, auth.PermNamespacesManage) {
			return nil, huma.Error403Forbidden("insufficient permissions")
		}
		ns, err := s.store.GetNamespaceByID(ctx, input.ID)
		if err != nil {
			return nil, huma.Error404NotFound("namespace not found")
		}
		return &namespaceOutput{Body: *ns}, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID: "update-namespace",
		Method:      http.MethodPut,
		Path:        "/v1/namespaces/{id}",
		Summary:     "Update a namespace",
		Tags:        []string{"Namespaces"},
	}, func(ctx context.Context, input *updateNamespaceInput) (*namespaceOutput, error) {
		key := auth.GetValidatedKey(ctx)
		if key != nil {
			if !auth.HasPermission(key, auth.PermNamespacesManage) {
				return nil, huma.Error403Forbidden("insufficient permissions")
			}
			if !auth.IsGlobalKey(key) {
				return nil, huma.Error403Forbidden("only global API keys can manage namespaces")
			}
		}
		ns, err := s.store.UpdateNamespace(ctx, input.ID, input.Body.Name, input.Body.Settings)
		if err != nil {
			return nil, huma.Error404NotFound("namespace not found")
		}
		return &namespaceOutput{Body: *ns}, nil
	})
}
```

- [ ] **Step 6: Register namespace routes in server.go**

In `internal/admin/server.go` `routes()`, add:
```go
s.registerNamespaceRoutes()
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/admin/... -run "TestHandleCreateNamespace|TestHandleListNamespaces" -v`
Expected: PASS

Run: `make test`
Expected: All tests pass

- [ ] **Step 8: Commit**

```bash
git add internal/admin/handler_namespaces.go internal/admin/handler_namespaces_test.go internal/admin/server.go internal/admin/testutil_test.go
git commit -m "feat: add namespace CRUD endpoints — POST/GET/LIST/PUT /v1/namespaces"
```

---

### Task 7: Namespace-Scoped API Key Creation and Listing

**Files:**
- Modify: `internal/admin/handler_apikeys.go`
- Modify: `internal/admin/handler_apikeys_test.go`

- [ ] **Step 1: Write tests for namespace-scoped API key behavior**

Add to `internal/admin/handler_apikeys_test.go`:

```go
func TestHandleCreateAPIKey_WithNamespace(t *testing.T) {
	srv := newTestServer(t)

	body := `{"name":"Billing Key","permissions":["notifications:send"],"namespace_id":"ns_default"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/apikeys", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		NamespaceID *string `json:"namespace_id"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.NamespaceID == nil || *resp.NamespaceID != "ns_default" {
		t.Fatalf("expected namespace_id 'ns_default', got %v", resp.NamespaceID)
	}
}

func TestHandleCreateAPIKey_InvalidNamespace(t *testing.T) {
	srv := newTestServer(t)

	body := `{"name":"Bad Key","permissions":["notifications:send"],"namespace_id":"ns_nonexistent"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/apikeys", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/admin/... -run "TestHandleCreateAPIKey_WithNamespace|TestHandleCreateAPIKey_InvalidNamespace" -v`
Expected: FAIL

- [ ] **Step 3: Update handler_apikeys.go**

Add `NamespaceID` to the create input:
```go
type createAPIKeyInput struct {
	Body struct {
		Name        string   `json:"name" required:"true" minLength:"1" doc:"Human-readable key name"`
		Permissions []string `json:"permissions,omitempty" doc:"Permission set (defaults to all except apikeys:manage)"`
		NamespaceID *string  `json:"namespace_id,omitempty" doc:"Namespace to scope this key to (omit for global key)"`
	}
}
```

Add `NamespaceID` to the created output:
```go
type apiKeyCreatedOutput struct {
	Body struct {
		ID          string    `json:"id"`
		Name        string    `json:"name"`
		RawKey      string    `json:"raw_key"`
		Permissions []string  `json:"permissions"`
		NamespaceID *string   `json:"namespace_id,omitempty"`
		CreatedAt   time.Time `json:"created_at"`
	}
}
```

Add `NamespaceID` to the list output items:
```go
type listAPIKeysOutput struct {
	Body []struct {
		ID          string    `json:"id"`
		Name        string    `json:"name"`
		Permissions []string  `json:"permissions"`
		NamespaceID *string   `json:"namespace_id,omitempty"`
		CreatedAt   time.Time `json:"created_at"`
	}
}
```

In the create handler, after permission validation, add namespace validation and enforcement:
```go
// Validate namespace exists if provided
nsID := input.Body.NamespaceID
if nsID != nil {
	if _, err := s.store.GetNamespaceByID(ctx, *nsID); err != nil {
		return nil, huma.Error400BadRequest("unknown namespace_id")
	}
}

// Enforce: scoped key can only create keys for same namespace
key := auth.GetValidatedKey(ctx)
if key != nil && !auth.IsGlobalKey(key) {
	// Scoped key — can only create keys for its own namespace
	if nsID == nil || *nsID != *key.NamespaceID {
		return nil, huma.Error403Forbidden("namespace-scoped keys can only create keys for their own namespace")
	}
}
```

Pass `nsID` to the store call:
```go
k, err := s.store.CreateAPIKey(ctx, keyID, keyHash, input.Body.Name, permissions, nsID)
```

Set `NamespaceID` on response:
```go
out.Body.NamespaceID = k.NamespaceID
```

In the list handler, scope by namespace for scoped keys:
```go
key := auth.GetValidatedKey(ctx)
var nsFilter *string
if key != nil && !auth.IsGlobalKey(key) {
	nsFilter = key.NamespaceID
}
keys, err := s.store.ListAPIKeys(ctx, nsFilter)
```

Add `NamespaceID` to list output mapping:
```go
NamespaceID: k.NamespaceID,
```

- [ ] **Step 4: Update mockStore.CreateAPIKey signature**

In `testutil_test.go`, update:
```go
func (m *mockStore) CreateAPIKey(ctx context.Context, id, keyHash, name string, permissions []string, namespaceID *string) (*models.APIKey, error) {
	k := models.APIKey{
		ID: id, KeyHash: keyHash, Name: name, Permissions: permissions,
		NamespaceID: namespaceID, CreatedAt: time.Now(),
	}
	m.apiKeys = append(m.apiKeys, k)
	return &k, nil
}

func (m *mockStore) ListAPIKeys(ctx context.Context, namespaceID *string) ([]models.APIKey, error) {
	if namespaceID == nil {
		return m.apiKeys, nil
	}
	var filtered []models.APIKey
	for _, k := range m.apiKeys {
		if k.NamespaceID != nil && *k.NamespaceID == *namespaceID {
			filtered = append(filtered, k)
		}
	}
	return filtered, nil
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/admin/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/admin/handler_apikeys.go internal/admin/handler_apikeys_test.go internal/admin/testutil_test.go
git commit -m "feat: namespace-scoped API key creation and listing"
```

---

### Task 8: Namespace-Scoped Type Handlers

**Files:**
- Modify: `internal/admin/handler_types.go`
- Modify: `internal/admin/handler_types_test.go` (if it exists)

- [ ] **Step 1: Update type handlers to use effective namespace**

In `internal/admin/handler_types.go`:

Update list handler to use the effective namespace from the API key:
```go
func(ctx context.Context, input *struct{}) (*typeListOutput, error) {
	key := auth.GetValidatedKey(ctx)
	nsID := auth.EffectiveNamespaceID(key, "")
	types, err := s.store.ListTypes(ctx, nsID)
	// ...
}
```

Update create handler to set namespace_id on the type:
```go
func(ctx context.Context, input *createTypeInput) (*typeOutput, error) {
	key := auth.GetValidatedKey(ctx)
	nsID := auth.EffectiveNamespaceID(key, "")
	nt, err := s.store.CreateType(ctx, &models.NotificationType{
		NamespaceID: nsID,  // NEW
		GroupID: input.Body.GroupID, Slug: input.Body.Slug, Name: input.Body.Name,
		// ... rest unchanged
	})
	// ...
}
```

- [ ] **Step 2: Verify tests pass**

Run: `go test ./internal/admin/... -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/admin/handler_types.go
git commit -m "feat: namespace-scoped type CRUD — list and create use effective namespace"
```

---

### Task 9: Namespace-Aware Send Handler

**Files:**
- Modify: `internal/admin/handler_send.go`
- Modify: `internal/admin/handler_send_test.go`

- [ ] **Step 1: Update send handler to resolve namespace**

In `internal/admin/handler_send.go`:

After the tenant validation, resolve the effective namespace:
```go
// Resolve namespace from API key
key := auth.GetValidatedKey(ctx)
nsID := auth.EffectiveNamespaceID(key, "")
```

Update type resolution to use namespace:
```go
if req.Type != "" {
	nt, err := s.store.GetTypeBySlug(ctx, nsID, req.Type)
	// ...
}
```

Set namespace on the notification model:
```go
n := &models.Notification{
	ID:          notifID,
	NamespaceID: nsID,  // NEW
	TenantID:    req.TenantID,
	// ... rest unchanged
}
```

Include namespace_id in the NATS publish:
```go
msg := map[string]any{
	"notification_id": notifID,
	"namespace_id":    nsID,  // NEW
	"tenant_id":       req.TenantID,
	// ... rest unchanged
}
```

Add namespace to the send log line (if any):
```go
s.logger.Error("failed to publish to NATS", "error", err,
	"notification_id", notifID,
	"namespace_id", nsID,  // NEW
)
```

- [ ] **Step 2: Update send test to pass**

The existing `TestHandleSend_DirectContent` test should still pass since the test server has a default namespace seeded and `EffectiveNamespaceID` returns `ns_default` when the key is nil (skipAuth mode).

Run: `go test ./internal/admin/... -run TestHandleSend -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/admin/handler_send.go
git commit -m "feat: tag notifications with namespace from API key on send"
```

---

### Task 10: NATS Message NamespaceID Field

**Files:**
- Modify: `internal/nats/messages.go`

- [ ] **Step 1: Add NamespaceID to SendMessage and DeliveryMessage**

In `internal/nats/messages.go`:

```go
type SendMessage struct {
	NotificationID string          `json:"notification_id"`
	NamespaceID    string          `json:"namespace_id"`  // NEW — after notification_id
	TenantID       string          `json:"tenant_id"`
	// ... rest unchanged
}

type DeliveryMessage struct {
	NotificationID string          `json:"notification_id"`
	NamespaceID    string          `json:"namespace_id"`  // NEW — after notification_id
	TenantID       string          `json:"tenant_id"`
	// ... rest unchanged
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/nats/...`
Expected: PASS (adding a JSON field is wire-compatible)

- [ ] **Step 3: Commit**

```bash
git add internal/nats/messages.go
git commit -m "feat: add NamespaceID field to SendMessage and DeliveryMessage"
```

---

### Task 11: Dispatch Namespace Pass-Through and Logging

**Files:**
- Modify: `internal/dispatch/dispatch.go`
- Modify: `internal/dispatch/template.go`

- [ ] **Step 1: Update dispatch to propagate NamespaceID**

In `internal/dispatch/dispatch.go` `handleSend`:

Add namespace to log context:
```go
log := d.logger.With("notification_id", msg.NotificationID, "namespace_id", msg.NamespaceID)
```

Pass namespace to template resolver:
```go
nt, err = d.templateResolver.Resolve(ctx, msg.NamespaceID, msg.Metadata.Type)
```

Set NamespaceID on DeliveryMessage:
```go
dm := &hermenats.DeliveryMessage{
	NotificationID: msg.NotificationID,
	NamespaceID:    msg.NamespaceID,  // NEW
	TenantID:       msg.TenantID,
	// ... rest unchanged
}
```

- [ ] **Step 2: Update TemplateResolver.Resolve for namespace-scoped lookup**

In `internal/dispatch/template.go`:

Change `Resolve` signature:
```go
func (tr *TemplateResolver) Resolve(ctx context.Context, namespaceID, slug string) (*models.NotificationType, error) {
```

Update cache key to include namespace:
```go
cacheKey := namespaceID + ":" + slug
if tr.cache != nil {
	data, err := tr.cache.GetTypeConfig(ctx, cacheKey)
	// ...
}
```

Update store call:
```go
nt, err := tr.store.GetTypeBySlug(ctx, namespaceID, slug)
```

Update cache write:
```go
tr.cache.SetTypeConfig(ctx, cacheKey, data, 5*time.Minute)
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./internal/dispatch/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/dispatch/dispatch.go internal/dispatch/template.go
git commit -m "feat: dispatch passes NamespaceID through pipeline, namespace-scoped template cache"
```

---

### Task 12: Worker Namespace Logging

**Files:**
- Modify: `internal/delivery/worker.go`

- [ ] **Step 1: Add namespace_id to worker log context**

In `internal/delivery/worker.go` `handleMessage`, after unmarshaling:

```go
func (w *Worker) handleMessage(ctx context.Context, data []byte) error {
	msg, err := hermenats.UnmarshalDelivery(data)
	if err != nil {
		w.logger.Error("unmarshal delivery", "error", err)
		return nil
	}

	log := w.logger.With("notification_id", msg.NotificationID, "namespace_id", msg.NamespaceID, "channel", w.channel)

	// Use log instead of w.logger for all subsequent log calls in this method
	// ...
}
```

Replace `w.logger.Error(...)` and `w.logger.Info(...)` calls in `handleMessage` with `log.Error(...)` and `log.Info(...)`.

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/delivery/...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/delivery/worker.go
git commit -m "feat: add namespace_id to delivery worker structured logs"
```

---

### Task 13: Inbox Namespace Filter

**Files:**
- Modify: `internal/inbox/handler_list.go`
- Modify: `internal/inbox/server.go` (InboxStore interface if ListInbox signature changes)
- Modify: `internal/store/postgres/inbox.go`
- Modify: `internal/store/interfaces.go` (InboxRepository)

- [ ] **Step 1: Add optional namespace query parameter to inbox list**

In `internal/inbox/handler_list.go`, update the input struct:
```go
type listInboxInput struct {
	Archived  bool   `query:"archived" default:"false" doc:"Filter archived notifications"`
	NamespaceID string `query:"namespace_id" doc:"Filter by namespace ID"`  // NEW
	Cursor    string `query:"cursor" doc:"Pagination cursor"`
	Limit     int    `query:"limit" default:"20" minimum:"1" maximum:"100" doc:"Page size"`
}
```

In the handler, resolve namespace slug to ID and pass to store:
```go
notifications, unreadCount, nextCursor, err := s.store.ListInbox(ctx, userID, input.Archived, input.NamespaceID, input.Cursor, input.Limit)
```

- [ ] **Step 2: Update ListInbox store signature and implementation**

In `internal/store/interfaces.go`, update `InboxRepository`:
```go
ListInbox(ctx context.Context, userID string, archived bool, namespaceID string, cursor string, limit int) ([]models.Notification, int, string, error)
```

In `internal/store/postgres/inbox.go`, add the namespace filter:
```go
func (s *Store) ListInbox(ctx context.Context, userID string, archived bool, namespaceID string, cursor string, limit int) ([]models.Notification, int, string, error) {
	// ... existing code ...

	// Add namespace filter if provided
	if namespaceID != "" {
		query += fmt.Sprintf(` AND namespace_id = $%d`, argIdx)
		args = append(args, namespaceID)
		argIdx++
	}

	// ... rest of query building ...
}
```

Update all Scan calls in `ListInbox` to include `&n.NamespaceID` after `&n.ID`.

- [ ] **Step 3: Update InboxStore interface in inbox/server.go**

```go
ListInbox(ctx context.Context, userID string, archived bool, namespaceID string, cursor string, limit int) ([]models.Notification, int, string, error)
```

- [ ] **Step 4: Update all callers of ListInbox**

Any test mocks or other callers need the updated signature. Pass `""` for the namespace filter where not filtering.

- [ ] **Step 5: Verify tests pass**

Run: `make test`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/inbox/ internal/store/interfaces.go internal/store/postgres/inbox.go
git commit -m "feat: add optional namespace filter to inbox list endpoint"
```

---

### Task 14: Admin Logging with Namespace Context

**Files:**
- Modify: `internal/admin/server.go` (logging middleware or handler-level logging)

- [ ] **Step 1: Add namespace_id to admin request logging**

In `internal/middleware/logging.go`, the logging middleware doesn't have access to the validated key context. Instead, add namespace logging at the handler level where it matters most.

The send handler (Task 9) already logs namespace_id on error. Review other handlers and add `"namespace_id", nsID` to any error log calls in `handler_types.go` and `handler_apikeys.go` where namespace context is available.

In `handler_types.go` create handler error log:
```go
s.logger.Error("failed to create type", "error", err, "slug", input.Body.Slug, "group_id", input.Body.GroupID, "namespace_id", nsID)
```

- [ ] **Step 2: Verify tests pass**

Run: `make test`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/admin/handler_types.go internal/admin/handler_apikeys.go
git commit -m "feat: add namespace_id to structured log context in admin handlers"
```

---

### Task 15: Final Verification

- [ ] **Step 1: Run full unit test suite**

Run: `make test`
Expected: All tests pass

- [ ] **Step 2: Run linter**

Run: `make lint`
Expected: No new lint errors

- [ ] **Step 3: Run integration tests (if infra is up)**

Run: `make infra-up && make test-integration`
Expected: All integration tests pass (existing tests work with default namespace)

- [ ] **Step 4: Verify migration up and down**

Run: `make migrate` (up)
Run migration down to verify rollback works cleanly

---

## Verification Checklist

- [ ] `namespaces` table exists with `ns_default` row after migration
- [ ] `POST /v1/namespaces` creates a new namespace (requires global key + `namespaces:manage`)
- [ ] `GET /v1/namespaces` lists all namespaces
- [ ] `POST /v1/apikeys` with `namespace_id` creates a scoped key
- [ ] Scoped API key can only create keys for its own namespace
- [ ] `POST /v1/send` with a scoped key tags notification with key's namespace
- [ ] `POST /v1/send` with a global key defaults to `ns_default` namespace
- [ ] Type resolution is namespace-scoped (same slug in different namespaces resolves correctly)
- [ ] `GET /v1/inbox?namespace=billing` filters notifications by namespace
- [ ] `GET /v1/inbox` without namespace filter returns all notifications
- [ ] NATS messages carry `namespace_id` through the pipeline
- [ ] Structured logs include `namespace_id` in admin, dispatch, and worker services
- [ ] All existing tests pass without modification (backward compatible via `ns_default`)
