# Phase 1: Foundation + Admin Service Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Set up the Go monorepo with shared infrastructure packages, database migrations, and the Admin service — producing a working API that can receive notification sends, manage types/groups, and publish to NATS.

**Architecture:** Single Go module monorepo. Admin service uses stdlib `net/http` for HTTP routing. Shared packages in `internal/` handle database (Postgres via pgx), messaging (NATS JetStream), caching (Redis), auth (API key with argon2), and ID generation (Crockford Base32). All database operations go through a shared `store` package since all services share one Postgres database.

**Tech Stack:** Go 1.22+, PostgreSQL (pgx), NATS JetStream (nats.go), Redis (go-redis), golang-migrate, argon2 (x/crypto), Docker Compose for local dev.

**Spec:** `docs/superpowers/specs/2026-03-20-hermes-notification-service-design.md`

---

## File Structure

```
hermes/
├── go.mod
├── go.sum
├── docker-compose.yml                    # Postgres, NATS, Redis for local dev
├── cmd/
│   └── admin/
│       └── main.go                       # Entry point, wires deps, starts HTTP server
├── internal/
│   ├── id/
│   │   ├── id.go                         # Crockford Base32 time-sortable ID generation
│   │   └── id_test.go
│   ├── config/
│   │   ├── config.go                     # Config loading from environment variables
│   │   └── config_test.go
│   ├── database/
│   │   ├── database.go                   # Postgres connection pool (pgx)
│   │   └── database_test.go
│   ├── messaging/
│   │   ├── nats.go                       # NATS JetStream client, stream/consumer setup
│   │   └── nats_test.go
│   ├── cache/
│   │   ├── redis.go                      # Redis client wrapper
│   │   └── redis_test.go
│   ├── auth/
│   │   ├── apikey.go                     # API key hashing + validation middleware
│   │   └── apikey_test.go
│   ├── models/
│   │   ├── models.go                     # All domain types: Tenant, User, Notification, Group, Type, Event, Preference
│   │   └── status.go                     # Status enum + rank function
│   ├── store/
│   │   ├── store.go                      # Store struct wrapping pgx pool
│   │   ├── tenants.go                    # Tenant CRUD
│   │   ├── tenants_test.go
│   │   ├── users.go                      # User upsert + lookup
│   │   ├── users_test.go
│   │   ├── groups.go                     # Group CRUD
│   │   ├── groups_test.go
│   │   ├── types.go                      # Notification type CRUD
│   │   ├── types_test.go
│   │   ├── notifications.go             # Notification insert + status query
│   │   ├── notifications_test.go
│   │   └── testutil_test.go             # Shared test helpers (DB setup/teardown)
│   ├── middleware/
│   │   ├── logging.go                    # Structured JSON request logging
│   │   ├── recovery.go                   # Panic recovery
│   │   └── ratelimit.go                  # Token bucket rate limiter per API key
│   └── admin/
│       ├── server.go                     # HTTP server, route registration, deps
│       ├── handler_send.go               # POST /v1/send
│       ├── handler_send_test.go
│       ├── handler_groups.go             # GET/POST/PUT /v1/groups
│       ├── handler_groups_test.go
│       ├── handler_types.go              # GET/POST/PUT/DELETE /v1/types
│       ├── handler_types_test.go
│       ├── handler_notifications.go      # GET /v1/notifications/:id
│       ├── handler_notifications_test.go
│       └── handler_health.go             # GET /healthz, /readyz
├── migrations/
│   ├── 000001_create_tenants.up.sql
│   ├── 000001_create_tenants.down.sql
│   ├── 000002_create_api_keys.up.sql
│   ├── 000002_create_api_keys.down.sql
│   ├── 000003_create_users.up.sql
│   ├── 000003_create_users.down.sql
│   ├── 000004_create_notification_groups.up.sql
│   ├── 000004_create_notification_groups.down.sql
│   ├── 000005_create_notification_types.up.sql
│   ├── 000005_create_notification_types.down.sql
│   ├── 000006_create_notifications.up.sql
│   ├── 000006_create_notifications.down.sql
│   ├── 000007_create_notification_events.up.sql
│   ├── 000007_create_notification_events.down.sql
│   ├── 000008_create_user_preferences.up.sql
│   └── 000008_create_user_preferences.down.sql
└── .gitignore
```

---

### Task 1: Go Module + Docker Compose + .gitignore

**Files:**
- Create: `go.mod`
- Create: `docker-compose.yml`
- Create: `.gitignore`

- [ ] **Step 1: Initialize Go module**

```bash
cd /Users/daryl/code/hermes
go mod init github.com/hermes-notifications/hermes
```

- [ ] **Step 2: Create docker-compose.yml**

```yaml
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: hermes
      POSTGRES_PASSWORD: hermes
      POSTGRES_DB: hermes
    ports:
      - "5432:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data

  nats:
    image: nats:2-alpine
    command: ["--jetstream", "--store_dir=/data"]
    ports:
      - "4222:4222"
      - "8222:8222"
    volumes:
      - natsdata:/data

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"

volumes:
  pgdata:
  natsdata:
```

- [ ] **Step 3: Create .gitignore**

```
# Binaries
/bin/
*.exe

# IDE
.idea/
.vscode/
*.swp

# OS
.DS_Store

# Superpowers
.superpowers/

# Environment
.env
.env.local
```

- [ ] **Step 4: Start Docker Compose and verify**

```bash
docker compose up -d
docker compose ps
```

Expected: all 3 services running (postgres, nats, redis).

- [ ] **Step 5: Commit**

```bash
git add go.mod docker-compose.yml .gitignore
git commit -m "feat: initialize Go module and Docker Compose for local dev"
```

---

### Task 2: ID Generation Package

**Files:**
- Create: `internal/id/id.go`
- Create: `internal/id/id_test.go`

Crockford Base32 time-sortable IDs: 48-bit ms timestamp + 80-bit random = 16 bytes, encoded to 26 chars.

- [ ] **Step 1: Write failing tests**

```go
// internal/id/id_test.go
package id_test

import (
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/internal/id"
)

func TestNew_Returns26CharString(t *testing.T) {
	got := id.New()
	if len(got) != 26 {
		t.Fatalf("expected 26 chars, got %d: %q", len(got), got)
	}
}

func TestNew_IsUnique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		v := id.New()
		if seen[v] {
			t.Fatalf("duplicate ID: %s", v)
		}
		seen[v] = true
	}
}

func TestNew_IsSortable(t *testing.T) {
	a := id.New()
	time.Sleep(2 * time.Millisecond)
	b := id.New()
	if a >= b {
		t.Fatalf("expected %s < %s", a, b)
	}
}

func TestNew_UsesValidCrockfordChars(t *testing.T) {
	const charset = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	valid := make(map[byte]bool)
	for i := 0; i < len(charset); i++ {
		valid[charset[i]] = true
	}
	for i := 0; i < 100; i++ {
		v := id.New()
		for j := 0; j < len(v); j++ {
			if !valid[v[j]] {
				t.Fatalf("invalid char %c in ID %s", v[j], v)
			}
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/daryl/code/hermes && go test ./internal/id/...
```

Expected: compilation error — package doesn't exist yet.

- [ ] **Step 3: Write implementation**

```go
// internal/id/id.go
package id

import (
	"crypto/rand"
	"encoding/binary"
	"time"
)

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// New generates a time-sortable Crockford Base32 ID.
// Layout: 48 bits ms timestamp + 80 bits random = 128 bits = 26 Crockford chars.
func New() string {
	var b [16]byte

	// 48-bit millisecond timestamp in the high bytes
	ms := uint64(time.Now().UnixMilli())
	binary.BigEndian.PutUint64(b[:8], ms<<16)

	// 80 bits of randomness: remaining 2 bytes of b[6:8] + b[8:16]
	// b[6] and b[7] already have the low 16 bits as zero from the shift, fill with random
	var rnd [10]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	b[6] = rnd[0]
	b[7] = rnd[1]
	copy(b[8:], rnd[2:])

	return encode(b[:])
}

func encode(src []byte) string {
	// 128 bits / 5 bits per char = 25.6 → 26 chars
	dst := make([]byte, 26)
	// Process 128 bits as a big integer, extracting 5 bits at a time from the top
	// Use uint64 pairs for the 128-bit value
	hi := binary.BigEndian.Uint64(src[:8])
	lo := binary.BigEndian.Uint64(src[8:])

	for i := 25; i >= 0; i-- {
		dst[i] = crockford[lo&0x1F]
		// Shift right by 5: lo gets low 5 bits of hi
		lo = (lo >> 5) | (hi << 59)
		hi >>= 5
	}
	return string(dst)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/daryl/code/hermes && go test ./internal/id/... -v
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/id/
git commit -m "feat: add Crockford Base32 time-sortable ID generation"
```

---

### Task 3: Config Package

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

Loads configuration from environment variables with sensible defaults.

- [ ] **Step 1: Write failing tests**

```go
// internal/config/config_test.go
package config_test

import (
	"testing"

	"github.com/hermes-notifications/hermes/internal/config"
)

func TestLoad_Defaults(t *testing.T) {
	cfg := config.Load()
	if cfg.DatabaseURL == "" {
		t.Fatal("expected default DatabaseURL")
	}
	if cfg.NATSUrl == "" {
		t.Fatal("expected default NATSUrl")
	}
	if cfg.RedisURL == "" {
		t.Fatal("expected default RedisURL")
	}
	if cfg.HTTPPort == 0 {
		t.Fatal("expected default HTTPPort")
	}
}

func TestLoad_OverrideFromEnv(t *testing.T) {
	t.Setenv("HERMES_HTTP_PORT", "9999")
	t.Setenv("HERMES_DATABASE_URL", "postgres://custom:5432/hermes")

	cfg := config.Load()
	if cfg.HTTPPort != 9999 {
		t.Fatalf("expected port 9999, got %d", cfg.HTTPPort)
	}
	if cfg.DatabaseURL != "postgres://custom:5432/hermes" {
		t.Fatalf("expected custom DB URL, got %s", cfg.DatabaseURL)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/daryl/code/hermes && go test ./internal/config/...
```

