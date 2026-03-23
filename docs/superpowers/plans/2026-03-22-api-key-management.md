# API Key Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add API key management with HMAC-based auth, permissions, Redis caching, Admin API CRUD, CLI commands, and environment-aware seed tool for staging/production.

**Architecture:** New ID package generates base64url IDs. HMAC-SHA256 replaces Argon2 for key verification with O(1) lookup by key ID. Permission strings stored as TEXT[] on api_keys. Redis caches per-key lookups. Admin API and CLI enable runtime key management. Seed tool bootstraps keys in staging/production via AWS Secrets Manager.

**Tech Stack:** Go, PostgreSQL, Redis, HMAC-SHA256, chi/huma, cobra CLI, AWS Secrets Manager SDK

**Spec:** `docs/superpowers/specs/2026-03-22-api-key-management-design.md`

---

## File Map

| Action | File | Responsibility |
|--------|------|----------------|
| Create | `internal/id/v2/id.go` | ID generator with configurable prefix, time bits, random bits |
| Create | `internal/id/v2/id_test.go` | Unit tests for ID generation and parsing |
| Modify | `internal/auth/apikey.go` | Replace Argon2 with HMAC + add GenerateAPIKey/ParseAPIKey |
| Modify | `internal/auth/apikey_test.go` | Tests for HMAC hash/verify, generate, parse |
| Create | `internal/auth/permissions.go` | Permission constants, RequirePermission middleware, context helpers |
| Create | `internal/auth/permissions_test.go` | Tests for permission middleware |
| Modify | `internal/auth/middleware.go` | ValidatedKey type, updated APIKeyMiddleware |
| Modify | `internal/auth/middleware_test.go` | Tests for updated middleware |
| Modify | `internal/models/models.go` | Add Permissions field to APIKey struct |
| Create | `migrations/000010_add_api_key_permissions.up.sql` | ALTER TABLE add permissions column |
| Create | `migrations/000010_add_api_key_permissions.down.sql` | DROP permissions column |
| Modify | `internal/store/apikeys.go` | Update CreateAPIKey, add GetAPIKeyByID, DeleteAPIKey, update ListAPIKeys |
| Modify | `internal/config/config.go` | Add APIKeyHMACSecret field |
| Modify | `internal/cache/redis.go` | Add GetAPIKey, SetAPIKey, InvalidateAPIKey methods |
| Create | `internal/admin/handler_apikeys.go` | POST/GET/DELETE /v1/apikeys handlers |
| Modify | `internal/admin/server.go` | Update AdminStore interface, add validateAPIKey with cache+HMAC, register routes, wire permissions |
| Modify | `internal/admin/testutil_test.go` | Add mock methods for new store operations |
| Create | `internal/admin/handler_apikeys_test.go` | Handler tests for API key CRUD |
| Create | `pkg/client/apikeys.go` | Client service for API key endpoints |
| Create | `pkg/client/apikeys_test.go` | Client tests |
| Modify | `pkg/client/client.go` | Add APIKeys service to Client struct |
| Create | `internal/cli/apikeys.go` | CLI commands: apikey create/list/revoke |
| Modify | `internal/cli/root.go` | Register apikey subcommand |
| Modify | `cmd/seed/main.go` | Environment-aware seed with AWS Secrets Manager |
| Modify | `cmd/admin/main.go` | Pass hmacSecret to NewServer |
| Modify | `tests/e2e/admin_test.go` | Update NewServer call and API key insertion for new signatures |
| Modify | `deploy/k8s/overlays/staging/external-secrets.yaml` | Add HMAC secret external secret |
| Modify | `deploy/k8s/overlays/production/external-secrets.yaml` | Add HMAC secret external secret |

---

## Task 1: New ID Package

**Files:**
- Create: `internal/id/v2/id.go`
- Create: `internal/id/v2/id_test.go`

- [ ] **Step 1: Write failing tests for ID generation**

Create `internal/id/v2/id_test.go`:

```go
package id_test

import (
	"encoding/base64"
	"strings"
	"testing"

	id "github.com/hermes-notifications/hermes/internal/id/v2"
)

func TestGenerator_New_WithPrefix(t *testing.T) {
	g := id.NewGenerator(id.Config{Prefix: "key", RandBits: 36})
	got := g.New()

	if !strings.HasPrefix(got, "key_") {
		t.Fatalf("expected prefix key_, got %s", got)
	}

	// 36 bits = 5 bytes (ceil(36/8)), base64url encodes to 8 chars with padding,
	// but without padding: ceil(5*4/3) = 7 chars? Let's verify:
	// Actually 5 bytes = 40 bits, base64url = ceil(40/6) = 7 chars (with 2 padding bits)
	// But we want 36 random bits packed into ceil(36/8)=5 bytes, base64url no-pad = 8 chars?
	// 5 bytes → base64 = 8 chars with 1 padding char → 7 chars no pad? No:
	// base64url(5 bytes) = ceil(5/3)*4 = 8, minus 1 pad = 7. Hmm.
	// Let's just check length empirically after implementation.
	suffix := strings.TrimPrefix(got, "key_")
	if len(suffix) == 0 {
		t.Fatal("suffix should not be empty")
	}

	// Verify it's valid base64url
	_, err := base64.RawURLEncoding.DecodeString(suffix)
	if err != nil {
		t.Fatalf("suffix is not valid base64url: %v", err)
	}
}

func TestGenerator_New_WithoutPrefix(t *testing.T) {
	g := id.NewGenerator(id.Config{RandBits: 64})
	got := g.New()

	if strings.Contains(got, "_") {
		t.Fatalf("expected no underscore without prefix, got %s", got)
	}

	_, err := base64.RawURLEncoding.DecodeString(got)
	if err != nil {
		t.Fatalf("not valid base64url: %v", err)
	}
}

func TestGenerator_New_WithTimeBits(t *testing.T) {
	g := id.NewGenerator(id.Config{Prefix: "ntf", TimeBits: 48, RandBits: 80})
	got := g.New()

	if !strings.HasPrefix(got, "ntf_") {
		t.Fatalf("expected prefix ntf_, got %s", got)
	}

	suffix := strings.TrimPrefix(got, "ntf_")
	raw, err := base64.RawURLEncoding.DecodeString(suffix)
	if err != nil {
		t.Fatalf("not valid base64url: %v", err)
	}

	// 48 time bits + 80 random bits = 128 bits = 16 bytes
	if len(raw) != 16 {
		t.Fatalf("expected 16 bytes, got %d", len(raw))
	}
}

func TestGenerator_New_Uniqueness(t *testing.T) {
	g := id.NewGenerator(id.Config{Prefix: "key", RandBits: 36})
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		v := g.New()
		if seen[v] {
			t.Fatalf("duplicate ID: %s", v)
		}
		seen[v] = true
	}
}

func TestGenerator_New_Sortable(t *testing.T) {
	g := id.NewGenerator(id.Config{Prefix: "ntf", TimeBits: 48, RandBits: 80})
	a := g.New()
	b := g.New()

	// Same millisecond, so they may or may not be ordered.
	// But both should be valid and start with the prefix.
	if !strings.HasPrefix(a, "ntf_") || !strings.HasPrefix(b, "ntf_") {
		t.Fatalf("prefix missing: a=%s b=%s", a, b)
	}
}

func TestParse(t *testing.T) {
	g := id.NewGenerator(id.Config{Prefix: "key", RandBits: 36})
	original := g.New()

	prefix, raw := id.Parse(original)
	if prefix != "key" {
		t.Fatalf("expected prefix key, got %s", prefix)
	}
	if len(raw) == 0 {
		t.Fatal("expected raw bytes")
	}
}

func TestParse_NoPrefix(t *testing.T) {
	g := id.NewGenerator(id.Config{RandBits: 64})
	original := g.New()

	prefix, raw := id.Parse(original)
	if prefix != "" {
		t.Fatalf("expected empty prefix, got %s", prefix)
	}
	if len(raw) == 0 {
		t.Fatal("expected raw bytes")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/id/v2/... -v`
