# Phase 4: Inbox Service + User Service Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Inbox Service (paginated inbox, read/unread, archive, delete) and User Service (profile, contacts, preferences) with JWT authentication and Centrifugo token issuance — completing the user-facing read path.

**Architecture:** Two new HTTP services using stdlib `net/http`. Both use JWT auth (SaaS provider signs tokens with user ID + tenant ID claims, Hermes validates signature via HMAC secret or JWKS). The Inbox Service also issues Centrifugo connection tokens. Shared store methods handle all DB queries. Centrifugo control events (cross-device sync) are published when inbox state changes.

**Tech Stack:** Go, stdlib `net/http`, JWT (golang-jwt/jwt/v5), PostgreSQL (existing store), Centrifugo (existing client), NATS (for Centrifugo control events).

**Spec:** `docs/superpowers/specs/2026-03-20-hermes-notification-service-design.md`

**Depends on:** Phase 1-3 (foundation, router, delivery workers).

---

## File Structure

```
hermes/
├── cmd/
│   ├── inbox/
│   │   └── main.go                       # Inbox Service entry point
│   └── user/
│       └── main.go                       # User Service entry point
├── internal/
│   ├── auth/
│   │   ├── jwt.go                        # JWT validation + claims extraction (new)
│   │   └── jwt_test.go
│   ├── store/
│   │   ├── inbox.go                      # Inbox queries: list, mark read, archive, delete (new)
│   │   ├── inbox_test.go
│   │   ├── users.go                      # Add UpdateUserContacts method (modify)
│   │   └── users_test.go                 # Add test (modify)
│   ├── inbox/
│   │   ├── server.go                     # Inbox Service HTTP server + routes
│   │   ├── handler_list.go              # GET /v1/inbox
│   │   ├── handler_list_test.go
│   │   ├── handler_actions.go           # PUT/DELETE read, archive, delete, read-all
│   │   ├── handler_actions_test.go
│   │   ├── handler_centrifugo.go        # GET /v1/inbox/centrifugo-token
│   │   └── handler_centrifugo_test.go
│   └── userservice/
│       ├── server.go                     # User Service HTTP server + routes
│       ├── handler_profile.go           # GET /v1/users/me, PUT /v1/users/me/contacts
│       ├── handler_profile_test.go
│       ├── handler_preferences.go       # GET/PUT/DELETE preferences
│       └── handler_preferences_test.go
```

---

### Task 1: JWT Authentication

**Files:**
- Create: `internal/auth/jwt.go`
- Create: `internal/auth/jwt_test.go`

JWT validation middleware + claims extraction for user-facing APIs. The SaaS provider signs JWTs with HMAC (shared secret). Claims: `sub` (user ID in Hermes), `tenant_id`.

- [ ] **Step 1: Install JWT library**

```bash
go get github.com/golang-jwt/jwt/v5
```

- [ ] **Step 2: Write JWT auth**

```go
// internal/auth/jwt.go
package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

const (
	ContextKeyUserID   contextKey = "user_id"
	ContextKeyTenantID contextKey = "tenant_id"
)

type HermesClaims struct {
	jwt.RegisteredClaims
	TenantID string `json:"tenant_id"`
}

// JWTMiddleware validates JWT Bearer tokens and extracts user_id + tenant_id into context.
func JWTMiddleware(secret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
				next.ServeHTTP(w, r)
				return
			}

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, `{"error":"missing authorization"}`, http.StatusUnauthorized)
				return
			}
			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

			claims := &HermesClaims{}
			token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
				}
				return secret, nil
			})
			if err != nil || !token.Valid {
				http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
				return
			}

			userID, _ := claims.GetSubject()
			if userID == "" || claims.TenantID == "" {
				http.Error(w, `{"error":"missing claims"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), ContextKeyUserID, userID)
			ctx = context.WithValue(ctx, ContextKeyTenantID, claims.TenantID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserIDFromContext extracts user_id from context (set by JWTMiddleware).
func UserIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ContextKeyUserID).(string)
	return v
}

// TenantIDFromContext extracts tenant_id from context.
func TenantIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ContextKeyTenantID).(string)
	return v
}
```

- [ ] **Step 3: Write tests**

```go
package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hermes-notifications/hermes/internal/auth"
)

