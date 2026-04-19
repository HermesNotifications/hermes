# Load Testing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build an end-to-end load-testing system for Hermes that scales linearly to 100k+ virtual users and 50k+ RPS, measures send-path capacity and inbox read-path performance (REST + Centrifugo WS), and runs both locally (docker-compose) and in-cluster (EKS via `k6-operator`).

**Architecture:** One k6 JS scenario codebase under `loadtest/`, a Go seeder (`cmd/loadseed/`) that populates Postgres directly, a dedicated Prometheus + Grafana stack for load-run metrics (Datadog continues to instrument Hermes itself), and a `k6-operator`-based cluster runner whose `parallelism` field scales load linearly. Scenarios share a single global API key and per-user JWTs. Correlation across systems via a `run_id` tag and `X-Load-Test-Run-Id` header.

**Tech Stack:** Go (seeder), k6 + JavaScript (scenarios), docker-compose (local), kubectl + `k6-operator` CRD (cluster), Prometheus remote-write + Grafana (metrics), GitHub Actions (CI).

**Spec:** `docs/superpowers/specs/2026-04-17-load-testing-design.md`

---

## File map

**Go seeder:**

- `cmd/loadseed/main.go` — CLI entry + flag parsing
- `cmd/loadseed/manifest.go` — manifest types, read/write
- `cmd/loadseed/seeder.go` — orchestration of all seeding phases
- `cmd/loadseed/apikey.go` — single API key insert
- `cmd/loadseed/tenants.go` — tenant inserts
- `cmd/loadseed/users.go` — user inserts via `COPY FROM`
- `cmd/loadseed/subscriptions.go` — categories + subscriptions + templates
- `cmd/loadseed/cleanup.go` — teardown
- `cmd/loadseed/seeder_test.go` — integration test (uses real DB, `integration` build tag)

**k6 scenarios + lib:**

- `loadtest/lib/seed.js` — `SharedArray` manifest loader + `pickTenant` / `pickUser` / `pickTemplate`
- `loadtest/lib/auth.js` — admin bearer header, per-user JWT minting
- `loadtest/lib/metrics.js` — custom `Trend`, `Gauge`, `Counter` definitions
- `loadtest/lib/payloads.js` — send-request body builders
- `loadtest/lib/centrifugo.js` — thin Centrifugo WS client wrapper
- `loadtest/lib/shared.js` — iteration-to-timestamp map shared by send + ws scenarios
- `loadtest/scenarios/send.js`
- `loadtest/scenarios/inbox-mixed.js`
- `loadtest/scenarios/soak.js`

**Local infra:**

- `loadtest/docker-compose.loadtest.yml` — adds Prometheus + Grafana to the dev compose
- `loadtest/prometheus/prometheus.yml` — scrape + remote-write-receiver config
- `loadtest/grafana/provisioning/datasources/prometheus.yaml`
- `loadtest/grafana/provisioning/dashboards/dashboards.yaml`
- `loadtest/dashboards/load-test.json`
- `loadtest/scripts/run-id.sh`
- `loadtest/scripts/run-local.sh`

**Kubernetes infra:**

- `loadtest/k8s/namespace.yaml`
- `loadtest/k8s/node-pool.md` — documentation for creating the tainted node pool (actual IaC lives in `infra/`)
- `loadtest/k8s/loadseed-job.yaml` — pre-test seeder Job template
- `loadtest/k8s/testrun.yaml` — `k6-operator` `TestRun` template
- `loadtest/k8s/prometheus-values.yaml`
- `loadtest/k8s/grafana-values.yaml`
- `loadtest/k8s/install.sh` — one-time cluster setup
- `loadtest/scripts/run-k8s.sh` — per-run helper

**Makefile:** new targets in root `Makefile`:
- `loadtest-local`, `loadtest-local-clean`, `loadtest-k8s`, `loadtest-k8s-clean`, `loadseed`

**CI:** `.github/workflows/loadtest.yml`

**Docs:** `loadtest/README.md`

**Artifacts (gitignored):** `artifacts/` at repo root; `loadtest/seed-manifest.json`

---

## Phase 0 — Setup

### Task 0.1: Directory scaffolding

**Files:**
- Create: `loadtest/.gitkeep`, `loadtest/scenarios/.gitkeep`, `loadtest/lib/.gitkeep`, `loadtest/k8s/.gitkeep`, `loadtest/dashboards/.gitkeep`, `loadtest/prometheus/.gitkeep`, `loadtest/grafana/provisioning/datasources/.gitkeep`, `loadtest/grafana/provisioning/dashboards/.gitkeep`, `loadtest/scripts/.gitkeep`
- Modify: `.gitignore`

- [ ] **Step 1: Create the directory skeleton**

```bash
mkdir -p loadtest/{scenarios,lib,k8s,dashboards,prometheus,grafana/provisioning/{datasources,dashboards},scripts} artifacts
touch loadtest/{,scenarios,lib,k8s,dashboards,prometheus,scripts}/.gitkeep
touch loadtest/grafana/provisioning/{datasources,dashboards}/.gitkeep
```

- [ ] **Step 2: Gitignore artifacts + manifest**

Append to `.gitignore`:

```
# load testing
/artifacts/
/loadtest/seed-manifest.json
```

- [ ] **Step 3: Commit**

```bash
git add loadtest/ .gitignore
git commit -m "chore(loadtest): scaffold directory layout"
```

---

## Phase 1 — Seeder manifest & skeleton

### Task 1.1: Manifest types + round-trip

**Files:**
- Create: `cmd/loadseed/manifest.go`, `cmd/loadseed/manifest_test.go`

- [ ] **Step 1: Write failing test**

`cmd/loadseed/manifest_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManifest_RoundTrip(t *testing.T) {
	m := &Manifest{
		SeededAt: "2026-04-17T00:00:00Z",
		RunSeedID: "abc123",
		APIKey:   "hms_dev_key_xxx_yyy",
		Tenants: []Tenant{
			{
				ID:    "t1",
				Users: []string{"u1", "u2"},
				Categories: []Category{
					{ID: "c1", Subscriptions: []Subscription{
						{ID: "s1", Templates: []Template{
							{ID: "tmpl1", Channels: []string{"inbox", "email"}},
						}},
					}},
				},
			},
		},
	}
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := m.Write(path); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadManifest(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.APIKey != m.APIKey || len(got.Tenants) != 1 || got.Tenants[0].Categories[0].Subscriptions[0].Templates[0].ID != "tmpl1" {
		t.Fatalf("mismatch: %+v", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}
```

- [ ] **Step 2: Run test — expect FAIL**

```bash
cd /Users/daryl/code/hermes && go test ./cmd/loadseed/ -run TestManifest_RoundTrip -v
```
Expected: `undefined: Manifest` etc.

- [ ] **Step 3: Write `manifest.go`**

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type Manifest struct {
	SeededAt  string   `json:"seeded_at"`
	RunSeedID string   `json:"run_seed_id"`
	APIKey    string   `json:"api_key"`
	Tenants   []Tenant `json:"tenants"`
}

type Tenant struct {
	ID         string     `json:"id"`
	Users      []string   `json:"users"`
	Categories []Category `json:"categories"`
}

type Category struct {
	ID            string         `json:"id"`
	Subscriptions []Subscription `json:"subscriptions"`
}

type Subscription struct {
	ID        string     `json:"id"`
	Templates []Template `json:"templates"`
}

type Template struct {
	ID       string   `json:"id"`
	Channels []string `json:"channels"`
}

func (m *Manifest) Write(path string) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

func ReadManifest(path string) (*Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &m, nil
}
```

- [ ] **Step 4: Run test — expect PASS**

```bash
cd /Users/daryl/code/hermes && go test ./cmd/loadseed/ -run TestManifest_RoundTrip -v
```

- [ ] **Step 5: Commit**

```bash
git add cmd/loadseed/manifest.go cmd/loadseed/manifest_test.go
git commit -m "feat(loadseed): manifest types and round-trip serialization"
```

### Task 1.2: CLI skeleton with flag parsing

**Files:**
- Create: `cmd/loadseed/main.go`

- [ ] **Step 1: Write `main.go` (buildable skeleton; `runSeed`/`runCleanup` stubs replaced in Phase 2)**

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/hermes-notifications/hermes/internal/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	Tenants                  int
	UsersPerTenant           int
	CategoriesPerTenant      int
	SubscriptionsPerCategory int
	TemplatesPerSubscription int
	DatabaseURL              string
	AdminURL                 string
	HMACSecret               string
	OutputPath               string
	Cleanup                  bool
}

func main() {
	cfg := parseFlags()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect db: %v", err)
	}
	defer pool.Close()

	if cfg.Cleanup {
		if err := runCleanup(ctx, pool, cfg); err != nil {
			log.Fatalf("cleanup: %v", err)
		}
		return
	}
	if err := runSeed(ctx, pool, cfg); err != nil {
		log.Fatalf("seed: %v", err)
	}
}

func parseFlags() Config {
	var cfg Config
	flag.IntVar(&cfg.Tenants, "tenants", 10, "number of tenants")
	flag.IntVar(&cfg.UsersPerTenant, "users-per-tenant", 10000, "users per tenant")
	flag.IntVar(&cfg.CategoriesPerTenant, "categories-per-tenant", 3, "subscription categories per tenant")
	flag.IntVar(&cfg.SubscriptionsPerCategory, "subscriptions-per-category", 2, "subscriptions per category")
	flag.IntVar(&cfg.TemplatesPerSubscription, "templates-per-subscription", 2, "templates per subscription")
	flag.StringVar(&cfg.DatabaseURL, "database-url", os.Getenv("HERMES_DATABASE_URL"), "Postgres URL")
	flag.StringVar(&cfg.AdminURL, "admin-url", "http://localhost:8080", "admin base URL (warm-up only)")
	flag.StringVar(&cfg.HMACSecret, "hmac-secret", envOr("HERMES_API_KEY_HMAC_SECRET", "hermes-dev-hmac-secret"), "HMAC secret for api-key hashing")
	flag.StringVar(&cfg.OutputPath, "output", "loadtest/seed-manifest.json", "manifest output path")
	flag.BoolVar(&cfg.Cleanup, "cleanup", false, "delete all seeded entities from the manifest")
	flag.Parse()

	if cfg.DatabaseURL == "" {
		log.Fatal("database-url is required (or HERMES_DATABASE_URL)")
	}
	return cfg
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// runID returns a short hex string suitable for tagging this seed run.
func runID() string { return uuid.NewString()[:8] }

// Stubs — replaced by real implementations in Phase 2 (Tasks 2.5, 2.6).
func runSeed(ctx context.Context, pool *pgxpool.Pool, cfg Config) error {
	return fmt.Errorf("not implemented")
}
func runCleanup(ctx context.Context, pool *pgxpool.Pool, cfg Config) error {
	return fmt.Errorf("not implemented")
}
```

- [ ] **Step 2: Verify it compiles**

```bash
cd /Users/daryl/code/hermes && go build ./cmd/loadseed/
```

- [ ] **Step 3: Commit**

```bash
git add cmd/loadseed/main.go
git commit -m "feat(loadseed): CLI skeleton with flag parsing"
```

---

## Phase 2 — Direct-to-DB seeding

All integration tests in this phase use the `integration` build tag and require `make infra-up` (Postgres reachable at `postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable`). Every test cleans up after itself by deleting the seeded tenants at teardown.

### Task 2.1: Insert a single API key

**Files:**
- Create: `cmd/loadseed/apikey.go`, `cmd/loadseed/apikey_test.go`

- [ ] **Step 1: Write failing integration test**

`cmd/loadseed/apikey_test.go`:

