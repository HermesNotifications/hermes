# Development

This guide gets you from a fresh checkout to a running Hermes stack and a productive edit loop.
For the higher-level picture, read [architecture.md](architecture.md) first; for contribution
mechanics (branches, hooks, CI) see [CONTRIBUTING.md](../CONTRIBUTING.md).

## Prerequisites

- **Go** (see `go.mod` for the toolchain version)
- **Docker** — runs the local infrastructure
- For the full Kubernetes dev loop: **[k3d](https://k3d.io)**,
  **[Tilt](https://docs.tilt.dev/install)**, **kubectl**
- For `make verify`: **[Helm](https://helm.sh/docs/intro/install/) v3** — `make verify-chart`
  renders `charts/hermes/` and checks it against the Go source. It fails rather than skips
  when Helm is missing, because a gate that quietly does not run is what let the chart
  drift in the first place.
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

## Parallel development sandboxes (one namespace per worker)

For several developers or agents working against one k3s cluster without colliding. Each
worker gets a namespace holding Postgres, Redis, NATS with JetStream, Mailpit and
DynamoDB-local:

```bash
make devworker-up                      # namespace hermes-dev-$USER
make devworker-up WORKER=alice         # or name it explicitly

eval "$(make devworker-env WORKER=alice)"
go run ./cmd/migrate/ -database-url "$HERMES_DATABASE_URL" -migrations-path ./migrations
go test ./... -tags=integration -p 1 -count=1     # the whole suite, including e2e

make devworker-list
make devworker-down WORKER=alice       # deletes the namespace and its volumes
```

Verified: two sandboxes run concurrently with distinct addresses, and the full
`-tags=integration` suite passes against one.

**`devworker-env` emits ClusterIPs, not `.svc` names.** Cluster DNS does not resolve from
the host, but on a k3s node the host routes to both the service and pod CIDRs, so these
URLs work from a plain shell with no port-forward. They are node-local — only reachable
from the machine running the cluster.

**NATS is a headless Service**, so its variable carries the *pod* IP and changes if
`nats-0` restarts. Re-run `devworker-env` after a restart.

### What a sandbox deliberately does not contain

- **The Hermes services.** There is no image registry on the cluster and containerd is
  separate from Docker, so service images would need `k3s ctr images import` per worker.
  Every verification task so far needed only infrastructure plus lightweight stand-ins. If
  you do need them: `make docker-<service>`, then
  `docker save hermes-<service>:latest | sudo k3s ctr images import -`, and set
  `imagePullPolicy: IfNotPresent`.
- **NATS TLS.** `base/infra/nats-certificates.yaml` pins `dnsNames` to `nats.hermes`, and
  kustomize does not rewrite strings inside `dnsNames`, so per-namespace certificates need
  per-namespace SANs. Sandboxes run plaintext NATS. To test TLS, issue a leaf with SANs for
  your namespace — this is how
  [ADR 0005](adr/0005-transport-security-for-infrastructure-connections.md) phase 2 was
  verified. Note that phase 4 moved the CA itself to a **`ClusterIssuer`** whose signing Secret
  lives in cert-manager's namespace (`deploy/k8s/pki/`), so a sandbox test should either
  reference that ClusterIssuer or create its own distinctly-named one — do not add a namespaced
  `ca` Issuer to a Hermes namespace, which is the shape `make verify` now rejects.
- **A LoadBalancer of any kind.** Every Service is ClusterIP on purpose: a cluster may
  already publish Postgres or Redis on 5432/6379 via LoadBalancer, and a second one would
  contend for the same address.
- **The `app.kubernetes.io/part-of: hermes` label.** A sandbox carrying it would be
  governed by the Hermes NetworkPolicies, which have no allow rules for it — the pods would
  get DNS egress and nothing else.

### Notes

Namespaces are labelled `hermes.io/devworker=true`, so a stray one is identifiable as
disposable rather than someone's real work. Volumes use the `local-path` provisioner and are
deleted with the namespace.

Sandboxes cost the cluster very little — the whole set requests well under 100m CPU. The
real constraint on parallel work is host CPU from concurrent Go builds, not the cluster.

This mode is also what `make dispatchbench` needs — it sweeps dispatch worker concurrency and
prefetch against a real backend (`BACKENDS=postgres|dynamo`) and is how the numbers in
[loadtest/](loadtest/) were produced. Note its pool constraint: `pool_max_conns` must be at
least the highest worker count in the sweep, or the run measures connection starvation rather
than dispatch throughput.

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

## Inbox demo

A React host application with the embeddable inbox widget in its header, plus the token-minting and
proxying backend it shares with future framework demos.

```bash
make demo-install     # dependencies, and build the workspace SDKs
make dev-demo         # token server on :8899, demo app on :5173
make demo-check       # typecheck, test and build the demo packages (no cluster needed)
```

`make dev-up` starts both automatically as Tilt resources. See
[examples/README.md](../examples/README.md), and
[Embedding the Inbox](embedding-the-inbox.md) for the widget itself.

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
examples/       Integration demos - React host app plus the shared token/proxy server
tests/browser/  Live browser E2E suite (Playwright) for the embedded inbox
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
| Run the TypeScript SDK tests | `make sdk-ts-test` |
| Check the demo packages | `make demo-check` |
| Run the live browser E2E suite | `make demo-e2e` (needs `make dev-up`) |
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