func makeJWT(t *testing.T, secret []byte, userID, tenantID string) string {
	t.Helper()
	claims := &auth.HermesClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		TenantID: tenantID,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := token.SignedString(secret)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

func TestJWTMiddleware_ValidToken(t *testing.T) {
	secret := []byte("test-secret")
	tokenStr := makeJWT(t, secret, "user-123", "tenant-456")

	var gotUserID, gotTenantID string
	handler := auth.JWTMiddleware(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserID = auth.UserIDFromContext(r.Context())
		gotTenantID = auth.TenantIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/v1/inbox", nil)
	req.Header.Set("Authorization", "Bearer "+tokenStr)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if gotUserID != "user-123" {
		t.Fatalf("expected user-123, got %s", gotUserID)
	}
	if gotTenantID != "tenant-456" {
		t.Fatalf("expected tenant-456, got %s", gotTenantID)
	}
}

func TestJWTMiddleware_MissingHeader(t *testing.T) {
	handler := auth.JWTMiddleware([]byte("secret"))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest("GET", "/v1/inbox", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestJWTMiddleware_InvalidToken(t *testing.T) {
	handler := auth.JWTMiddleware([]byte("secret"))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest("GET", "/v1/inbox", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestJWTMiddleware_SkipsHealthChecks(t *testing.T) {
	handler := auth.JWTMiddleware([]byte("secret"))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for healthz, got %d", rec.Code)
	}
}
```

- [ ] **Step 4: Run tests**

```bash
go test ./internal/auth/... -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/auth/jwt.go internal/auth/jwt_test.go
git commit -m "feat: add JWT authentication middleware with claims extraction"
```

---

### Task 2: Inbox Store Methods

**Files:**
- Create: `internal/store/inbox.go`
- Create: `internal/store/inbox_test.go`

Cursor-paginated inbox list, mark read/unread, archive/unarchive, soft delete, read all, unread count.

- [ ] **Step 1: Write inbox store methods**

Key methods:
- `ListInbox(ctx, userID string, archived bool, cursor string, limit int) ([]models.Notification, int, string, error)` — returns notifications + unread count + next cursor. Uses the partial index `idx_notifications_inbox`.
- `MarkRead(ctx, userID, notificationID string) error` — sets `read_at = NOW()`, `status = 'read'` (only if current rank < read rank)
- `MarkUnread(ctx, userID, notificationID string) error` — sets `read_at = NULL`, reverts status to `delivered` if was `read`
- `Archive(ctx, userID, notificationID string) error` — sets `archived_at = NOW()`, `status = 'archived'`
- `Unarchive(ctx, userID, notificationID string) error` — sets `archived_at = NULL`, reverts status
- `SoftDelete(ctx, userID, notificationID string) error` — sets `deleted_at = NOW()`
- `MarkAllRead(ctx, userID string) error` — bulk update

All methods include `user_id` in WHERE clause for authorization (user can only modify their own notifications).

Cursor format: `created_at|id` encoded as base64. Decode on input, use for `WHERE (created_at, id) < ($cursor_time, $cursor_id)`.

- [ ] **Step 2: Write integration tests**

Test list pagination (create 5 notifications, fetch with limit=2, verify cursor works), mark read/unread round-trip, archive/unarchive, soft delete, mark all read.

- [ ] **Step 3: Run tests**

```bash
go test ./internal/store/... -tags=integration -v
```

- [ ] **Step 4: Commit**

```bash
git add internal/store/inbox.go internal/store/inbox_test.go
git commit -m "feat: add inbox store methods with cursor pagination"
```

---

### Task 3: User Store — Update Contacts

**Files:**
- Modify: `internal/store/users.go` — add `UpdateUserContacts`, `GetUserPreferences`
- Modify: `internal/store/users_test.go`

- [ ] **Step 1: Add methods**

```go
func (s *Store) UpdateUserContacts(ctx context.Context, userID string, email, phone *string) (*models.User, error)
func (s *Store) GetUserPreferences(ctx context.Context, userID string) ([]models.UserPreference, error)
```

`UpdateUserContacts` updates email and/or phone on the user record. `GetUserPreferences` lists all preferences for a user (joins with notification_groups for the slug).

- [ ] **Step 2: Write tests, run, commit**

```bash
git commit -m "feat: add user contacts update and preferences list store methods"
```

---

### Task 4: Inbox Service — Server + List Handler

**Files:**
- Create: `internal/inbox/server.go`
- Create: `internal/inbox/handler_list.go`
- Create: `internal/inbox/handler_list_test.go`

The Inbox Service HTTP server with JWT auth middleware and the paginated list endpoint.

`server.go` defines an `InboxStore` interface (similar to AdminStore pattern), creates the server with JWT secret, Centrifugo client, NATS client, and store.

`handler_list.go` implements `GET /v1/inbox` — calls store.ListInbox, returns JSON with data, unread_count, cursor.

Config adds `HERMES_JWT_SECRET` env variable.

- [ ] **Step 1: Create server.go with InboxStore interface and routes**
- [ ] **Step 2: Create handler_list.go**
- [ ] **Step 3: Create handler_list_test.go with mock store**
- [ ] **Step 4: Run tests, commit**

```bash
git commit -m "feat: add inbox service with paginated list endpoint"
```

---

### Task 5: Inbox Service — Action Handlers

**Files:**
- Create: `internal/inbox/handler_actions.go`
- Create: `internal/inbox/handler_actions_test.go`

Handlers for read/unread, archive/unarchive, delete, read-all. Each publishes a Centrifugo control event for cross-device sync.

```
PUT    /v1/inbox/:id/read      → store.MarkRead + publish inbox.updated{action:"read"}
DELETE /v1/inbox/:id/read      → store.MarkUnread + publish inbox.updated{action:"unread"}
PUT    /v1/inbox/:id/archive   → store.Archive + publish inbox.updated{action:"archive"}
DELETE /v1/inbox/:id/archive   → store.Unarchive + publish inbox.updated{action:"unarchive"}
DELETE /v1/inbox/:id           → store.SoftDelete + publish inbox.updated{action:"delete"}
PUT    /v1/inbox/read-all      → store.MarkAllRead + publish inbox.updated{action:"read-all"}
```

Centrifugo publish is optional (nil-guarded for tests).

- [ ] **Step 1: Create handler_actions.go**
- [ ] **Step 2: Create handler_actions_test.go**
- [ ] **Step 3: Run tests, commit**

```bash
git commit -m "feat: add inbox action handlers with Centrifugo sync events"
```

---

### Task 6: Inbox Service — Centrifugo Token Endpoint

**Files:**
- Create: `internal/inbox/handler_centrifugo.go`
- Create: `internal/inbox/handler_centrifugo_test.go`

Issues a short-lived Centrifugo connection token (JWT signed with Centrifugo's HMAC secret). 1 hour TTL with ±10% jitter.

```
GET /v1/inbox/centrifugo-token → returns {"token": "eyJ..."}
```

The token includes: `sub` (user ID), `exp` (1h ± 10% jitter).

Config: `HERMES_CENTRIFUGO_TOKEN_SECRET` env var.

- [ ] **Step 1: Write handler**

```go
func (s *Server) handleCentrifugoToken(w http.ResponseWriter, r *http.Request) {
    userID := auth.UserIDFromContext(r.Context())

    // 1h base TTL with ±10% jitter (54-66 minutes)
    baseTTL := time.Hour
    jitter := time.Duration(rand.Int63n(int64(baseTTL/5))) - baseTTL/10
    exp := time.Now().Add(baseTTL + jitter)

    claims := jwt.MapClaims{
        "sub": userID,
        "exp": jwt.NewNumericDate(exp),
    }
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    tokenStr, err := token.SignedString(s.centrifugoSecret)
    // ...
}
```

- [ ] **Step 2: Write test, run, commit**

```bash
git commit -m "feat: add Centrifugo token issuance with jitter"
```

---

### Task 7: User Service — Server + Profile Handler

**Files:**
- Create: `internal/userservice/server.go`
- Create: `internal/userservice/handler_profile.go`
- Create: `internal/userservice/handler_profile_test.go`

User Service with JWT auth. Profile endpoints:
- `GET /v1/users/me` — returns user profile
- `PUT /v1/users/me/contacts` — updates email/phone

- [ ] **Step 1: Create server.go with UserStore interface**
- [ ] **Step 2: Create handler_profile.go**
- [ ] **Step 3: Create handler_profile_test.go with mock store**
- [ ] **Step 4: Run tests, commit**

```bash
git commit -m "feat: add user service with profile and contacts endpoints"
```

---

### Task 8: User Service — Preferences Handler

**Files:**
- Create: `internal/userservice/handler_preferences.go`
- Create: `internal/userservice/handler_preferences_test.go`

```
GET    /v1/users/me/preferences            → list all preferences
PUT    /v1/users/me/preferences/:group_id  → set/override channels for a group
DELETE /v1/users/me/preferences/:group_id  → revert to group defaults
```

- [ ] **Step 1: Create handler_preferences.go**
- [ ] **Step 2: Create handler_preferences_test.go**
- [ ] **Step 3: Run tests, commit**

```bash
git commit -m "feat: add user notification preferences endpoints"
```

---

### Task 9: Service Entry Points

**Files:**
- Create: `cmd/inbox/main.go`
- Create: `cmd/user/main.go`
- Modify: `internal/config/config.go` — add JWTSecret, CentrifugoTokenSecret

Both services wire up: Postgres pool, NATS client (for Centrifugo events), Centrifugo client (inbox only), JWT secret from config, store. Health checks. Graceful shutdown.

- Inbox Service: port 8086
- User Service: port 8087

Config additions:
```go
JWTSecret             string  // HERMES_JWT_SECRET
CentrifugoTokenSecret string  // HERMES_CENTRIFUGO_TOKEN_SECRET
CentrifugoAPIURL      string  // HERMES_CENTRIFUGO_API_URL
CentrifugoAPIKey      string  // HERMES_CENTRIFUGO_API_KEY
```

- [ ] **Step 1: Update config.go**
- [ ] **Step 2: Create cmd/inbox/main.go**
- [ ] **Step 3: Create cmd/user/main.go**
- [ ] **Step 4: Verify both compile**
- [ ] **Step 5: Commit**

```bash
git commit -m "feat: add inbox and user service entry points"
```

---

### Task 10: Integration Test

**Files:**
- Create: `tests/e2e/inbox_test.go`

Test the read path:
1. Create tenant, user, group, notification type, API key
2. Send a notification via Admin
3. Run Router + Workers + Event Writer to deliver
4. Create a JWT for the user
5. GET /v1/inbox — verify notification appears with unread_count=1
6. PUT /v1/inbox/:id/read — mark read
7. GET /v1/inbox — verify unread_count=0
8. PUT /v1/inbox/:id/archive — archive
9. GET /v1/inbox — verify empty (default excludes archived)
10. GET /v1/inbox?archived=true — verify appears

- [ ] **Step 1: Write test**
- [ ] **Step 2: Run, verify**
- [ ] **Step 3: Commit**

```bash
git commit -m "test: add inbox service integration test"
```

---

### Task 11: Tidy and Final Verification

- [ ] **Step 1: go mod tidy**
- [ ] **Step 2: Run all unit tests**
- [ ] **Step 3: Build all 8 binaries**

```bash
go build ./cmd/admin/ && go build ./cmd/router/ && go build ./cmd/worker-events/ && go build ./cmd/worker-email/ && go build ./cmd/worker-sms/ && go build ./cmd/worker-inbox/ && go build ./cmd/inbox/ && go build ./cmd/user/
```

- [ ] **Step 4: Commit tidy**

---

## Phase 4 Completion Criteria

- [ ] JWT authentication middleware with claims extraction
- [ ] Inbox store: paginated list with cursor, read/unread, archive/unarchive, soft delete, read-all, unread count
- [ ] User store: update contacts, list preferences
- [ ] Inbox Service with all endpoints (list, read, unread, archive, unarchive, delete, read-all, centrifugo-token)
- [ ] User Service with all endpoints (profile, contacts, preferences)
- [ ] Centrifugo token issuance with 1h TTL + ±10% jitter
- [ ] Centrifugo control events for cross-device sync on inbox actions
- [ ] Both services compile and have health checks
- [ ] Integration test for the read path
- [ ] All unit and integration tests pass
- [ ] All 8 binaries compile