```go
//go:build integration

package main

import (
	"context"
	"os"
	"testing"

	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/database"
)

func TestInsertAPIKey(t *testing.T) {
	dbURL := envOr("HERMES_DATABASE_URL", "postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable")
	ctx := context.Background()
	pool, err := database.NewPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer pool.Close()

	hmac := envOr("HERMES_API_KEY_HMAC_SECRET", "hermes-dev-hmac-secret")
	raw, keyID, err := insertAPIKey(ctx, pool, hmac, "loadtest-"+os.Getenv("HOSTNAME"))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM api_keys WHERE id = $1`, keyID) }()

	id, secret, err := auth.ParseAPIKey(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if id != keyID {
		t.Fatalf("id mismatch: %s != %s", id, keyID)
	}

	var hash string
	if err := pool.QueryRow(ctx, `SELECT key_hash FROM api_keys WHERE id = $1`, keyID).Scan(&hash); err != nil {
		t.Fatalf("query hash: %v", err)
	}
	if !auth.HMACVerifyAPIKey(secret, hash, hmac) {
		t.Fatalf("key does not verify")
	}
}
```

- [ ] **Step 2: Run test — expect FAIL (undefined)**

```bash
cd /Users/daryl/code/hermes && go test -tags=integration ./cmd/loadseed/ -run TestInsertAPIKey -v
```

- [ ] **Step 3: Write `apikey.go`**

```go
package main

