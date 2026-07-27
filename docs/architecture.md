# Architecture

Hermes is a Go monorepo of small, single-purpose services connected by **NATS JetStream**.
A notification enters through a thin ingestion service, moves through an asynchronous pipeline,
and is delivered on one or more channels. Every state change is recorded as an event, and the
notification's rolled-up status is what user-facing read APIs serve.

This document explains how the pieces fit together. For a per-service field reference (ports,
subjects, endpoints) see [services.md](services.md); for the database schema see
[data-model.md](data-model.md).

## The two paths

### Write path (API-key auth)

```
SaaS backend
   │  POST /v1/send            (API key)
   ▼
Send ──▶ NATS: notification.send
            │
            ▼
        Dispatch ──▶ NATS: delivery.email | delivery.sms | delivery.inbox
                        │
        ┌───────────────┼────────────────┐
        ▼               ▼                 ▼
   worker-email    worker-sms       worker-inbox
   (SMTP/SES)      (webhook)        (Centrifugo push)
        │               │                 │
        └──────── NATS: notification.events ────────┘
                        │
                        ▼
                  worker-events ──▶ Postgres (events + status rollup)
```

1. **Send** authenticates the caller's API key, applies idempotency, and publishes a
   `SendMessage` to `notification.send`. It does no template/channel resolution — it is a thin
   ingestion layer whose job is to get the request onto NATS quickly.
2. **Dispatch** consumes `notification.send`. It ensures the organization/user exist, persists the
   notification record (status `pending`), resolves the template and the channel set, and
   publishes a `DeliveryMessage` to one `delivery.<channel>` subject per resolved channel.
3. **Workers** (`worker-email`, `worker-sms`, `worker-inbox`) each consume their channel's
   subject, perform the delivery, and publish an `EventMessage` (success or failure) to
   `notification.events`.
4. **worker-events** consumes `notification.events`, batch-inserts rows into
   `notification_events`, and advances the notification's `status`.

### Read path (JWT auth)

- **Inbox** serves the user's notification list with cursor-based pagination plus per-item
  actions (read / unread / archive / unarchive / delete, mark-all-read).
- **User** serves the user's profile and per-subscription notification preferences.
- **Centrifugo** pushes inbox notifications to the browser in real time over WebSockets on a
  per-user channel (`user#<internal-user-id>`); `worker-inbox` publishes to it.

## Services at a glance

| Service | Port | Role | Auth |
|---|---|---|---|
| `send` | 8088 | Ingest `POST /v1/send`, idempotency, publish to NATS | API key |
| `admin` | 8080 | Manage organizations, API keys, categories, subscriptions, templates; issue JWTs | API key |
| `dispatch` | 8081 | Resolve template + channels, fan out to `delivery.*` | internal (NATS) |
| `worker-email` | 8083 | Deliver email (SMTP / SES) | internal (NATS) |
| `worker-sms` | 8084 | Deliver SMS (webhook) | internal (NATS) |
| `worker-inbox` | 8085 | Deliver inbox via Centrifugo push | internal (NATS) |
| `worker-events` | 8082 | Persist events, roll up status | internal (NATS) |
| `inbox` | 8086 | User inbox API | JWT |
| `user` | 8087 | User profile & preferences API | JWT |

CLI/one-shot tools (`migrate`, `seed`, `cleanup`, `loadseed`, `openapi`, `hermes`) are covered
in [services.md](services.md) and [cli.md](cli.md).

In local k3d, an ingress at `http://localhost:8888` routes to each backend by path; the
per-service ports above are the defaults each service binds when run directly.

## Messaging: NATS JetStream

Four streams. The three pipeline streams use **WorkQueue** retention (each message is
delivered to exactly one consumer and removed once acked); the DLQ uses **Limits**
retention (7 days / 1 GiB) so dead letters survive inspection reads:

| Stream | Subject(s) | Producer → Consumer |
|---|---|---|
| `NOTIFICATIONS` | `notification.send` | Send → Dispatch |
| `DELIVERY` | `delivery.email`, `delivery.sms`, `delivery.inbox` | Dispatch → Workers |
| `EVENTS` | `notification.events` | Dispatch & Workers → worker-events |
| `DLQ` | `dlq.>` | messaging layer (terminal failures) → operators (nats CLI) |

The wire contracts are shared Go structs in `internal/nats/` (package `hermenats`,
`internal/nats/messages.go`) and mirrored in the AsyncAPI spec at `api/async/asyncapi.yaml`:

- **`SendMessage`** — notification ID, organization, external user ID, optional direct `email`/`phone`,
  optional `content`, `metadata.template`, `data` (template render context), `channels`,
  `idempotency_key`, `attempt`.
- **`DeliveryMessage`** — notification ID, organization, resolved `user_id`, the target `channel`,
  the resolved `content`, `metadata`, the resolved `recipient` (email/phone), `attempt`.
