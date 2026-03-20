# Hermes — Notification Service Design

An internal notification platform for a SaaS provider. Hermes provides a single API for sending notifications across channels (email, SMS, inbox) with a persistent inbox, real-time push via Centrifugo, and multi-tenant support.

## Goals and Constraints

- **Internal service** for a SaaS provider to centralize notification delivery
- **Multi-tenant**: users belong to one tenant, single inbox across all apps within that tenant
- **Cloud-agnostic**: deploy on any Kubernetes cluster, no cloud-provider lock-in
- **Scale target**: 1–10M users initially, architecture must not preclude 100M+
- **Low hosting cost**: minimize infrastructure dependencies, right-size from the start

## Architecture Overview

Event-driven microservice pipeline. All inter-service communication flows through NATS JetStream.

```
SaaS Backend (API Key) ──► Send Service ──► NATS [notification.send]
                                                    │
                                              ┌─────▼─────┐
                                              │   Router   │
                                              └─────┬─────┘
                                    ┌───────────────┼───────────────┐
                                    ▼               ▼               ▼
                            NATS [delivery.   NATS [delivery.  NATS [delivery.
                              email]            sms]            inbox]
                                    │               │               │
                                    ▼               ▼               ▼
                              Email Worker    SMS Worker     Inbox Worker
                                    │               │               │
                                    ▼               ▼               ▼
                              SendGrid/SES    Twilio/etc.    Postgres +
                                                             Centrifugo push

All services ──► NATS [notification.events] ──► Event Writer ──► Postgres

User Apps (JWT) ──► Inbox Service (REST) ──► Postgres
                ──► Centrifugo (WebSocket) ──► Real-time push
```

## Infrastructure Stack

| Component   | Role                                        |
|-------------|---------------------------------------------|
| PostgreSQL  | Durable storage — notifications, users, templates, preferences, event log |
| NATS JetStream | Inter-service messaging, Centrifugo broker |
| Redis       | Centrifugo engine (presence, history), type config cache, idempotency key dedup |
| Centrifugo  | Real-time WebSocket push to clients. NATS broker, Redis engine |

## Services

Eight services in a Go monorepo:

| Service       | Responsibility |
|---------------|---------------|
| **Send**      | REST API for sending notifications. Validates, persists, publishes to NATS. Hosts admin endpoints (types, groups, notification status) |
| **Router**    | Subscribes to `notification.send`. Resolves templates, determines channels (group defaults + user preference overrides), fans out to `delivery.{channel}` |
| **Email Worker** | Subscribes to `delivery.email`. Delivers via pluggable provider adapter (SendGrid, SES, webhook) |
| **SMS Worker** | Subscribes to `delivery.sms`. Delivers via pluggable provider adapter (Twilio, webhook) |
| **Inbox Worker** | Subscribes to `delivery.inbox`. Persists inbox state to Postgres, publishes to Centrifugo for real-time push |
| **Event Writer** | Subscribes to `notification.events`. Batch-writes event log entries to Postgres, updates top-level notification status |
| **Inbox Service** | REST API for reading inbox (paginated list, read/unread, archive, delete). JWT auth, user-facing |
| **User Service** | REST API for user profiles, contact info, and notification preferences. JWT auth for self-service, API key for admin |

## Data Model

### IDs

All public-facing IDs use **Crockford Base32** encoding of a time-sortable binary value:
- 48 bits millisecond timestamp + 80 bits randomness
- 26 characters, lexicographically sortable by creation time
- URL-safe, case-insensitive, no coordination needed
- Stored as `text` in Postgres

### Tables

**tenants**

| Column | Type | Notes |
|--------|------|-------|
| id | uuid (PK) | UUIDv4 — tenants are few, no need for sortable IDs |
| name | text | |
| default_locale | text | For future i18n support |
| settings | jsonb | Tenant-level config |
| created_at | timestamptz | |

