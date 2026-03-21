# Hermes Integration Guide

Hermes is a multi-channel notification service. It accepts notification requests via a server-side Admin API, routes them through configurable channels (email, SMS, in-app inbox, webhooks), and exposes user-facing APIs for inbox management and notification preferences.

This guide walks through integrating Hermes into your SaaS application.

## Architecture Overview

Hermes runs as a set of microservices:

| Service | Port (default) | Purpose |
|---------|---------------|---------|
| **Admin** | 8080 | Server-side API: send notifications, manage groups/types, issue JWT tokens, manage signing keys |
| **Inbox** | 8081 | User-facing API: list/read/archive inbox notifications, Centrifugo token generation |
| **User** | 8082 | User-facing API: profile management, notification preferences |
| **Router** | - | Internal worker: routes notifications to delivery channels |
| **Workers** | - | Internal workers: email, SMS, inbox delivery, event writing |

**Admin API** is authenticated with API keys (server-to-server).
**Inbox and User APIs** are authenticated with JWTs (user-facing).

## Authentication

Hermes supports two JWT authentication flows for user-facing APIs:

### Flow 1: Hermes-Issued Tokens

Your backend calls the Admin API to exchange a user identifier for a Hermes JWT. This is the simplest approach.

```
Your Backend                     Hermes Admin API
    |                                  |
    |  POST /v1/auth/token             |
    |  { user_id, tenant_id }          |
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
    "tenant_id": "tenant-abc"
  }'
```

**Response:**

```json
{
  "token": "eyJhbGciOiJIUzI1NiIs...",
  "expires_at": "2026-03-21T12:00:00Z"
}
```

The `user_id` you provide is the **external user ID** in your system. Hermes auto-creates an internal user record if one does not exist. The returned JWT contains the Hermes **internal** user ID as `sub` and the `tenant_id` claim.

Tokens expire in approximately 1 hour (with jitter). Your backend should request a new token before expiry and pass it to the frontend.

### Flow 2: Provider-Issued Tokens (Bring Your Own JWT)

If your application already issues JWTs (e.g., from your auth system), you can configure Hermes to accept them directly. This avoids the extra token-exchange round trip.

**Step 1: Register your signing key with Hermes**

```bash
curl -X POST https://hermes.example.com/admin/v1/auth/keys \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "My Auth Service",
    "algorithm": "HS256",
    "secret": "your-jwt-signing-secret",
    "user_id_claim": "sub",
    "tenant_id_claim": "org_id"
  }'
```

| Field | Default | Description |
|-------|---------|-------------|
| `name` | (required) | A human-readable name for this key |
| `algorithm` | `HS256` | Signing algorithm. Supported: `HS256`, `HS384`, `HS512` |
| `secret` | (required) | The shared secret used to sign your JWTs |
| `user_id_claim` | `sub` | The JWT claim containing the user's external identifier |
| `tenant_id_claim` | `tenant_id` | The JWT claim containing the tenant/organization identifier |

**Step 2: Include the required claims in your JWTs**

Your JWTs must contain:
- A claim with the user's external ID (mapped by `user_id_claim`)
- A claim with the tenant ID (mapped by `tenant_id_claim`)
- An `exp` (expiration) claim

Example JWT payload when `user_id_claim: "sub"` and `tenant_id_claim: "org_id"`:

```json
{
  "sub": "alice@example.com",
  "org_id": "acme-corp",
  "exp": 1742572800
}
```

When Hermes receives a provider-issued JWT, it:
1. Validates the signature against registered signing keys
2. Extracts the user ID and tenant ID from the configured claims
3. Auto-creates the Hermes user (via `EnsureUser`) if they do not exist
4. Resolves the external ID to the Hermes internal user ID

**Step 3: Use the token with Inbox and User APIs**

```bash
curl https://hermes.example.com/inbox/v1/inbox \
  -H "Authorization: Bearer YOUR_EXISTING_JWT"
```

### Managing Signing Keys

**List all signing keys:**

