# Services Reference

Hermes is a monorepo. Each service has a `main.go` under `cmd/<name>/` and most of its logic in
a matching `internal/<name>/` package. Shared concerns (store, auth, messaging, config,
observability) live in `internal/`. This page is the per-service reference; for how they
interact see [architecture.md](architecture.md).

The exact HTTP endpoints for the public APIs are defined by the generated OpenAPI specs in
`api/` — treat those as authoritative and regenerate with `make openapi`. See
[api/README.md](api/README.md).

## Long-running services

### Send — `cmd/send` (port 8088, API-key auth)
Thin ingestion layer. Exposes `POST /v1/send`, authenticates the API key, applies idempotency,
and publishes a `SendMessage` to `notification.send`. Deliberately does **no** template or
channel resolution and minimal DB work — its job is to get the request onto NATS fast.
- **Produces:** `notification.send`
- **Depends on:** Postgres (API-key lookup), Redis (idempotency + key cache), NATS

### Admin — `cmd/admin` (port 8080, API-key auth)
Server-to-server management API and JWT issuer. Manages organizations, API keys, subscription
categories, subscriptions, notification templates, and users; exchanges a user identifier for a
Hermes JWT used by the read-path APIs and Centrifugo. Spec: `api/admin/openapi.yaml`.
- **Depends on:** Postgres, Redis

### Dispatch — `cmd/dispatch` (port 8081, internal)
Consumes `notification.send`. Ensures the organization/user exist, persists the notification record
(status `pending`), resolves the template and channel set (see channel-resolution order in
[architecture.md](architecture.md)), and fans out a `DeliveryMessage` per channel.
- **Consumes:** `notification.send` · **Produces:** `delivery.*`, `notification.events` (failures)
- **Depends on:** Postgres, Redis (template/channel cache), NATS

### Workers — `cmd/worker-email` (8083), `cmd/worker-sms` (8084), `cmd/worker-inbox` (8085)
Each consumes its `delivery.<channel>` subject, performs the delivery, and emits an
`EventMessage`:
- **worker-email** — SMTP or AWS SES (`internal/email`), HTML layout templating.
- **worker-sms** — POSTs to a configured SMS webhook.
- **worker-inbox** — pushes to Centrifugo on the user's channel for real-time inbox updates.
- **Consumes:** `delivery.{email,sms,inbox}` · **Produces:** `notification.events`

### Event Writer — `cmd/worker-events` (port 8082, internal)
Consumes `notification.events`, batch-inserts into `notification_events`, and advances the
notification `status` using a rank-guarded conditional update so out-of-order events can't
regress status (`internal/eventwriter`).
- **Consumes:** `notification.events` · **Depends on:** Postgres, NATS

### Inbox — `cmd/inbox` (port 8086, JWT auth)
User-facing inbox API: cursor-paginated list plus per-notification actions (read, unread,
archive, unarchive, delete) and mark-all-read; serves the unread count (Redis-cached).
Spec: `api/inbox/openapi.yaml`.
- **Depends on:** Postgres, Redis, Centrifugo. It holds **no NATS client** — it reads the store
  and pushes state changes straight to Centrifugo's HTTP API.

### User — `cmd/user` (port 8087, JWT auth)
User-facing profile and preferences API: read/update profile (email, phone, locale) and manage
per-subscription opt-in preferences. Spec: `api/user/openapi.yaml`.
- **Depends on:** Postgres, Redis

All services expose unauthenticated `GET /healthz` and `GET /readyz` probes.

`/readyz` reports on the dependencies a service cannot serve without; `/healthz` reports on state
only a restart can fix. For the five services that consume from NATS — dispatch, the three
delivery workers, the event writer — `/healthz` is therefore **not** a constant 200: it fails when
that pod's consumer has had work waiting and settled none of it for
`HERMES_NATS_CONSUMER_STALL_TIMEOUT` (10m), which restarts the container. See
[ADR 0022](adr/0022-liveness-follows-consumer-progress.md).

## One-shot / CLI tools

| Tool | Path | Purpose | Typical invocation |
|---|---|---|---|
| `migrate` | `cmd/migrate` | Run golang-migrate against Postgres | `make migrate` (k8s Job in prod) |
| `natsprovision` | `cmd/natsprovision` | Declare the JetStream streams, then exit. The only identity holding `STREAM.CREATE`/`UPDATE`; services only verify (see [architecture.md](architecture.md#streams-are-provisioned-not-self-declared)) | k8s Job in prod |
| `seed` | `cmd/seed` | Insert a dev API key | `make seed` |
| `cleanup` | `cmd/cleanup` | Delete `notification_events` older than the retention window | `make cleanup` (k8s CronJob in prod) |
| `loadseed` | `cmd/loadseed` | Generate the load-test dataset (organizations/users) | `make loadseed` |
| `openapi` | `cmd/openapi` | Emit OpenAPI 3.1 specs from the huma API definitions | `make openapi` |
| `dispatchbench` | `cmd/dispatchbench` | Sweep dispatch worker concurrency and prefetch against a real backend | `make dispatchbench` |
| `hermes` | `cmd/hermes` | Admin CLI + interactive inbox TUI | see [cli.md](cli.md) |

## Shared internal packages

| Package | Role |
|---|---|
| `internal/store`, `internal/store/postgres` | Repository interfaces and the Postgres implementation all services use |
| `internal/store/cached` | Redis-fronted repository decorators (e.g. organizations) |
| `internal/store/dynamo` | Optional DynamoDB-model store for the hot notification and event path, enabled by `HERMES_DYNAMO_ENDPOINT`. Delegates to the Postgres store, so Postgres stays required — see [architecture.md](architecture.md#the-dual-store) and [ADR 0001](adr/0001-dynamodb-model-via-extenddb.md) |
| `internal/provider` | Channel/provider registry (per [ADR 0002](adr/0002-provider-plugin-model-bus-native-isolation.md)). `Builtins` is constructed at package-init and read-only thereafter; delivery subjects remain per-channel, not per-provider |
| `internal/models` | Shared domain types and the notification status model (`status.go`) |
| `internal/nats` (`hermenats`) | NATS message contracts (`SendMessage`, `DeliveryMessage`, `EventMessage`, `DeadLetter`) |
| `internal/messaging` | NATS JetStream client: connect (TLS + NKey identity), stream declaration/verification, the consumer pool, and the retry/dead-letter ladder |
| `internal/auth` | API-key (HMAC-SHA256) with permissions, and multi-key JWT validation |
| `internal/config` | `HERMES_*` environment configuration (see [configuration.md](configuration.md)) |
| `internal/cache` | Redis client + caching helpers |
| `internal/centrifugo` | Centrifugo HTTP API client for real-time push |
| `internal/email` | SMTP/SES email provider + templating |
| `internal/id`, `internal/id/v2` | ID generation (`v2` is current; base62, sortable) |
| `internal/bootstrap` | Startup helpers (logger, DB/NATS/Redis connect, stream setup, serve) |
| `internal/middleware`, `internal/httputil` | HTTP middleware and health handlers |
| `internal/observability` | OpenTelemetry init + trace-aware slog handler |
| `internal/dispatch`, `internal/send`, `internal/admin`, `internal/inbox`, `internal/userservice`, `internal/delivery`, `internal/eventwriter` | Per-service business logic |
| `internal/cli`, `pkg/client` | CLI command tree and the Go API client it uses |
