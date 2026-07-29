# CLI Reference

The `hermes` command (`cmd/hermes`, logic in `internal/cli`) is an admin CLI for managing Hermes
resources over the Admin API, plus an interactive real-time inbox viewer. It talks to Hermes
through the Go client in `pkg/client`.

## Install

```bash
go install github.com/hermes-notifications/hermes/cmd/hermes@latest
# or from a checkout:
go install ./cmd/hermes
```

## Configuration

Global flags (available on every command):

| Flag | Env | Default | Purpose |
|---|---|---|---|
| `--url` | `HERMES_URL`, `HERMES_ADMIN_URL` | `http://localhost:8888` | Base URL (the k3d ingress by default). |
| `--api-key` | `HERMES_API_KEY` | _(none — required)_ | Admin API key. |
| `-o`, `--output` | — | `table` | Output format: `table` or `json`. |

```bash
export HERMES_URL=http://localhost:8888
export HERMES_API_KEY=hms_dev_key_xxxxxxx_yyyy
```

## Commands

### `hermes categories` — subscription categories
`list`, `create`, `update`, `delete`.

```bash
hermes categories list
hermes categories create --slug billing --name "Billing" --channels email,inbox --default-state on
hermes categories update --id sct_xxx --name "Billing & Invoices"
hermes categories delete --id sct_xxx
```
`--default-state` is one of `on`, `off`, `required`.

### `hermes templates` — notification templates
`list`, `create`, `update`, `delete` (per-channel content: email subject/body, SMS body,
inbox title/body).

### `hermes notifications` (alias `notif`) — send & inspect
```bash
# Send a templated notification
hermes notifications send \
  --organization-id <uuid> --user-id <external-id> \
  --template welcome --data '{"name":"Alice"}'

# Send direct content (no template)
hermes notifications send \
  --organization-id <uuid> --user-id <external-id> \
  --title "Hello" --body "Your order shipped" \
  --channels email,inbox

# Inspect status + event timeline
hermes notifications status --id <notification-id>
```
Other flags: `--email`/`--phone` (recipient overrides), `--action-url`, `--action-label`,
`--idempotency-key`. `--organization-id` and `--user-id` are required.

### `hermes auth` — token exchange
Exchange an organization + user identifier for a Hermes JWT (the same flow your backend uses for the
read-path APIs and Centrifugo).

### `hermes apikeys` — API key management
Create, list, and revoke API keys. A newly created key's raw secret is shown **once** — store it
immediately.

### `hermes inbox` — real-time inbox
- `hermes inbox listen --organization-id <uuid> --user-id <id>` — stream live notifications to the
  terminal (exchanges a JWT, subscribes to the user's Centrifugo channel `user#<internal-id>`).
- `hermes inbox open --organization-id <uuid> --user-id <id>` — full interactive TUI (Bubble Tea) with
  live updates.

Both default the WebSocket URL to `<base-url>/realtime/connection/websocket`; override with
`--ws-url` / `HERMES_WS_URL` (and `--inbox-url` / `HERMES_INBOX_URL` for `open`).

## Operational CLIs

These are separate binaries (not subcommands of `hermes`), usually run via `make` — see
[services.md](services.md):

| Make target | Binary | Purpose |
|---|---|---|
| `make migrate` | `cmd/migrate` | Apply database migrations |
| `make seed` | `cmd/seed` | Insert a dev API key |
| `make cleanup` | `cmd/cleanup` | Delete old `notification_events` |
| `make loadseed` | `cmd/loadseed` | Generate the load-test dataset |
| `make openapi` | `cmd/openapi` | Regenerate the OpenAPI specs |
| `make dispatchbench` | `cmd/dispatchbench` | Sweep dispatch worker concurrency and prefetch. Requires `make infra-up`; takes `BACKENDS=postgres\|dynamo`. Results live in [loadtest/](loadtest/) |