import (
	"context"

	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

// loadseedPermissions are the permissions granted to the single seeded API key.
// Matches cmd/seed/main.go allPermissions minus apikeys:manage (not needed for load runs).
var loadseedPermissions = []string{
	auth.PermNotificationsSend,
	auth.PermTemplatesManage,
	auth.PermTenantsManage,
}

// insertAPIKey generates a new API key, writes it to api_keys with the HMAC hash,
// and returns the raw plaintext key (to be stored in the manifest) and key ID.
func insertAPIKey(ctx context.Context, pool *pgxpool.Pool, hmacSecret, name string) (rawKey, keyID string, err error) {
	rawKey, keyID, err = auth.GenerateAPIKey("dev")
	if err != nil {
		return "", "", err
	}
	_, secret, err := auth.ParseAPIKey(rawKey)
	if err != nil {
		return "", "", err
	}
	hash := auth.HMACHashAPIKey(secret, hmacSecret)
	if _, err := pool.Exec(ctx,
		`INSERT INTO api_keys (id, key_hash, name, permissions) VALUES ($1, $2, $3, $4)`,
		keyID, hash, name, loadseedPermissions,
	); err != nil {
		return "", "", err
	}
	return rawKey, keyID, nil
}
```

- [ ] **Step 4: Run test — expect PASS**

```bash
cd /Users/daryl/code/hermes && go test -tags=integration ./cmd/loadseed/ -run TestInsertAPIKey -v
```

- [ ] **Step 5: Commit**

```bash
git add cmd/loadseed/apikey.go cmd/loadseed/apikey_test.go
git commit -m "feat(loadseed): insert single API key via direct DB"
```

### Task 2.2: Insert tenants

**Files:**
- Create: `cmd/loadseed/tenants.go`, `cmd/loadseed/tenants_test.go`

- [ ] **Step 1: Write failing integration test**

`cmd/loadseed/tenants_test.go`:

```go
//go:build integration

package main

import (
	"context"
	"testing"

	"github.com/hermes-notifications/hermes/internal/database"
)

func TestInsertTenants(t *testing.T) {
	ctx := context.Background()
	pool, err := database.NewPool(ctx, envOr("HERMES_DATABASE_URL", "postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer pool.Close()

	runID := runID()
	ids, err := insertTenants(ctx, pool, 3, runID)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM tenants WHERE id = ANY($1)`, ids) }()

	if len(ids) != 3 {
		t.Fatalf("got %d ids, want 3", len(ids))
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM tenants WHERE id = ANY($1)`, ids).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 3 {
		t.Fatalf("want 3 rows, got %d", count)
	}
}
```

- [ ] **Step 2: Run test — expect FAIL (undefined)**

```bash
cd /Users/daryl/code/hermes && go test -tags=integration ./cmd/loadseed/ -run TestInsertTenants -v
```

- [ ] **Step 3: Write `tenants.go`**

```go
package main

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// insertTenants creates n tenants and returns their UUIDs.
// Name is "loadtest-<runID>-<idx>" so runs are identifiable and easy to clean up manually.
func insertTenants(ctx context.Context, pool *pgxpool.Pool, n int, runID string) ([]string, error) {
	ids := make([]string, n)
	names := make([]string, n)
	for i := 0; i < n; i++ {
		ids[i] = uuid.NewString()
		names[i] = fmt.Sprintf("loadtest-%s-%d", runID, i)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO tenants (id, name)
		 SELECT unnest($1::uuid[]), unnest($2::text[])`,
		ids, names,
	); err != nil {
		return nil, fmt.Errorf("insert tenants: %w", err)
	}
	return ids, nil
}
```

- [ ] **Step 4: Run test — expect PASS**

```bash
cd /Users/daryl/code/hermes && go test -tags=integration ./cmd/loadseed/ -run TestInsertTenants -v
```

- [ ] **Step 5: Commit**

```bash
git add cmd/loadseed/tenants.go cmd/loadseed/tenants_test.go
git commit -m "feat(loadseed): bulk-insert tenants"
```

### Task 2.3: Insert users via COPY FROM

**Files:**
- Create: `cmd/loadseed/users.go`, `cmd/loadseed/users_test.go`

- [ ] **Step 1: Write failing integration test**

`cmd/loadseed/users_test.go`:

```go
//go:build integration

package main

import (
	"context"
	"testing"

	"github.com/hermes-notifications/hermes/internal/database"
)

func TestInsertUsers_Copy(t *testing.T) {
	ctx := context.Background()
	pool, err := database.NewPool(ctx, envOr("HERMES_DATABASE_URL", "postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer pool.Close()

	runID := runID()
	tenantIDs, err := insertTenants(ctx, pool, 1, runID)
	if err != nil {
		t.Fatalf("tenants: %v", err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM tenants WHERE id = ANY($1)`, tenantIDs) }()

	ids, err := insertUsers(ctx, pool, tenantIDs[0], 500, runID, 0)
	if err != nil {
		t.Fatalf("users: %v", err)
	}
	if len(ids) != 500 {
		t.Fatalf("want 500 ids, got %d", len(ids))
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE tenant_id = $1`, tenantIDs[0]).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 500 {
		t.Fatalf("want 500 rows, got %d", count)
	}

	// Deterministic email check
	var email string
	if err := pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, ids[0]).Scan(&email); err != nil {
		t.Fatalf("email: %v", err)
	}
	if email == "" {
		t.Fatalf("empty email")
	}
}
```

- [ ] **Step 2: Run test — expect FAIL**

```bash
cd /Users/daryl/code/hermes && go test -tags=integration ./cmd/loadseed/ -run TestInsertUsers_Copy -v
```

- [ ] **Step 3: Write `users.go`**

```go
package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// insertUsers bulk-inserts n users for the given tenant using pgx's CopyFrom.
// Deterministic IDs: lt-<runID>-<tenantIdx>-<userIdx>; stable email/phone derived the same way.
// tenantIdx is used in IDs so reruns against the same tenant produce the same user IDs.
func insertUsers(ctx context.Context, pool *pgxpool.Pool, tenantID string, n int, runID string, tenantIdx int) ([]string, error) {
	rows := make([][]any, n)
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("lt-%s-t%d-u%d", runID, tenantIdx, i)
		email := fmt.Sprintf("%s@loadtest.local", id)
		phone := fmt.Sprintf("+1555%010d", (tenantIdx*1_000_000)+i)
		extID := fmt.Sprintf("ext-%d-%d", tenantIdx, i)
		ids[i] = id
		rows[i] = []any{id, tenantID, extID, email, phone, "en"}
	}
	if _, err := pool.CopyFrom(ctx,
		pgx.Identifier{"users"},
		[]string{"id", "tenant_id", "external_id", "email", "phone", "locale"},
		pgx.CopyFromRows(rows),
	); err != nil {
		return nil, fmt.Errorf("copy users: %w", err)
	}
	return ids, nil
}
```

- [ ] **Step 4: Run test — expect PASS**

```bash
cd /Users/daryl/code/hermes && go test -tags=integration ./cmd/loadseed/ -run TestInsertUsers_Copy -v
```

- [ ] **Step 5: Commit**

```bash
git add cmd/loadseed/users.go cmd/loadseed/users_test.go
git commit -m "feat(loadseed): bulk-insert users via pgx CopyFrom"
```

### Task 2.4: Insert subscription categories, subscriptions, templates

**Files:**
- Create: `cmd/loadseed/subscriptions.go`, `cmd/loadseed/subscriptions_test.go`

- [ ] **Step 1: Write failing integration test**

`cmd/loadseed/subscriptions_test.go`:

```go
//go:build integration

package main

import (
	"context"
	"testing"

	"github.com/hermes-notifications/hermes/internal/database"
)

func TestInsertSubscriptionTree(t *testing.T) {
	ctx := context.Background()
	pool, err := database.NewPool(ctx, envOr("HERMES_DATABASE_URL", "postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer pool.Close()

	runID := runID()
	cats, err := insertSubscriptionTree(ctx, pool, runID, 0, 2, 2, 2)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	defer cleanupSubscriptionTree(ctx, pool, cats)

	if len(cats) != 2 {
		t.Fatalf("want 2 categories, got %d", len(cats))
	}
	if len(cats[0].Subscriptions) != 2 {
		t.Fatalf("want 2 subscriptions, got %d", len(cats[0].Subscriptions))
	}
	if len(cats[0].Subscriptions[0].Templates) != 2 {
		t.Fatalf("want 2 templates, got %d", len(cats[0].Subscriptions[0].Templates))
	}

	tmplID := cats[0].Subscriptions[0].Templates[0].ID
	var channels []string
	if err := pool.QueryRow(ctx, `SELECT default_channels FROM notification_templates WHERE id = $1`, tmplID).Scan(&channels); err != nil {
		t.Fatalf("query template: %v", err)
	}
	if len(channels) != 2 {
		t.Fatalf("want 2 channels, got %v", channels)
	}
}
```

- [ ] **Step 2: Run test — expect FAIL**

```bash
cd /Users/daryl/code/hermes && go test -tags=integration ./cmd/loadseed/ -run TestInsertSubscriptionTree -v
```

- [ ] **Step 3: Write `subscriptions.go`**

```go
package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// insertSubscriptionTree creates categories, subscriptions, and templates for a single tenant.
// tenantIdx namespaces the IDs so multiple tenants' trees don't collide.
// Hermes's subscription_categories and notification_templates tables have UNIQUE indexes
// on slug, so slugs include the runID + tenantIdx to stay globally unique across runs.
func insertSubscriptionTree(ctx context.Context, pool *pgxpool.Pool, runID string, tenantIdx, numCats, subsPerCat, tmplsPerSub int) ([]Category, error) {
	cats := make([]Category, numCats)

	for ci := 0; ci < numCats; ci++ {
		catID := fmt.Sprintf("lt-sct-%s-%d-%d", runID, tenantIdx, ci)
		catSlug := fmt.Sprintf("lt_%s_t%d_cat%d", runID, tenantIdx, ci)
		if _, err := pool.Exec(ctx,
			`INSERT INTO subscription_categories (id, slug, name, default_channels, default_state, sort_order)
			 VALUES ($1, $2, $3, '{inbox,email}', 'on', $4)`,
			catID, catSlug, fmt.Sprintf("Load Test Cat %d/%d", tenantIdx, ci), ci,
		); err != nil {
			return nil, fmt.Errorf("insert category: %w", err)
		}
		cat := Category{ID: catID, Subscriptions: make([]Subscription, subsPerCat)}

		for si := 0; si < subsPerCat; si++ {
			subID := fmt.Sprintf("lt-sub-%s-%d-%d-%d", runID, tenantIdx, ci, si)
			subSlug := fmt.Sprintf("sub%d", si)
			if _, err := pool.Exec(ctx,
				`INSERT INTO subscriptions (id, category_id, slug, name, sort_order)
				 VALUES ($1, $2, $3, $4, $5)`,
				subID, catID, subSlug, fmt.Sprintf("Sub %d/%d/%d", tenantIdx, ci, si), si,
			); err != nil {
				return nil, fmt.Errorf("insert subscription: %w", err)
			}
			sub := Subscription{ID: subID, Templates: make([]Template, tmplsPerSub)}

			for ti := 0; ti < tmplsPerSub; ti++ {
				tmplID := fmt.Sprintf("lt-tpl-%s-%d-%d-%d-%d", runID, tenantIdx, ci, si, ti)
				tmplSlug := fmt.Sprintf("lt_%s_t%d_c%d_s%d_tpl%d", runID, tenantIdx, ci, si, ti)
				if _, err := pool.Exec(ctx,
					`INSERT INTO notification_templates
					 (id, subscription_id, slug, name, default_channels, email_subject, email_body, inbox_title, inbox_body)
					 VALUES ($1, $2, $3, $4, '{inbox,email}', $5, $6, $7, $8)`,
					tmplID, subID, tmplSlug, fmt.Sprintf("Template %d", ti),
					"Load test {{.subject}}", "Hello {{.name}}, this is a load test.",
					"Load Test", "{{.name}}: {{.subject}}",
				); err != nil {
					return nil, fmt.Errorf("insert template: %w", err)
				}
				sub.Templates[ti] = Template{ID: tmplID, Channels: []string{"inbox", "email"}}
			}
			cat.Subscriptions[si] = sub
		}
		cats[ci] = cat
	}
	return cats, nil
}

// cleanupSubscriptionTree removes a tree of categories (cascades subscriptions + templates).
func cleanupSubscriptionTree(ctx context.Context, pool *pgxpool.Pool, cats []Category) {
	var tmplIDs, subIDs, catIDs []string
	for _, c := range cats {
		catIDs = append(catIDs, c.ID)
		for _, s := range c.Subscriptions {
			subIDs = append(subIDs, s.ID)
			for _, t := range s.Templates {
				tmplIDs = append(tmplIDs, t.ID)
			}
		}
	}
	_, _ = pool.Exec(ctx, `DELETE FROM notification_templates WHERE id = ANY($1)`, tmplIDs)
	_, _ = pool.Exec(ctx, `DELETE FROM subscriptions WHERE id = ANY($1)`, subIDs)
	_, _ = pool.Exec(ctx, `DELETE FROM subscription_categories WHERE id = ANY($1)`, catIDs)
}
```

- [ ] **Step 4: Run test — expect PASS**

```bash
cd /Users/daryl/code/hermes && go test -tags=integration ./cmd/loadseed/ -run TestInsertSubscriptionTree -v
```

- [ ] **Step 5: Commit**

```bash
git add cmd/loadseed/subscriptions.go cmd/loadseed/subscriptions_test.go
git commit -m "feat(loadseed): insert subscription categories, subscriptions, templates"
```

### Task 2.5: Orchestrate end-to-end seed + write manifest

**Files:**
- Create: `cmd/loadseed/seeder.go`, `cmd/loadseed/seeder_test.go`

- [ ] **Step 1: Write failing integration test**

`cmd/loadseed/seeder_test.go`:

```go
//go:build integration

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hermes-notifications/hermes/internal/database"
)

func TestSeeder_EndToEnd(t *testing.T) {
	ctx := context.Background()
	pool, err := database.NewPool(ctx, envOr("HERMES_DATABASE_URL", "postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer pool.Close()

	out := filepath.Join(t.TempDir(), "manifest.json")
	cfg := Config{
		Tenants:                  2,
		UsersPerTenant:           50,
		CategoriesPerTenant:      2,
		SubscriptionsPerCategory: 2,
		TemplatesPerSubscription: 2,
		DatabaseURL:              envOr("HERMES_DATABASE_URL", "postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable"),
		HMACSecret:               envOr("HERMES_API_KEY_HMAC_SECRET", "hermes-dev-hmac-secret"),
		OutputPath:               out,
	}

	if err := runSeed(ctx, pool, cfg); err != nil {
		t.Fatalf("seed: %v", err)
	}
	defer func() { _ = runCleanup(ctx, pool, cfg) }()

	m, err := ReadManifest(out)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if len(m.Tenants) != 2 {
		t.Fatalf("want 2 tenants, got %d", len(m.Tenants))
	}
	if len(m.Tenants[0].Users) != 50 {
		t.Fatalf("want 50 users, got %d", len(m.Tenants[0].Users))
	}
	if m.APIKey == "" {
		t.Fatalf("api key not set")
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("manifest not written: %v", err)
	}
}
```

- [ ] **Step 2: Run test — expect FAIL**

```bash
cd /Users/daryl/code/hermes && go test -tags=integration ./cmd/loadseed/ -run TestSeeder_EndToEnd -v
```

- [ ] **Step 3: Write `seeder.go` (real `runSeed`, replacing the stub in main.go)**

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func runSeed(ctx context.Context, pool *pgxpool.Pool, cfg Config) error {
	rid := runID()
	m := &Manifest{
		SeededAt:  time.Now().UTC().Format(time.RFC3339),
		RunSeedID: rid,
	}

	rawKey, _, err := insertAPIKey(ctx, pool, cfg.HMACSecret, "loadtest-"+rid)
	if err != nil {
		return fmt.Errorf("api key: %w", err)
	}
	m.APIKey = rawKey

	tenantIDs, err := insertTenants(ctx, pool, cfg.Tenants, rid)
	if err != nil {
		return fmt.Errorf("tenants: %w", err)
	}

	m.Tenants = make([]Tenant, cfg.Tenants)
	for i, tid := range tenantIDs {
		userIDs, err := insertUsers(ctx, pool, tid, cfg.UsersPerTenant, rid, i)
		if err != nil {
			return fmt.Errorf("users[%d]: %w", i, err)
		}
		cats, err := insertSubscriptionTree(ctx, pool, rid, i,
			cfg.CategoriesPerTenant, cfg.SubscriptionsPerCategory, cfg.TemplatesPerSubscription)
		if err != nil {
			return fmt.Errorf("tree[%d]: %w", i, err)
		}
		m.Tenants[i] = Tenant{ID: tid, Users: userIDs, Categories: cats}
	}

	if err := m.Write(cfg.OutputPath); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	fmt.Printf("load-test seed complete: %d tenants, %d users, run_seed_id=%s\n  manifest=%s\n  api_key=%s\n",
		cfg.Tenants, cfg.Tenants*cfg.UsersPerTenant, rid, cfg.OutputPath, rawKey)
	return nil
}
```

Remove the earlier `runSeed` stub from `main.go`.

- [ ] **Step 4: Run test — expect PASS**

```bash
cd /Users/daryl/code/hermes && go test -tags=integration ./cmd/loadseed/ -run TestSeeder_EndToEnd -v
```

- [ ] **Step 5: Commit**

```bash
git add cmd/loadseed/seeder.go cmd/loadseed/seeder_test.go cmd/loadseed/main.go
git commit -m "feat(loadseed): orchestrate end-to-end seeding"
```

### Task 2.6: Cleanup mode

**Files:**
- Create: `cmd/loadseed/cleanup.go`

- [ ] **Step 1: Write `cleanup.go`**

```go
package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// runCleanup reads the manifest and deletes every seeded entity.
// Order respects FK constraints: templates → subscriptions → categories → users → tenants → api_key.
// users are deleted by the tenants cascade is not safe — users table has no ON DELETE CASCADE —
// so we delete users explicitly first.
func runCleanup(ctx context.Context, pool *pgxpool.Pool, cfg Config) error {
	m, err := ReadManifest(cfg.OutputPath)
	if err != nil {
		return err
	}

	var allTmpl, allSub, allCat, allUsers, allTenants []string
	for _, t := range m.Tenants {
		allTenants = append(allTenants, t.ID)
		allUsers = append(allUsers, t.Users...)
		for _, c := range t.Categories {
			allCat = append(allCat, c.ID)
			for _, s := range c.Subscriptions {
				allSub = append(allSub, s.ID)
				for _, tpl := range s.Templates {
					allTmpl = append(allTmpl, tpl.ID)
				}
			}
		}
	}

	if _, err := pool.Exec(ctx, `DELETE FROM notification_templates WHERE id = ANY($1)`, allTmpl); err != nil {
		return fmt.Errorf("delete templates: %w", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM subscriptions WHERE id = ANY($1)`, allSub); err != nil {
		return fmt.Errorf("delete subscriptions: %w", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM subscription_categories WHERE id = ANY($1)`, allCat); err != nil {
		return fmt.Errorf("delete categories: %w", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = ANY($1)`, allUsers); err != nil {
		return fmt.Errorf("delete users: %w", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM tenants WHERE id = ANY($1)`, allTenants); err != nil {
		return fmt.Errorf("delete tenants: %w", err)
	}
	// Delete the API key by matching its name (which includes runID).
	if _, err := pool.Exec(ctx, `DELETE FROM api_keys WHERE name = $1`, "loadtest-"+m.RunSeedID); err != nil {
		return fmt.Errorf("delete api key: %w", err)
	}
	fmt.Printf("load-test cleanup complete (run_seed_id=%s)\n", m.RunSeedID)
	return nil
}
```

Remove the earlier `runCleanup` stub from `main.go`.

- [ ] **Step 2: Manual verification**

```bash
cd /Users/daryl/code/hermes
# From a clean seeded state (run TestSeeder_EndToEnd which leaves a manifest in a temp dir):
go run ./cmd/loadseed --tenants 2 --users-per-tenant 10 --output /tmp/lt-manifest.json
psql $HERMES_DATABASE_URL -c "SELECT COUNT(*) FROM users WHERE id LIKE 'lt-%';"
go run ./cmd/loadseed --cleanup --output /tmp/lt-manifest.json
psql $HERMES_DATABASE_URL -c "SELECT COUNT(*) FROM users WHERE id LIKE 'lt-%';"
```

Expected: first count > 0, second count = 0.

- [ ] **Step 3: Commit**

```bash
git add cmd/loadseed/cleanup.go cmd/loadseed/main.go
git commit -m "feat(loadseed): cleanup mode deletes all seeded entities"
```

### Task 2.7: Add Makefile target for loadseed

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Append to `Makefile`**

At end of file:

```makefile
# --- Load testing ---
.PHONY: loadseed loadseed-clean
loadseed:          ## Seed load-test dataset (default: 10 tenants, 10k users each)
	go run ./cmd/loadseed \
	  --tenants $(or $(LT_TENANTS),10) \
	  --users-per-tenant $(or $(LT_USERS),10000) \
	  --output loadtest/seed-manifest.json

loadseed-clean:    ## Delete all entities from the current seed manifest
	go run ./cmd/loadseed --cleanup --output loadtest/seed-manifest.json
```

- [ ] **Step 2: Verify help works**

```bash
cd /Users/daryl/code/hermes && make help | grep loadseed
```

Expected: two `loadseed` lines printed.

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "build(loadtest): Makefile targets for seeder"
```

---

## Phase 3 — k6 library helpers

k6 JS has no type checker; verification is by running k6 itself. Every lib file must be importable by `k6 run`.

### Task 3.1: Seed manifest loader + pick helpers

**Files:**
- Create: `loadtest/lib/seed.js`, `loadtest/lib/seed-smoke.js`

- [ ] **Step 1: Write `loadtest/lib/seed.js`**

```javascript
import { SharedArray } from 'k6/data';

// Manifest is loaded once per k6 process into a SharedArray so N VUs share one copy.
// The manifest path is passed via env SEED_MANIFEST (default: loadtest/seed-manifest.json).
const manifestPath = __ENV.SEED_MANIFEST || 'loadtest/seed-manifest.json';

export const tenants = new SharedArray('tenants', function () {
  const raw = open(manifestPath);
  const m = JSON.parse(raw);
  return m.tenants;
});

export const manifest = new SharedArray('manifest_meta', function () {
  const raw = open(manifestPath);
  const m = JSON.parse(raw);
  return [{ api_key: m.api_key, run_seed_id: m.run_seed_id, seeded_at: m.seeded_at }];
});

export function apiKey() {
  return manifest[0].api_key;
}

export function runSeedID() {
  return manifest[0].run_seed_id;
}

// pickTenant returns a tenant object selected deterministically by VU+iter.
export function pickTenant() {
  const idx = (__VU + __ITER) % tenants.length;
  return tenants[idx];
}

export function pickUser(tenant) {
  const idx = (__VU * 31 + __ITER) % tenant.users.length;
  return tenant.users[idx];
}

// pickTemplate walks tenant.categories[*].subscriptions[*].templates[*]
// and selects one uniformly at random (per-iteration variance).
export function pickTemplate(tenant) {
  const all = [];
  for (const c of tenant.categories) {
    for (const s of c.subscriptions) {
      for (const t of s.templates) all.push(t);
    }
  }
  return all[(__VU * 17 + __ITER * 7) % all.length];
}

// instanceRange returns [start, end) user indices for THIS runner pod,
// based on env vars INSTANCE_ID and INSTANCE_COUNT injected by k6-operator.
// Used by the WS scenario so no two pods connect the same user.
export function instanceRange(totalCount) {
  const id = parseInt(__ENV.INSTANCE_ID || '0', 10);
  const n = parseInt(__ENV.INSTANCE_COUNT || '1', 10);
  const per = Math.ceil(totalCount / n);
  return [id * per, Math.min((id + 1) * per, totalCount)];
}
```

- [ ] **Step 2: Write smoke test runner**

`loadtest/lib/seed-smoke.js`:

```javascript
import { tenants, apiKey, pickTenant, pickUser, pickTemplate } from './seed.js';

export const options = { vus: 1, iterations: 1 };

export default function () {
  if (!apiKey()) throw new Error('api_key missing from manifest');
  if (tenants.length === 0) throw new Error('no tenants in manifest');
  const t = pickTenant();
  const u = pickUser(t);
  const tpl = pickTemplate(t);
  console.log(JSON.stringify({ tenant: t.id, user: u, template: tpl.id }));
}
```

- [ ] **Step 3: Seed a tiny dataset and run the smoke**

```bash
cd /Users/daryl/code/hermes
make infra-up
go run ./cmd/loadseed --tenants 1 --users-per-tenant 5 --categories-per-tenant 1 --subscriptions-per-category 1 --templates-per-subscription 1
k6 run loadtest/lib/seed-smoke.js
```

Expected output: a single JSON log line with tenant/user/template IDs. Run cleanup after:

```bash
go run ./cmd/loadseed --cleanup
```

- [ ] **Step 4: Commit**

```bash
git add loadtest/lib/seed.js loadtest/lib/seed-smoke.js
git commit -m "feat(loadtest/lib): seed manifest loader with SharedArray"
```

### Task 3.2: Auth helpers (bearer + JWT)

**Files:**
- Create: `loadtest/lib/auth.js`
- Modify: `loadtest/lib/seed-smoke.js`

- [ ] **Step 1: Look up how Hermes signs inbox JWTs**

```bash
cd /Users/daryl/code/hermes && grep -rn "HERMES_JWT_SECRET\|jwt.Sign\|SignedString" internal/auth/ internal/config/ | head -20
```

Expected: the JWT secret env var name (likely `HERMES_JWT_SECRET`) and the signing algorithm (HS256). Note these down; the JS code needs to match.

- [ ] **Step 2: Write `loadtest/lib/auth.js`**

```javascript
import crypto from 'k6/crypto';
import encoding from 'k6/encoding';
import { apiKey } from './seed.js';

// HMAC-SHA256 JWT minting. Matches Hermes's inbox/user JWT verification
// (HS256 with HERMES_JWT_SECRET). Pure JS so it runs inside k6 with no
// external deps.
function base64UrlEncode(s) {
  // k6's encoding.b64encode returns standard base64; convert to URL-safe.
  return encoding.b64encode(s, 'rawstd').replace(/\+/g, '-').replace(/\//g, '_');
}

export function jwtFor(userID, tenantID, opts) {
  const secret = __ENV.HERMES_JWT_SECRET || 'test-jwt-secret';
  const now = Math.floor(Date.now() / 1000);
  const exp = now + (opts && opts.ttlSeconds ? opts.ttlSeconds : 3600);
  const header = { alg: 'HS256', typ: 'JWT' };
  const payload = {
    sub: userID,
    tenant_id: tenantID,
    iat: now,
    exp: exp,
    // Additional claims matching Hermes's expected schema; expand if the
    // inbox middleware rejects the token during Task 4.2 smoke.
  };
  const hb = base64UrlEncode(JSON.stringify(header));
  const pb = base64UrlEncode(JSON.stringify(payload));
  const signingInput = `${hb}.${pb}`;
  const sig = crypto.hmac('sha256', secret, signingInput, 'binary');
  const sb = encoding.b64encode(sig, 'rawstd').replace(/\+/g, '-').replace(/\//g, '_');
  return `${signingInput}.${sb}`;
}

export function adminHeaders(extra) {
  const h = {
    'Authorization': `Bearer ${apiKey()}`,
    'Content-Type': 'application/json',
    'X-Load-Test-Run-Id': __ENV.RUN_ID || 'local',
  };
  if (extra) Object.assign(h, extra);
  return h;
}

export function userHeaders(userID, tenantID, extra) {
  const h = {
    'Authorization': `Bearer ${jwtFor(userID, tenantID)}`,
    'Content-Type': 'application/json',
    'X-Load-Test-Run-Id': __ENV.RUN_ID || 'local',
  };
  if (extra) Object.assign(h, extra);
  return h;
}
```

- [ ] **Step 3: Extend smoke to verify auth helpers load**

Replace `loadtest/lib/seed-smoke.js` contents:

```javascript
import { tenants, apiKey, pickTenant, pickUser, pickTemplate } from './seed.js';
import { adminHeaders, userHeaders, jwtFor } from './auth.js';

export const options = { vus: 1, iterations: 1 };

export default function () {
  if (!apiKey()) throw new Error('api_key missing');
  const t = pickTenant();
  const u = pickUser(t);
  const tpl = pickTemplate(t);
  const tok = jwtFor(u, t.id);
  if (tok.split('.').length !== 3) throw new Error('bad jwt');
  const ah = adminHeaders();
  const uh = userHeaders(u, t.id);
  if (!ah.Authorization.startsWith('Bearer ')) throw new Error('bad bearer');
  if (!uh.Authorization.startsWith('Bearer ')) throw new Error('bad user bearer');
  console.log(JSON.stringify({ tenant: t.id, user: u, template: tpl.id, jwtHead: tok.slice(0, 20) }));
}
```

- [ ] **Step 4: Run smoke**

```bash
cd /Users/daryl/code/hermes && k6 run loadtest/lib/seed-smoke.js
```

Expected: single JSON line printed, exit code 0.

- [ ] **Step 5: Commit**

```bash
git add loadtest/lib/auth.js loadtest/lib/seed-smoke.js
git commit -m "feat(loadtest/lib): admin bearer + per-user JWT helpers"
```

### Task 3.3: Custom metrics module

**Files:**
- Create: `loadtest/lib/metrics.js`

- [ ] **Step 1: Write `loadtest/lib/metrics.js`**

```javascript
import { Trend, Counter, Gauge } from 'k6/metrics';

export const sendAckLatency     = new Trend('send_ack_latency', true);
export const wsConnectLatency   = new Trend('ws_connect_latency', true);
export const wsPushE2ELatency   = new Trend('ws_push_e2e_latency', true);
export const inboxListLatency   = new Trend('inbox_list_latency', true);
export const wsConnectionActive = new Gauge('ws_connection_active');
export const wsConnectionDrops  = new Counter('ws_connection_drops');
export const sendErrors         = new Counter('send_errors');
export const pushReceived       = new Counter('ws_push_received');
```

- [ ] **Step 2: Commit**

```bash
git add loadtest/lib/metrics.js
git commit -m "feat(loadtest/lib): custom k6 metrics"
```

### Task 3.4: Send payload builder

**Files:**
- Create: `loadtest/lib/payloads.js`

- [ ] **Step 1: Write `loadtest/lib/payloads.js`**

```javascript
import { uuidv4 } from 'https://jslib.k6.io/k6-utils/1.4.0/index.js';

// buildSendBody constructs a POST /v1/send request body.
// Channel selection is weighted by the env var CHANNEL_WEIGHTS (e.g., "inbox:70,email:30").
// Inline content path: pass opts.inline = true to bypass template lookup.
export function buildSendBody(tenant, userID, template, opts) {
  const channel = pickChannel(template.channels);
  if (opts && opts.inline) {
    return {
      to: { tenant_id: tenant.id, user_id: userID },
      channels: [channel],
      content: {
        inbox: channel === 'inbox' ? { title: 'Load test', body: 'Inline body ' + uuidv4() } : undefined,
        email: channel === 'email' ? { subject: 'Load test', body: 'Inline body ' + uuidv4() } : undefined,
      },
    };
  }
  return {
    to: { tenant_id: tenant.id, user_id: userID },
    channels: [channel],
    template: template.id,
    data: { subject: 'Load test ' + uuidv4().slice(0, 8), name: userID },
  };
}

function pickChannel(allowed) {
  const weights = parseWeights(__ENV.CHANNEL_WEIGHTS || 'inbox:70,email:30');
  const filtered = weights.filter(w => allowed.includes(w.channel));
  const total = filtered.reduce((s, w) => s + w.weight, 0);
  let r = Math.random() * total;
  for (const w of filtered) {
    r -= w.weight;
    if (r <= 0) return w.channel;
  }
  return filtered[0].channel;
}

function parseWeights(s) {
  return s.split(',').map(p => {
    const [c, w] = p.split(':');
    return { channel: c.trim(), weight: parseInt(w, 10) };
  });
}

export function idempotencyKey() {
  return uuidv4();
}
```

- [ ] **Step 2: Commit**

```bash
git add loadtest/lib/payloads.js
git commit -m "feat(loadtest/lib): send payload builder with channel weighting"
```

### Task 3.5: Iteration-timestamp shared map

**Files:**
- Create: `loadtest/lib/shared.js`

`ws_push_e2e_latency` correlates sends with WS pushes. The send scenario records `(notification_id → sent_at_ms)` and the WS scenario looks it up on push receive.

- [ ] **Step 1: Write `loadtest/lib/shared.js`**

```javascript
// Per-process in-memory map. k6 scenarios in the same test (send + ws) share a
// single JS runtime per VU, but do NOT share state across VUs. For e2e latency,
// the send scenario and ws scenario run in the SAME VU pool in inbox-mixed.js,
// so this map works for sends that happen on the same VU.
// For the current iteration of the design this is sufficient — we measure e2e
// on the subset of traffic where send and ws are paired by VU.
const sentAt = new Map();

export function recordSent(notificationID) {
  sentAt.set(notificationID, Date.now());
}

export function takeSent(notificationID) {
  const t = sentAt.get(notificationID);
  if (t !== undefined) sentAt.delete(notificationID);
  return t;
}
```

- [ ] **Step 2: Commit**

```bash
git add loadtest/lib/shared.js
git commit -m "feat(loadtest/lib): shared send->push timestamp map"
```

### Task 3.6: Centrifugo WS client wrapper

**Files:**
- Create: `loadtest/lib/centrifugo.js`

- [ ] **Step 1: Check what Hermes exposes for Centrifugo client auth**

```bash
cd /Users/daryl/code/hermes && grep -rn "centrifugo\|centrifugal" internal/ deploy/centrifugo/ | head -10
```

Note the Centrifugo WS URL pattern (typically `ws://host:port/connection/websocket`) and the connection-token scheme. If tokens are minted by the inbox service, record the endpoint; otherwise assume Hermes uses the same HS256 JWT via `user_id` claim.

- [ ] **Step 2: Write `loadtest/lib/centrifugo.js`**

```javascript
import ws from 'k6/ws';
import { jwtFor } from './auth.js';
import {
  wsConnectLatency, wsConnectionActive, wsConnectionDrops,
  pushReceived, wsPushE2ELatency,
} from './metrics.js';
import { takeSent } from './shared.js';

// centrifugoURL defaults to the in-cluster ingress at /connection/websocket.
// Override with CENTRIFUGO_URL (e.g., ws://localhost:8888/connection/websocket).
export function centrifugoURL() {
  return __ENV.CENTRIFUGO_URL || 'ws://localhost:8888/connection/websocket';
}

// connect opens a Centrifugo WS connection for the given user, subscribes to
// their personal channel, and invokes onPush(notification_id) for each
// incoming publication. Blocks until the socket is closed by setTimeout.
export function connect(userID, tenantID, onPush) {
  const url = centrifugoURL();
  const token = jwtFor(userID, tenantID);

  const start = Date.now();
  return ws.connect(url, {}, function (socket) {
    wsConnectionActive.add(1);

    socket.on('open', function () {
      wsConnectLatency.add(Date.now() - start);
      // Centrifugo v5 client protocol: connect + subscribe.
      socket.send(JSON.stringify({ id: 1, connect: { token: token, name: 'k6-loadtest' } }));
      socket.send(JSON.stringify({ id: 2, subscribe: { channel: `user#${userID}` } }));
    });

    socket.on('message', function (data) {
      let msg;
      try { msg = JSON.parse(data); } catch (e) { return; }
      // Centrifugo publications arrive as { push: { channel, pub: { data: {...} } } }.
      if (msg.push && msg.push.pub && msg.push.pub.data) {
        pushReceived.add(1);
        const payload = msg.push.pub.data;
        if (payload.notification_id) onPush(payload.notification_id);
      }
    });

    socket.on('close', function () { wsConnectionActive.add(-1); });
    socket.on('error', function (_e) { wsConnectionDrops.add(1); });

    socket.setTimeout(function () { socket.close(); },
      (parseInt(__ENV.WS_HOLD_SECONDS || '60', 10)) * 1000);
  });
}