Expected: compilation error.

- [ ] **Step 3: Write implementation**

```go
// internal/config/config.go
package config

import (
	"os"
	"strconv"
)

type Config struct {
	HTTPPort    int
	DatabaseURL string
	NATSUrl     string
	RedisURL    string
}

func Load() Config {
	return Config{
		HTTPPort:    envInt("HERMES_HTTP_PORT", 8080),
		DatabaseURL: envStr("HERMES_DATABASE_URL", "postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable"),
		NATSUrl:     envStr("HERMES_NATS_URL", "nats://localhost:4222"),
		RedisURL:    envStr("HERMES_REDIS_URL", "redis://localhost:6379/0"),
	}
}

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/daryl/code/hermes && go test ./internal/config/... -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat: add config package with env variable loading"
```

---

### Task 4: Models Package

**Files:**
- Create: `internal/models/models.go`
- Create: `internal/models/status.go`

Domain types used across all services. No tests needed — these are plain data types plus a small status rank function.

- [ ] **Step 1: Write status rank test**

```go
// internal/models/status_test.go
package models_test

import (
	"testing"

	"github.com/hermes-notifications/hermes/internal/models"
)

func TestStatusRank(t *testing.T) {
	tests := []struct {
		status models.NotificationStatus
		rank   int
	}{
		{models.StatusPending, 0},
		{models.StatusSent, 1},
		{models.StatusDelivered, 2},
		{models.StatusRead, 3},
		{models.StatusArchived, 4},
	}
	for _, tt := range tests {
		if got := tt.status.Rank(); got != tt.rank {
			t.Errorf("StatusRank(%s) = %d, want %d", tt.status, got, tt.rank)
		}
	}
}

func TestStatusRank_CannotRegress(t *testing.T) {
	current := models.StatusDelivered
	incoming := models.StatusSent
	if incoming.Rank() >= current.Rank() {
		t.Fatal("sent should not be >= delivered")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/daryl/code/hermes && go test ./internal/models/...
```

Expected: compilation error.

- [ ] **Step 3: Write models**

```go
// internal/models/models.go
package models

import "time"

type Tenant struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	DefaultLocale string    `json:"default_locale,omitempty"`
	Settings      []byte    `json:"settings,omitempty"` // raw JSON
	CreatedAt     time.Time `json:"created_at"`
}

type APIKey struct {
	ID        string    `json:"id"`
	KeyHash   string    `json:"-"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type User struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenant_id"`
	ExternalID string    `json:"external_id"`
	Email      *string   `json:"email,omitempty"`
	Phone      *string   `json:"phone,omitempty"`
	Locale     *string   `json:"locale,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type NotificationGroup struct {
	ID              string    `json:"id"`
	Slug            string    `json:"slug"`
	Name            string    `json:"name"`
	DefaultChannels []string  `json:"default_channels"`
	CreatedAt       time.Time `json:"created_at"`
}

type NotificationType struct {
	ID           string    `json:"id"`
	GroupID      string    `json:"group_id"`
	Slug         string    `json:"slug"`
	Name         string    `json:"name"`
	EmailSubject *string   `json:"email_subject,omitempty"`
	EmailBody    *string   `json:"email_body,omitempty"`
	SMSBody      *string   `json:"sms_body,omitempty"`
	InboxTitle   *string   `json:"inbox_title,omitempty"`
	InboxBody    *string   `json:"inbox_body,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type Notification struct {
	ID             string             `json:"id"`
	TenantID       string             `json:"tenant_id"`
	UserID         string             `json:"user_id"`
	TypeID         *string            `json:"type_id,omitempty"`
	GroupID        string             `json:"group_id"`
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

type NotificationEvent struct {
	ID             string    `json:"id"`
	NotificationID string    `json:"notification_id"`
	Channel        string    `json:"channel"`
	Event          string    `json:"event"`
	Severity       string    `json:"severity"` // info, warn, error
	Metadata       []byte    `json:"metadata,omitempty"` // raw JSON
	CreatedAt      time.Time `json:"created_at"`
}

type UserPreference struct {
	UserID   string   `json:"user_id"`
	GroupID  string   `json:"group_id"`
	Channels []string `json:"channels"`
}
```

```go
// internal/models/status.go
package models

type NotificationStatus string

const (
	StatusPending   NotificationStatus = "pending"
	StatusSent      NotificationStatus = "sent"
	StatusDelivered NotificationStatus = "delivered"
	StatusRead      NotificationStatus = "read"
	StatusArchived  NotificationStatus = "archived"
)

var statusRanks = map[NotificationStatus]int{
	StatusPending:   0,
	StatusSent:      1,
	StatusDelivered: 2,
	StatusRead:      3,
	StatusArchived:  4,
}

func (s NotificationStatus) Rank() int {
	return statusRanks[s]
}

