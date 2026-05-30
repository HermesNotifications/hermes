# Development

This guide gets you from a fresh checkout to a running Hermes stack and a productive edit loop.
For the higher-level picture, read [architecture.md](architecture.md) first; for contribution
mechanics (branches, hooks, CI) see [CONTRIBUTING.md](../CONTRIBUTING.md).

## Prerequisites

- **Go** (see `go.mod` for the toolchain version)
- **Docker** — runs the local infrastructure
- For the full Kubernetes dev loop: **[k3d](https://k3d.io)**,
  **[Tilt](https://docs.tilt.dev/install)**, **kubectl**
- For the admin portal: **pnpm**

## Two ways to run locally

Hermes supports two local workflows. Most service development uses the **k3d + Tilt** path; the
**Docker Compose** path is lighter and is what the integration tests expect.

### Option A — Kubernetes dev loop (k3d + Tilt) — recommended

A full local cluster with all services, infra, and hot reload:

```bash
make dev-up          # create the k3d cluster (hermes-dev) and start Tilt
```

Tilt (driven by the repo's `Tiltfile`) builds each service, brings up Postgres / NATS / Redis /
Centrifugo / Mailpit, runs migrations and the dev seed, and live-reloads a service's pod when you
change its code.

| Address | What |
|---|---|
| `http://localhost:8888` | Ingress — routes to backends by path |
| `http://localhost:3000` | Admin portal (Next.js) |
| `http://localhost:10350` | Tilt dashboard |

Useful targets:

```bash
make dev-status              # cluster + pod + service status
make dev-logs SERVICE=admin  # tail one service's logs
make dev-psql                # psql into the dev database
make dev-migrate             # re-run migrations (tilt trigger migrate)
make dev-ui                  # open the Tilt dashboard
make dev-down                # tear everything down
make dev-restart             # dev-down + dev-up
```

Add the observability stack (LGTM) with `tilt up -- --observability`, or Datadog dual-emit with
`--datadog` (needs `DD_API_KEY`). See
[observability/local-dev.md](observability/local-dev.md).

### Option B — Docker Compose + local binaries

Lighter weight, and the setup the test suite targets:

```bash
make infra-up        # Postgres, NATS (JetStream), Redis via docker compose
make migrate         # apply migrations
make seed            # insert a dev API key
make build           # build all services into bin/<service>/service
```

Then run individual services from `bin/`, or just use this mode to run integration tests
(see [testing.md](testing.md)). `make infra-down` stops the infrastructure.

## Building

```bash
make build            # all services (instrumented via Orchestrion)
make build-admin      # a single service
make FAST=1 build     # skip Orchestrion for faster iteration
make docker-admin     # build a service's production Docker image
```

Binaries land in `bin/<service>/service`. `FAST=1` swaps the Orchestrion-wrapped build for a
plain `go build` — handy for quick local loops where you don't need tracing instrumentation.

## Admin portal

```bash
make admin-install    # pnpm install in web/admin
make dev-admin        # next dev on :3000
```

See [web/admin/README.md](../web/admin/README.md).

## Project layout

```
cmd/            Service & tool entry points (admin, send, dispatch, worker-*, inbox, user,
                migrate, seed, cleanup, loadseed, openapi, hermes)
internal/       Shared packages — store, auth, nats, messaging, config, models, id, observability,
                and one package per service (dispatch, send, admin, inbox, userservice, …)
pkg/client/     Public Go API client used by the CLI
migrations/     golang-migrate SQL migrations
api/            Generated OpenAPI specs + hand-written AsyncAPI spec
deploy/         Dockerfiles, k3d config, Kustomize bases/overlays, ArgoCD/Kargo, observability
infra/          Terraform (AWS) and Crossplane
charts/hermes/  Helm chart
web/admin/      Next.js admin portal
loadtest/       k6 load-testing system
docs/           This documentation
tests/e2e/      End-to-end tests
```

## Common tasks

| Task | Command |
|---|---|
| Run unit tests | `make test` |
| Run integration tests | `make test-integration` (needs `make infra-up`) |
| Run e2e tests | `make test-e2e` (needs `make infra-up`) |
| Lint | `make lint` |
| Regenerate OpenAPI specs | `make openapi` |
| Validate the AsyncAPI spec | `make asyncapi-check` |
| Install git hooks | `make hooks` |
| List every target | `make help` |

## Adding a feature: the grain of the codebase

When you add a handler, follow the existing pattern: declare the method on the service's store
interface, implement it in `internal/store/postgres`, add it to the mock in that service's
`testutil_test.go`, and test the handler with `httptest`. New services must call
`internal/observability.Init` in `main.go` and follow the
[semantic conventions](observability/semantic-conventions.md). A new alert rule must ship with a
matching runbook in the same PR. See [testing.md](testing.md) for the testing details.