// recordE2EOnPush looks up the send timestamp in the shared map and, if
// present, records the end-to-end latency trend.
export function recordE2EOnPush(notificationID) {
  const t = takeSent(notificationID);
  if (t !== undefined) wsPushE2ELatency.add(Date.now() - t);
}
```

- [ ] **Step 3: Commit**

```bash
git add loadtest/lib/centrifugo.js
git commit -m "feat(loadtest/lib): Centrifugo WS connect helper"
```

---

## Phase 4 — Scenarios

### Task 4.1: Send scenario

**Files:**
- Create: `loadtest/scenarios/send.js`

- [ ] **Step 1: Write `loadtest/scenarios/send.js`**

```javascript
import http from 'k6/http';
import { check } from 'k6';
import { adminHeaders } from '../lib/auth.js';
import { pickTenant, pickUser, pickTemplate } from '../lib/seed.js';
import { buildSendBody, idempotencyKey } from '../lib/payloads.js';
import { sendAckLatency, sendErrors } from '../lib/metrics.js';
import { recordSent } from '../lib/shared.js';

const TARGET_RPS = parseInt(__ENV.TARGET_RPS || '100', 10);
const DURATION   = __ENV.DURATION || '1m';
const PREALLOC   = parseInt(__ENV.PREALLOC_VUS || String(Math.max(50, TARGET_RPS / 10)), 10);
const MAX_VUS    = parseInt(__ENV.MAX_VUS || String(PREALLOC * 4), 10);
const ADMIN_URL  = __ENV.ADMIN_URL || 'http://localhost:8080';

