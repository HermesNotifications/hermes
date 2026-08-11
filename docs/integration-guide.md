# Hermes Integration Guide

Hermes is a multi-channel notification service. It accepts notification requests via a server-side Admin API, routes them through configurable channels (email, SMS, in-app inbox, webhooks), and exposes user-facing APIs for inbox management and notification preferences.

This guide walks through integrating Hermes into your SaaS application.

## Architecture Overview

Hermes runs as a set of microservices:

| Service | Port (default) | Purpose |
|---------|---------------|---------|
| **Send** | 8088 | Server-side API: `POST /v1/send` (thin ingestion layer) |
| **Admin** | 8080 | Server-side API: manage categories/subscriptions/templates, issue JWT tokens |
| **Inbox** | 8086 | User-facing API: list/read/archive inbox notifications |
| **User** | 8087 | User-facing API: profile management, notification preferences |
| **Dispatch** | 8081 | Internal worker: resolves templates/channels, routes to delivery |
| **Workers** | 8082–8085 | Internal workers: email, SMS, inbox delivery, event writing |

**Admin API** is authenticated with API keys (server-to-server).
**Inbox and User APIs** are authenticated with JWTs (user-facing).

## Authentication

Your backend calls the Admin API to exchange a user identifier for a Hermes JWT. This token is used for all user-facing APIs and for connecting to Centrifugo (real-time WebSocket push).

```
Your Backend                     Hermes Admin API
    |                                  |
    |  POST /v1/auth/token             |
    |  { user_id, organization_id }          |
    |--------------------------------->|
    |                                  |
    |  { token, expires_at }           |
    |<---------------------------------|
    |                                  |
    |  Return token to frontend        |
    |                                  |
```

**Request:**

```bash
curl -X POST https://hermes.example.com/admin/v1/auth/token \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "user-ext-123",
    "organization_id": "organization-abc"
  }'
```

**Response:**

```json
{
  "token": "eyJhbGciOiJIUzI1NiIs...",
  "expires_at": "2026-03-21T12:00:00Z"
}
```

The `user_id` you provide is the **external user ID** in your system. Hermes auto-creates an internal user record if one does not exist. The returned JWT contains the Hermes **internal** user ID as `sub` and the `organization_id` claim.

Tokens expire in approximately 1 hour (with jitter). Your backend should request a new token before expiry and pass it to the frontend.

The same JWT is used for:
- **Inbox API** requests (`Authorization: Bearer <token>`)
- **User API** requests (`Authorization: Bearer <token>`)
- **Centrifugo WebSocket** connections (passed as the connection token)

## Setup Steps

Hermes organizes preferences as **categories** → **subscriptions**, with reusable
**templates** for content. (These were previously called "groups" and "types".)

### 1. Create an Organization