**api_keys**

| Column | Type | Notes |
|--------|------|-------|
| id | text (PK) | Crockford Base32 |
| key_hash | text | argon2 hash |
| name | text | Human-readable label |
| created_at | timestamptz | |

- API keys are global — a single key can send across tenants. Keys are not scoped to a tenant.
- Unique index on key_hash
- Cached in Redis

**users**

| Column | Type | Notes |
|--------|------|-------|
| id | text (PK) | Crockford Base32 |
| tenant_id | text (FK) | |
| external_id | text | SaaS provider's canonical user ID |
| email | text | Nullable — enriched later |
| phone | text | Nullable — enriched later |
| locale | text | Nullable — defaults to tenant locale. For future i18n |
| created_at | timestamptz | |

- Unique constraint: `(tenant_id, external_id)` - indexed
- Auto-created on first send if not exists using `INSERT ... ON CONFLICT (tenant_id, external_id) DO NOTHING` followed by select

**notification_groups**

Global table — defined by the SaaS provider, shared across all tenants. Groups organize notification types for preference management.

| Column | Type | Notes |
|--------|------|-------|
| id | text (PK) | Crockford Base32 |
| slug | text | e.g., `billing`, `security`, `marketing` |
| name | text | Human-readable name |
| default_channels | text[] | e.g., `{"email", "inbox"}` |
| created_at | timestamptz | |

- Unique constraint: `slug`

**notification_types**

Global table — defined by the SaaS provider, shared across all tenants. All tenants receive the same notification catalog; only user preferences differ.

| Column | Type | Notes |
|--------|------|-------|
| id | text (PK) | Crockford Base32 |
| group_id | text (FK) | |
| slug | text | e.g., `invoice.paid` |
| name | text | Human-readable name |
| email_subject | text | Go text/template. Nullable — null means channel not available for this type |
| email_body | text | Go html/template (auto-escapes HTML). Nullable |
| sms_body | text | Go text/template. Nullable |
| inbox_title | text | Go text/template. Nullable |
| inbox_body | text | Go text/template. Nullable |
| created_at | timestamptz | |

- Unique constraint: `slug`

**notifications**

| Column | Type | Notes |
|--------|------|-------|
| id | text (PK) | Crockford Base32, time-sortable |
| tenant_id | text (FK) | |
| user_id | text (FK) | |
| type_id | text (FK) | Nullable — null for direct sends |
| group_id | text (FK) | |
| title | text | Resolved title |
| body | text | Resolved body |
| action_url | text | Nullable |
| action_label | text | Nullable — button text |
| idempotency_key | text | Nullable. Also stored in Redis for fast duplicate detection (see below) |
| channels | text[] | Channels this was routed to |
| status | text | See status rollup rules below |
| created_at | timestamptz | |
| sent_at | timestamptz | Nullable |
| delivered_at | timestamptz | Nullable |
| read_at | timestamptz | Nullable |
| archived_at | timestamptz | Nullable |
| deleted_at | timestamptz | Nullable — soft delete |

**Notification status rollup rules:**

Status progression: `pending` → `sent` → `delivered` → `read` → `archived`

- `pending`: notification created, not yet processed by Router
- `sent`: at least one channel delivery has been attempted (Router published to delivery subjects)
- `delivered`: at least one channel has confirmed delivery (inbox persisted, email accepted by provider)
- `read`: user has read the inbox item. This is inbox-specific — `read_at` is set when the user marks the inbox notification as read
- `archived`: user has archived the inbox item

The Event Writer applies the rollup: status advances to the highest level achieved by any channel. It never regresses (e.g., an `email.failed` event does not move status back from `delivered` if inbox already succeeded).

**Out-of-order safety:** Events may arrive out of sequence (retries, concurrent workers). The Event Writer uses conditional updates to prevent regressions:

```sql
UPDATE notifications
SET status = $new_status,
    sent_at = COALESCE(sent_at, CASE WHEN $new_status_rank >= 1 THEN $event_time END),
    delivered_at = COALESCE(delivered_at, CASE WHEN $new_status_rank >= 2 THEN $event_time END)
WHERE id = $1
  AND status_rank(status) < status_rank($new_status)
```

Status rank mapping: `pending=0, sent=1, delivered=2, read=3, archived=4`. Implemented as a Postgres function or application-side logic. `COALESCE` ensures timestamps are only set once (first writer wins). The `WHERE` clause ensures a stale event cannot regress the status. If the update affects 0 rows, the event is silently skipped — it's already been superseded.

- Partial index for default inbox query:
  ```sql
  CREATE INDEX idx_notifications_inbox
    ON notifications (user_id, created_at DESC)
    WHERE archived_at IS NULL AND deleted_at IS NULL;
  ```
- Standard index `(user_id, created_at DESC)` for all-notifications view (add when needed)

**notification_events**

| Column | Type | Notes |
|--------|------|-------|
| id | text (PK) | Crockford Base32 |
| notification_id | text (FK) | |
| channel | text | email, sms, inbox |
| event | text | e.g., `email.routed`, `email.sent`, `email.failed`, `inbox.delivered`, `inbox.read` |
| severity | text | `info` (normal progression), `warn` (retriable issue), `error` (delivery failure) |
| metadata | jsonb | Provider response, error details |
| created_at | timestamptz | |

**user_preferences**

| Column | Type | Notes |
|--------|------|-------|
| user_id | text (FK) | |
| group_id | text (FK) | |
| channels | text[] | Overrides group defaults. Null means use defaults |

- Primary key: `(user_id, group_id)`

## API Design

### Authentication

- **Send API + Admin API**: API key in `Authorization` header. Keys are global and can send across tenants (see `api_keys` table).
- **Inbox + User API**: JWT signed by the SaaS provider. Claims include `sub` (Hermes user ID or external ID), `tenant_id`. Hermes validates the signature against a configured public key / JWKS endpoint.

### Send API (API Key auth)

```
POST /v1/send
```

Request body:
```json
{
  "tenant_id": "01HQJK5M3N8P2R4V6W8X0Y1Z34",
  "user_id": "ext_user_456",
  "type": "invoice.paid",
  "content": {
    "title": "...",
    "body": "...",
    "action_url": "...",
    "action_label": "..."
  },
  "data": { "invoice_number": "1234", "amount": "$99.00" },
  "channels": ["email", "inbox"],
  "group": "billing"
}
```

- Exactly one of `type` or `content` must be present
- `data` provides template variables when using `type`
- `channels` is optional — overrides group defaults + user preferences
- `group` is required for direct sends, inferred from type otherwise
- `user_id` is the external ID — user is auto-created if not exists
- `X-Idempotency-Key` header is optional — if provided, the Send Service checks Redis for a duplicate before creating a new one. Duplicate requests return the original `notification_id`.
  - Redis key: `idem:{tenant_id}:{idempotency_key}` → `notification_id`, with 24h TTL (auto-expires, no cleanup job needed)
  - On send: `SET idem:{tenant_id}:{key} {notification_id} NX EX 86400` — if key already exists, return the stored notification_id
  - The idempotency_key is also persisted on the notifications row for auditability, but Redis is the primary lookup path

**Validation:**
- Exactly one of `type` or `content` must be present — return `400` if both or neither
- `type` must reference an existing `notification_types.slug` — return `400` if not found
- `group` must reference an existing `notification_groups.slug` — return `400` if not found
- `group` is required for direct sends, inferred from type otherwise

**Rate limiting:** Per-API-key token bucket. Default: 1000 req/s burst, 500 req/s sustained. Configurable. Returns `429 Too Many Requests` when exceeded.

Response: `202 Accepted`
```json
{
  "notification_id": "01HQJK5M3N8P2R4V6W8X0Y1Z34"
}
```