// Shard the target rate across pods when running under k6-operator.
const instanceCount = parseInt(__ENV.INSTANCE_COUNT || '1', 10);
const perPodRate    = Math.max(1, Math.floor(TARGET_RPS / instanceCount));

export const options = {
  scenarios: {
    send: {
      executor: 'constant-arrival-rate',
      rate: perPodRate,
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: PREALLOC,
      maxVUs: MAX_VUS,
    },
  },
  thresholds: {
    send_ack_latency: ['p(99)<200'],
    http_req_failed: ['rate<0.01'],
  },
  tags: {
    scenario: 'send',
    run_id: __ENV.RUN_ID || 'local',
    instance_id: __ENV.INSTANCE_ID || '0',
    parallelism: __ENV.INSTANCE_COUNT || '1',
  },
};

export default function () {
  const t = pickTenant();
  const u = pickUser(t);
  const tpl = pickTemplate(t);
  const body = buildSendBody(t, u, tpl);
  const headers = adminHeaders({ 'X-Idempotency-Key': idempotencyKey() });

  const start = Date.now();
  const res = http.post(`${ADMIN_URL}/v1/send`, JSON.stringify(body), { headers });
  sendAckLatency.add(Date.now() - start);

  const ok = check(res, {
    'status 202': r => r.status === 202,
  });
  if (!ok) {
    sendErrors.add(1);
    return;
  }

  try {
    const parsed = JSON.parse(res.body);
    if (parsed.notification_id) recordSent(parsed.notification_id);
  } catch (e) { /* body not JSON — already failed above */ }
}
```

- [ ] **Step 2: Smoke the send scenario against local Hermes**

```bash
cd /Users/daryl/code/hermes
make infra-up
# Start admin service in another terminal or via tilt
go run ./cmd/loadseed --tenants 1 --users-per-tenant 10 --categories-per-tenant 1 --subscriptions-per-category 1 --templates-per-subscription 1
TARGET_RPS=5 DURATION=10s ADMIN_URL=http://localhost:8080 RUN_ID=smoke k6 run loadtest/scenarios/send.js
```

Expected: ~50 iterations, all 202s, exit code 0.

- [ ] **Step 3: Commit**

```bash
git add loadtest/scenarios/send.js
git commit -m "feat(loadtest): send scenario (constant-arrival-rate, sharded)"
```

### Task 4.2: Inbox-mixed scenario

**Files:**
- Create: `loadtest/scenarios/inbox-mixed.js`

- [ ] **Step 1: Write `loadtest/scenarios/inbox-mixed.js`**

```javascript
import http from 'k6/http';
import { sleep, check } from 'k6';
import { tenants, pickTenant, pickUser, pickTemplate, instanceRange } from '../lib/seed.js';
import { adminHeaders, userHeaders } from '../lib/auth.js';
import { buildSendBody, idempotencyKey } from '../lib/payloads.js';
import { connect, recordE2EOnPush } from '../lib/centrifugo.js';
import { sendAckLatency, sendErrors, inboxListLatency } from '../lib/metrics.js';
import { recordSent } from '../lib/shared.js';

const VUS        = parseInt(__ENV.VUS || '100', 10);
const SEND_RPS   = parseInt(__ENV.SEND_RPS || '50', 10);
const POLL_RPS   = parseInt(__ENV.POLL_RPS || '10', 10);
const DURATION   = __ENV.DURATION || '1m';
const ADMIN_URL  = __ENV.ADMIN_URL || 'http://localhost:8080';
const INBOX_URL  = __ENV.INBOX_URL || 'http://localhost:8086';

const instCount  = parseInt(__ENV.INSTANCE_COUNT || '1', 10);
const perPodVUs  = Math.max(1, Math.floor(VUS / instCount));
const perPodSend = Math.max(1, Math.floor(SEND_RPS / instCount));
const perPodPoll = Math.max(1, Math.floor(POLL_RPS / instCount));

// Flatten all (tenant, user) pairs for this instance's shard.
const allPairs = (function () {
  const pairs = [];
  for (const t of tenants) {
    for (const u of t.users) pairs.push({ tenant: t, user: u });
  }
  const [s, e] = instanceRange(pairs.length);
  return pairs.slice(s, e);
})();

export const options = {
  scenarios: {
    ws: {
      executor: 'constant-vus',
      vus: perPodVUs,
      duration: DURATION,
      exec: 'wsHold',
    },
    send: {
      executor: 'constant-arrival-rate',
      rate: perPodSend,
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: Math.max(50, perPodSend),
      maxVUs: Math.max(100, perPodSend * 4),
      exec: 'drive',
    },
    poll: {
      executor: 'constant-arrival-rate',
      rate: perPodPoll,
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: Math.max(10, perPodPoll),
      maxVUs: Math.max(50, perPodPoll * 4),
      exec: 'pollInbox',
    },
  },
  thresholds: {
    send_ack_latency: ['p(99)<200'],
    ws_push_e2e_latency: ['p(95)<1000'],
    http_req_failed: ['rate<0.01'],
  },
  tags: {
    scenario: 'inbox-mixed',
    run_id: __ENV.RUN_ID || 'local',
    instance_id: __ENV.INSTANCE_ID || '0',
    parallelism: __ENV.INSTANCE_COUNT || '1',
  },
};

function vuPair() {
  return allPairs[(__VU + __ITER) % allPairs.length];
}

export function wsHold() {
  const p = allPairs[__VU % allPairs.length];
  connect(p.user, p.tenant.id, recordE2EOnPush);
  // ws.connect blocks until the socket is closed by setTimeout inside the helper.
}

export function drive() {
  const p = vuPair();
  const tpl = pickTemplate(p.tenant);
  const body = buildSendBody(p.tenant, p.user, tpl);
  const headers = adminHeaders({ 'X-Idempotency-Key': idempotencyKey() });
  const start = Date.now();
  const res = http.post(`${ADMIN_URL}/v1/send`, JSON.stringify(body), { headers });
  sendAckLatency.add(Date.now() - start);
  if (res.status !== 202) { sendErrors.add(1); return; }
  try {
    const parsed = JSON.parse(res.body);
    if (parsed.notification_id) recordSent(parsed.notification_id);
  } catch (e) {}
}

export function pollInbox() {
  const p = vuPair();
  const h = userHeaders(p.user, p.tenant.id);
  const start = Date.now();
  const res = http.get(`${INBOX_URL}/v1/inbox/notifications?limit=20`, { headers: h });
  inboxListLatency.add(Date.now() - start);
  check(res, { 'inbox 200': r => r.status === 200 });
}
```

- [ ] **Step 2: Smoke run**

```bash
cd /Users/daryl/code/hermes
# Hermes (admin + inbox + centrifugo) must be running via tilt or docker-compose
VUS=10 SEND_RPS=5 POLL_RPS=2 DURATION=20s RUN_ID=smoke \
  ADMIN_URL=http://localhost:8080 \
  INBOX_URL=http://localhost:8086 \
  CENTRIFUGO_URL=ws://localhost:8000/connection/websocket \
  WS_HOLD_SECONDS=20 \
  k6 run loadtest/scenarios/inbox-mixed.js
```

Expected: non-zero `ws_push_e2e_latency` samples; exit code 0. If Centrifugo URL is wrong, adjust `CENTRIFUGO_URL` per `deploy/centrifugo/` config.

- [ ] **Step 3: Commit**

```bash
git add loadtest/scenarios/inbox-mixed.js
git commit -m "feat(loadtest): inbox-mixed scenario (ws + send + poll)"
```

### Task 4.3: Soak scenario

**Files:**
- Create: `loadtest/scenarios/soak.js`

- [ ] **Step 1: Write `loadtest/scenarios/soak.js`**

```javascript
// Soak is inbox-mixed at ~30% of capacity levels for a long duration.
// We re-export inbox-mixed's exec functions so the scenario code stays in one place.
export { wsHold, drive, pollInbox } from './inbox-mixed.js';
import { tenants } from '../lib/seed.js';

const VUS      = parseInt(__ENV.VUS || '1000', 10);
const SEND_RPS = parseInt(__ENV.SEND_RPS || '100', 10);
const POLL_RPS = parseInt(__ENV.POLL_RPS || '20', 10);
const DURATION = __ENV.DURATION || '4h';

const instCount  = parseInt(__ENV.INSTANCE_COUNT || '1', 10);
const perPodVUs  = Math.max(1, Math.floor(VUS / instCount));
const perPodSend = Math.max(1, Math.floor(SEND_RPS / instCount));
const perPodPoll = Math.max(1, Math.floor(POLL_RPS / instCount));

export const options = {
  scenarios: {
    ws:   { executor: 'constant-vus', vus: perPodVUs, duration: DURATION, exec: 'wsHold' },
    send: {
      executor: 'constant-arrival-rate', rate: perPodSend, timeUnit: '1s', duration: DURATION,
      preAllocatedVUs: Math.max(50, perPodSend), maxVUs: Math.max(100, perPodSend * 4), exec: 'drive',
    },
    poll: {
      executor: 'constant-arrival-rate', rate: perPodPoll, timeUnit: '1s', duration: DURATION,
      preAllocatedVUs: Math.max(10, perPodPoll), maxVUs: Math.max(50, perPodPoll * 4), exec: 'pollInbox',
    },
  },
  thresholds: {
    send_ack_latency: ['p(99)<200'],
    http_req_failed: ['rate<0.005'],
    ws_connection_drops: ['count<' + (VUS * 0.01 * (parseInt(DURATION) || 4))],
  },
  tags: {
    scenario: 'soak',
    run_id: __ENV.RUN_ID || 'local',
    instance_id: __ENV.INSTANCE_ID || '0',
    parallelism: __ENV.INSTANCE_COUNT || '1',
  },
};
```

- [ ] **Step 2: Syntax-check by invoking k6**

```bash
cd /Users/daryl/code/hermes
k6 inspect loadtest/scenarios/soak.js
```

Expected: prints the resolved options without error.

- [ ] **Step 3: Commit**

```bash
git add loadtest/scenarios/soak.js
git commit -m "feat(loadtest): soak scenario (reuses inbox-mixed execs)"
```

### Task 4.4: handleSummary → JSON artifact

**Files:**
- Create: `loadtest/lib/summary.js`
- Modify: `loadtest/scenarios/send.js`, `loadtest/scenarios/inbox-mixed.js`, `loadtest/scenarios/soak.js`

- [ ] **Step 1: Write `loadtest/lib/summary.js`**

```javascript
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.1.0/index.js';

