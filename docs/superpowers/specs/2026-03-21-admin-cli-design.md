# Hermes Admin CLI Design

## Overview

A command-line tool for managing Hermes notification infrastructure and simulating user inbox experiences. Built as a Cobra-based CLI on top of a reusable Go SDK that wraps the Admin HTTP API.

## Package Structure

```
pkg/client/          — Go SDK wrapping the Admin HTTP API
  client.go          — Client struct, config, HTTP helpers
  groups.go          — Group operations
  types.go           — Notification type operations
  notifications.go   — Send + status operations
  auth.go            — Token exchange + signing key operations

cmd/hermes/          — CLI entry point
  main.go            — Cobra root command setup

internal/cli/        — CLI command implementations
  root.go            — Root command, global flags
  groups.go          — hermes groups [list|create|update]
  types.go           — hermes types [list|create|update|delete]
  notifications.go   — hermes notifications [send|status]
  auth.go            — hermes auth [token]
  keys.go            — hermes keys [list|create|delete]
  inbox.go           — hermes inbox [listen]
```

## SDK Design (`pkg/client`)

Hand-written, idiomatic Go SDK. No internal package imports — only `net/http` and its own request/response types.

### Client

```go
type Client struct { ... }

func New(baseURL string, apiKey string, opts ...Option) *Client

type Option func(*Client)
func WithHTTPClient(c *http.Client) Option
```

### Resource Methods

Methods organized by resource sub-structs (single level of nesting):

- `client.Groups.List(ctx) ([]Group, error)`
- `client.Groups.Create(ctx, CreateGroupRequest) (*Group, error)`
- `client.Groups.Update(ctx, id, UpdateGroupRequest) (*Group, error)`
- `client.Types.List(ctx) ([]NotificationType, error)`
- `client.Types.Create(ctx, CreateTypeRequest) (*NotificationType, error)`
- `client.Types.Update(ctx, id, UpdateTypeRequest) (*NotificationType, error)`
- `client.Types.Delete(ctx, id) error`
- `client.Notifications.Send(ctx, SendRequest, opts ...SendOption) (*SendResponse, error)`
- `client.Notifications.GetStatus(ctx, id) (*NotificationStatus, error)`
- `client.Auth.ExchangeToken(ctx, TokenRequest) (*TokenResponse, error)`
- `client.SigningKeys.List(ctx) ([]SigningKey, error)`
- `client.SigningKeys.Create(ctx, CreateKeyRequest) (*SigningKey, error)`
- `client.SigningKeys.Delete(ctx, id) error`

`SendOption` is a functional option for request-level settings like idempotency key (sent as `X-Idempotency-Key` header, not body field).

### Request/Response Types

Types defined per resource file. Example:

```go
type Group struct {
    ID              string    `json:"id"`
    Slug            string    `json:"slug"`
    Name            string    `json:"name"`
    DefaultChannels []string  `json:"default_channels"`
    CreatedAt       time.Time `json:"created_at"`
}

type CreateGroupRequest struct {
    Slug            string   `json:"slug"`
    Name            string   `json:"name"`
    DefaultChannels []string `json:"default_channels,omitempty"`
}

type UpdateGroupRequest struct {
    Name            *string  `json:"name,omitempty"`
    DefaultChannels []string `json:"default_channels"` // no omitempty: empty slice clears channels
}

type TokenResponse struct {
    Token     string `json:"token"`
    ExpiresAt string `json:"expires_at"` // RFC3339
}

type SigningKey struct {
    ID            string    `json:"id"`
    Name          string    `json:"name"`
    Algorithm     string    `json:"algorithm"`
    UserIDClaim   string    `json:"user_id_claim"`
    TenantIDClaim string    `json:"tenant_id_claim"`
    Active        bool      `json:"active"`
    CreatedAt     time.Time `json:"created_at"`
}

// SendOption configures per-request behavior
type SendOption func(*sendOptions)
func WithIdempotencyKey(key string) SendOption // sets X-Idempotency-Key header
```

Response types include `CreatedAt` and all fields returned by the server. SDK types are illustrative — full definitions for all resources follow the same pattern.

The `--data` CLI flag accepts a raw JSON string (e.g., `--data '{"key":"value"}'`) which is unmarshaled into `map[string]any` before passing to the SDK.

### Error Handling

API errors return a typed `*APIError`:

```go
type APIError struct {
    StatusCode int
    Message    string
}
```

## CLI Commands & Flags

### Global Flags (Root Command)

| Flag | Env Var | Required | Description |
|------|---------|----------|-------------|
| `--url` | `HERMES_ADMIN_URL` | Yes | Admin service base URL |
| `--api-key` | `HERMES_API_KEY` | Yes | API key for authentication |
| `--output` / `-o` | — | No | Output format: `table` (default) or `json` |

No default URL — must be explicitly configured.

### Command Reference