Expected: compilation error — package does not exist

- [ ] **Step 3: Implement the ID package**

Create `internal/id/v2/id.go`:

```go
package id

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"time"
)

// Config defines how IDs are generated.
type Config struct {
	Prefix   string // optional type prefix, e.g. "key", "ntf"
	TimeBits int    // 0 = no time component, >0 = ms timestamp truncated to this many bits
	RandBits int    // required, number of random bits
}

// Generator produces IDs with a fixed configuration.
type Generator struct {
	cfg Config
}

// NewGenerator creates an ID generator with the given configuration.
func NewGenerator(cfg Config) *Generator {
	if cfg.RandBits <= 0 {
		panic("id: RandBits must be > 0")
	}
	return &Generator{cfg: cfg}
}

// New generates a new ID.
func (g *Generator) New() string {
	var buf []byte

	if g.cfg.TimeBits > 0 {
		timeBytes := g.cfg.TimeBits / 8
		if g.cfg.TimeBits%8 != 0 {
			timeBytes++
		}
		ms := uint64(time.Now().UnixMilli())
		// Store the lowest TimeBits of the millisecond timestamp in big-endian
		tb := make([]byte, 8)
		binary.BigEndian.PutUint64(tb, ms)
		// Take the last timeBytes bytes (least significant)
		buf = append(buf, tb[8-timeBytes:]...)
	}

	randBytes := g.cfg.RandBits / 8
	if g.cfg.RandBits%8 != 0 {
		randBytes++
	}
	rb := make([]byte, randBytes)
	if _, err := rand.Read(rb); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	buf = append(buf, rb...)

	encoded := base64.RawURLEncoding.EncodeToString(buf)

	if g.cfg.Prefix != "" {
		return g.cfg.Prefix + "_" + encoded
	}
	return encoded
}

// MustNew generates a new ID. Panics on error (convenience wrapper).
func (g *Generator) MustNew() string {
	return g.New()
}

// Parse splits an ID into its prefix and raw decoded bytes.
// Callers that know the config can split time/random portions themselves.
func Parse(id string) (prefix string, raw []byte) {
	data := id
	if idx := strings.Index(id, "_"); idx >= 0 {
		prefix = id[:idx]
		data = id[idx+1:]
	}

	decoded, err := base64.RawURLEncoding.DecodeString(data)
	if err != nil {
		return prefix, nil
	}
	return prefix, decoded
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/id/v2/... -v`
Expected: all tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/id/v2/
git commit -m "feat: add v2 ID package with base64url encoding and configurable bits"
```

---

## Task 2: HMAC-SHA256 Auth Functions

**Files:**
- Modify: `internal/auth/apikey.go`
- Modify: `internal/auth/apikey_test.go`

- [ ] **Step 1: Write failing tests for HMAC and key generation/parsing**

Replace contents of `internal/auth/apikey_test.go`:

```go
package auth_test

import (
	"strings"
	"testing"

	"github.com/hermes-notifications/hermes/internal/auth"
)

func TestHMACHashAndVerify(t *testing.T) {
	hmacKey := "test-hmac-secret"
	secret := "my-api-key-secret"

	hash := auth.HMACHashAPIKey(secret, hmacKey)
	if hash == "" {
		t.Fatal("hash should not be empty")
	}

	if !auth.HMACVerifyAPIKey(secret, hash, hmacKey) {
		t.Fatal("expected verification to succeed")
	}
}

func TestHMACVerify_WrongSecret(t *testing.T) {
	hmacKey := "test-hmac-secret"
	hash := auth.HMACHashAPIKey("correct-secret", hmacKey)

	if auth.HMACVerifyAPIKey("wrong-secret", hash, hmacKey) {
		t.Fatal("expected verification to fail with wrong secret")
	}
}

func TestHMACVerify_WrongHMACKey(t *testing.T) {
	hash := auth.HMACHashAPIKey("secret", "key1")

	if auth.HMACVerifyAPIKey("secret", hash, "key2") {
		t.Fatal("expected verification to fail with wrong HMAC key")
	}
}

func TestHMACHash_Deterministic(t *testing.T) {
	hmacKey := "test-hmac-secret"
	secret := "my-secret"
	h1 := auth.HMACHashAPIKey(secret, hmacKey)
	h2 := auth.HMACHashAPIKey(secret, hmacKey)

	if h1 != h2 {
		t.Fatalf("HMAC should be deterministic: %s != %s", h1, h2)
	}
}

