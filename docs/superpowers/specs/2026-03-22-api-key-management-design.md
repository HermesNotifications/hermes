# API Key Management for Staging & Production

## Problem

The Hermes seed tool only supports local development with a hardcoded key (`hms_dev_key`). There is no mechanism for creating, managing, or revoking API keys in staging or production. The current Argon2-based validation loads all keys from Postgres on every request and iterates through them, which does not scale.

## Goals

- Bootstrap API keys in staging and production environments
- Store raw keys securely in AWS Secrets Manager
- Manage keys at runtime via Admin API and CLI
- Enforce permission-scoped access on API keys
- Cache key lookups in Redis for performance
- Replace Argon2 with HMAC-SHA256 for fast verification with direct key ID lookup

## Non-Goals

- Migrating existing entity IDs (notifications, tenants, etc.) to the new ID format (follow-up work)
- Full RBAC or role-based system — permissions are a flat set on each key
- API key rate limiting (separate concern)

## Design

### New ID Package

**Package:** `internal/id/v2`

A new ID generator that produces base64url-encoded IDs with configurable random bits, optional time-based prefix for sorting, and optional type prefix for identification.

```go
type Config struct {
    Prefix   string // optional type prefix, e.g. "key", "ntf"
    TimeBits int    // 0 = no time component, >0 = ms timestamp truncated to this many bits
    RandBits int    // required, number of random bits
}

type Generator struct { ... }

func NewGenerator(cfg Config) *Generator
func (g *Generator) New() string
func (g *Generator) MustNew() string
func Parse(id string) (prefix string, timeBytes []byte, randBytes []byte)
```

**Output format:**
- With prefix: `<prefix>_<base64url(time bytes ++ random bytes)>` (no padding)
- Without prefix: `<base64url(time bytes ++ random bytes)>`

**For API keys:** `Config{Prefix: "key", TimeBits: 0, RandBits: 36}` produces IDs like `key_a8f3B2` (6 base64url characters = 36 bits, ~69 billion possible values).

The existing `internal/id/` package remains unchanged. New code uses `internal/id/v2`. Migration of other entities is separate follow-up work.

### API Key Format

Full key format: `hms_[<env>_]key_<key_id>_<secret>`

| Part | Description | Example |
|---|---|---|
| `hms_` | Product prefix | Fixed |
| `<env>_` | Environment (omitted for production) | `stg_`, `dev_` |
| `key_` | Type prefix from ID generator | Fixed |
| `<key_id>` | 36-bit random ID, base64url (6 chars) | `a8f3B2` |
| `_` | Separator | Fixed |
| `<secret>` | 128-bit random secret, base64url (22 chars) | `a8f3B2c1D4e5f6g7H8j9Kw` |

**Examples:**
- Production: `hms_key_a8f3B2_a8f3B2c1D4e5f6g7H8j9Kw`
- Staging: `hms_stg_key_a8f3B2_a8f3B2c1D4e5f6g7H8j9Kw`
- Dev: `hms_dev_key` (hardcoded, backward compatible)

**Generation:** 16 bytes from `crypto/rand` for the secret, encoded as base64url without padding. New function in `internal/auth/`:

```go
func GenerateAPIKey(prefix string) (raw string, keyID string, err error)
```

### HMAC-SHA256 Hashing

Replace Argon2 with HMAC-SHA256 for API key verification.

**New config variable:** `HERMES_API_KEY_HMAC_SECRET` — a secret key used to compute HMACs. Stored in AWS Secrets Manager.

**New functions in `internal/auth/`:**

```go
func HMACHashAPIKey(secret, hmacKey string) string       // hex-encoded HMAC
func HMACVerifyAPIKey(secret, hash, hmacKey string) bool  // constant-time comparison
```

Existing Argon2 functions remain for backward compatibility but are not used by new code.

**Security properties:**
- HMAC-SHA256 verification is microseconds vs. Argon2's milliseconds
- Direct lookup by key ID (no iteration) — O(1) instead of O(n)
- Constant-time comparison prevents timing attacks
- Generic 401 response for both "key not found" and "invalid secret" — no enumeration

### Database Schema

**Migration:** Add permissions column to `api_keys`.

```sql
ALTER TABLE api_keys ADD COLUMN permissions TEXT[] NOT NULL DEFAULT '{}';
```

No new tables. The `api_keys` table stores:
- `id` — the key ID (e.g., `key_a8f3B2`)
- `key_hash` — HMAC-SHA256 hash of the secret
- `name` — human-readable name
- `permissions` — array of permission strings
- `created_at` — timestamp

### Permissions

**Initial permission set:**

| Permission | Scope |
|---|---|
| `apikeys:manage` | Create, list, revoke API keys |
| `notifications:send` | Send notifications |
| `templates:manage` | CRUD notification types and templates |
| `tenants:manage` | CRUD tenants, groups, channels |

**Defaults:**
- Seed tool creates keys with all permissions
- API-created keys default to all except `apikeys:manage`

**Enforcement:** New middleware `RequirePermission(perm string)` reads permissions from request context (set during key validation) and returns 403 if the required permission is missing.

**Route mapping:**

| Route group | Required permission |
|---|---|
| `/v1/apikeys/*` | `apikeys:manage` |
| `/v1/notifications/*` | `notifications:send` |
| `/v1/types/*` | `templates:manage` |
| `/v1/tenants/*` | `tenants:manage` |

### Redis Caching

**Strategy:** Per-key caching with TTL and invalidation on mutation.

