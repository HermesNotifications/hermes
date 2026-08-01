# AGENTS.md

## Purpose and sources of truth

Hermes is an event-driven notification platform: a Go monorepo of API services,
delivery workers, and CLI tools, with a Next.js admin portal and TypeScript SDKs.

Before changing code, read [CLAUDE.md](CLAUDE.md) and the documentation relevant to the
area you are changing. `CLAUDE.md` carries detailed architecture and observability guidance;
the human documentation lives in [docs/](docs/README.md).

When sources disagree, use this order:

1. The code and tests.
2. The relevant documentation in `docs/`.
3. `CLAUDE.md`.
4. This file.

Keep work within the current worktree. Use relative paths and do not access or modify the
repository's main checkout.

## Repository map

- `cmd/` — service and one-shot-tool entry points.
- `internal/` — service packages plus shared config, auth, messaging, models, observability,
  storage, NATS contracts, and IDs.
- `internal/store/` — the shared Postgres-backed persistence implementation.
- `migrations/` — SQL migrations.
- `api/` — generated OpenAPI artifacts and the hand-written AsyncAPI contract.
- `tests/e2e/` — tagged end-to-end tests.
- `web/admin/` — Next.js admin portal.
- `sdks/typescript/` — pnpm-workspace TypeScript SDKs.
- `deploy/`, `charts/`, and `infra/` — Kubernetes, Helm, GitOps, and cloud infrastructure.

## Local workflows

Use the smallest relevant command first:

```bash
make test                         # Go unit tests; no infrastructure
make lint                         # Go linting
make build-admin                  # Build one service (substitute a service name)
make test-integration             # Requires Postgres, NATS, and Redis
make test-e2e                     # Requires Postgres, NATS, and Redis
make infra-up                     # Start the Docker Compose test infrastructure
make verify                       # Full static/local verification gate
make openapi-check
make asyncapi-check
```

For the full local cluster and hot reload, use `make dev-up` (k3d + Tilt). For a quicker
integration-test setup, use `make infra-up`, `make migrate`, `make seed`, and `make build`.
`make help` lists every target.

The root `package.json` pins pnpm via Corepack. Preserve that version and use pnpm for the
admin portal and TypeScript SDKs. Relevant checks include:

```bash
pnpm --filter "@hermes/admin" test
pnpm --filter "./sdks/typescript/packages/*" --parallel run --if-present test
```

## Implementation conventions

- Keep the Send service thin: it authenticates, applies idempotency, and publishes to NATS.
  Dispatch persists notifications, resolves templates/channels, and fans out to workers.
- A service owns its narrow store interface. For a new handler, add the interface method,
  implement it in `internal/store/postgres`, update that service's `testutil_test.go` mock,
  and test the handler with `httptest`.
- Keep unit tests infrastructure-free. Integration and E2E tests must use the `integration`
  build tag and run against real Postgres, NATS, and Redis.
- Status is monotonic: `pending` → `sent` → `delivered` → `read` → `archived`. Do not add a
  path that regresses it.
- NATS messages in `internal/nats/` are cross-service contracts. When changing one, update
  `api/async/asyncapi.yaml` and run `make asyncapi-check`.
- When changing a public HTTP API, regenerate/validate OpenAPI with the relevant `make openapi`
  command and keep generated artifacts in sync.
- Configuration is environment-based and uses the `HERMES_` prefix. See
  [docs/configuration.md](docs/configuration.md) rather than inventing configuration paths.
- Match surrounding Go and TypeScript style. For TypeScript tests, use typed fakes instead of
  module mocks; Vitest alone does not type-check tests.

## Observability, operations, and documentation

- New services must initialize `internal/observability.Init` and follow
  [observability semantic conventions](docs/observability/semantic-conventions.md). Never add
  unbounded-cardinality metric labels.
- An alert rule requires a matching runbook in the same change.
- For a durable architecture decision (datastore, messaging/auth contract, or another costly
  reversal), create or update an ADR in `docs/adr/` in the same change. Clarify an existing ADR
  in place; create a superseding ADR for a substantive reversal.
- Update user-facing, API, and operational documentation alongside behavior changes.
- Do not commit secrets, generated local state, or `.env` values.

## Before handing off

Run focused tests and formatting/linting appropriate to the change, then report exactly what
ran and what remains unverified. For changes spanning multiple layers, prefer `make verify`;
run infrastructure-backed tests when the change touches storage, messaging, caching, or the
delivery pipeline.