| Command | Flags | Description |
|---------|-------|-------------|
| `hermes groups list` | — | List all notification groups |
| `hermes groups create` | `--slug`, `--name`, `--channels` | Create a group |
| `hermes groups update` | `--id`, `--name`, `--channels` | Update a group |
| `hermes types list` | — | List all notification types |
| `hermes types create` | `--group-id`, `--slug`, `--name`, `--email-subject`, `--email-body`, `--sms-body`, `--inbox-title`, `--inbox-body` | Create a type with templates |
| `hermes types update` | `--id`, `--name`, `--email-subject`, `--email-body`, `--sms-body`, `--inbox-title`, `--inbox-body` | Update a type |
| `hermes types delete` | `--id` | Delete a type |
| `hermes notifications send` | `--tenant-id`, `--user-id`, `--type`, `--channels`, `--data` (JSON), `--title`, `--body`, `--action-url`, `--action-label`, `--group`, `--idempotency-key` | Send a notification |
| `hermes notifications status` | `--id` | Get notification with delivery events |
| `hermes auth token` | `--tenant-id`, `--user-id` | Exchange API key for user JWT |
| `hermes keys list` | — | List JWT signing keys |
| `hermes keys create` | `--name`, `--secret`, `--algorithm`, `--user-id-claim`, `--tenant-id-claim` | Create a signing key |
| `hermes keys delete` | `--id` | Delete a signing key |
| `hermes inbox listen` | `--tenant-id`, `--user-id`, `--centrifugo-url`, `--inbox-url` | Listen for real-time inbox notifications |

## Inbox Listen Flow

The `hermes inbox listen` command simulates a user connecting to Centrifugo for real-time notifications.

### Steps

1. **Get user JWT** — calls `POST /v1/auth/token` with `--tenant-id` and `--user-id` via the SDK
2. **Get Centrifugo token** — calls `GET /v1/inbox/centrifugo-token` on the inbox service (`--inbox-url`), authenticated with the JWT from step 1
3. **Connect to Centrifugo** — uses `centrifugal/centrifuge-go` to open a WebSocket connection to `--centrifugo-url`
4. **Subscribe** — subscribes to the `user#{userID}` channel
5. **Print events** — prints each incoming event to stdout
6. **Run until interrupted** — blocks on SIGINT/SIGTERM for graceful disconnect

### Additional Flags

| Flag | Env Var | Required | Description |
|------|---------|----------|-------------|
| `--centrifugo-url` | `HERMES_CENTRIFUGO_URL` | Yes | Centrifugo WebSocket endpoint |
| `--inbox-url` | `HERMES_INBOX_URL` | Yes | Inbox service base URL (for token endpoint) |

### Output Examples

**Table mode:**
```
Listening on user#abc123 ...
TIME                  TYPE            NOTIFICATION_ID      ACTION
2026-03-21 10:15:03   inbox.updated   01JNQX7K8M...       read
2026-03-21 10:15:07   inbox.updated   01JNQX7K8M...       archive
```

**JSON mode:**
```json
{"type":"inbox.updated","notification_id":"01JNQX7K8M...","action":"read"}
```

## Dependencies

New dependencies to add:

- `github.com/spf13/cobra` — CLI framework
- `github.com/centrifugal/centrifuge-go` — Centrifugo WebSocket client

## Authentication

The CLI authenticates to the Admin API using an API key passed via `--api-key` flag or `HERMES_API_KEY` environment variable. It is purely an HTTP client — no direct database access.

For the inbox listen feature, the CLI performs a two-step token exchange:
1. API key -> user JWT (via admin service `POST /v1/auth/token`)
2. User JWT -> Centrifugo token (via inbox service `GET /v1/inbox/centrifugo-token`)

**Prerequisite:** Both the admin and inbox services must share the same Hermes internal signing key (configured via `HERMES_JWT_SECRET`). The admin service issues JWTs signed with this key, and the inbox service's JWT middleware validates against it via the signing keys table. The `EnsureHermesSigningKey` startup logic in both services handles this automatically when they share the same `HERMES_JWT_SECRET`.

## Testing Strategy

### SDK (`pkg/client`)
Unit tests with `httptest.Server` returning canned responses. Tests verify:
- Request method, path, headers, body marshaling
- Response unmarshaling
- Error handling (`*APIError` for 4xx/5xx)

### CLI (`internal/cli`)
Test command wiring by executing commands against an `httptest.Server`. Verify:
- Flags parse correctly
- Required flags are enforced
- Output formatting works for both table and JSON modes

### Inbox Listen
Unit test the token exchange steps and Centrifugo client construction:
- Two `httptest.Server` instances: one for admin (token exchange), one for inbox (Centrifugo token)
- The inbox mock validates the JWT from the admin mock — use a shared test signing secret to generate valid JWTs in test fixtures
- Full WebSocket integration test behind `integration` build tag if desired

No new infrastructure required — all unit tests use `httptest`.

## Known Limitations

- No `hermes groups get` or `hermes types get` by slug/ID. Users must use `list` to find IDs for `update` commands. Single-resource lookup can be added when the admin API exposes it.
- For type-based sends (`--type`), `--channels` overrides are not validated against which channels have templates defined. The server accepts the request but channels without templates will be skipped by the router.