export function handleSummary(data) {
  const out = {};
  const runID = __ENV.RUN_ID || 'local';
  out[`artifacts/${runID}/summary.json`] = JSON.stringify(data, null, 2);
  out['stdout'] = textSummary(data, { indent: ' ', enableColors: true });
  return out;
}
```

- [ ] **Step 2: Export from each scenario**

In each of `send.js`, `inbox-mixed.js`, `soak.js`, add at the top of the file (after existing imports):

```javascript
export { handleSummary } from '../lib/summary.js';
```

- [ ] **Step 3: Verify one scenario runs and writes artifact**

```bash
cd /Users/daryl/code/hermes
mkdir -p artifacts
TARGET_RPS=2 DURATION=5s RUN_ID=smoke k6 run loadtest/scenarios/send.js
ls artifacts/smoke/summary.json
```

Expected: file exists with per-metric summary JSON.

- [ ] **Step 4: Commit**

```bash
git add loadtest/lib/summary.js loadtest/scenarios/send.js loadtest/scenarios/inbox-mixed.js loadtest/scenarios/soak.js
git commit -m "feat(loadtest): write per-run summary.json artifact"
```

---

## Phase 5 — Local infra (Prometheus + Grafana)

### Task 5.1: Docker-compose extension + Prom config

**Files:**
- Create: `loadtest/docker-compose.loadtest.yml`, `loadtest/prometheus/prometheus.yml`, `loadtest/grafana/provisioning/datasources/prometheus.yaml`, `loadtest/grafana/provisioning/dashboards/dashboards.yaml`

- [ ] **Step 1: Write `loadtest/prometheus/prometheus.yml`**

```yaml
global:
  scrape_interval: 15s

# k6 pushes via remote-write on the receiver endpoint.
# Enabled by the --web.enable-remote-write-receiver flag on startup.
scrape_configs: []
```

- [ ] **Step 2: Write `loadtest/docker-compose.loadtest.yml`**

```yaml
services:
  loadtest-prometheus:
    image: prom/prometheus:v2.55.0
    command:
      - --config.file=/etc/prometheus/prometheus.yml
      - --web.enable-remote-write-receiver
      - --storage.tsdb.retention.time=24h
    ports:
      - "9090:9090"
    volumes:
      - ./loadtest/prometheus/prometheus.yml:/etc/prometheus/prometheus.yml:ro

  loadtest-grafana:
    image: grafana/grafana:11.3.0
    environment:
      GF_AUTH_ANONYMOUS_ENABLED: "true"
      GF_AUTH_ANONYMOUS_ORG_ROLE: "Admin"
      GF_SECURITY_ADMIN_PASSWORD: "admin"
    ports:
      - "3001:3000"
    volumes:
      - ./loadtest/grafana/provisioning:/etc/grafana/provisioning:ro
      - ./loadtest/dashboards:/var/lib/grafana/dashboards:ro
    depends_on:
      - loadtest-prometheus
```

- [ ] **Step 3: Write Grafana provisioning**

`loadtest/grafana/provisioning/datasources/prometheus.yaml`:

```yaml
apiVersion: 1
datasources:
  - name: Prometheus
    type: prometheus
    access: proxy
    url: http://loadtest-prometheus:9090
    isDefault: true
```

`loadtest/grafana/provisioning/dashboards/dashboards.yaml`:

```yaml
apiVersion: 1
providers:
  - name: 'loadtest'
    orgId: 1
    folder: 'Load Test'
    type: file
    disableDeletion: false
    updateIntervalSeconds: 10
    options:
      path: /var/lib/grafana/dashboards
```

- [ ] **Step 4: Placeholder dashboard so Grafana starts cleanly**

`loadtest/dashboards/load-test.json`:

```json
{
  "title": "Load Test",
  "uid": "loadtest",
  "schemaVersion": 39,
  "version": 1,
  "refresh": "10s",
  "panels": [],
  "templating": { "list": [] }
}
```

- [ ] **Step 5: Verify the stack starts**

```bash
cd /Users/daryl/code/hermes
docker compose -f docker-compose.yml -f loadtest/docker-compose.loadtest.yml up -d loadtest-prometheus loadtest-grafana
curl -sf http://localhost:9090/-/ready
curl -sf http://localhost:3001/api/health
docker compose -f docker-compose.yml -f loadtest/docker-compose.loadtest.yml stop loadtest-prometheus loadtest-grafana
```

Expected: both health checks return OK.

- [ ] **Step 6: Commit**

```bash
git add loadtest/docker-compose.loadtest.yml loadtest/prometheus/ loadtest/grafana/ loadtest/dashboards/
git commit -m "feat(loadtest): docker-compose Prometheus + Grafana for local runs"
```

### Task 5.2: Grafana dashboard JSON

**Files:**
- Modify: `loadtest/dashboards/load-test.json`

- [ ] **Step 1: Build a real dashboard with the critical panels**

Replace `loadtest/dashboards/load-test.json` contents with a dashboard containing panels for (minimum): actual send rate (`rate(iterations{scenario="send"}[30s])`), send_ack_latency p50/p95/p99 (`histogram_quantile` over `k6_send_ack_latency`), error rate (`rate(http_req_failed{expected_response="true"}[30s])`), ws_connection_active, ws_connection_drops, ws_push_e2e_latency p95, inbox_list_latency p95, VU count.

Rather than write the full ~400-line JSON here, use Grafana UI to build it:

```bash
# Start the stack, send some traffic so metrics exist, open Grafana, build the dashboard, then export JSON.
cd /Users/daryl/code/hermes
docker compose -f docker-compose.yml -f loadtest/docker-compose.loadtest.yml up -d
TARGET_RPS=5 DURATION=30s RUN_ID=dash-build k6 run \
  --out experimental-prometheus-rw=http://localhost:9090/api/v1/write \
  loadtest/scenarios/send.js
# Open http://localhost:3001, log in (admin/admin), build the dashboard with the panels above.
# Share → Export → Save to file → overwrite loadtest/dashboards/load-test.json
```

Required panels (engineer verifies each shows data before exporting):

| Panel | PromQL |
|---|---|
| Send rate | `sum by (scenario)(rate(k6_iterations_total{scenario="send"}[30s]))` |
| Send p99 | `histogram_quantile(0.99, sum by (le)(rate(k6_send_ack_latency_bucket{run_id="$run_id"}[30s])))` |
| Send p50/p95 | same as above with 0.5 / 0.95 |
| Error rate | `sum(rate(k6_http_req_failed_total{run_id="$run_id"}[30s])) / sum(rate(k6_http_reqs_total{run_id="$run_id"}[30s]))` |
| WS active | `k6_ws_connection_active{run_id="$run_id"}` |
| WS drops | `rate(k6_ws_connection_drops_total{run_id="$run_id"}[1m])` |
| E2E p95 | `histogram_quantile(0.95, sum by (le)(rate(k6_ws_push_e2e_latency_bucket{run_id="$run_id"}[30s])))` |
| Inbox p95 | `histogram_quantile(0.95, sum by (le)(rate(k6_inbox_list_latency_bucket{run_id="$run_id"}[30s])))` |
| Active VUs | `k6_vus{run_id="$run_id"}` |

Dashboard must have a `run_id` template variable sourced from `label_values(k6_iterations_total, run_id)`.

- [ ] **Step 2: Verify imported dashboard loads**

Restart Grafana (`docker compose restart loadtest-grafana`). Open `http://localhost:3001/d/loadtest/load-test`. All panels should render (might be empty if no traffic is flowing — that's fine).

- [ ] **Step 3: Commit**

```bash
git add loadtest/dashboards/load-test.json
git commit -m "feat(loadtest): Grafana dashboard for load runs"
```

### Task 5.3: Local Makefile targets

**Files:**
- Create: `loadtest/scripts/run-local.sh`
- Modify: `Makefile`

- [ ] **Step 1: Write `loadtest/scripts/run-local.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail

SCENARIO="${SCENARIO:?SCENARIO required (send|inbox-mixed|soak)}"
RUN_ID="${RUN_ID:-$(date +%Y%m%d-%H%M%S)-$RANDOM}"
export RUN_ID

cd "$(dirname "$0")/../.."

# Ensure Prom+Grafana up
docker compose -f docker-compose.yml -f loadtest/docker-compose.loadtest.yml up -d loadtest-prometheus loadtest-grafana

# Ensure a seed manifest exists
if [ ! -f loadtest/seed-manifest.json ]; then
  echo "Seeding (default sizes)…"
  go run ./cmd/loadseed
fi

mkdir -p "artifacts/$RUN_ID"

k6 run \
  --out experimental-prometheus-rw=http://localhost:9090/api/v1/write \
  --tag run_id="$RUN_ID" \
  "loadtest/scenarios/${SCENARIO}.js"

echo ""
echo "Run complete: $RUN_ID"
echo "Summary: artifacts/$RUN_ID/summary.json"
echo "Dashboard: http://localhost:3001/d/loadtest/load-test?var-run_id=$RUN_ID&from=now-10m&to=now"
```

Make executable: `chmod +x loadtest/scripts/run-local.sh`.

- [ ] **Step 2: Add Makefile targets**

Append to `Makefile` (under the `# --- Load testing ---` section from Task 2.7):

```makefile
.PHONY: loadtest-local loadtest-local-clean
loadtest-local:    ## Run a local load test (SCENARIO=send|inbox-mixed|soak TARGET_RPS=... DURATION=...)
	SCENARIO=$(or $(SCENARIO),send) \
	TARGET_RPS=$(or $(TARGET_RPS),50) \
	VUS=$(or $(VUS),50) \
	DURATION=$(or $(DURATION),30s) \
	ADMIN_URL=$(or $(ADMIN_URL),http://localhost:8080) \
	INBOX_URL=$(or $(INBOX_URL),http://localhost:8086) \
	CENTRIFUGO_URL=$(or $(CENTRIFUGO_URL),ws://localhost:8000/connection/websocket) \
	loadtest/scripts/run-local.sh

loadtest-local-clean: ## Tear down local load-test infra and clean seed
	docker compose -f docker-compose.yml -f loadtest/docker-compose.loadtest.yml down -v
	[ -f loadtest/seed-manifest.json ] && go run ./cmd/loadseed --cleanup || true
	rm -f loadtest/seed-manifest.json
```

- [ ] **Step 3: End-to-end smoke**

```bash
cd /Users/daryl/code/hermes
make infra-up
# Start admin/inbox/centrifugo (tilt or docker)
make loadtest-local SCENARIO=send TARGET_RPS=5 DURATION=10s
# Verify artifacts/<run_id>/summary.json exists and Grafana URL printed
```

Expected: test completes, artifact written, Grafana URL printed.

- [ ] **Step 4: Commit**

```bash
git add loadtest/scripts/run-local.sh Makefile
git commit -m "feat(loadtest): local Makefile targets and runner script"
```

---

## Phase 6 — Kubernetes infra

### Task 6.1: Namespace + node-pool docs

**Files:**
- Create: `loadtest/k8s/namespace.yaml`, `loadtest/k8s/node-pool.md`

- [ ] **Step 1: Write `loadtest/k8s/namespace.yaml`**

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: loadtest
  labels:
    purpose: load-testing
```

- [ ] **Step 2: Write `loadtest/k8s/node-pool.md`**

```markdown
# Load-test node pool

Load-test generator pods must run on a dedicated node pool so they do not compete with Hermes services for CPU, memory, or network.

## Pool spec

- **Name:** `loadtest-generators`
- **Instance type:** `c7g.2xlarge` (Graviton, 8 vCPU, 16 GiB) — matches the project's multi-cloud/ARM preference.
- **Autoscaling:** min 0 / max sized per planned parallelism (each TestRun pod requests 2 vCPU / 4 GiB; budget = `parallelism × 2` vCPU headroom + 20%).
- **Taint:** `loadtest=true:NoSchedule`
- **Label:** `pool=loadtest-generators`