```bash
curl https://hermes.example.com/admin/v1/auth/keys \
  -H "Authorization: Bearer YOUR_API_KEY"
```

Secrets are never returned in API responses.

**Delete a signing key:**

```bash
curl -X DELETE https://hermes.example.com/admin/v1/auth/keys/KEY_ID \
  -H "Authorization: Bearer YOUR_API_KEY"
```

## Setup Steps

### 1. Create a Tenant

Tenants represent isolated organizations in Hermes. Create one per customer (or one for your whole app if single-tenant).

Tenants are currently created via direct database insertion or migration:

```sql
INSERT INTO tenants (id, name) VALUES ('my-tenant', 'My App');
```

### 2. Create an API Key

API keys authenticate server-to-server calls to the Admin API. Store the raw key securely -- it is hashed before storage and cannot be retrieved.

### 3. Create Notification Groups

Groups organize notification types and control default delivery channels.

```bash
curl -X POST https://hermes.example.com/admin/v1/groups \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "slug": "account",
    "name": "Account Notifications",
    "default_channels": ["email", "inbox"]
  }'
```

Available channels: `email`, `sms`, `inbox`, `webhook`.

### 4. Create Notification Types (Optional)

Types are templates within a group. They define reusable content templates with variable substitution.

```bash
curl -X POST https://hermes.example.com/admin/v1/types \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "slug": "welcome",
    "name": "Welcome Email",
    "group_id": "GROUP_ID",
    "email_subject": "Welcome to {{.app_name}}, {{.user_name}}!",
    "email_body": "<h1>Welcome!</h1><p>Hi {{.user_name}}, thanks for joining.</p>",
    "inbox_title": "Welcome to {{.app_name}}",
    "inbox_body": "Hi {{.user_name}}, thanks for joining."
  }'
```

Templates use Go `text/template` syntax. Variables are passed via the `data` field when sending.

### 5. Register a JWT Signing Key (Optional)

