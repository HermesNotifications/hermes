# API Reference

Hermes exposes three public HTTP APIs plus an asynchronous NATS contract. This page is the map;
for a worked, end-to-end walkthrough (token exchange, creating resources, sending, inbox, real-time)
see the **[Integration Guide](../integration-guide.md)**.

## Authentication

Two modes (details in [architecture.md](../architecture.md#authentication-and-authorization)):

| API | Auth | Header |
|---|---|---|
| **Send** (`/v1/send`) | API key | `Authorization: Bearer hms_…` |
| **Admin** | API key | `Authorization: Bearer hms_…` |
| **Inbox** | JWT | `Authorization: Bearer <jwt>` |
| **User** | JWT | `Authorization: Bearer <jwt>` |

API keys are for your server-to-server backend. JWTs identify an end user and are obtained by
exchanging an organization + user identifier on the Admin API; the same JWT authenticates the Centrifugo
WebSocket connection for real-time push.

## Specs

OpenAPI 3.1 specs are generated from the services' [huma](https://huma.rocks) definitions and
committed under `api/`:

| API | Spec |
|---|---|
| Admin | `api/admin/openapi.yaml` · `.json` |
| Inbox | `api/inbox/openapi.yaml` · `.json` |
| User | `api/user/openapi.yaml` · `.json` |

The asynchronous NATS contract (the four JetStream streams and the WebSocket channel) is a
hand-written AsyncAPI 3 spec at `api/async/asyncapi.yaml`. These mirror the Go message structs in
`internal/nats/messages.go` — see [architecture.md](../architecture.md#messaging-nats-jetstream).

Render a spec locally with any OpenAPI viewer, e.g.:

```bash
npx @redocly/cli preview-docs api/admin/openapi.yaml
```

## Regenerating

The specs are build artifacts — never hand-edit them. Regenerate after changing an API
definition:

```bash
make openapi          # regenerate all OpenAPI specs (cmd/openapi)
make openapi-check    # CI gate: fails if api/ is out of date
make asyncapi-check   # validate the AsyncAPI spec
```

`openapi-check` runs `make openapi` then `git diff --exit-code api/`, so a PR that changes an API
without regenerating will fail CI.

## SDKs

Client/server SDKs are generated from the OpenAPI specs. See the `sdk-*` targets in the
`Makefile` (TypeScript, Python, Java, .NET); the TypeScript packages live under
`sdks/typescript/` and are consumed by the [admin portal](../../web/admin/README.md). SDK naming:
`hermes-server-sdk` (server-to-server) and `hermes-client` (user-facing).

## Sending a notification

```bash
curl -X POST http://localhost:8888/v1/send \
  -H "Authorization: Bearer $HERMES_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "to": { "organization_id": "<uuid>", "user_id": "<external-id>" },
    "template": "welcome",
    "data": { "name": "Alice" }
  }'
```

`http://localhost:8888` is the local k3d ingress; the Send service itself listens on `8088`. You
can also send from the [CLI](../cli.md) (`hermes notifications send`). For the full request/response
shapes and the inbox/user endpoints, see the [Integration Guide](../integration-guide.md) and the
OpenAPI specs.
