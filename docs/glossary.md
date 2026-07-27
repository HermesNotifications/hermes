# Glossary

Domain terms used throughout Hermes. See [data-model.md](data-model.md) for the underlying
tables and [architecture.md](architecture.md) for how they flow through the pipeline.

**App** — The product that integrates Hermes, and the **isolation and trust boundary**. An API key
authenticates the app, not any one organization. The app is not a table or a column: one Hermes
installation (one database) serves exactly one app, and that separation is what isolates apps from
each other. See [ADR 0003](adr/0003-rename-tenant-to-organization.md).

**Organization** — One customer of the app, on whose behalf notifications are sent. Identified by a
UUID. An organization is a routing and data-partitioning label, **not** a security boundary: a single
app sends for many organizations, and the same organization may be served by more than one app. API
keys are deliberately not scoped to an organization.

**User** — A notification recipient within an organization. Hermes stores its own user row keyed by
your application's identifier (see *external ID*), holding contact info (email, phone) and locale.

**External ID** — Your application's identifier for a user (`users.external_id`). Unique per
organization. You send notifications addressed by organization + external ID; Hermes maps it to its internal
user ID.

**Internal user ID** — Hermes's own `usr_…` ID for a user. It is the `sub` claim of issued JWTs
and the suffix of the user's real-time channel (`user#<internal-user-id>`).

**Subscription category** — A top-level grouping of notification preferences (e.g. *Account*,
*General*, *Marketing*), with default channels and a default state (`on`/`off`/`required`).

**Subscription** — A specific preference within a category. A user opts in/out per subscription;
absence of an explicit choice falls back to the category's default state.

**Template** — Reusable, per-channel content (email subject/body, SMS body, inbox title/body)
referenced by slug when sending. A send may instead provide direct content with no template.

**Channel** — A delivery medium: `email`, `sms`, or `inbox`. The set of channels for a given
notification is resolved as: explicit request channels → user preference → category default.

**Notification** — A single message to a single user. It carries content, a channel set, and a
`status` that advances through its lifecycle.

**Status** — The rolled-up lifecycle state of a notification: `pending` → `sent` → `delivered` →
`read` → `archived` (or terminal `failed`). It only ever advances. See
[data-model.md](data-model.md#notification-status-model).

**Event** — An immutable record of something that happened to a notification on a channel
(e.g. `email.sent`, `sms.failed`), with a severity and metadata. Events are the source of truth;
status is derived from them by `worker-events`.

**Delivery** — The act of a worker handing a notification to a channel provider (SMTP/SES,
SMS webhook, Centrifugo push) and emitting the resulting event.

**Idempotency key** — A caller-supplied key that lets a send be retried safely; Hermes dedupes on
`(organization, idempotency_key)` so retries don't create duplicate notifications.

**API key** — A server-to-server credential (`hms_…`) for the Send and Admin APIs. Only an
HMAC-SHA256 hash is stored.

**JWT** — A short-lived, HMAC-signed token identifying a user; used for the read-path (Inbox,
User) APIs and the Centrifugo WebSocket connection. Backends obtain one via token exchange on the
Admin API.

**Dispatch** — The service that turns an ingested send into per-channel delivery messages
(template + channel resolution, fan-out).

**Centrifugo** — The real-time server that pushes inbox notifications to connected clients over
WebSockets on user-scoped channels.

**LGTM stack** — Loki (logs), Grafana (dashboards), Tempo (traces), (Prometheus) (metrics) — the
in-cluster observability stack. See [observability/](observability/README.md).