**Cache key:** `hermes:apikey:<key_id>`
**Value:** JSON `{key_hash, permissions}`
**TTL:** 5 minutes

**Validation flow:**

```
Bearer hms_[env_]key_<id>_<secret>
    ↓
Parse → extract key_id + secret
    ↓
Check Redis: hermes:apikey:<key_id>
    ├─ Hit → deserialize {key_hash, permissions}
    └─ Miss → SELECT by key_id from Postgres, cache in Redis with TTL
    ↓
HMAC verify secret against key_hash (constant-time)
    ├─ Fail → 401 (generic)
    └─ Pass → attach key_id + permissions to request context
```

**Invalidation:**
- On revoke: delete `hermes:apikey:<key_id>` from Redis
- On create: no action needed (cache miss handles it)
- TTL provides fallback if invalidation fails

**Implementation:** New `internal/cache/apikeys.go` with `APIKeyCache` struct providing `Get`, `Set`, `Invalidate` methods. The Admin service receives a cache instance at construction.

### Admin API Endpoints

All endpoints require the `apikeys:manage` permission.

**POST /v1/apikeys** — Create a new API key

Request:
```json
{
  "name": "CI Pipeline",
  "permissions": ["notifications:send", "templates:manage"]
}
```

Response (201):
```json
{
  "id": "key_a8f3B2",
  "name": "CI Pipeline",
  "raw_key": "hms_key_a8f3B2_a8f3B2c1D4e5f6g7H8j9Kw",
  "permissions": ["notifications:send", "templates:manage"],
  "created_at": "2026-03-22T00:00:00Z"
}
```

`raw_key` is returned only on creation — it cannot be retrieved again.

**GET /v1/apikeys** — List all API keys

Response (200):
```json
{
  "api_keys": [
    {
      "id": "key_a8f3B2",
      "name": "CI Pipeline",
      "permissions": ["notifications:send", "templates:manage"],
      "created_at": "2026-03-22T00:00:00Z"
    }
  ]
}
```

**DELETE /v1/apikeys/{id}** — Revoke an API key

Response: 204 No Content

- Deletes the key from Postgres
- Invalidates `hermes:apikey:<key_id>` in Redis
- Prevents self-deletion (cannot revoke the key used to authenticate the request)
- Returns 404 if not found

### Seed Tool

**`cmd/seed/main.go`** becomes environment-aware.

**Flags:**
- `--database-url` (or `HERMES_DATABASE_URL`) — required
- `--hmac-secret` (or `HERMES_API_KEY_HMAC_SECRET`) — required for non-dev
- `--env` — `dev` (default), `staging`, `production`
- `--force` — rotate existing key even if one exists
- `--aws-region` — defaults to `us-east-1`

**Behavior:**

| Aspect | Dev | Staging / Production |
|---|---|---|
| Key generation | Hardcoded `hms_dev_key` | Random via `auth.GenerateAPIKey` |
| Permissions | All | All |
| Hashing | HMAC-SHA256 | HMAC-SHA256 |
| Secret storage | Stdout only | AWS Secrets Manager (`hermes/<env>` → `admin_api_key`) |
| Idempotency | `ON CONFLICT DO NOTHING` | Check Secrets Manager first; skip if exists unless `--force` |
| Key name | "Development" | "Bootstrap (<env>)" |

**Force/rotate flow:**
1. Generate new key
2. Hash and insert new row in Postgres
3. Update `admin_api_key` property in Secrets Manager
4. Old key remains in Postgres (still valid)
5. Print warning: old key is still valid, revoke via `hermes apikey revoke` if needed

### CLI Commands

**New subcommand group:** `hermes apikey`

| Command | Description |
|---|---|
| `hermes apikey create --name "CI Pipeline" [--permissions notifications:send,templates:manage]` | Create key via Admin API |
| `hermes apikey list` | List all keys |
| `hermes apikey revoke --id key_a8f3B2` | Revoke a key |

Uses existing `--url`, `--api-key`, `--output` root flags. Calls Admin API endpoints via `pkg/client`.

### Infrastructure

**AWS Secrets Manager** — Add two properties to `hermes/staging` and `hermes/production`:
- `api_key_hmac_secret` — HMAC signing key for key verification
- `admin_api_key` — bootstrap API key (written by seed tool)

**External Secrets** — Add to staging and production `external-secrets.yaml`:

```yaml
- secretKey: HERMES_API_KEY_HMAC_SECRET
  remoteRef:
    key: hermes/<env>
    property: api_key_hmac_secret
- secretKey: HERMES_ADMIN_API_KEY
  remoteRef:
    key: hermes/<env>
    property: admin_api_key
```

**Config** — Add to `internal/config/config.go`:
- `APIKeyHMACSecret string` — `HERMES_API_KEY_HMAC_SECRET`, required in staging/production

### Migration Path

1. Deploy the new code (HMAC auth + permissions)
2. Run seed tool with `--env staging` and `--env production` to create bootstrap keys
3. Update any CI/CD or operator scripts to use the new key format
4. The old dev seed key format is replaced on next local seed run — local dev only, no production concern

### Testing Strategy

- **Unit tests:** HMAC hash/verify, ID generation, permission middleware, API key parsing, handler tests with mock store
- **Integration tests:** Redis cache get/set/invalidate, store operations with permissions column
- **E2E tests:** Full flow — create key via seed, authenticate, create another key via API, revoke, verify revoked key fails