### Inbox API (JWT auth)

Tenant and user ID derived from JWT claims.

```
GET    /v1/inbox                        # Paginated list (default: non-archived, non-deleted)
GET    /v1/inbox?archived=true          # Archived items
PUT    /v1/inbox/:id/read               # Mark read
DELETE /v1/inbox/:id/read               # Mark unread
PUT    /v1/inbox/:id/archive            # Archive
DELETE /v1/inbox/:id/archive            # Unarchive
DELETE /v1/inbox/:id                    # Soft delete
PUT    /v1/inbox/read-all               # Mark all as read
```

Paginated response (cursor-based using `created_at` + `id`):
```json
{
  "data": [
    {
      "id": "01HQJK5M3N8P2R4V6W8X0Y1Z34",
      "title": "Invoice Paid",
      "body": "Your invoice #1234 has been paid.",
      "action": {
        "url": "https://app.example.com/invoices/1234",
        "label": "View Invoice"
      },
      "group": "billing",
      "read": false,
      "created_at": "2026-03-20T12:00:00Z"
    }
  ],
  "unread_count": 12,
  "cursor": "..."
}
```

### User / Preferences API (JWT auth for self-service, API Key for admin)

```
GET    /v1/users/me                        # Current user profile
PUT    /v1/users/me/contacts               # Update email, phone
GET    /v1/users/me/preferences            # Notification preferences
PUT    /v1/users/me/preferences/:group_id  # Override channels for a group
DELETE /v1/users/me/preferences/:group_id  # Revert to group defaults
```

### Admin / Config API (API Key auth)

```
GET    /v1/types
POST   /v1/types
PUT    /v1/types/:id
DELETE /v1/types/:id

GET    /v1/groups
POST   /v1/groups
PUT    /v1/groups/:id

GET    /v1/notifications/:id               # Status + event log
```

## Event Flow

### Send Pipeline

1. **Send Service** receives `POST /v1/send`, validates, auto-creates user if needed, persists notification to Postgres with `status: pending`, publishes to `notification.send` on NATS, returns `202 Accepted`
2. **Router** subscribes to `notification.send` (consumer group). Resolves templates (type config cached in Redis, key: `type:{slug}`, TTL: 5 min, invalidated on type update). Determines channels: explicit override → user preferences → group defaults. Publishes to `delivery.{channel}` per channel. Publishes routing events to `notification.events`
3. **Delivery Workers** subscribe to their channel subject. Deliver via provider adapter. Publish result events to `notification.events`
4. **Event Writer** subscribes to `notification.events`. Batch-inserts to notification_events table (batch size: 100 events or 500ms flush interval, whichever comes first). Updates top-level notification status and timestamps using the rollup rules

### NATS Message Schema

All delivery subjects use a common envelope:

```json
{
  "notification_id": "01HQJK5M3N8P2R4V6W8X0Y1Z34",
  "tenant_id": "01HQJK5M3N8P2R4V6W8X0Y1Z34",
  "user_id": "01HQJK5M3N8P2R4V6W8X0Y1Z34",
  "channel": "email",
  "content": {
    "title": "Invoice Paid",
    "body": "Your invoice #1234 has been paid.",
    "action_url": "https://app.example.com/invoices/1234",
    "action_label": "View Invoice"
  },
  "metadata": {
    "group": "billing",
    "type": "invoice.paid"
  },
  "attempt": 1,
  "correlation_id": "req_abc123"
}
```

`correlation_id` flows from the original API request through every NATS message and event log entry for end-to-end tracing.

### Failure Handling

- NATS JetStream provides at-least-once delivery — messages redeliver if not acked
- Workers ack only after successful delivery + event publication
- Configurable max retry count per subject, then dead-letter subject for manual inspection
- Partial failure: if email fails but inbox succeeds, event log reflects both, top-level status reflects best outcome (see status rollup rules in Data Model)

