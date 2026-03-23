# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Test Commands

```bash
# Start local infrastructure (Postgres, NATS with JetStream, Redis)
make infra-up

# Build all services (binaries output to bin/<service>/service)
make build

# Build a single service
make build-admin

# Run unit tests (no infrastructure needed)
make test

# Run a single test
go test ./internal/store/... -run TestCreateGroup -v

# Run integration tests (requires make infra-up)
make test-integration

# Run E2E tests (requires make infra-up)
make test-e2e

# Lint
make lint

# Run database migrations
make migrate

# Build Docker image for a service
make docker-admin

# Install git hooks (one-time setup, requires lefthook)
make hooks

# Validate AsyncAPI spec
make asyncapi-check

# Show all available Make targets
make help
```

## Architecture

Hermes is an event-driven notification platform built as a Go monorepo of 8 microservices. Notifications flow through an async pipeline via NATS JetStream:

```
SaaS Backend → Admin Service → NATS [notification.send] → Dispatch → NATS [delivery.*] → Workers → NATS [notification.events] → Event Writer → Postgres
```

**Write path (API key auth):** Admin Service validates, persists to Postgres, publishes to NATS. Dispatch resolves templates (Redis-cached) and channels, fans out to delivery subjects. Workers deliver (email/SMS via webhook, inbox via Centrifugo push). Event Writer batch-inserts events and updates notification status.

**Read path (JWT auth):** Inbox Service serves paginated inbox with cursor-based pagination. User Service manages profiles and notification preferences. Centrifugo provides real-time WebSocket push.

### Service Ports (local dev defaults)
- Admin: 8080, Dispatch: 8081, Event Writer: 8082
- Email Worker: 8083, SMS Worker: 8084, Inbox Worker: 8085
- Inbox Service: 8086, User Service: 8087

### Key Design Patterns

**Store interfaces per service:** Each service defines its own store interface (e.g., `AdminStore`, `InboxStore`, `UserStore`) that `*store.Store` satisfies. This enables mock-based unit tests — see `testutil_test.go` in each service package for the mock store pattern.

**Notification status rollup:** Status only advances (pending→sent→delivered→read→archived), never regresses. The Event Writer uses conditional SQL with rank comparison in the WHERE clause to handle out-of-order events.

**Channel resolution order:** Explicit override from send request → user preferences per group → group default channels. For type-based sends, channels are filtered to those with templates defined.

**Two auth modes:** API key (argon2 hashed) for server-to-server Admin API. JWT (HMAC-signed by the SaaS provider) for user-facing Inbox/User APIs. Health endpoints (`/healthz`, `/readyz`) skip auth.

**NATS message types:** Shared structs in `internal/nats/` (package `hermenats`) define the contract between services. `SendMessage`, `DeliveryMessage`, `EventMessage`.

**IDs:** Crockford Base32 time-sortable IDs (`internal/id/`) for all entities except tenants (UUIDv4). 48-bit ms timestamp + 80-bit random = 26 chars, lexicographically sortable.

### Infrastructure
- **Postgres:** Single shared database. All services use `internal/store/`. Migrations in `migrations/` via golang-migrate.
- **NATS JetStream:** Three streams — NOTIFICATIONS, DELIVERY, EVENTS. WorkQueue retention (each message consumed once).
- **Redis:** Centrifugo engine + notification type config cache + idempotency key dedup.
- **Centrifugo:** Real-time WebSocket push. User-limited channels (`user#<user_id>`). NATS broker, Redis engine.

### Config
All config via environment variables with `HERMES_` prefix. Defaults target Docker Compose local dev. See `internal/config/config.go` for all fields.

### Testing Strategy
- **Unit tests** (`*_test.go` without build tags): mock store interfaces, httptest for handlers. No infrastructure needed.
- **Integration tests** (`//go:build integration`): real Postgres/NATS/Redis via Docker Compose. Store tests, cache tests, messaging tests.
- **E2E tests** (`tests/e2e/`, `//go:build integration`): wire up multiple services in-process against real infrastructure. Test full notification pipeline.

When writing new handlers, follow the existing pattern: define methods on the store interface, implement in `internal/store/`, create mock in the service's `testutil_test.go`, test handlers with httptest.

## Tool Usage Rules

**Always use dedicated tools instead of shell commands:**
- File search: use `Glob` (not `find` or `ls`)
- Content search: use `Grep` (not `grep` or `rg`)
- Read files: use `Read` (not `cat`, `head`, `tail`, or `xargs cat`)
- Edit files: use `Edit` (not `sed` or `awk`)
- Write files: use `Write` (not `echo` or `cat <<EOF`)

These tools are always available without permission prompts. Shell equivalents require manual approval and should only be used for actual shell operations (running builds, starting services, etc.).

**Validation:** Use `jq . file.json > /dev/null` to validate JSON files.

**Worktree and subagent rules:**
- Always use relative paths or paths within your current working directory.
- Never use absolute paths to the main repo (e.g., `/Users/.../hermes/...`) from a worktree — your copy of the code is in the worktree directory.
- Never search outside your working directory. The worktree contains the full repo.