Only needed if using Flow 2 (provider-issued tokens). See the [Authentication](#flow-2-provider-issued-tokens-bring-your-own-jwt) section above.

## Sending Notifications

### Using a Notification Type (Templated)

```bash
curl -X POST https://hermes.example.com/admin/v1/send \
  -H "Authorization: Bearer YOUR_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "my-tenant",
    "user_id": "ext-user-123",
    "type": "welcome",
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
    "tenant_id": "my-tenant",
    "user_id": "ext-user-123",
    "group": "account",
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
  "notification_id": "01KM8EJ4JKTKJQBPXXMPJVGK4X"
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

If the same tenant + idempotency key has been seen before, Hermes returns the existing notification ID with a `202 Accepted` status.

### Checking Notification Status

```bash
curl https://hermes.example.com/admin/v1/notifications/NOTIFICATION_ID \
  -H "Authorization: Bearer YOUR_API_KEY"
```

**Response:**

```json
{
  "notification": {
    "id": "01KM8EJ4JKTKJQBPXXMPJVGK4X",
    "tenant_id": "my-tenant",
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
      "event": "delivered",
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
      "id": "01KM8EJ4JKTKJQBPXXMPJVGK4X",
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
  "tenant_id": "my-tenant",
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

### List Notification Preferences

```bash
curl https://hermes.example.com/user/v1/users/me/preferences \
  -H "Authorization: Bearer USER_JWT"
```

**Response:**

```json
{
  "data": [
    {
      "user_id": "usr-internal-id",
      "group_id": "group-1",
      "channels": ["email", "inbox"]
    }
  ]
}
```

### Set Preference for a Group

```bash
curl -X PUT https://hermes.example.com/user/v1/users/me/preferences/GROUP_ID \
  -H "Authorization: Bearer USER_JWT" \
  -H "Content-Type: application/json" \
  -d '{
    "channels": ["inbox"]
  }'
```

This overrides the group's default channels for this user. The user will only receive notifications via the specified channels.

### Delete Preference (Reset to Default)

```bash
curl -X DELETE https://hermes.example.com/user/v1/users/me/preferences/GROUP_ID \
  -H "Authorization: Bearer USER_JWT"
```

## Real-Time Updates via Centrifugo

Hermes uses [Centrifugo](https://centrifugal.dev/) for real-time WebSocket delivery of inbox events (new notification, read, archive, etc.).

### Getting a Centrifugo Connection Token

```bash
curl https://hermes.example.com/inbox/v1/inbox/centrifugo-token \
  -H "Authorization: Bearer USER_JWT"
```

**Response:**

```json
{
  "token": "eyJhbGciOiJIUzI1NiIs..."
}
```

### Connecting from the Frontend

Using the [centrifuge-js](https://github.com/centrifugal/centrifuge-js) client:

```javascript
import { Centrifuge } from 'centrifuge';

// 1. Get the Centrifugo token from your backend / Hermes inbox API
const tokenResp = await fetch('/inbox/v1/inbox/centrifugo-token', {
  headers: { 'Authorization': `Bearer ${userJwt}` }
});
const { token } = await tokenResp.json();

// 2. Connect to Centrifugo
const centrifuge = new Centrifuge('wss://centrifugo.example.com/connection/websocket', {
  token: token
});

// 3. Subscribe to the user's channel
const sub = centrifuge.newSubscription(`user#${userId}`);

sub.on('publication', (ctx) => {
  const event = ctx.data;
  console.log('Inbox event:', event);
  // event.type = "inbox.updated"
  // event.notification_id = "..."
  // event.action = "read" | "unread" | "archive" | "unarchive" | "delete" | "read-all"
});

sub.subscribe();
centrifuge.connect();
```

The channel format is `user#<hermes_internal_user_id>`. Events are published when inbox actions occur (mark read, archive, new delivery, etc.).

## Admin API Reference

All Admin endpoints require an API key: `Authorization: Bearer YOUR_API_KEY`.

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/auth/token` | Exchange user ID + tenant ID for a Hermes JWT |
| `POST` | `/v1/auth/keys` | Register a JWT signing key |
| `GET` | `/v1/auth/keys` | List JWT signing keys (secrets masked) |
| `DELETE` | `/v1/auth/keys/:id` | Delete a JWT signing key |
| `POST` | `/v1/groups` | Create a notification group |
| `GET` | `/v1/groups` | List notification groups |
| `PUT` | `/v1/groups/:id` | Update a notification group |
| `POST` | `/v1/types` | Create a notification type |
| `GET` | `/v1/types` | List notification types |
| `PUT` | `/v1/types/:id` | Update a notification type |
| `DELETE` | `/v1/types/:id` | Delete a notification type |
| `POST` | `/v1/send` | Send a notification |
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
| `GET` | `/v1/inbox/centrifugo-token` | Get Centrifugo connection token |

## User API Reference

All User endpoints require a user JWT: `Authorization: Bearer USER_JWT`.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/users/me` | Get current user profile |
| `PUT` | `/v1/users/me/contacts` | Update email and/or phone |
| `GET` | `/v1/users/me/preferences` | List notification preferences |
| `PUT` | `/v1/users/me/preferences/:group_id` | Set channel preference for a group |
| `DELETE` | `/v1/users/me/preferences/:group_id` | Delete preference (reset to group default) |

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `HERMES_HTTP_PORT` | `8080` | HTTP server port |
| `HERMES_DATABASE_URL` | `postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable` | PostgreSQL connection string |
| `HERMES_NATS_URL` | `nats://localhost:4222` | NATS server URL |
| `HERMES_REDIS_URL` | `redis://localhost:6379/0` | Redis URL (used for idempotency cache) |
| `HERMES_JWT_SECRET` | `hermes-jwt-secret` | Secret for Hermes-issued JWTs |
| `HERMES_CENTRIFUGO_TOKEN_SECRET` | `centrifugo-token-secret` | Secret for Centrifugo connection tokens |
| `HERMES_CENTRIFUGO_API_URL` | `http://localhost:8000` | Centrifugo HTTP API URL |
| `HERMES_CENTRIFUGO_API_KEY` | `centrifugo-api-key` | Centrifugo API key |