### NATS Stream Configuration

| Stream | Subjects | Retention | Storage | Max Age |
|--------|----------|-----------|---------|---------|
| NOTIFICATIONS | `notification.send` | WorkQueue | File | 7 days |
| DELIVERY | `delivery.email`, `delivery.sms`, `delivery.inbox` | WorkQueue | File | 7 days |
| EVENTS | `notification.events` | WorkQueue | File | 7 days |

- WorkQueue retention: messages are removed once acked by a consumer, ensuring each message is processed exactly once per consumer group
- File storage for durability across NATS restarts
- Max age is a safety net — messages should be consumed well before 7 days

### Health Checks

All services expose:
- `GET /healthz` — liveness probe (process is running)
- `GET /readyz` — readiness probe (dependencies are reachable: Postgres, NATS, Redis as applicable)

## Centrifugo Integration

### Channel Naming

Each user has a user-limited channel: `user#<user_id>`. The `#` separator creates a user-limited channel — Centrifugo automatically restricts subscription to the user whose ID matches the suffix in the connection token. No separate subscription token is needed. This channel does not use a Centrifugo namespace — it operates in the default namespace with presence and history configured at the server level.

### Connection Flow

1. Client authenticates with JWT to Inbox Service
2. Inbox Service returns a Centrifugo connection token (short-lived JWT signed with Centrifugo's secret, contains user ID and allowed channels)
3. Client connects to Centrifugo WebSocket with this token
4. Client subscribes to `user#<user_id>`
5. Token refresh: Centrifugo calls back to Inbox Service when token nears expiry. TTL: 5–10 minutes

### Push Payload

New notification:
```json
{
  "id": "01HQJK5M3N8P2R4V6W8X0Y1Z34",
  "title": "Invoice Paid",
  "body": "Your invoice #1234 has been paid.",
  "action": {
    "url": "https://app.example.com/invoices/1234",
    "label": "View Invoice"
  },
  "group": "billing",
  "created_at": "2026-03-20T12:00:00Z"
}
```

Control event (for cross-device sync):
```json
{
  "type": "inbox.updated",
  "notification_id": "01HQJK5M3N8P2R4V6W8X0Y1Z34",
  "action": "read"
}
```

### Centrifugo Configuration

```json
{
  "engine": "redis",
  "broker": "nats",
  "token_hmac_secret_key": "...",
  "api_key": "...",
  "presence": true,
  "history_size": 50,
  "history_ttl": "24h",
  "user_subscribe_to_personal": true,
  "allow_user_limited_channels": true
}
```

Channel history (Redis-backed) provides a reconnection buffer — briefly disconnected clients get missed messages replayed without hitting Postgres.

### Publishing

Inbox Worker uses Centrifugo's server-side HTTP publish API (not direct NATS bridging) for full control over payload formatting.

## Delivery Adapters

Each channel worker uses a pluggable provider interface:

```go
type DeliveryProvider interface {
    Send(ctx context.Context, req DeliveryRequest) (DeliveryResult, error)
    Name() string
}
```

Configured per-tenant or globally via environment/config. Built-in adapters to ship:

| Channel | Adapters |
|---------|----------|
| Email   | SendGrid, SES, Webhook |
| SMS     | Twilio, Webhook |
| Inbox   | Internal (Postgres + Centrifugo) |

The Webhook adapter is the escape hatch — the SaaS provider can route to any provider by implementing a simple HTTP endpoint.

## Templates

- Templates stored in `notification_types` table, per-channel fields
- Engine: Go `html/template` for email bodies (auto-escapes HTML to prevent XSS), Go `text/template` for SMS and inbox. Restricted function set (no shell, no file access)
- Template variables come from the `data` field in the send request
- Cached in Redis by the Router. Key: `type:{slug}`, TTL: 5 min, invalidated on type update via admin API

**Future i18n path:**
- Add `notification_type_translations` table with `locale` column
- Router resolves: user locale → tenant default locale → fallback
- No schema changes to the notifications table
- `action_label` on direct sends is caller-provided — SaaS provider handles their own i18n

## Monorepo Structure

```
hermes/
├── cmd/
│   ├── send/
│   ├── router/
│   ├── worker-email/
│   ├── worker-sms/
│   ├── worker-inbox/
│   ├── worker-events/
│   ├── inbox/
│   └── user/
├── internal/
│   ├── config/          # Config loading (env/file)
│   ├── database/        # Postgres connection, migrations
│   ├── messaging/       # NATS client wrapper
│   ├── cache/           # Redis client wrapper
│   ├── auth/            # API key + JWT validation
│   ├── models/          # Shared domain types
│   └── centrifugo/      # Centrifugo publish client
├── pkg/
│   └── hermesv1/        # API types (could become a Go client SDK)
├── migrations/          # SQL migrations (shared database)
├── deploy/
│   ├── k8s/             # Kubernetes manifests
│   └── docker/          # Dockerfiles (one per service)
├── api/                 # OpenAPI specs
├── docs/
├── go.mod
└── go.sum
```

- Single Go module, all services share one `go.mod`
- Single Postgres database, services own their tables but share access
- One Dockerfile per service, multi-stage builds for minimal images
- Migrations: use golang-migrate, run as a Kubernetes Job before service deployments. All migrations must be backward-compatible to support rolling deploys

## Kubernetes Deployment

### Single namespace: `hermes`

| Service | Min replicas | Scaling trigger | Notes |
|---------|-------------|----------------|-------|
| Send | 2 | Request rate (HPA) | Stateless — validate and publish |
| Router | 2 | NATS consumer lag (KEDA) | CPU-bound (template rendering) |
| Email Worker | 2 | NATS consumer lag (KEDA) | Rate-limit to provider quotas |
| SMS Worker | 2 | NATS consumer lag (KEDA) | Rate-limit to provider quotas |
| Inbox Worker | 2 | NATS consumer lag (KEDA) | Postgres write + Centrifugo publish |
| Event Writer | 2 | NATS consumer lag (KEDA) | Batch inserts |
| Inbox Service | 2 | Request rate (HPA) | Read-heavy |
| User Service | 1 | Request rate (HPA) | Low traffic |
| Centrifugo | 2 | WebSocket connections | Redis engine enables multi-replica |
| PostgreSQL | 1 | Managed | Managed service preferred (RDS, CloudSQL, etc.) |
| Redis | 1 | - | Centrifugo engine only |
| NATS | 3 | - | JetStream cluster, 3-node quorum |

### Load Balancing

- **Nginx Ingress Controller** for cloud-agnostic L7 routing, TLS termination, and WebSocket upgrade support
- Exposed via `LoadBalancer` type Service, which provisions the cloud provider's native L4 load balancer automatically
- Path-based routing:
  - `/v1/send`, `/v1/types`, `/v1/groups`, `/v1/notifications` → Send service
  - `/v1/inbox` → Inbox Service
  - `/v1/users` → User Service
  - `/centrifugo` → Centrifugo (WebSocket)
- **Cloud-native pivot**: swap nginx ingress controller for AWS ALB Controller / GKE Ingress / Azure AGIC. Ingress resource definitions remain the same.

### Resource Strategy

- Start small: 100–200m CPU, 128–256Mi memory per service
- NATS consumers scale horizontally via consumer groups
- HPA for request-facing services (Send, Inbox Service, User Service)
- KEDA for NATS consumer services (Router, workers, Event Writer)

### Observability

- Structured JSON logging from all services with `correlation_id`
- Prometheus metrics via standard Go client (request latency, NATS consumer lag, delivery success/failure rates)
- Correlation IDs in logs for end-to-end tracing without heavy tracing infrastructure
