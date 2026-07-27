# Data Model

All services share one Postgres database, accessed through `internal/store`. The schema is
managed with [golang-migrate](https://github.com/golang-migrate/migrate): numbered pairs in
`migrations/` (`NNNNNN_<name>.up.sql` / `.down.sql`), applied with `make migrate`.

> This page describes the **current** schema (after all migrations). Some tables were renamed
> along the way — notably the old `notification_groups`/`notification_types`/`user_preferences`
> were replaced by `subscription_categories`/`subscriptions`/`user_subscriptions` plus
> `notification_templates` in migration `000011`. The current names are what the code uses.

## Entities

```
organizations ──< users ──< user_subscriptions >── subscriptions >── subscription_categories
   │                                              │                      │
   └──< notifications >── notification_templates ─┘                      │
              │           (template_id, nullable)                        │
              │                                                          │
              ├── category_id (nullable) ─────────────────────────────── ┘
              │
              └──< notification_events

api_keys          (standalone)
jwt_signing_keys  (standalone)
```

### `organizations`
The isolation boundary. **UUID** primary key (the one entity not using a base62 ID).
Columns: `id`, `name`, `default_locale` (default `en`), `settings` (JSONB), `created_at`.

### `users`
A recipient within an organization. `id` (base62, `usr_…`), `organization_id` → `organizations`, `external_id`
(your application's user identifier), `email`, `phone`, `locale`, `created_at`.
Unique on `(organization_id, external_id)` — each external user maps to exactly one Hermes user
per organization.

### `subscription_categories`
Top-level grouping of notification preferences (e.g. *Account*, *General*, *Marketing*).
`id`, `slug` (unique), `name`, `default_channels` (text[]), `default_state`
(`on` / `off` / `required`), `sort_order`, `created_at`. Three categories are seeded by default.

### `subscriptions`
A specific preference within a category. `id`, `category_id` → `subscription_categories`,
`slug`, `name`, `sort_order`, `created_at`. Unique on `(category_id, slug)`.

### `notification_templates`
Reusable per-channel content. `id`, `subscription_id` → `subscriptions` (nullable), `slug`
(unique), `name`, `default_channels` (text[]), and the channel bodies: `email_subject`,
`email_body`, `sms_body`, `inbox_title`, `inbox_body`, `created_at`.

### `user_subscriptions`
A user's opt-in/out for a subscription. Composite PK `(user_id, subscription_id)`, plus
`opted_in` (bool) and `created_at`. Absence means "fall back to the category default state."

### `notifications`
One notification to one user. `id` (base62, time-sortable), `organization_id` → `organizations`,
`user_id` → `users`, `template_id` → `notification_templates` (nullable, for direct-content
sends), `category_id` → `subscription_categories` (nullable), `title`, `body`, `action_url`,
`action_label`, `idempotency_key`, `channels` (text[]), `status` (default `pending`), and the
lifecycle timestamps `created_at`, `sent_at`, `delivered_at`, `read_at`, `archived_at`,
`deleted_at`.
- **Inbox index:** `(user_id, created_at DESC)` partial, `WHERE archived_at IS NULL AND
  deleted_at IS NULL` — backs the cursor-paginated inbox.
- **Idempotency index:** unique `(organization_id, idempotency_key)` partial,
  `WHERE idempotency_key IS NOT NULL` — enforces dedup at the database.

### `notification_events`
Append-only delivery log. `id`, `notification_id`, `channel`, `event` (e.g. `email.sent`,
`sms.failed`), `severity` (`info`/`warning`/`error`), `metadata` (JSONB), `created_at`.
Indexed by `(notification_id, created_at)` to render a timeline. (The FK back to
`notifications` was intentionally dropped so events can be written/retained independently.)

### `api_keys`
`id` (the `key_…` ID embedded in the raw key), `key_hash` (HMAC-SHA256, unique), `name`,
`permissions` (text[]), `created_at`. Only the hash is stored — see
[architecture.md](architecture.md#authentication-details).

### `jwt_signing_keys`
Accepted JWT signing keys (multiple may be active for rotation). `id`, `name`, `algorithm`
(default `HS256`), `secret`, `user_id_claim` (default `sub`), `organization_id_claim` (default
`organization_id`), `active`, `created_at`.

> Migration `000012` also creates Better Auth tables used by the [admin portal](../web/admin/README.md).

## Notification status model

Status **only advances, never regresses** (`internal/models/status.go`):

| Status | Rank | Meaning |
|---|---|---|
| `pending` | 0 | Created, not yet delivered |
| `failed` | 0 | Terminal failure (same rank as pending — not an advancement) |
| `sent` | 1 | Handed to the channel provider |
| `delivered` | 2 | Confirmed delivered |
| `read` | 3 | User opened it |
| `archived` | 4 | User archived it |

`worker-events` updates `status` with a rank comparison in the SQL `WHERE` clause, so events
that arrive out of order can never move a notification backward.

## Retention

`notification_events` grows unboundedly, so `cmd/cleanup` deletes events older than
`HERMES_EVENT_RETENTION_DAYS` (default 90). Run it with `make cleanup`; in production it runs as
a Kubernetes CronJob (see [self-hosting/configuration.md](self-hosting/configuration.md)).