## Taint tolerations

Every pod in the `loadtest` namespace must tolerate the taint and node-select onto the pool. Applied by every manifest in `loadtest/k8s/` via:

```yaml
tolerations:
  - key: loadtest
    operator: Equal
    value: "true"
    effect: NoSchedule
nodeSelector:
  pool: loadtest-generators
```

## IaC

The actual pool is created by Terraform / Crossplane under `infra/` in the staging cluster. This document is the contract; add the pool to the staging cluster's node group spec before running cluster-mode load tests.
```

- [ ] **Step 3: Commit**

```bash
git add loadtest/k8s/namespace.yaml loadtest/k8s/node-pool.md
git commit -m "feat(loadtest/k8s): namespace and node-pool contract doc"
```

### Task 6.2: Prometheus + Grafana cluster install config

**Files:**
- Create: `loadtest/k8s/prometheus-values.yaml`, `loadtest/k8s/grafana-values.yaml`, `loadtest/k8s/install.sh`

Use the standalone Prometheus and Grafana Helm charts (not `kube-prometheus-stack`) to keep the install minimal.

- [ ] **Step 1: Write `loadtest/k8s/prometheus-values.yaml`**

```yaml
# prometheus-community/prometheus
server:
  extraArgs:
    web.enable-remote-write-receiver: ""
  persistentVolume:
    enabled: true
    size: 20Gi
  retention: 24h
  tolerations:
    - key: loadtest
      operator: Equal
      value: "true"
      effect: NoSchedule
  nodeSelector:
    pool: loadtest-generators
  resources:
    requests: { cpu: 500m, memory: 2Gi }
    limits:   { cpu: 2,    memory: 4Gi }

alertmanager:
  enabled: false
pushgateway:
  enabled: false
prometheus-node-exporter:
  enabled: false
kube-state-metrics:
  enabled: false
```

- [ ] **Step 2: Write `loadtest/k8s/grafana-values.yaml`**

```yaml
# grafana/grafana
adminPassword: admin
service:
  type: ClusterIP
tolerations:
  - key: loadtest
    operator: Equal
    value: "true"
    effect: NoSchedule
nodeSelector:
  pool: loadtest-generators
datasources:
  datasources.yaml:
    apiVersion: 1
    datasources:
      - name: Prometheus
        type: prometheus
        access: proxy
        url: http://loadtest-prometheus-server
        isDefault: true
dashboardProviders:
  dashboardproviders.yaml:
    apiVersion: 1
    providers:
      - name: loadtest
        orgId: 1
        folder: Load Test
        type: file
        disableDeletion: false
        options:
          path: /var/lib/grafana/dashboards/loadtest
dashboardsConfigMaps:
  loadtest: loadtest-dashboards
```

- [ ] **Step 3: Write `loadtest/k8s/install.sh`**

```bash
#!/usr/bin/env bash
# One-time installer for the load-test observability + runner stack.
# Assumes kubectl context points at the target cluster and you have cluster-admin.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

kubectl apply -f "$SCRIPT_DIR/namespace.yaml"

# Helm repos
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts >/dev/null 2>&1 || true
helm repo add grafana https://grafana.github.io/helm-charts >/dev/null 2>&1 || true
helm repo add grafana-k6 https://grafana.github.io/helm-charts >/dev/null 2>&1 || true
helm repo update >/dev/null

# k6-operator
helm upgrade --install k6-operator grafana/k6-operator \
  --namespace loadtest \
  --set tolerations[0].key=loadtest \
  --set tolerations[0].operator=Equal \
  --set tolerations[0].value=true \
  --set tolerations[0].effect=NoSchedule \
  --set nodeSelector.pool=loadtest-generators

# Prometheus
helm upgrade --install loadtest-prometheus prometheus-community/prometheus \
  --namespace loadtest \
  -f "$SCRIPT_DIR/prometheus-values.yaml"

# Dashboard ConfigMap (sourced from loadtest/dashboards/)
kubectl -n loadtest create configmap loadtest-dashboards \
  --from-file="$SCRIPT_DIR/../dashboards/" \
  --dry-run=client -o yaml | kubectl apply -f -

# Grafana
helm upgrade --install loadtest-grafana grafana/grafana \
  --namespace loadtest \
  -f "$SCRIPT_DIR/grafana-values.yaml"

echo ""
echo "Install complete. Port-forward Grafana:"
echo "  kubectl -n loadtest port-forward svc/loadtest-grafana 3001:80"
```

Make executable: `chmod +x loadtest/k8s/install.sh`.

- [ ] **Step 4: Commit**

```bash
git add loadtest/k8s/prometheus-values.yaml loadtest/k8s/grafana-values.yaml loadtest/k8s/install.sh
git commit -m "feat(loadtest/k8s): cluster install of k6-operator, Prom, Grafana"
```

### Task 6.3: TestRun + loadseed Job templates

**Files:**
- Create: `loadtest/k8s/testrun.yaml`, `loadtest/k8s/loadseed-job.yaml`, `loadtest/k8s/scenarios-configmap.yaml`

- [ ] **Step 1: Write `loadtest/k8s/scenarios-configmap.yaml`**

```yaml
# Generated on each run by `make loadtest-k8s`. This is a template, not applied directly.
apiVersion: v1
kind: ConfigMap
metadata:
  name: loadtest-scenarios
  namespace: loadtest
data:
  # Populated at runtime from loadtest/scenarios/*.js and loadtest/lib/*.js.
  # The runner script flattens lib/ imports into each scenario via `k6 compile` or
  # ships the whole tree — see loadtest/scripts/run-k8s.sh.
```

- [ ] **Step 2: Write `loadtest/k8s/loadseed-job.yaml`**

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: loadseed
  namespace: loadtest
spec:
  backoffLimit: 0
  template:
    metadata:
      labels: { app: loadseed }
    spec:
      restartPolicy: Never
      tolerations:
        - key: loadtest
          operator: Equal
          value: "true"
          effect: NoSchedule
      nodeSelector:
        pool: loadtest-generators
      containers:
        - name: loadseed
          image: "${LOADSEED_IMAGE}"   # built+pushed by CI, substituted by run-k8s.sh
          args:
            - --tenants=${LT_TENANTS}
            - --users-per-tenant=${LT_USERS}
            - --output=/manifest/seed-manifest.json
          env:
            - name: HERMES_DATABASE_URL
              valueFrom:
                secretKeyRef:
                  name: hermes-loadtest-bootstrap
                  key: database_url
            - name: HERMES_API_KEY_HMAC_SECRET
              valueFrom:
                secretKeyRef:
                  name: hermes-loadtest-bootstrap
                  key: hmac_secret
          volumeMounts:
            - { name: manifest, mountPath: /manifest }
      volumes:
        - name: manifest
          persistentVolumeClaim:
            claimName: loadtest-manifest
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: loadtest-manifest
  namespace: loadtest
spec:
  accessModes: [ReadWriteMany]
  resources:
    requests:
      storage: 1Gi
```

> **Note:** `ReadWriteMany` requires EFS (AWS) or equivalent. If the cluster only supports `ReadWriteOnce`, fall back to copying the manifest into each TestRun pod via an initContainer that reads it from a short-lived `ReadWriteOnce` PVC, or switch to a ConfigMap if manifest size allows. The install script defaults to EFS; adjust as needed.

- [ ] **Step 3: Write `loadtest/k8s/testrun.yaml`**

```yaml
apiVersion: k6.io/v1alpha1
kind: TestRun
metadata:
  name: ${RUN_NAME}
  namespace: loadtest
spec:
  parallelism: ${PARALLELISM}
  script:
    configMap:
      name: loadtest-scenarios
      file: ${SCENARIO}.js
  arguments: --out experimental-prometheus-rw=http://loadtest-prometheus-server/api/v1/write --tag run_id=${RUN_ID}
  runner:
    tolerations:
      - key: loadtest
        operator: Equal
        value: "true"
        effect: NoSchedule
    nodeSelector:
      pool: loadtest-generators
    resources:
      requests: { cpu: 2,  memory: 4Gi }
      limits:   { cpu: 4,  memory: 8Gi }
    env:
      - { name: RUN_ID,        value: "${RUN_ID}" }
      - { name: TARGET_RPS,    value: "${TARGET_RPS}" }
      - { name: VUS,           value: "${VUS}" }
      - { name: SEND_RPS,      value: "${SEND_RPS}" }
      - { name: POLL_RPS,      value: "${POLL_RPS}" }
      - { name: DURATION,      value: "${DURATION}" }
      - { name: ADMIN_URL,     value: "${ADMIN_URL}" }
      - { name: INBOX_URL,     value: "${INBOX_URL}" }
      - { name: CENTRIFUGO_URL, value: "${CENTRIFUGO_URL}" }
      - { name: HERMES_JWT_SECRET,
          valueFrom: { secretKeyRef: { name: hermes-loadtest-bootstrap, key: jwt_secret } } }
      - { name: SEED_MANIFEST, value: "/manifest/seed-manifest.json" }
    volumeMounts:
      - { name: manifest, mountPath: /manifest, readOnly: true }
    volumes:
      - name: manifest
        persistentVolumeClaim:
          claimName: loadtest-manifest
```

- [ ] **Step 4: Commit**

```bash
git add loadtest/k8s/testrun.yaml loadtest/k8s/loadseed-job.yaml loadtest/k8s/scenarios-configmap.yaml
git commit -m "feat(loadtest/k8s): TestRun + loadseed Job + scenarios ConfigMap templates"
```

### Task 6.4: Cluster-mode runner script

**Files:**
- Create: `loadtest/scripts/run-k8s.sh`
- Modify: `Makefile`

- [ ] **Step 1: Write `loadtest/scripts/run-k8s.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail

SCENARIO="${SCENARIO:?SCENARIO required}"
PARALLELISM="${PARALLELISM:-2}"
RUN_ID="${RUN_ID:-$(date +%Y%m%d-%H%M%S)-$RANDOM}"
RUN_NAME="loadtest-${RUN_ID}"
LOADSEED_IMAGE="${LOADSEED_IMAGE:?LOADSEED_IMAGE required (e.g., ghcr.io/…/loadseed:latest)}"

: "${TARGET_RPS:=500}"
: "${VUS:=1000}"
: "${SEND_RPS:=100}"
: "${POLL_RPS:=20}"
: "${DURATION:=10m}"
: "${LT_TENANTS:=10}"
: "${LT_USERS:=10000}"
: "${ADMIN_URL:=http://hermes-admin.hermes.svc.cluster.local:8080}"
: "${INBOX_URL:=http://hermes-inbox.hermes.svc.cluster.local:8086}"
: "${CENTRIFUGO_URL:=ws://hermes-centrifugo.hermes.svc.cluster.local:8000/connection/websocket}"

export SCENARIO PARALLELISM RUN_ID RUN_NAME LOADSEED_IMAGE \
  TARGET_RPS VUS SEND_RPS POLL_RPS DURATION LT_TENANTS LT_USERS \
  ADMIN_URL INBOX_URL CENTRIFUGO_URL

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
K8S_DIR="$SCRIPT_DIR/../k8s"
cd "$SCRIPT_DIR/../.."

# 1) Build the scenarios ConfigMap from the repo's JS files.
kubectl -n loadtest create configmap loadtest-scenarios \
  --from-file=loadtest/scenarios/ \
  --from-file=loadtest/lib/ \
  --dry-run=client -o yaml | kubectl apply -f -

# 2) Seed the dataset (blocks until complete).
envsubst < "$K8S_DIR/loadseed-job.yaml" | kubectl apply -f -
kubectl -n loadtest wait --for=condition=complete job/loadseed --timeout=30m

# 3) Apply the TestRun.
envsubst < "$K8S_DIR/testrun.yaml" | kubectl apply -f -

# 4) Wait for completion (k6-operator sets .status.stage to "finished").
echo "Waiting for test run $RUN_NAME…"
until kubectl -n loadtest get testrun "$RUN_NAME" -o jsonpath='{.status.stage}' 2>/dev/null | grep -qE 'finished|error'; do
  sleep 10
done
STAGE=$(kubectl -n loadtest get testrun "$RUN_NAME" -o jsonpath='{.status.stage}')
echo "TestRun stage: $STAGE"

# 5) Collect per-pod summary.json from runner pod logs (k6 prints to stdout).
mkdir -p "artifacts/$RUN_ID"
for pod in $(kubectl -n loadtest get pods -l app=k6,testrun="$RUN_NAME" -o name); do
  kubectl -n loadtest logs "$pod" > "artifacts/$RUN_ID/${pod##*/}.log" || true
done

# 6) Print Grafana URL (user runs kubectl port-forward separately).
echo ""
echo "Run complete: $RUN_ID ($STAGE)"
echo "Artifacts: artifacts/$RUN_ID/"
echo "Dashboard: kubectl -n loadtest port-forward svc/loadtest-grafana 3001:80 → http://localhost:3001/d/loadtest/load-test?var-run_id=$RUN_ID"

[ "$STAGE" = "finished" ] || exit 1
```