func TestGenerateAPIKey(t *testing.T) {
	raw, keyID, err := auth.GenerateAPIKey("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Production format: hms_key_<id>_<secret>
	if !strings.HasPrefix(raw, "hms_key_") {
		t.Fatalf("expected hms_key_ prefix, got %s", raw)
	}

	if keyID == "" {
		t.Fatal("keyID should not be empty")
	}

	// Key should contain the keyID
	if !strings.Contains(raw, keyID) {
		t.Fatalf("raw key should contain keyID %s: %s", keyID, raw)
	}
}

func TestGenerateAPIKey_WithEnvPrefix(t *testing.T) {
	raw, _, err := auth.GenerateAPIKey("stg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(raw, "hms_stg_key_") {
		t.Fatalf("expected hms_stg_key_ prefix, got %s", raw)
	}
}

func TestParseAPIKey_Production(t *testing.T) {
	raw, expectedID, err := auth.GenerateAPIKey("")
	if err != nil {
		t.Fatal(err)
	}

	keyID, secret, err := auth.ParseAPIKey(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if keyID != expectedID {
		t.Fatalf("expected keyID %s, got %s", expectedID, keyID)
	}
	if secret == "" {
		t.Fatal("secret should not be empty")
	}
}

func TestParseAPIKey_Staging(t *testing.T) {
	raw, expectedID, err := auth.GenerateAPIKey("stg")
	if err != nil {
		t.Fatal(err)
	}

	keyID, secret, err := auth.ParseAPIKey(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if keyID != expectedID {
		t.Fatalf("expected keyID %s, got %s", expectedID, keyID)
	}
	if secret == "" {
		t.Fatal("secret should not be empty")
	}
}

func TestParseAPIKey_Invalid(t *testing.T) {
	cases := []string{"", "invalid", "hms_", "hms_key_", "bearer token"}
	for _, c := range cases {
		_, _, err := auth.ParseAPIKey(c)
		if err == nil {
			t.Fatalf("expected error for %q", c)
		}
	}
}

func TestGenerateAndVerify_RoundTrip(t *testing.T) {
	hmacKey := "test-hmac-secret"

	raw, _, err := auth.GenerateAPIKey("")
	if err != nil {
		t.Fatal(err)
	}

	_, secret, err := auth.ParseAPIKey(raw)
	if err != nil {
		t.Fatal(err)
	}

	hash := auth.HMACHashAPIKey(secret, hmacKey)
	if !auth.HMACVerifyAPIKey(secret, hash, hmacKey) {
		t.Fatal("round-trip verification failed")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/auth/... -run TestHMAC -v`
Expected: compilation errors — functions don't exist

- [ ] **Step 3: Replace apikey.go with HMAC implementation**

Replace contents of `internal/auth/apikey.go`:

```go
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	id "github.com/hermes-notifications/hermes/internal/id/v2"
)

var apiKeyIDGen = id.NewGenerator(id.Config{Prefix: "key", RandBits: 36})

// HMACHashAPIKey computes an HMAC-SHA256 hash of the secret using hmacKey.
// Returns the hex-encoded HMAC.
func HMACHashAPIKey(secret, hmacKey string) string {
	mac := hmac.New(sha256.New, []byte(hmacKey))
	mac.Write([]byte(secret))
	return hex.EncodeToString(mac.Sum(nil))
}

// HMACVerifyAPIKey verifies a secret against an HMAC hash using constant-time comparison.
func HMACVerifyAPIKey(secret, hash, hmacKey string) bool {
	expected, err := hex.DecodeString(hash)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(hmacKey))
	mac.Write([]byte(secret))
	return hmac.Equal(mac.Sum(nil), expected)
}

// GenerateAPIKey creates a new API key with the given environment prefix.
// envPrefix is "" for production, "stg" for staging, "dev" for development.
// Returns the full raw key string and the key ID (e.g., "key_a8f3B2").
func GenerateAPIKey(envPrefix string) (raw string, keyID string, err error) {
	keyID = apiKeyIDGen.New()

	secretBytes := make([]byte, 16) // 128 bits
	if _, err := rand.Read(secretBytes); err != nil {
		return "", "", fmt.Errorf("generate secret: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(secretBytes)

	if envPrefix != "" {
		raw = fmt.Sprintf("hms_%s_%s_%s", envPrefix, keyID, secret)
	} else {
		raw = fmt.Sprintf("hms_%s_%s", keyID, secret)
	}
	return raw, keyID, nil
}

// ParseAPIKey extracts the key ID and secret from a raw API key string.
// Handles formats:
//   - hms_key_<id>_<secret>       (production)
//   - hms_stg_key_<id>_<secret>   (staging)
//   - hms_dev_key_<id>_<secret>   (dev)
func ParseAPIKey(raw string) (keyID string, secret string, err error) {
	if !strings.HasPrefix(raw, "hms_") {
		return "", "", fmt.Errorf("invalid api key format: missing hms_ prefix")
	}

	trimmed := strings.TrimPrefix(raw, "hms_")

	// Remove optional environment prefix (stg_, dev_)
	for _, env := range []string{"stg_", "dev_"} {
		trimmed = strings.TrimPrefix(trimmed, env)
	}

	// Now trimmed should be "key_<id>_<secret>"
	if !strings.HasPrefix(trimmed, "key_") {
		return "", "", fmt.Errorf("invalid api key format: missing key_ prefix")
	}
	trimmed = strings.TrimPrefix(trimmed, "key_")

	// Split into <id>_<secret>
	// The ID is 6 base64url chars (from 36-bit / 5 bytes), the secret is 22 chars (from 128-bit / 16 bytes)
	// Find the underscore separator
	idx := strings.Index(trimmed, "_")
	if idx <= 0 || idx >= len(trimmed)-1 {
		return "", "", fmt.Errorf("invalid api key format: cannot split id and secret")
	}

	idPart := trimmed[:idx]
	secret = trimmed[idx+1:]

	keyID = "key_" + idPart
	return keyID, secret, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/auth/... -v`
Expected: all tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/auth/apikey.go internal/auth/apikey_test.go
git commit -m "feat: replace Argon2 with HMAC-SHA256 for API key auth, add generate/parse"
```

---

## Task 3: Permission Constants and Middleware

**Files:**
- Create: `internal/auth/permissions.go`
- Create: `internal/auth/permissions_test.go`
- Modify: `internal/auth/middleware.go`

- [ ] **Step 1: Write failing tests for permission middleware**

Create `internal/auth/permissions_test.go`:

```go
package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hermes-notifications/hermes/internal/auth"
)

func TestRequirePermission_Allowed(t *testing.T) {
	handler := auth.RequirePermission(auth.PermNotificationsSend)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(auth.WithValidatedKey(req.Context(), &auth.ValidatedKey{
		ID:          "key_abc123",
		Permissions: []string{auth.PermNotificationsSend, auth.PermTemplatesManage},
	}))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestRequirePermission_Denied(t *testing.T) {
	handler := auth.RequirePermission(auth.PermAPIKeysManage)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(auth.WithValidatedKey(req.Context(), &auth.ValidatedKey{
		ID:          "key_abc123",
		Permissions: []string{auth.PermNotificationsSend},
	}))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestRequirePermission_NoKeyInContext(t *testing.T) {
	handler := auth.RequirePermission(auth.PermNotificationsSend)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestValidatePermissions(t *testing.T) {
	valid := []string{auth.PermNotificationsSend, auth.PermTemplatesManage}
	if err := auth.ValidatePermissions(valid); err != nil {
		t.Fatalf("expected valid: %v", err)
	}

	invalid := []string{auth.PermNotificationsSend, "foo:bar"}
	if err := auth.ValidatePermissions(invalid); err == nil {
		t.Fatal("expected error for unknown permission")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/auth/... -run TestRequirePermission -v`
Expected: compilation errors

- [ ] **Step 3: Implement permissions.go**

Create `internal/auth/permissions.go`:

```go
package auth

import (
	"context"
	"fmt"
	"net/http"
)

const (
	PermAPIKeysManage    = "apikeys:manage"
	PermNotificationsSend = "notifications:send"
	PermTemplatesManage  = "templates:manage"
	PermTenantsManage    = "tenants:manage"
)

// AllPermissions is the complete set of valid permissions.
var AllPermissions = []string{
	PermAPIKeysManage,
	PermNotificationsSend,
	PermTemplatesManage,
	PermTenantsManage,
}

// DefaultPermissions is the set granted to API-created keys (excludes apikeys:manage).
var DefaultPermissions = []string{
	PermNotificationsSend,
	PermTemplatesManage,
	PermTenantsManage,
}

// ValidatedKey holds the result of a successful API key validation.
type ValidatedKey struct {
	ID          string
	Permissions []string
}

type contextKey string

const validatedKeyContextKey contextKey = "validatedKey"

// WithValidatedKey stores a ValidatedKey in the request context.
func WithValidatedKey(ctx context.Context, key *ValidatedKey) context.Context {
	return context.WithValue(ctx, validatedKeyContextKey, key)
}

// GetValidatedKey retrieves the ValidatedKey from the request context.
func GetValidatedKey(ctx context.Context) *ValidatedKey {
	v, _ := ctx.Value(validatedKeyContextKey).(*ValidatedKey)
	return v
}

// RequirePermission returns middleware that checks the validated key has the required permission.
func RequirePermission(perm string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := GetValidatedKey(r.Context())
			if key == nil {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			for _, p := range key.Permissions {
				if p == perm {
					next.ServeHTTP(w, r)
					return
				}
			}
			http.Error(w, `{"error":"insufficient permissions"}`, http.StatusForbidden)
		})
	}
}

// ValidatePermissions checks that all provided permissions are in the known set.
func ValidatePermissions(perms []string) error {
	valid := make(map[string]bool, len(AllPermissions))
	for _, p := range AllPermissions {
		valid[p] = true
	}
	for _, p := range perms {
		if !valid[p] {
			return fmt.Errorf("unknown permission: %s", p)
		}
	}
	return nil
}
```

- [ ] **Step 4: Update middleware.go to use ValidatedKey**

Replace contents of `internal/auth/middleware.go`:

```go
package auth

import (
	"net/http"
	"strings"
)

// APIKeyValidator validates a raw API key and returns the validated key on success.
type APIKeyValidator func(rawKey string) *ValidatedKey

// APIKeyMiddleware returns HTTP middleware that validates API keys from the Authorization header.
func APIKeyMiddleware(validate APIKeyValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
				next.ServeHTTP(w, r)
				return
			}

			key := r.Header.Get("Authorization")
			if key == "" {
				http.Error(w, `{"error":"missing api key"}`, http.StatusUnauthorized)
				return
			}
			key = strings.TrimPrefix(key, "Bearer ")

			validated := validate(key)
			if validated == nil {
				http.Error(w, `{"error":"invalid api key"}`, http.StatusUnauthorized)
				return
			}

			ctx := WithValidatedKey(r.Context(), validated)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
```

- [ ] **Step 5: Run all auth tests**

Run: `go test ./internal/auth/... -v`
Expected: all tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/auth/permissions.go internal/auth/permissions_test.go internal/auth/middleware.go
git commit -m "feat: add permission constants, RequirePermission middleware, and ValidatedKey context"
```

---

## Task 4: Database Migration and Model Update

**Files:**
- Create: `migrations/000010_add_api_key_permissions.up.sql`
- Create: `migrations/000010_add_api_key_permissions.down.sql`
- Modify: `internal/models/models.go`

- [ ] **Step 1: Create up migration**

Create `migrations/000010_add_api_key_permissions.up.sql`:

```sql
ALTER TABLE api_keys ADD COLUMN permissions TEXT[] NOT NULL DEFAULT '{}';
```

- [ ] **Step 2: Create down migration**

Create `migrations/000010_add_api_key_permissions.down.sql`:

```sql
ALTER TABLE api_keys DROP COLUMN permissions;
```

- [ ] **Step 3: Update APIKey model**

In `internal/models/models.go`, add `Permissions` field to `APIKey`:

```go
type APIKey struct {
	ID          string    `json:"id"`
	KeyHash     string    `json:"-"`
	Name        string    `json:"name"`
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"created_at"`
}
```

- [ ] **Step 4: Commit**

```bash
git add migrations/000010_add_api_key_permissions.up.sql migrations/000010_add_api_key_permissions.down.sql internal/models/models.go
git commit -m "feat: add permissions column to api_keys table and model"
```

---

## Task 5: Store Layer Updates

**Files:**
- Modify: `internal/store/apikeys.go`

- [ ] **Step 1: Update CreateAPIKey to accept permissions**

Update `internal/store/apikeys.go` — change `CreateAPIKey` signature and SQL:

```go
func (s *Store) CreateAPIKey(ctx context.Context, id, keyHash, name string, permissions []string) (*models.APIKey, error) {
	k := &models.APIKey{ID: id, KeyHash: keyHash, Name: name, Permissions: permissions}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO api_keys (id, key_hash, name, permissions) VALUES ($1, $2, $3, $4) RETURNING created_at`,
		k.ID, k.KeyHash, k.Name, k.Permissions,
	).Scan(&k.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create api key: %w", err)
	}
	return k, nil
}
```

Note: The `id` parameter is now passed in (generated by the caller using `GenerateAPIKey`), rather than being generated internally with `id.New()`.

- [ ] **Step 2: Update ListAPIKeys to include permissions**

```go
func (s *Store) ListAPIKeys(ctx context.Context) ([]models.APIKey, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, key_hash, name, permissions, created_at FROM api_keys`)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	var keys []models.APIKey
	for rows.Next() {
		var k models.APIKey
		if err := rows.Scan(&k.ID, &k.KeyHash, &k.Name, &k.Permissions, &k.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}
```

- [ ] **Step 3: Add GetAPIKeyByID**

```go
func (s *Store) GetAPIKeyByID(ctx context.Context, id string) (*models.APIKey, error) {
	var k models.APIKey
	err := s.pool.QueryRow(ctx,
		`SELECT id, key_hash, name, permissions, created_at FROM api_keys WHERE id = $1`,
		id,
	).Scan(&k.ID, &k.KeyHash, &k.Name, &k.Permissions, &k.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get api key: %w", err)
	}
	return &k, nil
}
```

Add imports: `"errors"` and `"github.com/jackc/pgx/v5"`
```

- [ ] **Step 4: Add DeleteAPIKey**

```go
func (s *Store) DeleteAPIKey(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM api_keys WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("api key not found: %s", id)
	}
	return nil
}
```

- [ ] **Step 5: Run build to check compilation**

Run: `go build ./internal/store/...`
Expected: compilation errors from callers of old `CreateAPIKey` signature — this is expected, will fix in later tasks

- [ ] **Step 6: Commit**

```bash
git add internal/store/apikeys.go
git commit -m "feat: update store with permissions, add GetAPIKeyByID and DeleteAPIKey"
```

---

## Task 6: Config Update

**Files:**
- Modify: `internal/config/config.go`

- [ ] **Step 1: Add APIKeyHMACSecret to config**

Add the field to the `Config` struct and `Load()`:

```go
type Config struct {
	HTTPPort         int
	DatabaseURL      string
	NATSUrl          string
	RedisURL         string
	JWTSecret        string
	APIKeyHMACSecret string
	CentrifugoAPIURL string
	CentrifugoAPIKey string
	EmailWebhookURL  string
	SMSWebhookURL    string
}
```

In `Load()`, add:

```go
APIKeyHMACSecret: envStr("HERMES_API_KEY_HMAC_SECRET", "hermes-dev-hmac-secret"),
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/config/... -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/config/config.go
git commit -m "feat: add APIKeyHMACSecret config field"
```

---

## Task 7: Redis Cache Methods

**Files:**
- Modify: `internal/cache/redis.go`

- [ ] **Step 1: Add API key cache methods**

Add to `internal/cache/redis.go`:

```go
// GetAPIKey returns the cached API key data (JSON bytes) for the given key ID.
// Returns (data, nil) on hit, (nil, nil) on miss.
func (c *Client) GetAPIKey(ctx context.Context, keyID string) ([]byte, error) {
	val, err := c.rdb.Get(ctx, "apikey:"+keyID).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get api key: %w", err)
	}
	return val, nil
}

// SetAPIKey caches API key data (JSON bytes) for the given key ID.
func (c *Client) SetAPIKey(ctx context.Context, keyID string, data []byte, ttl time.Duration) error {
	return c.rdb.Set(ctx, "apikey:"+keyID, data, ttl).Err()
}

// InvalidateAPIKey removes the cached API key for the given key ID.
func (c *Client) InvalidateAPIKey(ctx context.Context, keyID string) error {
	return c.rdb.Del(ctx, "apikey:"+keyID).Err()
}
```

- [ ] **Step 2: Run build**

Run: `go build ./internal/cache/...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/cache/redis.go
git commit -m "feat: add API key cache methods to Redis client"
```

---

## Task 8: Admin Store Interface and Server Updates

**Files:**
- Modify: `internal/admin/server.go`
- Modify: `internal/admin/testutil_test.go`

- [ ] **Step 1: Update AdminStore interface**

In `internal/admin/server.go`, update the API Keys section of `AdminStore`:

```go
// API Keys
CreateAPIKey(ctx context.Context, id, keyHash, name string, permissions []string) (*models.APIKey, error)
ListAPIKeys(ctx context.Context) ([]models.APIKey, error)
GetAPIKeyByID(ctx context.Context, id string) (*models.APIKey, error)
DeleteAPIKey(ctx context.Context, id string) error
```

- [ ] **Step 2: Add hmacSecret field to Server struct**

Add `hmacSecret string` to the `Server` struct and update `NewServer`:

```go
type Server struct {
	store      AdminStore
	nats       *messaging.Client
	cache      *cache.Client
	pool       *pgxpool.Pool
	logger     *slog.Logger
	router     chi.Router
	api        huma.API
	skipAuth   bool
	jwtSecret  []byte
	hmacSecret string
}

func NewServer(store AdminStore, nats *messaging.Client, cache *cache.Client, pool *pgxpool.Pool, jwtSecret []byte, hmacSecret string, logger *slog.Logger) *Server {
	s := &Server{
		store:      store,
		nats:       nats,
		cache:      cache,
		pool:       pool,
		jwtSecret:  jwtSecret,
		hmacSecret: hmacSecret,
		logger:     logger,
		router:     chi.NewRouter(),
	}
	// ... rest unchanged
}
```

- [ ] **Step 3: Replace validateAPIKey with HMAC + cache lookup**

Replace the `validateAPIKey` method:

```go
func (s *Server) validateAPIKey(rawKey string) *auth.ValidatedKey {
	keyID, secret, err := auth.ParseAPIKey(rawKey)
	if err != nil {
		return nil
	}

	// Try cache first
	var keyHash string
	var permissions []string
	if s.cache != nil {
		cached, err := s.cache.GetAPIKey(context.Background(), keyID)
		if err != nil {
			s.logger.Error("cache get api key failed", "error", err)
		} else if cached != nil {
			var entry struct {
				KeyHash     string   `json:"key_hash"`
				Permissions []string `json:"permissions"`
			}
			if json.Unmarshal(cached, &entry) == nil {
				keyHash = entry.KeyHash
				permissions = entry.Permissions
			}
		}
	}

	// Cache miss — load from store
	if keyHash == "" {
		k, err := s.store.GetAPIKeyByID(context.Background(), keyID)
		if err != nil {
			return nil // generic failure — no distinction between not found and error
		}
		keyHash = k.KeyHash
		permissions = k.Permissions

		// Populate cache
		if s.cache != nil {
			entry, _ := json.Marshal(struct {
				KeyHash     string   `json:"key_hash"`
				Permissions []string `json:"permissions"`
			}{keyHash, permissions})
			if err := s.cache.SetAPIKey(context.Background(), keyID, entry, 5*time.Minute); err != nil {
				s.logger.Error("cache set api key failed", "error", err)
			}
		}
	}

	if !auth.HMACVerifyAPIKey(secret, keyHash, s.hmacSecret) {
		return nil
	}

	return &auth.ValidatedKey{ID: keyID, Permissions: permissions}
}
```

Add `"encoding/json"` and `"time"` imports to the file.

- [ ] **Step 4: Update routes() to register API key routes with permission middleware**

Update `routes()` to use chi Groups with permission middleware for all route groups:

```go
func (s *Server) routes() {
	s.router.Get("/healthz", httputil.HealthzHandler())
	if s.pool != nil {
		s.router.Get("/readyz", httputil.ReadyzHandler(s.pool.Ping))
	} else {
		s.router.Get("/readyz", httputil.ReadyzHandler())
	}

	// Apply permission middleware per route group using chi's Route grouping.
	// Huma registers on the underlying chi router, so we use chi middleware.
	s.router.Group(func(r chi.Router) {
		r.Use(auth.RequirePermission(auth.PermTenantsManage))
		// Groups and tenants share the tenants:manage permission
	})

	s.registerGroupRoutes()
	s.registerTypeRoutes()
	s.registerSendRoutes()
	s.registerNotificationRoutes()
	s.registerAuthRoutes()
	s.registerAPIKeyRoutes()
}
```

Note: Because huma registers routes directly on the router, the cleanest approach is to check permissions inside each handler by calling `auth.GetValidatedKey(ctx)` and checking permissions. Update `registerAPIKeyRoutes()` handlers to check `auth.PermAPIKeysManage` at the top of each handler. Apply the same pattern to existing route registrations in a follow-up. For now, the API key handlers are the only ones that enforce permissions.

In `registerAPIKeyRoutes()`, add at the top of each handler function:

```go
key := auth.GetValidatedKey(ctx)
if key == nil || !auth.HasPermission(key, auth.PermAPIKeysManage) {
	return nil, huma.Error403Forbidden("insufficient permissions")
}
```

Add a helper to `internal/auth/permissions.go`:

```go
// HasPermission checks if a validated key has a specific permission.
func HasPermission(key *ValidatedKey, perm string) bool {
	for _, p := range key.Permissions {
		if p == perm {
			return true
		}
	}
	return false
}
```

- [ ] **Step 5: Update mock store in testutil_test.go**

Add mock methods:

```go
func (m *mockStore) CreateAPIKey(ctx context.Context, id, keyHash, name string, permissions []string) (*models.APIKey, error) {
	k := models.APIKey{
		ID:          id,
		KeyHash:     keyHash,
		Name:        name,
		Permissions: permissions,
		CreatedAt:   time.Now(),
	}
	m.apiKeys = append(m.apiKeys, k)
	return &k, nil
}

func (m *mockStore) GetAPIKeyByID(ctx context.Context, id string) (*models.APIKey, error) {
	for _, k := range m.apiKeys {
		if k.ID == id {
			return &k, nil
		}
	}
	return nil, fmt.Errorf("api key not found: %s", id)
}

func (m *mockStore) DeleteAPIKey(ctx context.Context, id string) error {
	for i, k := range m.apiKeys {
		if k.ID == id {
			m.apiKeys = append(m.apiKeys[:i], m.apiKeys[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("api key not found: %s", id)
}
```

Update `newTestServer` to pass the new `hmacSecret` parameter:

```go
srv := admin.NewServer(store, nil, nil, nil, []byte("test-jwt-secret"), "test-hmac-secret", logger)
```

- [ ] **Step 6: Run build**

Run: `go build ./internal/admin/...`
Expected: compilation error about missing `registerAPIKeyRoutes` — expected, will add in next task

- [ ] **Step 7: Commit**

```bash
git add internal/admin/server.go internal/admin/testutil_test.go
git commit -m "feat: update admin server with HMAC validation, cache, and permissions"
```

---

## Task 9: API Key Handlers

**Files:**
- Create: `internal/admin/handler_apikeys.go`
- Create: `internal/admin/handler_apikeys_test.go`

- [ ] **Step 1: Write failing handler tests**

Create `internal/admin/handler_apikeys_test.go`:

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

func TestHandleListAPIKeys(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/apikeys", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Body []models.APIKey `json:"body"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)
}

func TestHandleCreateAPIKey(t *testing.T) {
	srv := newTestServer(t)

	body := `{"name":"Test Key","permissions":["notifications:send"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/apikeys", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Body struct {
			ID          string   `json:"id"`
			Name        string   `json:"name"`
			RawKey      string   `json:"raw_key"`
			Permissions []string `json:"permissions"`
		}
	}
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.Body.Name != "Test Key" {
		t.Fatalf("expected name 'Test Key', got %s", resp.Body.Name)
	}
	if resp.Body.RawKey == "" {
		t.Fatal("expected raw_key to be set")
	}
	if len(resp.Body.Permissions) != 1 || resp.Body.Permissions[0] != "notifications:send" {
		t.Fatalf("expected permissions [notifications:send], got %v", resp.Body.Permissions)
	}
}

func TestHandleCreateAPIKey_InvalidPermission(t *testing.T) {
	srv := newTestServer(t)

	body := `{"name":"Bad Key","permissions":["foo:bar"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/apikeys", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleCreateAPIKey_DefaultPermissions(t *testing.T) {
	srv := newTestServer(t)

	body := `{"name":"Default Key"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/apikeys", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Body struct {
			Permissions []string `json:"permissions"`
		}
	}
	json.NewDecoder(rec.Body).Decode(&resp)

	// Should default to all except apikeys:manage
	if len(resp.Body.Permissions) != 3 {
		t.Fatalf("expected 3 default permissions, got %d: %v", len(resp.Body.Permissions), resp.Body.Permissions)
	}
}

func TestHandleDeleteAPIKey(t *testing.T) {
	srv := newTestServer(t)

	// Create a key first
	body := `{"name":"To Delete","permissions":["notifications:send"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/apikeys", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	var createResp struct {
		Body struct {
			ID string `json:"id"`
		}
	}
	json.NewDecoder(rec.Body).Decode(&createResp)
	keyID := createResp.Body.ID

	// Delete it
	req = httptest.NewRequest(http.MethodDelete, "/v1/apikeys/"+keyID, nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleDeleteAPIKey_NotFound(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/v1/apikeys/key_nonexistent", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/admin/... -run TestHandleAPIKey -v`
Expected: compilation error — handler file doesn't exist

- [ ] **Step 3: Implement handler_apikeys.go**

Create `internal/admin/handler_apikeys.go`:

```go
package admin

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/hermes-notifications/hermes/internal/auth"
)

// --- Input/Output types ---

type createAPIKeyInput struct {
	Body struct {
		Name        string   `json:"name" required:"true" minLength:"1" doc:"Human-readable key name"`
		Permissions []string `json:"permissions,omitempty" doc:"Permission set (defaults to all except apikeys:manage)"`
	}
}

type apiKeyCreatedOutput struct {
	Body struct {
		ID          string    `json:"id"`
		Name        string    `json:"name"`
		RawKey      string    `json:"raw_key"`
		Permissions []string  `json:"permissions"`
		CreatedAt   time.Time `json:"created_at"`
	}
}

type apiKeyOutput struct {
	Body struct {
		ID          string    `json:"id"`
		Name        string    `json:"name"`
		Permissions []string  `json:"permissions"`
		CreatedAt   time.Time `json:"created_at"`
	}
}

type listAPIKeysOutput struct {
	Body []struct {
		ID          string    `json:"id"`
		Name        string    `json:"name"`
		Permissions []string  `json:"permissions"`
		CreatedAt   time.Time `json:"created_at"`
	}
}

type deleteAPIKeyInput struct {
	ID string `path:"id" doc:"API key ID"`
}

// --- Route registration ---

func (s *Server) registerAPIKeyRoutes() {
	huma.Register(s.api, huma.Operation{
		OperationID:   "list-api-keys",
		Method:        http.MethodGet,
		Path:          "/v1/apikeys",
		Summary:       "List all API keys",
		Tags:          []string{"API Keys"},
	}, func(ctx context.Context, input *struct{}) (*listAPIKeysOutput, error) {
		keys, err := s.store.ListAPIKeys(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to list api keys")
		}
		out := &listAPIKeysOutput{}
		for _, k := range keys {
			out.Body = append(out.Body, struct {
				ID          string    `json:"id"`
				Name        string    `json:"name"`
				Permissions []string  `json:"permissions"`
				CreatedAt   time.Time `json:"created_at"`
			}{
				ID:          k.ID,
				Name:        k.Name,
				Permissions: k.Permissions,
				CreatedAt:   k.CreatedAt,
			})
		}
		return out, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID:   "create-api-key",
		Method:        http.MethodPost,
		Path:          "/v1/apikeys",
		Summary:       "Create a new API key",
		Tags:          []string{"API Keys"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, input *createAPIKeyInput) (*apiKeyCreatedOutput, error) {
		permissions := input.Body.Permissions
		if len(permissions) == 0 {
			permissions = auth.DefaultPermissions
		}

		if err := auth.ValidatePermissions(permissions); err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}

		rawKey, keyID, err := auth.GenerateAPIKey("")
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to generate api key")
		}

		_, secret, err := auth.ParseAPIKey(rawKey)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to parse generated key")
		}

		keyHash := auth.HMACHashAPIKey(secret, s.hmacSecret)

		k, err := s.store.CreateAPIKey(ctx, keyID, keyHash, input.Body.Name, permissions)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to create api key")
		}

		out := &apiKeyCreatedOutput{}
		out.Body.ID = k.ID
		out.Body.Name = k.Name
		out.Body.RawKey = rawKey
		out.Body.Permissions = k.Permissions
		out.Body.CreatedAt = k.CreatedAt
		return out, nil
	})

	huma.Register(s.api, huma.Operation{
		OperationID:   "delete-api-key",
		Method:        http.MethodDelete,
		Path:          "/v1/apikeys/{id}",
		Summary:       "Revoke an API key",
		Tags:          []string{"API Keys"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, input *deleteAPIKeyInput) (*struct{}, error) {
		// Prevent self-deletion
		validated := auth.GetValidatedKey(ctx)
		if validated != nil && validated.ID == input.ID {
			return nil, huma.Error400BadRequest("cannot revoke the key used to authenticate this request")
		}

		if err := s.store.DeleteAPIKey(ctx, input.ID); err != nil {
			return nil, huma.Error404NotFound("api key not found")
		}

		// Invalidate cache
		if s.cache != nil {
			_ = s.cache.InvalidateAPIKey(ctx, input.ID)
		}

		return nil, nil
	})
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/admin/... -v`
Expected: all tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/admin/handler_apikeys.go internal/admin/handler_apikeys_test.go
git commit -m "feat: add API key CRUD handlers with permission validation"
```

---

## Task 10: Fix All Callers of Changed Signatures

The `NewServer` signature changed (added `hmacSecret`), `CreateAPIKey` signature changed, and E2E tests need updating.

**Files:**
- Modify: `cmd/admin/main.go`
- Modify: `cmd/seed/main.go` (temporary fix — full rewrite in Task 13)
- Modify: `tests/e2e/admin_test.go`

- [ ] **Step 1: Update cmd/admin/main.go**

Update the `NewServer` call in `cmd/admin/main.go` to pass `cfg.APIKeyHMACSecret`:

```go
srv := admin.NewServer(st, natsClient, redisClient, pool, []byte(cfg.JWTSecret), cfg.APIKeyHMACSecret, logger)
```

- [ ] **Step 2: Update seed tool temporarily**

In `cmd/seed/main.go`, update the INSERT to include permissions:

```go
tag, err := pool.Exec(ctx,
	`INSERT INTO api_keys (id, key_hash, name, permissions) VALUES ($1, $2, $3, $4) ON CONFLICT (id) DO NOTHING`,
	devKeyID, keyHash, "Development", []string{"apikeys:manage", "notifications:send", "templates:manage", "tenants:manage"},
)
```

- [ ] **Step 3: Update E2E tests**

In `tests/e2e/admin_test.go`:
1. Update the `NewServer` call to pass `"test-hmac-secret"` as the new `hmacSecret` parameter
2. Update the API key insertion to include permissions column
3. Update the raw key and hash to use HMAC instead of Argon2

- [ ] **Step 4: Build everything**

Run: `go build ./...`
Expected: PASS — all callers updated

- [ ] **Step 5: Run all unit tests**

Run: `go test ./... -count=1`
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/admin/main.go cmd/seed/main.go tests/e2e/admin_test.go
git commit -m "fix: update all callers for new NewServer and CreateAPIKey signatures"
```

---

## Task 11: Client Package

**Files:**
- Create: `pkg/client/apikeys.go`
- Modify: `pkg/client/client.go`

- [ ] **Step 1: Write client tests**

Create `pkg/client/apikeys_test.go`:

```go
package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hermes-notifications/hermes/pkg/client"
)

func TestAPIKeysService_List(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/apikeys" || r.Method != http.MethodGet {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode([]client.APIKey{{ID: "key_abc", Name: "Test"}})
	}))
	defer ts.Close()

	c := client.New(ts.URL, "test-key")
	keys, err := c.APIKeys.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].ID != "key_abc" {
		t.Fatalf("unexpected keys: %+v", keys)
	}
}

func TestAPIKeysService_Create(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/apikeys" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(client.APIKeyCreated{ID: "key_abc", Name: "Test", RawKey: "hms_key_abc_secret"})
	}))
	defer ts.Close()

	c := client.New(ts.URL, "test-key")
	created, err := c.APIKeys.Create(context.Background(), client.CreateAPIKeyRequest{Name: "Test"})
	if err != nil {
		t.Fatal(err)
	}
	if created.RawKey != "hms_key_abc_secret" {
		t.Fatalf("unexpected raw_key: %s", created.RawKey)
	}
}

func TestAPIKeysService_Delete(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/apikeys/key_abc" || r.Method != http.MethodDelete {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	c := client.New(ts.URL, "test-key")
	err := c.APIKeys.Delete(context.Background(), "key_abc")
	if err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./pkg/client/... -run TestAPIKeys -v`
Expected: compilation errors

- [ ] **Step 3: Implement apikeys.go**

Create `pkg/client/apikeys.go`:

```go
package client

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type APIKey struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"created_at"`
}

type APIKeyCreated struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	RawKey      string    `json:"raw_key"`
	Permissions []string  `json:"permissions"`
	CreatedAt   time.Time `json:"created_at"`
}

type CreateAPIKeyRequest struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions,omitempty"`
}

type APIKeysService struct {
	client *Client
}

func (s *APIKeysService) List(ctx context.Context) ([]APIKey, error) {
	req, err := s.client.newRequest(ctx, http.MethodGet, "/v1/apikeys", nil)
	if err != nil {
		return nil, err
	}
	var keys []APIKey
	if err := s.client.do(req, &keys); err != nil {
		return nil, err
	}
	return keys, nil
}

func (s *APIKeysService) Create(ctx context.Context, body CreateAPIKeyRequest) (*APIKeyCreated, error) {
	req, err := s.client.newRequest(ctx, http.MethodPost, "/v1/apikeys", body)
	if err != nil {
		return nil, err
	}
	var created APIKeyCreated
	if err := s.client.do(req, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

func (s *APIKeysService) Delete(ctx context.Context, id string) error {
	req, err := s.client.newRequest(ctx, http.MethodDelete, fmt.Sprintf("/v1/apikeys/%s", id), nil)
	if err != nil {
		return err
	}
	return s.client.do(req, nil)
}
```

- [ ] **Step 4: Add APIKeys service to Client**

In `pkg/client/client.go`, add to `Client` struct:

```go
APIKeys       *APIKeysService
```

And in `New()`:

```go
c.APIKeys = &APIKeysService{client: c}
```

- [ ] **Step 5: Run tests**

Run: `go test ./pkg/client/... -v`
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add pkg/client/apikeys.go pkg/client/apikeys_test.go pkg/client/client.go
git commit -m "feat: add API keys client service"
```

---

## Task 12: CLI Commands

**Files:**
- Create: `internal/cli/apikeys.go`
- Modify: `internal/cli/root.go`

- [ ] **Step 1: Implement CLI commands**

Create `internal/cli/apikeys.go`:

```go
package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/hermes-notifications/hermes/pkg/client"
	"github.com/spf13/cobra"
)

func newAPIKeysCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "apikey", Short: "Manage API keys"}
	cmd.AddCommand(newAPIKeysListCmd())
	cmd.AddCommand(newAPIKeysCreateCmd())
	cmd.AddCommand(newAPIKeysRevokeCmd())
	return cmd
}

func newAPIKeysListCmd() *cobra.Command {
	return &cobra.Command{
		Use: "list", Short: "List all API keys",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClientFromCmd(cmd)
			keys, err := c.APIKeys.List(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if getOutput(cmd) == "json" {
				return printJSON(out, keys)
			}
			var rows [][]string
			for _, k := range keys {
				rows = append(rows, []string{
					k.ID,
					bold(k.Name),
					strings.Join(k.Permissions, ","),
					fmtTime(k.CreatedAt.Format(time.RFC3339)),
				})
			}
			printTable(out, []string{"ID", "NAME", "PERMISSIONS", "CREATED"}, rows)
			return nil
		},
	}
}

func newAPIKeysCreateCmd() *cobra.Command {
	var name string
	var permissions []string
	cmd := &cobra.Command{
		Use: "create", Short: "Create a new API key",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClientFromCmd(cmd)
			req := client.CreateAPIKeyRequest{Name: name}
			if len(permissions) > 0 {
				req.Permissions = permissions
			}
			k, err := c.APIKeys.Create(cmd.Context(), req)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if getOutput(cmd) == "json" {
				return printJSON(out, k)
			}
			fmt.Fprintf(out, "ID:          %s\n", k.ID)
			fmt.Fprintf(out, "Name:        %s\n", k.Name)
			fmt.Fprintf(out, "API Key:     %s\n", bold(k.RawKey))
			fmt.Fprintf(out, "Permissions: %s\n", strings.Join(k.Permissions, ", "))
			fmt.Fprintf(out, "\n%s\n", dim("Store this key securely — it cannot be retrieved again."))
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Key name (required)")
	cmd.Flags().StringSliceVar(&permissions, "permissions", nil, "Permissions (comma-separated)")
	cmd.MarkFlagRequired("name")
	return cmd
}

func newAPIKeysRevokeCmd() *cobra.Command {
	var id string
	cmd := &cobra.Command{
		Use: "revoke", Short: "Revoke an API key",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClientFromCmd(cmd)
			if err := c.APIKeys.Delete(cmd.Context(), id); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if getOutput(cmd) == "json" {
				return printJSON(out, map[string]string{"status": "revoked", "id": id})
			}
			fmt.Fprintf(out, "%s %s\n", success("Revoked key"), bold(id))
			return nil
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "API key ID (required)")
	cmd.MarkFlagRequired("id")
	return cmd
}
```

- [ ] **Step 2: Register in root.go**

In `internal/cli/root.go`, add to `newRootCmd()`:

```go
cmd.AddCommand(newAPIKeysCmd())
```

- [ ] **Step 3: Run build**

Run: `go build ./cmd/hermes/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/cli/apikeys.go internal/cli/root.go
git commit -m "feat: add hermes apikey create/list/revoke CLI commands"
```

---

## Task 13: Seed Tool Rewrite

**Files:**
- Modify: `cmd/seed/main.go`

- [ ] **Step 1: Rewrite seed tool**

Replace contents of `cmd/seed/main.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

var allPermissions = []string{
	auth.PermAPIKeysManage,
	auth.PermNotificationsSend,
	auth.PermTemplatesManage,
	auth.PermTenantsManage,
}

func main() {
	dbURL := flag.String("database-url", os.Getenv("HERMES_DATABASE_URL"), "PostgreSQL connection URL")
	hmacSecret := flag.String("hmac-secret", os.Getenv("HERMES_API_KEY_HMAC_SECRET"), "HMAC secret for key hashing")
	env := flag.String("env", "dev", "Environment: dev, staging, production")
	force := flag.Bool("force", false, "Force rotation even if key exists")
	revokePrevious := flag.Bool("revoke-previous", false, "Revoke previous bootstrap key on rotation")
	awsRegion := flag.String("aws-region", "us-east-1", "AWS region for Secrets Manager")
	flag.Parse()

	if *dbURL == "" {
		log.Fatal("database-url is required (or set HERMES_DATABASE_URL)")
	}

	ctx := context.Background()
	pool, err := database.NewPool(ctx, *dbURL)
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	switch *env {
	case "dev":
		seedDev(ctx, pool, *hmacSecret)
	case "staging", "production":
		if *hmacSecret == "" {
			log.Fatal("hmac-secret is required for non-dev environments (or set HERMES_API_KEY_HMAC_SECRET)")
		}
		seedManaged(ctx, pool, *env, *hmacSecret, *awsRegion, *force, *revokePrevious)
	default:
		log.Fatalf("unknown environment: %s", *env)
	}
}

func seedDev(ctx context.Context, pool *pgxpool.Pool, hmacSecret string) {
	if hmacSecret == "" {
		hmacSecret = "hermes-dev-hmac-secret"
	}

	rawKey, keyID, err := auth.GenerateAPIKey("dev")
	if err != nil {
		log.Fatalf("generate api key: %v", err)
	}

	_, secret, err := auth.ParseAPIKey(rawKey)
	if err != nil {
		log.Fatalf("parse api key: %v", err)
	}

	keyHash := auth.HMACHashAPIKey(secret, hmacSecret)

	tag, err := pool.Exec(ctx,
		`INSERT INTO api_keys (id, key_hash, name, permissions) VALUES ($1, $2, $3, $4) ON CONFLICT (id) DO NOTHING`,
		keyID, keyHash, "Development", allPermissions,
	)
	if err != nil {
		log.Fatalf("insert dev API key: %v", err)
	}

	if tag.RowsAffected() == 0 {
		fmt.Println("dev API key already exists (use --force with staging/production to rotate)")
	} else {
		fmt.Printf("Dev API key seeded:\n  %s\n", rawKey)
	}
}

func seedManaged(ctx context.Context, pool *pgxpool.Pool, env, hmacSecret, awsRegion string, force, revokePrevious bool) {
	secretID := "hermes/" + env

	// Load AWS config
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(awsRegion))
	if err != nil {
		log.Fatalf("load AWS config: %v", err)
	}
	sm := secretsmanager.NewFromConfig(cfg)

	// Check if key already exists in Secrets Manager
	if !force {
		existing, err := getSecretProperty(ctx, sm, secretID, "admin_api_key")
		if err != nil {
			log.Fatalf("check existing secret: %v", err)
		}
		if existing != "" {
			fmt.Printf("Bootstrap key already exists in %s. Use --force to rotate.\n", secretID)
			return
		}
	}

	// Find existing bootstrap key ID (for --revoke-previous)
	var oldKeyID string
	if force {
		old, _ := getSecretProperty(ctx, sm, secretID, "admin_api_key")
		if old != "" {
			if id, _, err := auth.ParseAPIKey(old); err == nil {
				oldKeyID = id
			}
		}
	}

	// Generate new key
	envPrefix := ""
	if env == "staging" {
		envPrefix = "stg"
	}
	rawKey, keyID, err := auth.GenerateAPIKey(envPrefix)
	if err != nil {
		log.Fatalf("generate api key: %v", err)
	}

	_, secret, err := auth.ParseAPIKey(rawKey)
	if err != nil {
		log.Fatalf("parse api key: %v", err)
	}

	keyHash := auth.HMACHashAPIKey(secret, hmacSecret)
	keyName := fmt.Sprintf("Bootstrap (%s)", env)

	// Insert into database
	tag, err := pool.Exec(ctx,
		`INSERT INTO api_keys (id, key_hash, name, permissions) VALUES ($1, $2, $3, $4) ON CONFLICT (id) DO NOTHING`,
		keyID, keyHash, keyName, allPermissions,
	)
	if err != nil {
		log.Fatalf("insert API key: %v", err)
	}
	if tag.RowsAffected() == 0 {
		log.Fatalf("key ID collision — this should not happen, try again")
	}

	// Write to Secrets Manager
	if err := setSecretProperty(ctx, sm, secretID, "admin_api_key", rawKey); err != nil {
		log.Fatalf("write to Secrets Manager: %v", err)
	}

	fmt.Printf("Bootstrap key created for %s:\n  ID: %s\n  Stored in: %s → admin_api_key\n", env, keyID, secretID)

	// Revoke previous if requested
	if revokePrevious && oldKeyID != "" {
		_, err := pool.Exec(ctx, `DELETE FROM api_keys WHERE id = $1`, oldKeyID)
		if err != nil {
			log.Printf("WARNING: failed to revoke previous key %s: %v", oldKeyID, err)
		} else {
			fmt.Printf("  Previous key %s revoked.\n", oldKeyID)
		}
	} else if oldKeyID != "" {
		fmt.Printf("  WARNING: Previous key %s is still valid. Revoke it via:\n", oldKeyID)
		fmt.Printf("    hermes apikey revoke --id %s\n", oldKeyID)
	}
}

func getSecretProperty(ctx context.Context, sm *secretsmanager.Client, secretID, property string) (string, error) {
	out, err := sm.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretID),
	})
	if err != nil {
		return "", nil // Secret may not exist yet
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(*out.SecretString), &m); err != nil {
		return "", err
	}
	return m[property], nil
}

func setSecretProperty(ctx context.Context, sm *secretsmanager.Client, secretID, property, value string) error {
	// Get existing secret
	out, err := sm.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretID),
	})
	var m map[string]string
	if err != nil {
		m = make(map[string]string)
	} else {
		if err := json.Unmarshal([]byte(*out.SecretString), &m); err != nil {
			m = make(map[string]string)
		}
	}

	m[property] = value
	updated, err := json.Marshal(m)
	if err != nil {
		return err
	}

	_, err = sm.PutSecretValue(ctx, &secretsmanager.PutSecretValueInput{
		SecretId:     aws.String(secretID),
		SecretString: aws.String(string(updated)),
	})
	return err
}
```

- [ ] **Step 2: Add AWS SDK dependency**

Run: `go get github.com/aws/aws-sdk-go-v2/config github.com/aws/aws-sdk-go-v2/service/secretsmanager`

- [ ] **Step 3: Run build**

Run: `go build ./cmd/seed/...`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/seed/main.go go.mod go.sum
git commit -m "feat: rewrite seed tool with environment-aware key generation and AWS Secrets Manager"
```

---

## Task 14: Infrastructure Config

**Files:**
- Modify: `deploy/k8s/overlays/staging/external-secrets.yaml`
- Modify: `deploy/k8s/overlays/production/external-secrets.yaml`

- [ ] **Step 1: Add HMAC secret to staging external-secrets.yaml**

Add to the `hermes-secrets` ExternalSecret data list in `deploy/k8s/overlays/staging/external-secrets.yaml`:

```yaml
    - secretKey: HERMES_API_KEY_HMAC_SECRET
      remoteRef:
        key: hermes/staging
        property: api_key_hmac_secret
```

- [ ] **Step 2: Add HMAC secret to production external-secrets.yaml**

Add to the `hermes-secrets` ExternalSecret data list in `deploy/k8s/overlays/production/external-secrets.yaml`:

```yaml
    - secretKey: HERMES_API_KEY_HMAC_SECRET
      remoteRef:
        key: hermes/production
        property: api_key_hmac_secret
```

- [ ] **Step 3: Commit**

```bash
git add deploy/k8s/overlays/staging/external-secrets.yaml deploy/k8s/overlays/production/external-secrets.yaml
git commit -m "feat: add API key HMAC secret to external secrets for staging and production"
```

---

## Task 15: Integration and Final Verification

- [ ] **Step 1: Run full unit test suite**

Run: `go test ./... -count=1`
Expected: all PASS

- [ ] **Step 2: Run linter**

Run: `make lint`
Expected: PASS

- [ ] **Step 3: Run build for all services**

Run: `make build`
Expected: all binaries build successfully

- [ ] **Step 4: Run integration tests (if infra available)**

Run: `make infra-up && make migrate && make test-integration`
Expected: all PASS

- [ ] **Step 5: Commit any fixes**

If any fixes were needed, commit them:

```bash
git add -A
git commit -m "fix: address integration test issues"
```