An organization is one of your customers, on whose behalf you send notifications. Create one per
customer (or a single one if your app doesn't distinguish them). Organization IDs are UUIDs assigned
by Hermes.

> **Organizations are not a security boundary.** Your API key authenticates your *app*, and can act
> on behalf of any organization in your installation — that is deliberate, since one app serves many
> organizations and the same organization may be served by more than one app. The isolation boundary
> is the app itself, enforced by running a separate Hermes installation per app. See
> [ADR 0003](adr/0003-rename-tenant-to-organization.md).

```bash
curl -X POST https://hermes.example.com/admin/v1/organizations \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{ "name": "My App" }'
```

**Response:**

```json
{ "id": "9b2e7c14-3f5a-4d61-8b0e-2a1c4f9d7e30", "name": "My App", "created_at": "2026-03-21T10:00:00Z" }
```

Use the returned `id` as `organization_id` in subsequent calls.

### 2. Create an API Key

API keys authenticate server-to-server calls to the Admin API (`POST /admin/v1/apikeys`). The raw key is shown **once** on creation -- store it securely. Only an HMAC-SHA256 hash is persisted, so it cannot be retrieved later.

A key is **not** tied to an organization. One application serves many customers, so the
organization is a per-request parameter on every send — see
[ADR 0012](adr/0012-api-keys-are-not-scoped-to-organizations.md).

### 3. Create Subscription Categories

Categories group related notifications and define default delivery channels and a default opt-in state. Three categories (`account`, `general`, `marketing`) are seeded by default; create more as needed.

```bash
curl -X POST https://hermes.example.com/admin/v1/subscriptions/categories \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "slug": "account",
    "name": "Account Notifications",
    "default_channels": ["email", "inbox"],
    "default_state": "on"
  }'
```

`default_state` is one of `on`, `off`, or `required` (required categories can't be opted out of). Available channels: `email`, `sms`, `inbox`.

### 4. Create Subscriptions

A subscription is a specific notification a user can opt in or out of, within a category.

```bash
curl -X POST https://hermes.example.com/admin/v1/subscriptions/categories/CATEGORY_ID/subscriptions \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "slug": "invoice-ready",
    "name": "Invoice Ready"
  }'
```

### 5. Create Templates (Optional)

Templates define reusable per-channel content with variable substitution. Link a template to a subscription via `subscription_id`, or leave it null for a standalone template.

```bash
curl -X POST https://hermes.example.com/admin/v1/templates \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "slug": "welcome",
    "name": "Welcome Email",
    "subscription_id": "SUBSCRIPTION_ID",
    "default_channels": ["email", "inbox"],
    "email_subject": "Welcome to {{.app_name}}, {{.user_name}}!",
    "email_body": "<h1>Welcome!</h1><p>Hi {{.user_name}}, thanks for joining.</p>",
    "inbox_title": "Welcome to {{.app_name}}",
    "inbox_body": "Hi {{.user_name}}, thanks for joining."
  }'
```

Templates use Go `text/template` syntax. Variables are passed via the `data` field when sending.

## Sending Notifications

The recipient is always given under `to` (`organization_id` + external `user_id`, plus optional
`email`/`phone` overrides). Provide **either** a `template` slug **or** direct `content` --
they are mutually exclusive. `POST /v1/send` is served by both the Admin service and the
dedicated high-throughput Send service.

### Using a Template

```bash
curl -X POST https://hermes.example.com/admin/v1/send \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "to": {
      "organization_id": "9b2e7c14-3f5a-4d61-8b0e-2a1c4f9d7e30",
      "user_id": "ext-user-123"
    },
    "template": "welcome",
    "data": {
      "app_name": "Acme App",
      "user_name": "Alice"
    }
  }'
```

### Using Direct Content

```bash
curl -X POST https://hermes.example.com/admin/v1/send \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "to": {
      "organization_id": "9b2e7c14-3f5a-4d61-8b0e-2a1c4f9d7e30",
      "user_id": "ext-user-123"
    },
    "content": {
      "title": "Your invoice is ready",
      "body": "Invoice #1234 for $99.00 is now available.",
      "action_url": "https://app.example.com/invoices/1234",
      "action_label": "View Invoice"
    },
    "channels": ["email", "inbox"]
  }'
```

**Response:**

```json
{
  "notification_id": "2qFh8Kd0Rb3Lm9Tx1Vn7Yc"
}
```

### Idempotency

To prevent duplicate notifications (e.g., on retries), include an `X-Idempotency-Key` header:

```bash
curl -X POST https://hermes.example.com/admin/v1/send \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "X-Idempotency-Key: invoice-1234-ready" \
  -H "Content-Type: application/json" \
  -d '{ ... }'
```

If the same organization + idempotency key has been seen before, Hermes returns the existing notification ID with a `202 Accepted` status.

### When Hermes cannot accept a send

`POST /v1/send` returns `503 Service Unavailable` with a `Retry-After` header when the
notification cannot be handed to the pipeline — either the message bus is unreachable, or the
delivery backlog has reached its configured ceiling because something downstream has stalled.

```http
HTTP/1.1 503 Service Unavailable
Content-Type: application/problem+json
Retry-After: 5
```

**Retry, honouring `Retry-After`.** A 503 here means the notification was *not* accepted — no
notification ID was issued and nothing will be delivered — so unlike a timeout there is no
ambiguity about whether the send happened. Retrying with the same `X-Idempotency-Key` is safe
and is the recommended pattern: if an earlier attempt did in fact succeed, the retry returns
that attempt's notification ID instead of creating a second notification.

Hermes rejects rather than silently dropping work that it has already acknowledged; see
[ADR 0010](adr/0010-bounded-work-streams-reject-rather-than-drop.md). A `202` therefore remains
a real commitment.

### Rate Limits

Requests are rate limited per credential — per API key on the Send and Admin APIs, per user on
the Inbox and User APIs. Exceeding the limit returns `429 Too Many Requests`:

```http
HTTP/1.1 429 Too Many Requests
Content-Type: application/json
Retry-After: 3
RateLimit-Limit: 2000
RateLimit-Remaining: 0
RateLimit-Reset: 3

{"error": "rate limit exceeded"}
```

| Header | Meaning |
|---|---|
| `Retry-After` | Whole seconds to wait before retrying. Always at least `1`. |
| `RateLimit-Limit` | Sustained requests per second allowed for this credential. |
| `RateLimit-Remaining` | Requests available right now. Sent on successful responses too. |
| `RateLimit-Reset` | Seconds until capacity is available. Sent only on a 429. |

**Honour `Retry-After`.** Retrying sooner does not shorten the wait, and retrying in a tight loop
wastes your own quota. Back off, ideally with jitter so a fleet of your workers does not
synchronise.

Defaults are 2000 req/s (5000 burst) for Send, 500/s (1000 burst) for Admin, and 20/s (50 burst)
per user for Inbox and User. Operators can change these per deployment — see
[configuration](configuration.md#http-rate-limiting) — so treat the values in `RateLimit-Limit`
as authoritative over anything hardcoded in your client.

If you are self-hosting or running against a multi-replica deployment, note that the limit is
enforced per replica; the effective ceiling is higher than the per-replica figure and varies with
autoscaling. Do not build a client that depends on hitting an exact threshold.

`/healthz` and `/readyz` are never rate limited.

### Checking Notification Status

```bash
curl https://hermes.example.com/admin/v1/notifications/NOTIFICATION_ID \
  -H "Authorization: Bearer YOUR_API_KEY"
```

**Response:**

```json
{
  "notification": {
    "id": "2qFh8Kd0Rb3Lm9Tx1Vn7Yc",
    "organization_id": "my-organization",
    "user_id": "usr-internal-id",
    "status": "delivered",
    "channels": ["email", "inbox"],
    "created_at": "2026-03-21T10:00:00Z",
    "sent_at": "2026-03-21T10:00:01Z",
    "delivered_at": "2026-03-21T10:00:02Z"
  },
  "events": [
    {
      "id": "...",
      "channel": "email",
      "event": "email.delivered",
      "severity": "info",
      "created_at": "2026-03-21T10:00:02Z"
    }
  ]
}
```

## Inbox API (User-Facing)

All Inbox endpoints require a valid JWT in the `Authorization: Bearer <token>` header.

### List Inbox

```bash
curl "https://hermes.example.com/inbox/v1/inbox?limit=20&archived=false" \
  -H "Authorization: Bearer USER_JWT"
```

**Response:**

```json
{
  "data": [
    {
      "id": "2qFh8Kd0Rb3Lm9Tx1Vn7Yc",
      "title": "Your invoice is ready",
      "body": "Invoice #1234 for $99.00 is now available.",
      "action_url": "https://app.example.com/invoices/1234",
      "action_label": "View Invoice",
      "status": "delivered",
      "created_at": "2026-03-21T10:00:00Z",
      "read_at": null
    }
  ],
  "unread_count": 3,
  "cursor": ""
}
```

**Query parameters:**
- `limit` (int, default 20) -- number of items per page
- `cursor` (string) -- pagination cursor from previous response
- `archived` (bool, default false) -- if true, returns archived notifications

> **Treat cursors as short-lived and be ready for them to be rejected.** A cursor encodes
> backend-specific state, and Hermes can be deployed against either of two stores (see
> [architecture.md](architecture.md#the-dual-store)). A cursor issued by one is **not valid**
> on the other, so if an operator switches the backend, in-flight cursors start returning
> `invalid cursor`. Handle that by discarding the cursor and re-requesting the first page —
> do not persist cursors across sessions or treat one as a stable bookmark.

### Mark as Read

```bash
curl -X PUT https://hermes.example.com/inbox/v1/inbox/NOTIFICATION_ID/read \
  -H "Authorization: Bearer USER_JWT"
```

### Mark as Unread

```bash
curl -X DELETE https://hermes.example.com/inbox/v1/inbox/NOTIFICATION_ID/read \
  -H "Authorization: Bearer USER_JWT"
```

### Mark All as Read

```bash
curl -X PUT https://hermes.example.com/inbox/v1/inbox/read-all \
  -H "Authorization: Bearer USER_JWT"
```

### Archive

```bash
curl -X PUT https://hermes.example.com/inbox/v1/inbox/NOTIFICATION_ID/archive \
  -H "Authorization: Bearer USER_JWT"
```

### Unarchive

```bash
curl -X DELETE https://hermes.example.com/inbox/v1/inbox/NOTIFICATION_ID/archive \
  -H "Authorization: Bearer USER_JWT"
```

### Delete (Soft)

```bash
curl -X DELETE https://hermes.example.com/inbox/v1/inbox/NOTIFICATION_ID \
  -H "Authorization: Bearer USER_JWT"
```

## User API (User-Facing)

All User endpoints require a valid JWT in the `Authorization: Bearer <token>` header.

### Get Profile

```bash
curl https://hermes.example.com/user/v1/users/me \
  -H "Authorization: Bearer USER_JWT"
```

**Response:**

```json
{
  "id": "usr-internal-id",
  "organization_id": "my-organization",
  "external_id": "ext-user-123",
  "email": "alice@example.com",
  "phone": "+1234567890",
  "created_at": "2026-03-21T10:00:00Z"
}
```

### Update Contact Info

```bash
curl -X PUT https://hermes.example.com/user/v1/users/me/contacts \
  -H "Authorization: Bearer USER_JWT" \
  -H "Content-Type: application/json" \
  -d '{
    "email": "alice-new@example.com",
    "phone": "+1987654321"
  }'
```

Preferences are modeled as a **preference center**: categories, each containing the
subscriptions a user can opt in or out of. A subscription in a `required` category is not
toggleable.

### List Notification Preferences

```bash
curl https://hermes.example.com/user/v1/users/me/preferences \
  -H "Authorization: Bearer USER_JWT"
```

**Response:**

```json
{
  "categories": [
    {
      "id": "sct_default_account",
      "slug": "account",
      "name": "Account",
      "default_channels": ["email", "inbox"],
      "default_state": "required",
      "subscriptions": [
        {
          "id": "sub_invoice_ready",
          "slug": "invoice-ready",
          "name": "Invoice Ready",
          "opted_in": true,
          "toggleable": true
        }
      ]
    }
  ]
}
```

### Set a Preference

Opt the user in or out of a single subscription:

```bash
curl -X PUT https://hermes.example.com/user/v1/users/me/preferences/SUBSCRIPTION_ID \
  -H "Authorization: Bearer USER_JWT" \
  -H "Content-Type: application/json" \
  -d '{
    "opted_in": false
  }'
```

### Delete a Preference (Reset to Default)

Removes the explicit choice so the subscription falls back to its category's default state:

```bash
curl -X DELETE https://hermes.example.com/user/v1/users/me/preferences/SUBSCRIPTION_ID \
  -H "Authorization: Bearer USER_JWT"
```

## Real-Time Updates via Centrifugo

Hermes uses [Centrifugo](https://centrifugal.dev/) for real-time WebSocket delivery of inbox events (new notification, read, archive, etc.).

The same JWT from `POST /v1/auth/token` is used to connect to Centrifugo -- no separate token endpoint is needed. Centrifugo is configured with the same HMAC signing secret as Hermes (`HERMES_JWT_SECRET`).

### Connecting from the Frontend

Using the [centrifuge-js](https://github.com/centrifugal/centrifuge-js) client:

```javascript
import { Centrifuge } from 'centrifuge';

// Use the same Hermes JWT for Centrifugo connection
const centrifuge = new Centrifuge('wss://centrifugo.example.com/connection/websocket', {
  token: userJwt
});

// Subscribe to the user's channel
const sub = centrifuge.newSubscription(`user#${hermesUserId}`);

sub.on('publication', (ctx) => {
  const event = ctx.data;
  if (event.type === 'notification.new') {
    // A new notification was delivered to the inbox:
    //   event.id, event.title, event.body, event.created_at, event.timestamp
    //   event.action = { url, label }   // present only if the notification has an action
  } else if (event.type === 'inbox.updated') {
    // The user's inbox state changed (from an inbox API action):
    //   event.notification_id
    //   event.action = "read" | "unread" | "archive" | "unarchive" | "delete" | "read-all"
    //   event.unread_count, event.timestamp
  }
});

sub.subscribe();
centrifuge.connect();
```

Two event types arrive on the user's channel: **`notification.new`** when a notification is
delivered to the inbox, and **`inbox.updated`** when inbox state changes via an Inbox API action
(both carry a `timestamp` in epoch milliseconds).

The channel format is `user#<hermes_internal_user_id>`. Events are published when inbox actions occur (mark read, archive, new delivery, etc.).

**Important:** Centrifugo's `token_hmac_secret_key` must be set to the same value as `HERMES_JWT_SECRET` so it can validate the Hermes-issued JWTs.

## Admin API Reference

All Admin endpoints require an API key: `Authorization: Bearer YOUR_API_KEY`.

This is the high-level map; the generated spec at `api/admin/openapi.yaml` is authoritative.

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/auth/token` | Exchange user ID + organization ID for a Hermes JWT |
| `POST` / `GET` | `/v1/organizations` | Create / list organizations |
| `POST` / `GET` / `DELETE` | `/v1/apikeys` (`/:id`) | Create, list, revoke API keys |
| `GET` / `POST` | `/v1/subscriptions/categories` | List / create subscription categories |
| `GET` / `PUT` / `DELETE` | `/v1/subscriptions/categories/:id` | Get / update / delete a category |
| `GET` / `POST` | `/v1/subscriptions/categories/:category_id/subscriptions` | List / create subscriptions in a category |
| `PUT` / `DELETE` | `/v1/subscriptions/:id` | Update / delete a subscription |
| `GET` / `POST` | `/v1/templates` | List / create templates |
| `PUT` / `DELETE` | `/v1/templates/:id` | Update / delete a template |
| `POST` | `/v1/send` | Send a notification |
| `GET` | `/v1/notifications` | List recent notifications |
| `GET` | `/v1/notifications/:id` | Get notification status and events |

## Inbox API Reference

All Inbox endpoints require a user JWT: `Authorization: Bearer USER_JWT`.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/inbox` | List inbox notifications |
| `PUT` | `/v1/inbox/read-all` | Mark all notifications as read |
| `PUT` | `/v1/inbox/:id/read` | Mark notification as read |
| `DELETE` | `/v1/inbox/:id/read` | Mark notification as unread |
| `PUT` | `/v1/inbox/:id/archive` | Archive notification |
| `DELETE` | `/v1/inbox/:id/archive` | Unarchive notification |
| `DELETE` | `/v1/inbox/:id` | Soft-delete notification |

## User API Reference

All User endpoints require a user JWT: `Authorization: Bearer USER_JWT`.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/users/me` | Get current user profile |
| `PUT` | `/v1/users/me/contacts` | Update email and/or phone |
| `GET` | `/v1/users/me/preferences` | List the preference center (categories + subscriptions) |
| `PUT` | `/v1/users/me/preferences/:subscription_id` | Opt in/out of a subscription |
| `DELETE` | `/v1/users/me/preferences/:subscription_id` | Delete preference (reset to category default) |

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `HERMES_HTTP_PORT` | `8080` | HTTP server port |
| `HERMES_DATABASE_URL` | `postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable` | PostgreSQL connection string |
| `HERMES_NATS_URL` | `nats://localhost:4222` | NATS server URL |
| `HERMES_REDIS_URL` | `redis://localhost:6379/0` | Redis URL (used for idempotency cache) |
| `HERMES_JWT_SECRET` | `hermes-jwt-secret` | Secret for Hermes-issued JWTs (also used by Centrifugo) |
| `HERMES_CENTRIFUGO_API_URL` | `http://localhost:8000` | Centrifugo HTTP API URL |
| `HERMES_CENTRIFUGO_API_KEY` | `centrifugo-api-key` | Centrifugo API key |