Make executable: `chmod +x loadtest/scripts/run-k8s.sh`.

- [ ] **Step 2: Append Makefile targets**

```makefile
.PHONY: loadtest-k8s loadtest-k8s-clean loadtest-k8s-install
loadtest-k8s-install: ## One-time install of k6-operator + Prom + Grafana in loadtest namespace
	loadtest/k8s/install.sh

loadtest-k8s:      ## Run a cluster load test (SCENARIO=... PARALLELISM=... VUS=... DURATION=... LOADSEED_IMAGE=...)
	SCENARIO=$(or $(SCENARIO),send) \
	PARALLELISM=$(or $(PARALLELISM),2) \
	TARGET_RPS=$(or $(TARGET_RPS),500) \
	VUS=$(or $(VUS),1000) \
	DURATION=$(or $(DURATION),10m) \
	LOADSEED_IMAGE=$(or $(LOADSEED_IMAGE),ghcr.io/hermes-notifications/loadseed:latest) \
	loadtest/scripts/run-k8s.sh

loadtest-k8s-clean: ## Delete the last TestRun and the seed Job
	kubectl -n loadtest delete testrun --all || true
	kubectl -n loadtest delete job loadseed --ignore-not-found
	# Leaves seeded DB data intact; run loadseed --cleanup from inside a pod if needed.
```

- [ ] **Step 3: Commit**

```bash
git add loadtest/scripts/run-k8s.sh Makefile
git commit -m "feat(loadtest): cluster-mode runner script and Makefile targets"
```

### Task 6.5: Dockerfile for loadseed

**Files:**
- Create: `cmd/loadseed/Dockerfile`

- [ ] **Step 1: Check conventions for other service Dockerfiles**

```bash
cd /Users/daryl/code/hermes && ls cmd/*/Dockerfile 2>/dev/null; find deploy/docker -name 'Dockerfile*' | head
```

Model the loadseed Dockerfile on whatever convention exists (multi-stage build, distroless or alpine runtime base).

- [ ] **Step 2: Write `cmd/loadseed/Dockerfile`**

```dockerfile
# syntax=docker/dockerfile:1.7

FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/loadseed ./cmd/loadseed

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/loadseed /loadseed
ENTRYPOINT ["/loadseed"]
```

Adjust the `FROM` lines if the project uses a different Go minor version or different base images (check an existing service Dockerfile).

- [ ] **Step 3: Build locally**

```bash
cd /Users/daryl/code/hermes && docker build -f cmd/loadseed/Dockerfile -t loadseed:dev .
docker run --rm loadseed:dev --help
```

Expected: help text printed listing all flags.

- [ ] **Step 4: Commit**

```bash
git add cmd/loadseed/Dockerfile
git commit -m "build(loadseed): Dockerfile for cluster-mode seeding"
```

---

## Phase 7 — CI workflow

### Task 7.1: GitHub Actions load-test workflow

**Files:**
- Create: `.github/workflows/loadtest.yml`

- [ ] **Step 1: Study existing CI conventions**

```bash
cd /Users/daryl/code/hermes && cat .github/workflows/ci.yml | head -80
```

Note: Go version, cache actions used, how AWS/k8s creds are obtained (OIDC role, secret names). The new workflow reuses these patterns.

- [ ] **Step 2: Write `.github/workflows/loadtest.yml`**

```yaml
name: Load Test

on:
  workflow_dispatch:
    inputs:
      scenario:
        description: "Scenario to run"
        required: true
        default: send
        type: choice
        options: [send, inbox-mixed, soak]
      parallelism:
        description: "TestRun parallelism"
        required: true
        default: "2"
      vus:
        description: "Virtual users (inbox-mixed/soak)"
        default: "1000"
      target_rps:
        description: "Target RPS (send)"
        default: "500"
      duration:
        description: "Test duration (k6 format)"
        default: "10m"
  schedule:
    # Nightly soak at 2am UTC
    - cron: "0 2 * * *"

permissions:
  id-token: write   # AWS OIDC
  contents: read

jobs:
  run:
    runs-on: ubuntu-latest
    environment: staging
    steps:
      - uses: actions/checkout@v4

      - uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: ${{ secrets.AWS_LOADTEST_ROLE_ARN }}
          aws-region: us-east-1

      - name: Update kubeconfig
        run: aws eks update-kubeconfig --name ${{ secrets.STAGING_CLUSTER }} --region us-east-1

      - uses: actions/setup-go@v5
        with: { go-version: '1.23' }

      - name: Build and push loadseed image
        env:
          REGISTRY: ${{ secrets.ECR_REGISTRY }}
        run: |
          aws ecr get-login-password | docker login --username AWS --password-stdin $REGISTRY
          IMG=$REGISTRY/loadseed:${{ github.sha }}
          docker build -f cmd/loadseed/Dockerfile -t $IMG .
          docker push $IMG
          echo "LOADSEED_IMAGE=$IMG" >> $GITHUB_ENV

      - name: Determine run parameters
        id: params
        run: |
          if [ "${{ github.event_name }}" = "schedule" ]; then
            echo "scenario=soak" >> $GITHUB_OUTPUT
            echo "parallelism=3" >> $GITHUB_OUTPUT
            echo "duration=4h" >> $GITHUB_OUTPUT
            echo "vus=2000" >> $GITHUB_OUTPUT
            echo "target_rps=300" >> $GITHUB_OUTPUT
          else
            echo "scenario=${{ inputs.scenario }}" >> $GITHUB_OUTPUT
            echo "parallelism=${{ inputs.parallelism }}" >> $GITHUB_OUTPUT
            echo "duration=${{ inputs.duration }}" >> $GITHUB_OUTPUT
            echo "vus=${{ inputs.vus }}" >> $GITHUB_OUTPUT
            echo "target_rps=${{ inputs.target_rps }}" >> $GITHUB_OUTPUT
          fi

      - name: Run load test
        env:
          SCENARIO: ${{ steps.params.outputs.scenario }}
          PARALLELISM: ${{ steps.params.outputs.parallelism }}
          DURATION: ${{ steps.params.outputs.duration }}
          VUS: ${{ steps.params.outputs.vus }}
          TARGET_RPS: ${{ steps.params.outputs.target_rps }}
        run: make loadtest-k8s

      - uses: actions/upload-artifact@v4
        if: always()
        with:
          name: loadtest-artifacts-${{ github.run_id }}
          path: artifacts/
```

- [ ] **Step 3: Validate workflow syntax**

```bash
cd /Users/daryl/code/hermes && yq -e '.jobs.run.steps | length > 0' .github/workflows/loadtest.yml
```

If `yq` isn't installed, skip this and commit; GitHub will surface syntax errors on push.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/loadtest.yml
git commit -m "ci: add workflow_dispatch + nightly soak load-test workflow"
```

---

## Phase 8 — Documentation

### Task 8.1: Load-test README

**Files:**
- Create: `loadtest/README.md`

- [ ] **Step 1: Write `loadtest/README.md`**

````markdown
# Hermes Load Testing

End-to-end load-testing system for the Hermes notification platform. Scales from a local docker-compose smoke test to a 100k+ VU cluster run via the same scenario code.

**Design spec:** [`docs/superpowers/specs/2026-04-17-load-testing-design.md`](../docs/superpowers/specs/2026-04-17-load-testing-design.md)

## Quick start — local

Requires `make infra-up` plus Hermes services running (admin, inbox, Centrifugo).

```bash
make loadseed                              # seed default dataset (10 tenants × 10k users)
make loadtest-local SCENARIO=send TARGET_RPS=100 DURATION=30s
# → artifacts/<run_id>/summary.json
# → Grafana at http://localhost:3001
make loadtest-local-clean                  # teardown + cleanup seed
```

## Scenarios

| Scenario | Purpose | Key env |
|---|---|---|
| `send` | Write-path capacity | `TARGET_RPS`, `DURATION`, `CHANNEL_WEIGHTS` |
| `inbox-mixed` | WS + REST read path + driving send | `VUS`, `SEND_RPS`, `POLL_RPS`, `DURATION` |
| `soak` | Long-duration stability | `VUS`, `SEND_RPS`, `POLL_RPS`, `DURATION` (default 4h) |

All scenarios honor `RUN_ID`, `INSTANCE_ID`, `INSTANCE_COUNT` (the last two set by `k6-operator`), `ADMIN_URL`, `INBOX_URL`, `CENTRIFUGO_URL`, `HERMES_JWT_SECRET`.

## Cluster runs

One-time install:

```bash
aws eks update-kubeconfig --name <staging-cluster>
make loadtest-k8s-install
```

Per run:

```bash
LOADSEED_IMAGE=ghcr.io/hermes-notifications/loadseed:latest \
make loadtest-k8s SCENARIO=inbox-mixed PARALLELISM=10 VUS=50000 DURATION=30m
```

Linear scale-out: double `PARALLELISM` to double the load. The scenario code sees `INSTANCE_COUNT` and divides its per-pod rate accordingly.

## Metrics

Generator-side metrics go to the dedicated `loadtest` Prometheus. Hermes service-side metrics continue to flow to Datadog. Correlate by `run_id` (appears as a metric tag on the Prom side and as the `X-Load-Test-Run-Id` trace tag on the DD side).

Custom k6 metrics:

- `send_ack_latency` — `POST /v1/send` ack latency
- `ws_connect_latency`, `ws_connection_active`, `ws_connection_drops`
- `ws_push_e2e_latency` — send-ack → WS push received (headline e2e metric)
- `inbox_list_latency`

## Non-goals (v1)

No SMS scenarios. No multi-region generators. No chaos/failure injection. No production-target runs.

## Files

- `scenarios/*.js` — k6 scenario entrypoints
- `lib/*.js` — shared helpers (seed, auth, metrics, payloads, Centrifugo WS)
- `k8s/` — namespace, TestRun template, Helm values for Prom+Grafana+k6-operator
- `dashboards/` — Grafana JSON
- `scripts/` — `run-local.sh`, `run-k8s.sh`
- `../cmd/loadseed/` — Go seeder (direct-to-DB inserts)
````

- [ ] **Step 2: Commit**

```bash
git add loadtest/README.md
git commit -m "docs(loadtest): README with quick start and cluster instructions"
```

---

## Verification checklist

Before considering the plan fully executed, confirm all of the following pass against a running Hermes stack (local or staging):

- [ ] `make loadseed` creates 10 tenants × 10k users in under 30 seconds.
- [ ] `make loadseed-clean` removes every seeded row.
- [ ] `make loadtest-local SCENARIO=send TARGET_RPS=100 DURATION=30s` completes with 0 failed HTTP requests and writes `artifacts/<run_id>/summary.json`.
- [ ] The Grafana dashboard at `http://localhost:3001/d/loadtest/load-test` shows all panels populated during a local run.
- [ ] `make loadtest-local SCENARIO=inbox-mixed VUS=20 SEND_RPS=5 DURATION=30s` reports non-zero `ws_push_e2e_latency` samples.
- [ ] `k6 inspect loadtest/scenarios/soak.js` prints valid options.
- [ ] `make loadtest-k8s-install` succeeds against a fresh cluster (staging or test).
- [ ] `make loadtest-k8s SCENARIO=send PARALLELISM=2 TARGET_RPS=50 DURATION=2m` completes with stage `finished` and collects runner logs into `artifacts/<run_id>/`.
- [ ] `GET /v1/send` during the run carries `X-Load-Test-Run-Id` header (verify via Hermes request logs).
- [ ] `.github/workflows/loadtest.yml` runs via workflow_dispatch against staging end-to-end (pilot run).