- **`EventMessage`** — notification ID, `channel`, `event` (e.g. `email.sent`, `sms.failed`),
  `severity` (`info`/`warning`/`error`), and free-form `metadata`.
- **`DeadLetter`** — terminally failed message envelope: original `subject`, source
  `stream`/`consumer`, `reason` (`max_deliveries`/`terminated`), `attempts`, the handler
  `error`, `failed_at`, and the original `payload` verbatim. See the
  [dead-letter-queue runbook](observability/runbooks/dead-letter-queue.md).

## Key design patterns

**Store interfaces per service.** Each service declares the slice of persistence it needs as its
own interface (e.g. `AdminStore`, `InboxStore`, `UserStore`); the concrete `*store.Store` (over
Postgres) satisfies all of them. Handlers depend on the interface, so unit tests substitute a
mock — see each package's `testutil_test.go` and [testing.md](testing.md).

**Status only advances, never regresses.** Statuses are ranked
(`pending`/`failed` = 0, `sent` = 1, `delivered` = 2, `read` = 3, `archived` = 4; see
`internal/models/status.go`). Because events can arrive out of order, `worker-events` updates
status with a rank comparison in the SQL `WHERE` clause, so a late `sent` event can never pull a
notification back from `read`. `failed` shares rank 0 with `pending` — it's a terminal outcome,
not an advancement.

**Channel resolution order.** Dispatch resolves the delivery channels as: explicit channels on
the send request → the user's per-subscription preference → the subscription category's default
channels. For template-driven sends, the set is narrowed to channels the template actually
defines content for.

**Two auth modes.** API keys (HMAC-SHA256, see below) authenticate server-to-server traffic to
Send and Admin. JWTs (HMAC-signed, multi-key) authenticate user-facing traffic to Inbox and
User. `/healthz` and `/readyz` skip auth on every service.

**Idempotency.** Send dedupes on an idempotency key (per organization) so retried client requests don't
produce duplicate notifications; the same key is also enforced by a unique partial index on
`notifications` (see [data-model.md](data-model.md)).

**Caching.** Redis fronts hot, slow-changing reads — template/subscription config, API-key
lookups, JWT signing keys, and inbox unread counts — and backs Centrifugo's engine and the
idempotency dedup. Caches use short TTLs and fall back to Postgres on a miss.

## Authentication details

**The isolation boundary is the app, not the organization.** An API key authenticates the
*app* — the product integrating Hermes — and is deliberately **not** scoped to an
organization: one app sends on behalf of many organizations, and the same organization may
be served by more than one app, so a key scoped to one would break the core use case. There
is no `app` entity in the schema; one installation (one database) serves exactly one app,
and that deployment separation is the entire enforcement mechanism. Consequently the
`organization_id` on a send request and the `organization_id` JWT claim are routing and
partitioning labels, not authorization scopes. See
[ADR 0003](adr/0003-rename-tenant-to-organization.md).

**API keys** (`internal/auth/apikey.go`). Raw key format is
`hms_[<env>_]key_<id>_<secret>` (env prefix `stg`/`dev`, omitted in production). Only an
HMAC-SHA256 hash of the secret (keyed by `HERMES_API_KEY_HMAC_SECRET`) is stored, in
`api_keys.key_hash`; verification recomputes the HMAC and compares in constant time.

**JWTs** (`internal/auth/jwt.go`). Tokens are Hermes-issued and HMAC-signed. The middleware
accepts any of several active signing keys (`jwt_signing_keys`), so keys can be rotated without
downtime; non-HMAC algorithms are rejected. The `sub` claim is the internal user ID and an
`organization_id` claim accompanies the request. Backends obtain a token by exchanging a user
identifier via the Admin auth endpoint (see [Integration Guide](integration-guide.md)).

## IDs

IDs are generated by `internal/id/v2` (base62, lexicographically sortable). Two pre-configured
generators cover most entities:

- **Notification IDs** — 48-bit millisecond timestamp + 80-bit random → 22 chars, no prefix,
  time-sortable.
- **Prefixed IDs** — a type prefix plus random bits, e.g. `usr_…` (users), `key_…` (API keys).

Organizations are the exception: they use UUIDs. (The older `internal/id` Crockford-Base32 package is
superseded by `internal/id/v2`.)

## Infrastructure

- **Postgres** — single shared database; all services go through `internal/store`. Migrations in
  `migrations/` via golang-migrate.
- **NATS JetStream** — the four streams above.
- **Redis** — cache, idempotency dedup, and Centrifugo engine.
- **Centrifugo** — real-time WebSocket push on user-scoped channels; NATS broker, Redis engine.

## Where to go next

- [services.md](services.md) — exact ports, subjects, and endpoints per service.
- [data-model.md](data-model.md) — the schema and status model.
- [development.md](development.md) — run it all locally.
- [observability/architecture.md](observability/architecture.md) — telemetry topology.