func (s NotificationStatus) String() string {
	return string(s)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/daryl/code/hermes && go test ./internal/models/... -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/models/
git commit -m "feat: add domain models and notification status with rank"
```

---

### Task 5: Database Package + Migrations

**Files:**
- Create: `internal/database/database.go`
- Create: `migrations/000001_create_tenants.up.sql` through `000008_create_user_preferences.down.sql`

The database package wraps pgx pool creation. Migrations use golang-migrate format. Tests require Docker Compose running (`go:build integration`).

- [ ] **Step 1: Install dependencies**

```bash
cd /Users/daryl/code/hermes
go get github.com/jackc/pgx/v5
go get github.com/jackc/pgx/v5/pgxpool
go get github.com/golang-migrate/migrate/v4
go get github.com/golang-migrate/migrate/v4/database/postgres
go get github.com/golang-migrate/migrate/v4/source/file
```

- [ ] **Step 2: Write database package**

```go
// internal/database/database.go
package database

import (
	"context"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}

func RunMigrations(databaseURL, migrationsPath string) error {
	m, err := migrate.New("file://"+migrationsPath, databaseURL)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}
```

- [ ] **Step 3: Write all migration files**

```sql
-- migrations/000001_create_tenants.up.sql
CREATE TABLE tenants (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    default_locale TEXT NOT NULL DEFAULT 'en',
    settings JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

```sql
-- migrations/000001_create_tenants.down.sql
DROP TABLE IF EXISTS tenants;
```

```sql
-- migrations/000002_create_api_keys.up.sql
CREATE TABLE api_keys (
    id TEXT PRIMARY KEY,
    key_hash TEXT NOT NULL,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_api_keys_key_hash ON api_keys (key_hash);
```

```sql
-- migrations/000002_create_api_keys.down.sql
DROP TABLE IF EXISTS api_keys;
```

```sql
-- migrations/000003_create_users.up.sql
CREATE TABLE users (
    id TEXT PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    external_id TEXT NOT NULL,
    email TEXT,
    phone TEXT,
    locale TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_users_tenant_external ON users (tenant_id, external_id);
```

```sql
-- migrations/000003_create_users.down.sql
DROP TABLE IF EXISTS users;
```

```sql
-- migrations/000004_create_notification_groups.up.sql
CREATE TABLE notification_groups (
    id TEXT PRIMARY KEY,
    slug TEXT NOT NULL,
    name TEXT NOT NULL,
    default_channels TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_notification_groups_slug ON notification_groups (slug);
```

```sql
-- migrations/000004_create_notification_groups.down.sql
DROP TABLE IF EXISTS notification_groups;
```

```sql
-- migrations/000005_create_notification_types.up.sql
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
```

```sql
-- migrations/000005_create_notification_types.down.sql
DROP TABLE IF EXISTS notification_types;
```

```sql
-- migrations/000006_create_notifications.up.sql
CREATE TABLE notifications (
    id TEXT PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    user_id TEXT NOT NULL REFERENCES users(id),
    type_id TEXT REFERENCES notification_types(id),
    group_id TEXT NOT NULL REFERENCES notification_groups(id),
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    action_url TEXT,
    action_label TEXT,
    idempotency_key TEXT,
    channels TEXT[] NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    sent_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    read_at TIMESTAMPTZ,
    archived_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ
);

CREATE INDEX idx_notifications_inbox
    ON notifications (user_id, created_at DESC)
    WHERE archived_at IS NULL AND deleted_at IS NULL;

CREATE UNIQUE INDEX idx_notifications_idempotency
    ON notifications (tenant_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
```

```sql
-- migrations/000006_create_notifications.down.sql
DROP TABLE IF EXISTS notifications;
```

```sql
-- migrations/000007_create_notification_events.up.sql
CREATE TABLE notification_events (
    id TEXT PRIMARY KEY,
    notification_id TEXT NOT NULL REFERENCES notifications(id),
    channel TEXT NOT NULL,
    event TEXT NOT NULL,
    severity TEXT NOT NULL DEFAULT 'info',
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_notification_events_notification ON notification_events (notification_id, created_at);
```

```sql
-- migrations/000007_create_notification_events.down.sql
DROP TABLE IF EXISTS notification_events;
```

```sql
-- migrations/000008_create_user_preferences.up.sql
CREATE TABLE user_preferences (
    user_id TEXT NOT NULL REFERENCES users(id),
    group_id TEXT NOT NULL REFERENCES notification_groups(id),
    channels TEXT[],
    PRIMARY KEY (user_id, group_id)
);
```

```sql
-- migrations/000008_create_user_preferences.down.sql
DROP TABLE IF EXISTS user_preferences;
```

- [ ] **Step 4: Write integration test for migrations**

```go
//go:build integration

// internal/database/database_test.go
package database_test

import (
	"context"
	"os"
	"testing"

	"github.com/hermes-notifications/hermes/internal/database"
)

func testDatabaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("HERMES_DATABASE_URL")
	if url == "" {
		url = "postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable"
	}
	return url
}

func TestNewPool(t *testing.T) {
	ctx := context.Background()
	pool, err := database.NewPool(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	var result int
	err = pool.QueryRow(ctx, "SELECT 1").Scan(&result)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if result != 1 {
		t.Fatalf("expected 1, got %d", result)
	}
}

func TestRunMigrations(t *testing.T) {
	err := database.RunMigrations(testDatabaseURL(t), "../../migrations")
	if err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	// Verify tables exist
	ctx := context.Background()
	pool, err := database.NewPool(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	tables := []string{"tenants", "api_keys", "users", "notification_groups",
		"notification_types", "notifications", "notification_events", "user_preferences"}

	for _, table := range tables {
		var exists bool
		err := pool.QueryRow(ctx,
			"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)", table).Scan(&exists)
		if err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("table %s does not exist", table)
		}
	}
}
```

- [ ] **Step 5: Run integration tests**

```bash
cd /Users/daryl/code/hermes && go test ./internal/database/... -tags=integration -v
```

Expected: PASS — pool connects, migrations create all 8 tables.

- [ ] **Step 6: Commit**

```bash
git add internal/database/ migrations/
git commit -m "feat: add database package with pgx pool and all migrations"
```

---

### Task 6: NATS Messaging Package

**Files:**
- Create: `internal/messaging/nats.go`
- Create: `internal/messaging/nats_test.go`

Wraps NATS JetStream connection, stream creation, and publish/subscribe helpers.

- [ ] **Step 1: Install dependency**

```bash
cd /Users/daryl/code/hermes && go get github.com/nats-io/nats.go
```

- [ ] **Step 2: Write integration test**

```go
//go:build integration

// internal/messaging/nats_test.go
package messaging_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/internal/messaging"
)

func testNATSUrl(t *testing.T) string {
	t.Helper()
	url := os.Getenv("HERMES_NATS_URL")
	if url == "" {
		url = "nats://localhost:4222"
	}
	return url
}

func TestConnect_And_SetupStreams(t *testing.T) {
	client, err := messaging.Connect(testNATSUrl(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Close()

	if err := client.SetupStreams(context.Background()); err != nil {
		t.Fatalf("SetupStreams: %v", err)
	}
}

func TestPublish_And_Subscribe(t *testing.T) {
	client, err := messaging.Connect(testNATSUrl(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer client.Close()

	if err := client.SetupStreams(context.Background()); err != nil {
		t.Fatalf("SetupStreams: %v", err)
	}

	payload := []byte(`{"test": true}`)
	if err := client.Publish("notification.send", payload); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	received := make(chan []byte, 1)
	if err := client.Subscribe("notification.send", "test-consumer", func(data []byte) error {
		received <- data
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	select {
	case msg := <-received:
		if string(msg) != string(payload) {
			t.Fatalf("expected %s, got %s", payload, msg)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for message")
	}
}
```

- [ ] **Step 3: Write implementation**

```go
// internal/messaging/nats.go
package messaging

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type Client struct {
	conn *nats.Conn
	js   jetstream.JetStream
}

type StreamConfig struct {
	Name     string
	Subjects []string
}

var Streams = []StreamConfig{
	{Name: "NOTIFICATIONS", Subjects: []string{"notification.send"}},
	{Name: "DELIVERY", Subjects: []string{"delivery.email", "delivery.sms", "delivery.inbox"}},
	{Name: "EVENTS", Subjects: []string{"notification.events"}},
}

func Connect(url string) (*Client, error) {
	nc, err := nats.Connect(url)
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("jetstream: %w", err)
	}
	return &Client{conn: nc, js: js}, nil
}

func (c *Client) SetupStreams(ctx context.Context) error {
	for _, s := range Streams {
		_, err := c.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
			Name:      s.Name,
			Subjects:  s.Subjects,
			Retention: jetstream.WorkQueuePolicy,
			Storage:   jetstream.FileStorage,
			MaxAge:    7 * 24 * time.Hour,
		})
		if err != nil {
			return fmt.Errorf("create stream %s: %w", s.Name, err)
		}
	}
	return nil
}

func (c *Client) Publish(subject string, data []byte) error {
	_, err := c.js.Publish(context.Background(), subject, data)
	return err
}

func (c *Client) Subscribe(subject, consumer string, handler func(data []byte) error) error {
	// Find which stream owns this subject
	streamName := ""
	for _, s := range Streams {
		for _, subj := range s.Subjects {
			if subj == subject {
				streamName = s.Name
				break
			}
		}
	}
	if streamName == "" {
		return fmt.Errorf("no stream found for subject %s", subject)
	}

	cons, err := c.js.CreateOrUpdateConsumer(context.Background(), streamName, jetstream.ConsumerConfig{
		Durable:       consumer,
		FilterSubject: subject,
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		return fmt.Errorf("create consumer: %w", err)
	}

	_, err = cons.Consume(func(msg jetstream.Msg) {
		if err := handler(msg.Data()); err != nil {
			_ = msg.Nak()
			return
		}
		_ = msg.Ack()
	})
	return err
}

func (c *Client) Close() {
	c.conn.Close()
}
```

- [ ] **Step 4: Run integration tests**

```bash
cd /Users/daryl/code/hermes && go test ./internal/messaging/... -tags=integration -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/messaging/
git commit -m "feat: add NATS JetStream messaging package with stream setup"
```

---

### Task 7: Redis Cache Package

**Files:**
- Create: `internal/cache/redis.go`
- Create: `internal/cache/redis_test.go`

Wraps go-redis client. Provides typed helpers for type config caching and idempotency key operations.

- [ ] **Step 1: Install dependency**

```bash
cd /Users/daryl/code/hermes && go get github.com/redis/go-redis/v9
```

- [ ] **Step 2: Write integration test**

```go
//go:build integration

// internal/cache/redis_test.go
package cache_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/internal/cache"
)

func testRedisURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("HERMES_REDIS_URL")
	if url == "" {
		url = "redis://localhost:6379/0"
	}
	return url
}

func TestConnect(t *testing.T) {
	c, err := cache.Connect(testRedisURL(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()
}

func TestIdempotencyKey_SetNX(t *testing.T) {
	c, err := cache.Connect(testRedisURL(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()

	ctx := context.Background()
	key := "test-tenant:test-key-" + time.Now().Format(time.RFC3339Nano)

	// First set should succeed
	existing, err := c.SetIdempotencyKey(ctx, key, "notif-1", time.Hour)
	if err != nil {
		t.Fatalf("SetIdempotencyKey: %v", err)
	}
	if existing != "" {
		t.Fatalf("expected empty (new key), got %s", existing)
	}

	// Second set should return existing
	existing, err = c.SetIdempotencyKey(ctx, key, "notif-2", time.Hour)
	if err != nil {
		t.Fatalf("SetIdempotencyKey: %v", err)
	}
	if existing != "notif-1" {
		t.Fatalf("expected notif-1, got %s", existing)
	}
}

func TestTypeConfig_Cache(t *testing.T) {
	c, err := cache.Connect(testRedisURL(t))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()

	ctx := context.Background()
	slug := "test.type." + time.Now().Format(time.RFC3339Nano)
	data := []byte(`{"slug":"test","email_subject":"Hello"}`)

	// Cache miss
	got, err := c.GetTypeConfig(ctx, slug)
	if err != nil {
		t.Fatalf("GetTypeConfig: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil on cache miss")
	}

	// Set
	if err := c.SetTypeConfig(ctx, slug, data, 5*time.Minute); err != nil {
		t.Fatalf("SetTypeConfig: %v", err)
	}

	// Cache hit
	got, err = c.GetTypeConfig(ctx, slug)
	if err != nil {
		t.Fatalf("GetTypeConfig: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("expected %s, got %s", data, got)
	}

	// Invalidate
	if err := c.InvalidateTypeConfig(ctx, slug); err != nil {
		t.Fatalf("InvalidateTypeConfig: %v", err)
	}
	got, err = c.GetTypeConfig(ctx, slug)
	if err != nil {
		t.Fatalf("GetTypeConfig after invalidate: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil after invalidation")
	}
}
```

- [ ] **Step 3: Write implementation**

```go
// internal/cache/redis.go
package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Client struct {
	rdb *redis.Client
}

func Connect(redisURL string) (*Client, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis URL: %w", err)
	}
	rdb := redis.NewClient(opts)
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return &Client{rdb: rdb}, nil
}

// SetIdempotencyKey attempts to set an idempotency key. Returns "" if the key was new,
// or the existing notification_id if the key already existed.
func (c *Client) SetIdempotencyKey(ctx context.Context, key, notificationID string, ttl time.Duration) (string, error) {
	set, err := c.rdb.SetNX(ctx, "idem:"+key, notificationID, ttl).Result()
	if err != nil {
		return "", fmt.Errorf("setnx: %w", err)
	}
	if set {
		return "", nil // new key
	}
	// Key already existed — return stored value
	existing, err := c.rdb.Get(ctx, "idem:"+key).Result()
	if err != nil {
		return "", fmt.Errorf("get existing: %w", err)
	}
	return existing, nil
}

func (c *Client) GetTypeConfig(ctx context.Context, slug string) ([]byte, error) {
	val, err := c.rdb.Get(ctx, "type:"+slug).Bytes()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get type config: %w", err)
	}
	return val, nil
}

func (c *Client) SetTypeConfig(ctx context.Context, slug string, data []byte, ttl time.Duration) error {
	return c.rdb.Set(ctx, "type:"+slug, data, ttl).Err()
}

func (c *Client) InvalidateTypeConfig(ctx context.Context, slug string) error {
	return c.rdb.Del(ctx, "type:"+slug).Err()
}

func (c *Client) Close() {
	c.rdb.Close()
}
```

- [ ] **Step 4: Run integration tests**

```bash
cd /Users/daryl/code/hermes && go test ./internal/cache/... -tags=integration -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cache/
git commit -m "feat: add Redis cache package with idempotency and type config helpers"
```

---

### Task 8: API Key Auth Package

**Files:**
- Create: `internal/auth/apikey.go`
- Create: `internal/auth/apikey_test.go`

API key hashing with argon2 and validation middleware.

- [ ] **Step 1: Install dependency**

```bash
cd /Users/daryl/code/hermes && go get golang.org/x/crypto
```

- [ ] **Step 2: Write failing tests**

```go
// internal/auth/apikey_test.go
package auth_test

import (
	"testing"

	"github.com/hermes-notifications/hermes/internal/auth"
)

func TestHashAPIKey_And_Verify(t *testing.T) {
	raw := "hms_test_key_abc123"
	hash, err := auth.HashAPIKey(raw)
	if err != nil {
		t.Fatalf("HashAPIKey: %v", err)
	}

	if !auth.VerifyAPIKey(raw, hash) {
		t.Fatal("expected key to verify")
	}
}

func TestVerifyAPIKey_WrongKey(t *testing.T) {
	raw := "hms_test_key_abc123"
	hash, err := auth.HashAPIKey(raw)
	if err != nil {
		t.Fatalf("HashAPIKey: %v", err)
	}

	if auth.VerifyAPIKey("hms_wrong_key", hash) {
		t.Fatal("expected wrong key to not verify")
	}
}

func TestHashAPIKey_DifferentEachTime(t *testing.T) {
	raw := "hms_test_key_abc123"
	h1, _ := auth.HashAPIKey(raw)
	h2, _ := auth.HashAPIKey(raw)
	if h1 == h2 {
		t.Fatal("expected different hashes (different salts)")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

```bash
cd /Users/daryl/code/hermes && go test ./internal/auth/...
```

Expected: compilation error.

- [ ] **Step 4: Write implementation**

```go
// internal/auth/apikey.go
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    = 1
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonKeyLen  = 32
	saltLen      = 16
)

func HashAPIKey(raw string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	hash := argon2.IDKey([]byte(raw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("%s$%s",
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

func VerifyAPIKey(raw, encoded string) bool {
	parts := strings.SplitN(encoded, "$", 2)
	if len(parts) != 2 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	hash := argon2.IDKey([]byte(raw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return subtle.ConstantTimeCompare(hash, expected) == 1
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd /Users/daryl/code/hermes && go test ./internal/auth/... -v
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/auth/
git commit -m "feat: add API key hashing with argon2"
```

---

### Task 9: Store Package — Core + Tenants + Groups + Types

**Files:**
- Create: `internal/store/store.go`
- Create: `internal/store/testutil_test.go`
- Create: `internal/store/tenants.go`
- Create: `internal/store/tenants_test.go`
- Create: `internal/store/groups.go`
- Create: `internal/store/groups_test.go`
- Create: `internal/store/types.go`
- Create: `internal/store/types_test.go`

Shared database query layer. Integration tests run against real Postgres.

- [ ] **Step 1: Write store struct and test helpers**

```go
// internal/store/store.go
package store

import "github.com/jackc/pgx/v5/pgxpool"

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}
```

```go
//go:build integration

// internal/store/testutil_test.go
package store_test

import (
	"context"
	"os"
	"testing"

	"github.com/hermes-notifications/hermes/internal/database"
	"github.com/hermes-notifications/hermes/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testStore(t *testing.T) (*store.Store, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("HERMES_DATABASE_URL")
	if url == "" {
		url = "postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable"
	}
	if err := database.RunMigrations(url, "../../migrations"); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	pool, err := database.NewPool(context.Background(), url)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return store.New(pool), pool
}

// cleanTable truncates a table for test isolation
func cleanTable(t *testing.T, pool *pgxpool.Pool, tables ...string) {
	t.Helper()
	for _, table := range tables {
		_, err := pool.Exec(context.Background(), "TRUNCATE "+table+" CASCADE")
		if err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
}
```

- [ ] **Step 2: Write groups CRUD + test**

```go
// internal/store/groups.go
package store

import (
	"context"
	"fmt"

	"github.com/hermes-notifications/hermes/internal/id"
	"github.com/hermes-notifications/hermes/internal/models"
)

func (s *Store) CreateGroup(ctx context.Context, slug, name string, defaultChannels []string) (*models.NotificationGroup, error) {
	g := &models.NotificationGroup{
		ID:              id.New(),
		Slug:            slug,
		Name:            name,
		DefaultChannels: defaultChannels,
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO notification_groups (id, slug, name, default_channels)
		 VALUES ($1, $2, $3, $4)
		 RETURNING created_at`,
		g.ID, g.Slug, g.Name, g.DefaultChannels,
	).Scan(&g.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create group: %w", err)
	}
	return g, nil
}

func (s *Store) GetGroupBySlug(ctx context.Context, slug string) (*models.NotificationGroup, error) {
	g := &models.NotificationGroup{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, slug, name, default_channels, created_at
		 FROM notification_groups WHERE slug = $1`, slug,
	).Scan(&g.ID, &g.Slug, &g.Name, &g.DefaultChannels, &g.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get group by slug: %w", err)
	}
	return g, nil
}

func (s *Store) GetGroupByID(ctx context.Context, id string) (*models.NotificationGroup, error) {
	g := &models.NotificationGroup{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, slug, name, default_channels, created_at
		 FROM notification_groups WHERE id = $1`, id,
	).Scan(&g.ID, &g.Slug, &g.Name, &g.DefaultChannels, &g.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get group by id: %w", err)
	}
	return g, nil
}

func (s *Store) ListGroups(ctx context.Context) ([]models.NotificationGroup, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, slug, name, default_channels, created_at
		 FROM notification_groups ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	defer rows.Close()

	var groups []models.NotificationGroup
	for rows.Next() {
		var g models.NotificationGroup
		if err := rows.Scan(&g.ID, &g.Slug, &g.Name, &g.DefaultChannels, &g.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan group: %w", err)
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

func (s *Store) UpdateGroup(ctx context.Context, id, name string, defaultChannels []string) (*models.NotificationGroup, error) {
	g := &models.NotificationGroup{}
	err := s.pool.QueryRow(ctx,
		`UPDATE notification_groups SET name = $2, default_channels = $3
		 WHERE id = $1
		 RETURNING id, slug, name, default_channels, created_at`,
		id, name, defaultChannels,
	).Scan(&g.ID, &g.Slug, &g.Name, &g.DefaultChannels, &g.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("update group: %w", err)
	}
	return g, nil
}
```

```go
//go:build integration

// internal/store/groups_test.go
package store_test

import (
	"context"
	"testing"
)

func TestCreateGroup_And_GetBySlug(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notification_groups")

	ctx := context.Background()
	g, err := s.CreateGroup(ctx, "billing", "Billing", []string{"email", "inbox"})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if g.Slug != "billing" {
		t.Fatalf("expected slug billing, got %s", g.Slug)
	}

	got, err := s.GetGroupBySlug(ctx, "billing")
	if err != nil {
		t.Fatalf("GetGroupBySlug: %v", err)
	}
	if got.ID != g.ID {
		t.Fatalf("expected ID %s, got %s", g.ID, got.ID)
	}
}

func TestListGroups(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notification_groups")

	ctx := context.Background()
	s.CreateGroup(ctx, "billing", "Billing", []string{"email"})
	s.CreateGroup(ctx, "security", "Security", []string{"email", "sms"})

	groups, err := s.ListGroups(ctx)
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
}

func TestUpdateGroup(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notification_groups")

	ctx := context.Background()
	g, _ := s.CreateGroup(ctx, "billing", "Billing", []string{"email"})

	updated, err := s.UpdateGroup(ctx, g.ID, "Billing Notifications", []string{"email", "inbox"})
	if err != nil {
		t.Fatalf("UpdateGroup: %v", err)
	}
	if updated.Name != "Billing Notifications" {
		t.Fatalf("expected updated name, got %s", updated.Name)
	}
	if len(updated.DefaultChannels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(updated.DefaultChannels))
	}
}
```

- [ ] **Step 3: Write types CRUD + test**

```go
// internal/store/types.go
package store

import (
	"context"
	"fmt"

	"github.com/hermes-notifications/hermes/internal/id"
	"github.com/hermes-notifications/hermes/internal/models"
)

func (s *Store) CreateType(ctx context.Context, input *models.NotificationType) (*models.NotificationType, error) {
	input.ID = id.New()
	err := s.pool.QueryRow(ctx,
		`INSERT INTO notification_types (id, group_id, slug, name, email_subject, email_body, sms_body, inbox_title, inbox_body)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING created_at`,
		input.ID, input.GroupID, input.Slug, input.Name,
		input.EmailSubject, input.EmailBody, input.SMSBody,
		input.InboxTitle, input.InboxBody,
	).Scan(&input.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create type: %w", err)
	}
	return input, nil
}

func (s *Store) GetTypeBySlug(ctx context.Context, slug string) (*models.NotificationType, error) {
	t := &models.NotificationType{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, group_id, slug, name, email_subject, email_body, sms_body, inbox_title, inbox_body, created_at
		 FROM notification_types WHERE slug = $1`, slug,
	).Scan(&t.ID, &t.GroupID, &t.Slug, &t.Name,
		&t.EmailSubject, &t.EmailBody, &t.SMSBody,
		&t.InboxTitle, &t.InboxBody, &t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get type by slug: %w", err)
	}
	return t, nil
}

func (s *Store) GetTypeByID(ctx context.Context, id string) (*models.NotificationType, error) {
	t := &models.NotificationType{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, group_id, slug, name, email_subject, email_body, sms_body, inbox_title, inbox_body, created_at
		 FROM notification_types WHERE id = $1`, id,
	).Scan(&t.ID, &t.GroupID, &t.Slug, &t.Name,
		&t.EmailSubject, &t.EmailBody, &t.SMSBody,
		&t.InboxTitle, &t.InboxBody, &t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get type by id: %w", err)
	}
	return t, nil
}

func (s *Store) ListTypes(ctx context.Context) ([]models.NotificationType, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, group_id, slug, name, email_subject, email_body, sms_body, inbox_title, inbox_body, created_at
		 FROM notification_types ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list types: %w", err)
	}
	defer rows.Close()

	var types []models.NotificationType
	for rows.Next() {
		var t models.NotificationType
		if err := rows.Scan(&t.ID, &t.GroupID, &t.Slug, &t.Name,
			&t.EmailSubject, &t.EmailBody, &t.SMSBody,
			&t.InboxTitle, &t.InboxBody, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan type: %w", err)
		}
		types = append(types, t)
	}
	return types, rows.Err()
}

func (s *Store) UpdateType(ctx context.Context, input *models.NotificationType) (*models.NotificationType, error) {
	err := s.pool.QueryRow(ctx,
		`UPDATE notification_types
		 SET name = $2, email_subject = $3, email_body = $4, sms_body = $5, inbox_title = $6, inbox_body = $7
		 WHERE id = $1
		 RETURNING id, group_id, slug, name, email_subject, email_body, sms_body, inbox_title, inbox_body, created_at`,
		input.ID, input.Name, input.EmailSubject, input.EmailBody,
		input.SMSBody, input.InboxTitle, input.InboxBody,
	).Scan(&input.ID, &input.GroupID, &input.Slug, &input.Name,
		&input.EmailSubject, &input.EmailBody, &input.SMSBody,
		&input.InboxTitle, &input.InboxBody, &input.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("update type: %w", err)
	}
	return input, nil
}

func (s *Store) DeleteType(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM notification_types WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete type: %w", err)
	}
	return nil
}
```

```go
//go:build integration

// internal/store/types_test.go
package store_test

import (
	"context"
	"testing"

	"github.com/hermes-notifications/hermes/internal/models"
)

func TestCreateType_And_GetBySlug(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notification_types", "notification_groups")

	ctx := context.Background()
	g, _ := s.CreateGroup(ctx, "billing", "Billing", []string{"email", "inbox"})

	subject := "Invoice {{.invoice_number}} paid"
	nt, err := s.CreateType(ctx, &models.NotificationType{
		GroupID:      g.ID,
		Slug:         "invoice.paid",
		Name:         "Invoice Paid",
		EmailSubject: &subject,
	})
	if err != nil {
		t.Fatalf("CreateType: %v", err)
	}

	got, err := s.GetTypeBySlug(ctx, "invoice.paid")
	if err != nil {
		t.Fatalf("GetTypeBySlug: %v", err)
	}
	if got.ID != nt.ID {
		t.Fatalf("expected ID %s, got %s", nt.ID, got.ID)
	}
}

func TestDeleteType(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notification_types", "notification_groups")

	ctx := context.Background()
	g, _ := s.CreateGroup(ctx, "billing", "Billing", []string{"email"})
	nt, _ := s.CreateType(ctx, &models.NotificationType{
		GroupID: g.ID, Slug: "invoice.paid", Name: "Invoice Paid",
	})

	if err := s.DeleteType(ctx, nt.ID); err != nil {
		t.Fatalf("DeleteType: %v", err)
	}

	_, err := s.GetTypeByID(ctx, nt.ID)
	if err == nil {
		t.Fatal("expected error after delete")
	}
}
```

- [ ] **Step 4: Run integration tests**

```bash
cd /Users/daryl/code/hermes && go test ./internal/store/... -tags=integration -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit -m "feat: add store package with groups and types CRUD"
```

---

### Task 10: Store Package — Users + Notifications

**Files:**
- Create: `internal/store/users.go`
- Create: `internal/store/users_test.go`
- Create: `internal/store/notifications.go`
- Create: `internal/store/notifications_test.go`

- [ ] **Step 1: Write users upsert + test**

```go
// internal/store/users.go
package store

import (
	"context"
	"fmt"

	"github.com/hermes-notifications/hermes/internal/id"
	"github.com/hermes-notifications/hermes/internal/models"
)

// EnsureUser creates a user if they don't exist, returns the user either way.
func (s *Store) EnsureUser(ctx context.Context, tenantID, externalID string) (*models.User, error) {
	newID := id.New()
	u := &models.User{}
	err := s.pool.QueryRow(ctx,
		`WITH ins AS (
			INSERT INTO users (id, tenant_id, external_id)
			VALUES ($1, $2, $3)
			ON CONFLICT (tenant_id, external_id) DO NOTHING
			RETURNING id, tenant_id, external_id, email, phone, locale, created_at
		)
		SELECT * FROM ins
		UNION ALL
		SELECT id, tenant_id, external_id, email, phone, locale, created_at
		FROM users
		WHERE tenant_id = $2 AND external_id = $3
		LIMIT 1`,
		newID, tenantID, externalID,
	).Scan(&u.ID, &u.TenantID, &u.ExternalID, &u.Email, &u.Phone, &u.Locale, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("ensure user: %w", err)
	}
	return u, nil
}

func (s *Store) GetUserByID(ctx context.Context, id string) (*models.User, error) {
	u := &models.User{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, external_id, email, phone, locale, created_at
		 FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.TenantID, &u.ExternalID, &u.Email, &u.Phone, &u.Locale, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get user by id: %w", err)
	}
	return u, nil
}
```

```go
//go:build integration

// internal/store/users_test.go
package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestEnsureUser_CreatesOnFirstCall(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "users", "tenants")

	ctx := context.Background()
	tenantID := uuid.New().String()
	pool.Exec(ctx, "INSERT INTO tenants (id, name) VALUES ($1, $2)", tenantID, "Test Tenant")

	u, err := s.EnsureUser(ctx, tenantID, "ext-user-1")
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}
	if u.ExternalID != "ext-user-1" {
		t.Fatalf("expected ext-user-1, got %s", u.ExternalID)
	}
}

func TestEnsureUser_ReturnsSameOnSecondCall(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "users", "tenants")

	ctx := context.Background()
	tenantID := uuid.New().String()
	pool.Exec(ctx, "INSERT INTO tenants (id, name) VALUES ($1, $2)", tenantID, "Test Tenant")

	u1, _ := s.EnsureUser(ctx, tenantID, "ext-user-1")
	u2, _ := s.EnsureUser(ctx, tenantID, "ext-user-1")

	if u1.ID != u2.ID {
		t.Fatalf("expected same ID, got %s and %s", u1.ID, u2.ID)
	}
}
```

- [ ] **Step 2: Write notifications insert + status query**

```go
// internal/store/notifications.go
package store

import (
	"context"
	"fmt"

	"github.com/hermes-notifications/hermes/internal/models"
)

func (s *Store) CreateNotification(ctx context.Context, n *models.Notification) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO notifications (id, tenant_id, user_id, type_id, group_id, title, body, action_url, action_label, idempotency_key, channels, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		n.ID, n.TenantID, n.UserID, n.TypeID, n.GroupID,
		n.Title, n.Body, n.ActionURL, n.ActionLabel,
		n.IdempotencyKey, n.Channels, n.Status,
	)
	if err != nil {
		return fmt.Errorf("create notification: %w", err)
	}
	return nil
}

func (s *Store) GetNotificationByID(ctx context.Context, id string) (*models.Notification, error) {
	n := &models.Notification{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, user_id, type_id, group_id, title, body, action_url, action_label,
		        idempotency_key, channels, status, created_at, sent_at, delivered_at, read_at, archived_at, deleted_at
		 FROM notifications WHERE id = $1`, id,
	).Scan(&n.ID, &n.TenantID, &n.UserID, &n.TypeID, &n.GroupID,
		&n.Title, &n.Body, &n.ActionURL, &n.ActionLabel,
		&n.IdempotencyKey, &n.Channels, &n.Status,
		&n.CreatedAt, &n.SentAt, &n.DeliveredAt, &n.ReadAt, &n.ArchivedAt, &n.DeletedAt)
	if err != nil {
		return nil, fmt.Errorf("get notification by id: %w", err)
	}
	return n, nil
}

// GetNotificationByIdempotencyKey checks for an existing notification with the given key (within 24h).
func (s *Store) GetNotificationByIdempotencyKey(ctx context.Context, tenantID, key string) (*models.Notification, error) {
	n := &models.Notification{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, user_id, type_id, group_id, title, body, action_url, action_label,
		        idempotency_key, channels, status, created_at, sent_at, delivered_at, read_at, archived_at, deleted_at
		 FROM notifications
		 WHERE tenant_id = $1 AND idempotency_key = $2 AND created_at > NOW() - INTERVAL '24 hours'`, tenantID, key,
	).Scan(&n.ID, &n.TenantID, &n.UserID, &n.TypeID, &n.GroupID,
		&n.Title, &n.Body, &n.ActionURL, &n.ActionLabel,
		&n.IdempotencyKey, &n.Channels, &n.Status,
		&n.CreatedAt, &n.SentAt, &n.DeliveredAt, &n.ReadAt, &n.ArchivedAt, &n.DeletedAt)
	if err != nil {
		return nil, fmt.Errorf("get by idempotency key: %w", err)
	}
	return n, nil
}

func (s *Store) GetNotificationEvents(ctx context.Context, notificationID string) ([]models.NotificationEvent, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, notification_id, channel, event, severity, metadata, created_at
		 FROM notification_events
		 WHERE notification_id = $1 ORDER BY created_at`, notificationID)
	if err != nil {
		return nil, fmt.Errorf("get events: %w", err)
	}
	defer rows.Close()

	var events []models.NotificationEvent
	for rows.Next() {
		var e models.NotificationEvent
		if err := rows.Scan(&e.ID, &e.NotificationID, &e.Channel, &e.Event, &e.Severity, &e.Metadata, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
```

```go
//go:build integration

// internal/store/notifications_test.go
package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/hermes-notifications/hermes/internal/id"
	"github.com/hermes-notifications/hermes/internal/models"
)

func TestCreateNotification_And_GetByID(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notifications", "users", "notification_types", "notification_groups", "tenants")

	ctx := context.Background()

	tenantID := uuid.New().String()
	pool.Exec(ctx, "INSERT INTO tenants (id, name) VALUES ($1, $2)", tenantID, "Test")
	u, _ := s.EnsureUser(ctx, tenantID, "ext-1")
	g, _ := s.CreateGroup(ctx, "billing", "Billing", []string{"email", "inbox"})

	n := &models.Notification{
		ID:       id.New(),
		TenantID: tenantID,
		UserID:   u.ID,
		GroupID:  g.ID,
		Title:    "Test Notification",
		Body:     "Test body",
		Channels: []string{"email", "inbox"},
		Status:   models.StatusPending,
	}

	if err := s.CreateNotification(ctx, n); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	got, err := s.GetNotificationByID(ctx, n.ID)
	if err != nil {
		t.Fatalf("GetNotificationByID: %v", err)
	}
	if got.Title != "Test Notification" {
		t.Fatalf("expected title, got %s", got.Title)
	}
	if string(got.Status) != "pending" {
		t.Fatalf("expected pending, got %s", got.Status)
	}
}

func TestGetNotificationByIdempotencyKey(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notifications", "users", "notification_types", "notification_groups", "tenants")

	ctx := context.Background()
	tenantID := uuid.New().String()
	pool.Exec(ctx, "INSERT INTO tenants (id, name) VALUES ($1, $2)", tenantID, "Test")
	u, _ := s.EnsureUser(ctx, tenantID, "ext-1")
	g, _ := s.CreateGroup(ctx, "billing", "Billing", []string{"email"})

	idemKey := "inv-123-paid"
	n := &models.Notification{
		ID:             id.New(),
		TenantID:       tenantID,
		UserID:         u.ID,
		GroupID:        g.ID,
		Title:          "Test",
		Body:           "Body",
		IdempotencyKey: &idemKey,
		Channels:       []string{"email"},
		Status:         models.StatusPending,
	}
	s.CreateNotification(ctx, n)

	got, err := s.GetNotificationByIdempotencyKey(ctx, tenantID, "inv-123-paid")
	if err != nil {
		t.Fatalf("GetByIdempotencyKey: %v", err)
	}
	if got.ID != n.ID {
		t.Fatalf("expected %s, got %s", n.ID, got.ID)
	}
}
```

- [ ] **Step 3: Install uuid dependency for tests**

```bash
cd /Users/daryl/code/hermes && go get github.com/google/uuid
```

- [ ] **Step 4: Run integration tests**

```bash
cd /Users/daryl/code/hermes && go test ./internal/store/... -tags=integration -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit -m "feat: add users and notifications to store package"
```

---

### Task 11: Middleware — Logging, Recovery, Rate Limiting

**Files:**
- Create: `internal/middleware/logging.go`
- Create: `internal/middleware/recovery.go`
- Create: `internal/middleware/ratelimit.go`

Standard `net/http` middleware chain. No external dependencies.

- [ ] **Step 1: Write middleware**

```go
// internal/middleware/logging.go
package middleware

import (
	"log/slog"
	"net/http"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}
```

```go
// internal/middleware/recovery.go
package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
)

func Recovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					logger.Error("panic recovered",
						"error", err,
						"stack", string(debug.Stack()),
					)
					http.Error(w, "internal server error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
```

```go
// internal/middleware/ratelimit.go
package middleware

import (
	"net/http"
	"sync"
	"time"
)

type tokenBucket struct {
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per second
	lastRefill time.Time
	mu         sync.Mutex
}

func (b *tokenBucket) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens += elapsed * b.refillRate
	if b.tokens > b.maxTokens {
		b.tokens = b.maxTokens
	}
	b.lastRefill = now

	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// RateLimit returns middleware that rate-limits by a key extracted from the request.
// burst is the max tokens (1000), sustained is tokens/sec (500).
func RateLimit(keyFunc func(*http.Request) string, burst int, sustained int) func(http.Handler) http.Handler {
	buckets := sync.Map{}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFunc(r)
			val, _ := buckets.LoadOrStore(key, &tokenBucket{
				tokens:     float64(burst),
				maxTokens:  float64(burst),
				refillRate: float64(sustained),
				lastRefill: time.Now(),
			})
			bucket := val.(*tokenBucket)

			if !bucket.allow() {
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/middleware/
git commit -m "feat: add HTTP middleware — logging, recovery, rate limiting"
```

---

### Task 12: Admin Service — Server + Health Checks

**Files:**
- Create: `internal/admin/server.go`
- Create: `internal/admin/handler_health.go`
- Create: `cmd/admin/main.go`

Wire up the HTTP server with middleware chain, health endpoints, and dependency injection.

- [ ] **Step 1: Write server**

```go
// internal/admin/server.go
package admin

import (
	"log/slog"
	"net/http"

	"github.com/hermes-notifications/hermes/internal/cache"
	"github.com/hermes-notifications/hermes/internal/messaging"
	"github.com/hermes-notifications/hermes/internal/middleware"
	"github.com/hermes-notifications/hermes/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Server struct {
	store  *store.Store
	nats   *messaging.Client
	cache  *cache.Client
	pool   *pgxpool.Pool
	logger *slog.Logger
	mux    *http.ServeMux
}

func NewServer(store *store.Store, nats *messaging.Client, cache *cache.Client, pool *pgxpool.Pool, logger *slog.Logger) *Server {
	s := &Server{
		store:  store,
		nats:   nats,
		cache:  cache,
		pool:   pool,
		logger: logger,
		mux:    http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /readyz", s.handleReadyz)

	// Admin CRUD — added in subsequent tasks
	// s.mux.HandleFunc("GET /v1/groups", ...)
	// s.mux.HandleFunc("POST /v1/groups", ...)
}

func (s *Server) Handler() http.Handler {
	var h http.Handler = s.mux
	h = middleware.Logging(s.logger)(h)
	h = middleware.Recovery(s.logger)(h)
	return h
}
```

```go
// internal/admin/handler_health.go
package admin

import (
	"context"
	"net/http"
	"time"
)

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.pool.Ping(ctx); err != nil {
		http.Error(w, "postgres not ready", http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}
```

- [ ] **Step 2: Write main.go**

```go
// cmd/admin/main.go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hermes-notifications/hermes/internal/admin"
	"github.com/hermes-notifications/hermes/internal/cache"
	"github.com/hermes-notifications/hermes/internal/config"
	"github.com/hermes-notifications/hermes/internal/database"
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
		logger.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	natsClient, err := messaging.Connect(cfg.NATSUrl)
	if err != nil {
		logger.Error("nats connection failed", "error", err)
		os.Exit(1)
	}
	defer natsClient.Close()

	if err := natsClient.SetupStreams(ctx); err != nil {
		logger.Error("nats stream setup failed", "error", err)
		os.Exit(1)
	}

	redisClient, err := cache.Connect(cfg.RedisURL)
	if err != nil {
		logger.Error("redis connection failed", "error", err)
		os.Exit(1)
	}
	defer redisClient.Close()

	st := store.New(pool)
	srv := admin.NewServer(st, natsClient, redisClient, pool, logger)

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler: srv.Handler(),
	}

	go func() {
		logger.Info("admin service starting", "port", cfg.HTTPPort)
		if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
			logger.Error("http server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	httpServer.Shutdown(shutdownCtx)
}
```

- [ ] **Step 3: Verify it compiles and health checks work**

```bash
cd /Users/daryl/code/hermes && go build ./cmd/admin/
```

Expected: compiles successfully.

```bash
# With Docker Compose running:
./admin &
curl -s http://localhost:8080/healthz
curl -s http://localhost:8080/readyz
kill %1
```

Expected: both return "ok".

- [ ] **Step 4: Commit**

```bash
git add internal/admin/ cmd/admin/
git commit -m "feat: add admin service with HTTP server and health checks"
```

---

### Task 13: Admin Service — Groups CRUD Handlers

**Files:**
- Create: `internal/admin/handler_groups.go`
- Create: `internal/admin/handler_groups_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/admin/handler_groups_test.go
package admin_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hermes-notifications/hermes/internal/admin"
	"github.com/hermes-notifications/hermes/internal/models"
)

// mockStore implements the store methods needed by admin handlers.
// We test against the HTTP layer with a mock store.
type mockStore struct {
	groups []models.NotificationGroup
}

func (m *mockStore) CreateGroup(slug, name string, channels []string) (*models.NotificationGroup, error) {
	g := &models.NotificationGroup{ID: "test-id", Slug: slug, Name: name, DefaultChannels: channels}
	m.groups = append(m.groups, *g)
	return g, nil
}

func (m *mockStore) ListGroups() ([]models.NotificationGroup, error) {
	return m.groups, nil
}

// ... This approach requires an interface. See Step 3 for the real pattern.
```

Actually, for unit-testable handlers, we need the Server to depend on an interface rather than the concrete Store. Let's adjust:

- [ ] **Step 2: Define store interface for admin service**

Add to `internal/admin/server.go`:

```go
// Add a StoreInterface that the admin server depends on
type AdminStore interface {
	CreateGroup(ctx context.Context, slug, name string, defaultChannels []string) (*models.NotificationGroup, error)
	GetGroupByID(ctx context.Context, id string) (*models.NotificationGroup, error)
	ListGroups(ctx context.Context) ([]models.NotificationGroup, error)
	UpdateGroup(ctx context.Context, id, name string, defaultChannels []string) (*models.NotificationGroup, error)
	CreateType(ctx context.Context, input *models.NotificationType) (*models.NotificationType, error)
	GetTypeByID(ctx context.Context, id string) (*models.NotificationType, error)
	GetTypeBySlug(ctx context.Context, slug string) (*models.NotificationType, error)
	ListTypes(ctx context.Context) ([]models.NotificationType, error)
	UpdateType(ctx context.Context, input *models.NotificationType) (*models.NotificationType, error)
	DeleteType(ctx context.Context, id string) error
	EnsureUser(ctx context.Context, tenantID, externalID string) (*models.User, error)
	CreateNotification(ctx context.Context, n *models.Notification) error
	GetNotificationByID(ctx context.Context, id string) (*models.Notification, error)
	GetNotificationByIdempotencyKey(ctx context.Context, tenantID, key string) (*models.Notification, error)
	GetNotificationEvents(ctx context.Context, notificationID string) ([]models.NotificationEvent, error)
	GetGroupBySlug(ctx context.Context, slug string) (*models.NotificationGroup, error)
}
```

Update `Server` struct to use the interface. The concrete `store.Store` already satisfies it.

- [ ] **Step 3: Write groups handlers**

```go
// internal/admin/handler_groups.go
package admin

import (
	"encoding/json"
	"net/http"
)

type createGroupRequest struct {
	Slug            string   `json:"slug"`
	Name            string   `json:"name"`
	DefaultChannels []string `json:"default_channels"`
}

type updateGroupRequest struct {
	Name            string   `json:"name"`
	DefaultChannels []string `json:"default_channels"`
}

func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := s.store.ListGroups(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.jsonResponse(w, http.StatusOK, groups)
}

func (s *Server) handleCreateGroup(w http.ResponseWriter, r *http.Request) {
	var req createGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.clientError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Slug == "" || req.Name == "" {
		s.clientError(w, http.StatusBadRequest, "slug and name are required")
		return
	}

	g, err := s.store.CreateGroup(r.Context(), req.Slug, req.Name, req.DefaultChannels)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.jsonResponse(w, http.StatusCreated, g)
}

func (s *Server) handleUpdateGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req updateGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.clientError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	g, err := s.store.UpdateGroup(r.Context(), id, req.Name, req.DefaultChannels)
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.jsonResponse(w, http.StatusOK, g)
}

func (s *Server) jsonResponse(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (s *Server) clientError(w http.ResponseWriter, status int, message string) {
	s.jsonResponse(w, status, map[string]string{"error": message})
}

func (s *Server) serverError(w http.ResponseWriter, err error) {
	s.logger.Error("internal error", "error", err)
	s.clientError(w, http.StatusInternalServerError, "internal server error")
}
```

- [ ] **Step 4: Register routes in server.go**

Add to the `routes()` method:

```go
s.mux.HandleFunc("GET /v1/groups", s.handleListGroups)
s.mux.HandleFunc("POST /v1/groups", s.handleCreateGroup)
s.mux.HandleFunc("PUT /v1/groups/{id}", s.handleUpdateGroup)
```

- [ ] **Step 5: Write handler tests with mock store**

```go
// internal/admin/handler_groups_test.go
package admin_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleCreateGroup(t *testing.T) {
	srv := newTestServer(t)

	body := `{"slug":"billing","name":"Billing","default_channels":["email","inbox"]}`
	req := httptest.NewRequest("POST", "/v1/groups", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp["slug"] != "billing" {
		t.Fatalf("expected slug billing, got %v", resp["slug"])
	}
}

func TestHandleListGroups(t *testing.T) {
	srv := newTestServer(t)

	// Create a group first
	body := `{"slug":"billing","name":"Billing","default_channels":["email"]}`
	req := httptest.NewRequest("POST", "/v1/groups", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	// List
	req = httptest.NewRequest("GET", "/v1/groups", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
```

The `newTestServer` helper creates an admin.Server backed by an in-memory mock store. Define it in a test helper file (`internal/admin/testutil_test.go`) — it implements the `AdminStore` interface with in-memory slices.

- [ ] **Step 6: Run tests**

```bash
cd /Users/daryl/code/hermes && go test ./internal/admin/... -v
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/admin/
git commit -m "feat: add groups CRUD handlers to admin service"
```

---

### Task 14: Admin Service — Types CRUD Handlers

**Files:**
- Create: `internal/admin/handler_types.go`
- Create: `internal/admin/handler_types_test.go`

Same pattern as groups — CRUD handlers with cache invalidation on write.

- [ ] **Step 1: Write handlers**

```go
// internal/admin/handler_types.go
package admin

import (
	"encoding/json"
	"net/http"

	"github.com/hermes-notifications/hermes/internal/models"
)

type createTypeRequest struct {
	GroupID      string  `json:"group_id"`
	Slug         string  `json:"slug"`
	Name         string  `json:"name"`
	EmailSubject *string `json:"email_subject"`
	EmailBody    *string `json:"email_body"`
	SMSBody      *string `json:"sms_body"`
	InboxTitle   *string `json:"inbox_title"`
	InboxBody    *string `json:"inbox_body"`
}

func (s *Server) handleListTypes(w http.ResponseWriter, r *http.Request) {
	types, err := s.store.ListTypes(r.Context())
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.jsonResponse(w, http.StatusOK, types)
}

func (s *Server) handleCreateType(w http.ResponseWriter, r *http.Request) {
	var req createTypeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.clientError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Slug == "" || req.Name == "" || req.GroupID == "" {
		s.clientError(w, http.StatusBadRequest, "slug, name, and group_id are required")
		return
	}

	nt, err := s.store.CreateType(r.Context(), &models.NotificationType{
		GroupID:      req.GroupID,
		Slug:         req.Slug,
		Name:         req.Name,
		EmailSubject: req.EmailSubject,
		EmailBody:    req.EmailBody,
		SMSBody:      req.SMSBody,
		InboxTitle:   req.InboxTitle,
		InboxBody:    req.InboxBody,
	})
	if err != nil {
		s.serverError(w, err)
		return
	}
	s.jsonResponse(w, http.StatusCreated, nt)
}

func (s *Server) handleUpdateType(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req createTypeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.clientError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	// Get existing to find slug for cache invalidation
	existing, err := s.store.GetTypeByID(r.Context(), id)
	if err != nil {
		s.clientError(w, http.StatusNotFound, "type not found")
		return
	}

	updated, err := s.store.UpdateType(r.Context(), &models.NotificationType{
		ID:           id,
		Name:         req.Name,
		EmailSubject: req.EmailSubject,
		EmailBody:    req.EmailBody,
		SMSBody:      req.SMSBody,
		InboxTitle:   req.InboxTitle,
		InboxBody:    req.InboxBody,
	})
	if err != nil {
		s.serverError(w, err)
		return
	}

	// Invalidate cache
	s.cache.InvalidateTypeConfig(r.Context(), existing.Slug)

	s.jsonResponse(w, http.StatusOK, updated)
}

func (s *Server) handleDeleteType(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	existing, err := s.store.GetTypeByID(r.Context(), id)
	if err != nil {
		s.clientError(w, http.StatusNotFound, "type not found")
		return
	}

	if err := s.store.DeleteType(r.Context(), id); err != nil {
		s.serverError(w, err)
		return
	}

	s.cache.InvalidateTypeConfig(r.Context(), existing.Slug)
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 2: Register routes**

Add to `routes()`:

```go
s.mux.HandleFunc("GET /v1/types", s.handleListTypes)
s.mux.HandleFunc("POST /v1/types", s.handleCreateType)
s.mux.HandleFunc("PUT /v1/types/{id}", s.handleUpdateType)
s.mux.HandleFunc("DELETE /v1/types/{id}", s.handleDeleteType)
```

- [ ] **Step 3: Write tests (same mock pattern as groups)**

- [ ] **Step 4: Run tests**

```bash
cd /Users/daryl/code/hermes && go test ./internal/admin/... -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/admin/
git commit -m "feat: add types CRUD handlers with cache invalidation"
```

---

### Task 15: Admin Service — Send Handler

**Files:**
- Create: `internal/admin/handler_send.go`
- Create: `internal/admin/handler_send_test.go`

The main send endpoint. Validates input, checks idempotency, ensures user, persists notification, publishes to NATS.

- [ ] **Step 1: Write handler**

```go
// internal/admin/handler_send.go
package admin

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/hermes-notifications/hermes/internal/id"
	"github.com/hermes-notifications/hermes/internal/models"
)

type sendRequest struct {
	TenantID string   `json:"tenant_id"`
	UserID   string   `json:"user_id"` // external ID
	Type     string   `json:"type,omitempty"`
	Content  *content `json:"content,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
	Channels []string `json:"channels,omitempty"`
	Group    string   `json:"group,omitempty"`
}

type content struct {
	Title       string `json:"title"`
	Body        string `json:"body"`
	ActionURL   string `json:"action_url,omitempty"`
	ActionLabel string `json:"action_label,omitempty"`
}

type sendResponse struct {
	NotificationID string `json:"notification_id"`
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	var req sendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.clientError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	// Validate: exactly one of type or content
	if (req.Type == "" && req.Content == nil) || (req.Type != "" && req.Content != nil) {
		s.clientError(w, http.StatusBadRequest, "exactly one of 'type' or 'content' must be provided")
		return
	}
	if req.TenantID == "" || req.UserID == "" {
		s.clientError(w, http.StatusBadRequest, "tenant_id and user_id are required")
		return
	}

	ctx := r.Context()

	// Check idempotency key
	idemKey := r.Header.Get("X-Idempotency-Key")
	if idemKey != "" {
		// Check Redis first
		existing, err := s.cache.SetIdempotencyKey(ctx, req.TenantID+":"+idemKey, "", time.Hour)
		if err == nil && existing != "" {
			s.jsonResponse(w, http.StatusAccepted, sendResponse{NotificationID: existing})
			return
		}
		// Redis miss — check Postgres
		if existing == "" {
			n, err := s.store.GetNotificationByIdempotencyKey(ctx, req.TenantID, idemKey)
			if err == nil && n != nil {
				// Backfill Redis
				s.cache.SetIdempotencyKey(ctx, req.TenantID+":"+idemKey, n.ID, time.Hour)
				s.jsonResponse(w, http.StatusAccepted, sendResponse{NotificationID: n.ID})
				return
			}
		}
	}

	// Resolve group
	var groupID string
	if req.Type != "" {
		nt, err := s.store.GetTypeBySlug(ctx, req.Type)
		if err != nil {
			s.clientError(w, http.StatusBadRequest, "unknown notification type")
			return
		}
		groupID = nt.GroupID
	} else {
		if req.Group == "" {
			s.clientError(w, http.StatusBadRequest, "group is required for direct sends")
			return
		}
		g, err := s.store.GetGroupBySlug(ctx, req.Group)
		if err != nil {
			s.clientError(w, http.StatusBadRequest, "unknown group")
			return
		}
		groupID = g.ID
	}

	// Ensure user exists
	user, err := s.store.EnsureUser(ctx, req.TenantID, req.UserID)
	if err != nil {
		s.serverError(w, err)
		return
	}

	// Build notification
	notifID := id.New()
	n := &models.Notification{
		ID:       notifID,
		TenantID: req.TenantID,
		UserID:   user.ID,
		GroupID:  groupID,
		Channels: req.Channels,
		Status:   models.StatusPending,
	}

	if req.Content != nil {
		n.Title = req.Content.Title
		n.Body = req.Content.Body
		if req.Content.ActionURL != "" {
			n.ActionURL = &req.Content.ActionURL
		}
		if req.Content.ActionLabel != "" {
			n.ActionLabel = &req.Content.ActionLabel
		}
	}

	if req.Type != "" {
		nt, _ := s.store.GetTypeBySlug(ctx, req.Type)
		n.TypeID = &nt.ID
	}

	if idemKey != "" {
		n.IdempotencyKey = &idemKey
	}

	// Persist
	if err := s.store.CreateNotification(ctx, n); err != nil {
		s.serverError(w, err)
		return
	}

	// Update Redis idempotency with actual notification ID
	if idemKey != "" {
		s.cache.SetIdempotencyKey(ctx, req.TenantID+":"+idemKey, notifID, time.Hour)
	}

	// Publish to NATS
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
			"group": req.Group,
			"type":  req.Type,
		},
		"data":    req.Data,
		"attempt": 1,
	}
	if len(req.Channels) > 0 {
		msg["channels"] = req.Channels
	}

	msgBytes, _ := json.Marshal(msg)
	if err := s.nats.Publish("notification.send", msgBytes); err != nil {
		s.logger.Error("failed to publish to NATS", "error", err, "notification_id", notifID)
		// Notification is persisted — it can be retried. Don't fail the request.
	}

	s.jsonResponse(w, http.StatusAccepted, sendResponse{NotificationID: notifID})
}
```

- [ ] **Step 2: Register route**

Add to `routes()`:

```go
s.mux.HandleFunc("POST /v1/send", s.handleSend)
```

- [ ] **Step 3: Write tests for send handler**

Test cases:
- Valid direct send with content → 202
- Valid templated send with type → 202
- Missing both type and content → 400
- Both type and content → 400
- Missing tenant_id → 400
- Idempotency key returns existing ID on retry → 202

- [ ] **Step 4: Run tests**

```bash
cd /Users/daryl/code/hermes && go test ./internal/admin/... -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/admin/
git commit -m "feat: add POST /v1/send handler with idempotency and NATS publish"
```

---

### Task 16: Admin Service — Notification Status Handler

**Files:**
- Create: `internal/admin/handler_notifications.go`
- Create: `internal/admin/handler_notifications_test.go`

Returns notification status + event log.

- [ ] **Step 1: Write handler**

```go
// internal/admin/handler_notifications.go
package admin

import "net/http"

type notificationStatusResponse struct {
	Notification any   `json:"notification"`
	Events       any   `json:"events"`
}

func (s *Server) handleGetNotification(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	n, err := s.store.GetNotificationByID(r.Context(), id)
	if err != nil {
		s.clientError(w, http.StatusNotFound, "notification not found")
		return
	}

	events, err := s.store.GetNotificationEvents(r.Context(), id)
	if err != nil {
		s.serverError(w, err)
		return
	}

	s.jsonResponse(w, http.StatusOK, notificationStatusResponse{
		Notification: n,
		Events:       events,
	})
}
```

- [ ] **Step 2: Register route**

```go
s.mux.HandleFunc("GET /v1/notifications/{id}", s.handleGetNotification)
```

- [ ] **Step 3: Write test**

- [ ] **Step 4: Run tests**

```bash
cd /Users/daryl/code/hermes && go test ./internal/admin/... -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/admin/
git commit -m "feat: add GET /v1/notifications/:id status + event log handler"
```

---

### Task 17: API Key Auth Middleware + Integration

**Files:**
- Modify: `internal/auth/apikey.go` — add HTTP middleware
- Modify: `internal/admin/server.go` — wire auth middleware to API routes
- Create: `internal/store/apikeys.go`
- Create: `internal/store/apikeys_test.go`

- [ ] **Step 1: Add API key store methods**

```go
// internal/store/apikeys.go
package store

import (
	"context"
	"fmt"

	"github.com/hermes-notifications/hermes/internal/id"
	"github.com/hermes-notifications/hermes/internal/models"
)

func (s *Store) CreateAPIKey(ctx context.Context, keyHash, name string) (*models.APIKey, error) {
	k := &models.APIKey{ID: id.New(), KeyHash: keyHash, Name: name}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO api_keys (id, key_hash, name) VALUES ($1, $2, $3) RETURNING created_at`,
		k.ID, k.KeyHash, k.Name,
	).Scan(&k.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create api key: %w", err)
	}
	return k, nil
}

func (s *Store) GetAPIKeyByHash(ctx context.Context, keyHash string) (*models.APIKey, error) {
	k := &models.APIKey{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, key_hash, name, created_at FROM api_keys WHERE key_hash = $1`, keyHash,
	).Scan(&k.ID, &k.KeyHash, &k.Name, &k.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get api key: %w", err)
	}
	return k, nil
}

func (s *Store) ListAPIKeys(ctx context.Context) ([]models.APIKey, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, key_hash, name, created_at FROM api_keys`)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	var keys []models.APIKey
	for rows.Next() {
		var k models.APIKey
		if err := rows.Scan(&k.ID, &k.KeyHash, &k.Name, &k.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}
```

- [ ] **Step 2: Add API key middleware to auth package**

```go
// Add to internal/auth/apikey.go

type APIKeyValidator func(ctx context.Context, rawKey string) bool

func APIKeyMiddleware(validate APIKeyValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("Authorization")
			if key == "" {
				http.Error(w, `{"error":"missing api key"}`, http.StatusUnauthorized)
				return
			}
			// Strip "Bearer " prefix if present
			key = strings.TrimPrefix(key, "Bearer ")

			if !validate(r.Context(), key) {
				http.Error(w, `{"error":"invalid api key"}`, http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

- [ ] **Step 3: Wire auth middleware into admin routes**

In `server.go`, wrap API routes (not health checks) with auth middleware. The validator function loads all API key hashes, caches them in Redis, and verifies against each.

- [ ] **Step 4: Run all tests**

```bash
cd /Users/daryl/code/hermes && go test ./... -v && go test ./... -tags=integration -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/auth/ internal/store/ internal/admin/
git commit -m "feat: add API key auth middleware and wire into admin routes"
```

---

### Task 18: End-to-End Smoke Test

**Files:**
- Create: `tests/e2e/admin_test.go`

Full integration test: start Docker Compose, run migrations, create a group, create a type, send a notification, verify it's persisted and a NATS message was published.

- [ ] **Step 1: Write e2e test**

```go
//go:build integration

// tests/e2e/admin_test.go
package e2e_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/hermes-notifications/hermes/internal/admin"
	// ... wire up all real deps
)

func TestSendNotification_E2E(t *testing.T) {
	// 1. Connect to real Postgres, NATS, Redis
	// 2. Run migrations
	// 3. Create a tenant directly in DB
	// 4. Create an API key
	// 5. POST /v1/groups to create "billing" group
	// 6. POST /v1/types to create "invoice.paid" type
	// 7. POST /v1/send with type "invoice.paid"
	// 8. Verify: 202 response with notification_id
	// 9. GET /v1/notifications/:id — verify status is "pending"
	// 10. Verify NATS message was published to notification.send
}
```

- [ ] **Step 2: Run e2e test**

```bash
cd /Users/daryl/code/hermes && go test ./tests/e2e/... -tags=integration -v
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add tests/
git commit -m "test: add end-to-end smoke test for admin service send flow"
```

---

### Task 19: Tidy and Final Verification

- [ ] **Step 1: Run go mod tidy**

```bash
cd /Users/daryl/code/hermes && go mod tidy
```

- [ ] **Step 2: Run all unit tests**

```bash
go test ./... -v
```

Expected: all PASS.

- [ ] **Step 3: Run all integration tests (Docker Compose must be running)**

```bash
go test ./... -tags=integration -v
```

Expected: all PASS.

- [ ] **Step 4: Verify the binary builds**

```bash
go build ./cmd/admin/
```

Expected: clean build.

- [ ] **Step 5: Commit tidy**

```bash
git add go.mod go.sum
git commit -m "chore: go mod tidy"
```

---

## Phase 1 Completion Criteria

- [ ] Go module initialized with all dependencies
- [ ] Docker Compose brings up Postgres, NATS, Redis
- [ ] All 8 migration files create the full schema
- [ ] Shared packages: id, config, database, messaging, cache, auth, models, store, middleware
- [ ] Admin service runs and exposes: POST /v1/send, GET/POST/PUT /v1/groups, GET/POST/PUT/DELETE /v1/types, GET /v1/notifications/:id, GET /healthz, GET /readyz
- [ ] Send endpoint validates, persists to Postgres, publishes to NATS
- [ ] Idempotency keys checked in Redis with Postgres fallback
- [ ] Type config cache invalidated on type update
- [ ] API key auth middleware protects all routes
- [ ] All unit and integration tests pass
