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
  auth.go            — Token exchange

cmd/hermes/          — CLI entry point
  main.go            — Cobra root command setup

internal/cli/        — CLI command implementations
  root.go            — Root command, global flags
  groups.go          — hermes groups [list|create|update]
  types.go           — hermes types [list|create|update|delete]
  notifications.go   — hermes notifications [send|status]
  auth.go            — hermes auth [token]
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
| `hermes inbox listen` | `--tenant-id`, `--user-id`, `--centrifugo-url` | Listen for real-time inbox notifications |

## Inbox Listen Flow

The `hermes inbox listen` command simulates a user connecting to Centrifugo for real-time notifications.

### Steps

1. **Get unified JWT** — calls `POST /v1/auth/token` with `--tenant-id` and `--user-id` via the SDK. This returns a JWT that works for both API auth and Centrifugo WebSocket auth (signed with the shared `HERMES_JWT_SECRET`).
2. **Connect to Centrifugo** — uses `centrifugal/centrifuge-go` to open a WebSocket connection to `--centrifugo-url`, authenticating with the JWT from step 1.
3. **Subscribe** — subscribes to the `user#{userID}` channel (where `userID` is the internal Hermes user ID from the JWT `sub` claim).
4. **Print events** — prints each incoming event to stdout.
5. **Run until interrupted** — blocks on SIGINT/SIGTERM for graceful disconnect.

### Additional Flags

| Flag | Env Var | Required | Description |
|------|---------|----------|-------------|
| `--centrifugo-url` | `HERMES_CENTRIFUGO_URL` | Yes | Centrifugo WebSocket endpoint |

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

For the inbox listen feature, the CLI performs a single token exchange:
- API key -> unified JWT (via admin service `POST /v1/auth/token`)

The unified JWT is signed with `HERMES_JWT_SECRET` (HS256) and works for both Hermes API auth and Centrifugo WebSocket auth. Centrifugo is configured with the same secret (`token_hmac_secret_key`), so no separate Centrifugo token endpoint is needed.

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
Unit test the token exchange and Centrifugo client construction:
- Single `httptest.Server` for admin (token exchange)
- Verify the returned JWT is passed correctly to the Centrifugo client constructor
- Full WebSocket integration test behind `integration` build tag if desired

No new infrastructure required — all unit tests use `httptest`.

## Known Limitations

- No `hermes groups get` or `hermes types get` by slug/ID. Users must use `list` to find IDs for `update` commands. Single-resource lookup can be added when the admin API exposes it.
- For type-based sends (`--type`), `--channels` overrides are not validated against which channels have templates defined. The server accepts the request but channels without templates will be skipped by the router.
