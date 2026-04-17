# Load Testing Strategy — Design

**Date:** 2026-04-17
**Status:** Draft for implementation planning
**Scope:** Send pipeline and inbox (read path + Centrifugo push). SMS is explicitly out of v1.

## Goals

Priorities, in order:

1. **Capacity planning.** Find throughput ceilings and size the stack (EKS nodes, Postgres, NATS, Centrifugo) with confidence.
2. **Soak / stability.** Run sustained mid-load for hours to surface leaks, queue buildup, connection exhaustion, resource drift.
3. **Future-friendly.** Same scenarios must be reusable later for regression guards in CI and pre-launch SLA validation, without rewrite.

**Scale target:** design must scale linearly to 100k+ virtual users and 50k+ RPS. Day-one runs can be smaller, but nothing in the architecture fixes a ceiling.

**Non-goals (v1):**
- Dedicated SMS scenarios (SMS is covered incidentally by the mixed scenario if at all).
- Multi-region load generation.
- Chaos / failure injection during runs.
- Automatic cost reporting per run.
- Production-target runs (staging only).

## Tooling

- **k6 (Grafana)** for all scenarios. JS-scripted, Go runtime, first-class Prometheus remote-write, mature distributed execution via `k6-operator`.
- **`k6-operator`** on Kubernetes for horizontal fan-out. Linear scaling is the `parallelism` field on a `TestRun` CRD — no script changes to double load.
- **Prometheus + Grafana**, deployed dedicated to load runs, as the load-testing observability plane. Hermes services continue to ship to Datadog; the two planes are correlated by `run_id`, not merged.

## Architecture

New top-level directory `loadtest/`:

```
loadtest/
  scenarios/           # k6 JS — one file per workload
    send.js
    inbox-mixed.js
    soak.js
  lib/                 # shared helpers (auth, payloads, seed manifest, WS client, metrics)
    auth.js
    seed.js
    metrics.js
    centrifugo.js
    payloads.js
  k8s/                 # k6-operator TestRun template, namespace, node-selector, RBAC
    testrun.yaml
    namespace.yaml
    prometheus/        # Prom + Grafana install (Helm values or manifests)
  dashboards/          # Grafana JSON, provisioned from a ConfigMap
  docker-compose.loadtest.yml
cmd/
  loadseed/            # Go seeder: builds dataset, emits seed-manifest.json
```

Two entry points, one scenario codebase:

- **Local:** `make loadtest-local SCENARIO=send RPS=500 DURATION=5m` runs `k6 run` against docker-compose / k3d.
- **Cluster:** `make loadtest-k8s SCENARIO=mixed PARALLELISM=10 VUS=50000 DURATION=30m` applies a `TestRun` CRD; `k6-operator` spreads VUs across N pods.

## Scenarios

### 1. `send.js` — write-path capacity

- Executor: `constant-arrival-rate` targeting `TARGET_RPS`.
- Each iteration picks a random seeded tenant → user → template and `POST`s `/v1/send` with that template-ref, a UUID idempotency key, and per-request variance (payload vars, channel override, recipient selection).
- Channel mix parameterized: default 70% inbox, 30% email. No SMS.
- Auth: single shared API key from the seed manifest (API keys are currently global with permissions in Hermes's schema — one is all we need).
- Measures send-API ack latency (`send_ack_latency`) and throughput. Dispatch and worker pressure surfaces in Hermes's own Datadog metrics, correlated via `run_id`.

### 2. `inbox-mixed.js` — realistic read path

Three sub-scenarios in one test, sharing a VU pool:

- **`ws` (`constant-vus`):** `VUS` users open Centrifugo WS connections, subscribe to `user#<id>`, sit idle receiving pushes. Tracks connection success, drops, and pushes received.
- **`poll` (`constant-arrival-rate`, smaller):** a subset periodically `GET`s `/v1/inbox/notifications` with cursor pagination and occasionally `PATCH`es notifications to mark-read. Simulates a user opening the app.
- **Co-runner (`constant-arrival-rate`):** `POST`s `/v1/send` at a configurable RPS targeting the connected users, so WS traffic is realistic and measurable end-to-end.

End-to-end latency is the headline metric: idempotency key = correlation ID, `send` scenario records `(key → sent_at_ns)` in a per-instance shared map, `ws` scenario looks it up on receive and reports `ws_push_e2e_latency`.

### 3. `soak.js` — stability

- Composition of the mixed scenario at ~30% of capacity-run levels.
- `DURATION=4h` default.
- Same metrics plus Hermes server-side memory / FD growth observed through the existing DD dashboards.
- Success = flat resource graphs and no error-rate drift over the run window.

## Data Seeding

Go command at `cmd/loadseed/` builds a deterministic dataset. Seeding goes **direct to Postgres** (not via the admin API), following the same pattern as the existing `cmd/seed/`. Reasons:

- **There is no `POST /v1/users` endpoint** in the admin API — users are only listable. Direct DB is the only way to create them at the scale we need.
- **Speed:** 1 M-row batched `COPY FROM` completes in seconds; 1 M admin-API POSTs would take tens of minutes and stress the very API the real test is measuring.
- **Precedent:** `cmd/seed/main.go` already writes directly to `api_keys`, `user`, `account`.

The seeder reuses the server SDK only for entities that already have POST endpoints and where API coverage is useful (none strictly needed for v1 — everything goes direct to DB).

**Inputs (flags / env):**

| Flag | Default | Purpose |
|---|---|---|
| `--tenants` | 10 | Number of tenants to create |
| `--users-per-tenant` | 10 000 | Users per tenant |
| `--categories-per-tenant` | 3 | Subscription categories per tenant (matches the migration-default shape: `account`, `general`, `marketing`) |
| `--subscriptions-per-category` | 2 | Subscriptions per category |
| `--templates-per-subscription` | 2 | Templates per subscription |
| `--database-url` | `$HERMES_DATABASE_URL` | Postgres connection URL |
| `--admin-url` | `http://localhost:8080` | Admin base URL (used only for warm-up verification) |
| `--hmac-secret` | `$HERMES_API_KEY_HMAC_SECRET` | HMAC secret for API-key hashing |
| `--output` | `seed-manifest.json` | Manifest output path |
| `--cleanup` | false | If set, delete all tenants in the manifest (cascades everything) |

**What it creates:**

1. N tenants (UUIDs via `uuid.New().String()` — Hermes uses UUIDs, not Crockford IDs).
2. **One global API key** with permissions `[notifications:send, templates:manage, tenants:manage]`, written to `api_keys` directly. Raw key captured once, stored in the manifest, used for every send during the run.
3. M users per tenant, inserted via `COPY FROM` for speed. Deterministic `email`, `phone`, and `external_id` derived from `(tenant_index, user_index)` so reruns are reproducible.
4. C subscription categories per tenant (`subscription_categories` table).
5. S subscriptions per category (`subscriptions` table).
6. T templates per subscription (`notification_templates` table) — each with inbox + email bodies populated, SMS empty.

**Idempotency:** re-running with the same manifest is a no-op (`INSERT … ON CONFLICT DO NOTHING`). `--cleanup` issues `DELETE FROM tenants WHERE id = ANY(...)`; FK cascades remove users, and seeded categories/subscriptions/templates are deleted by id (those tables are not tenant-scoped at the DB layer — the manifest tracks every seeded row).

**Manifest shape:**

```json
{
  "seeded_at": "2026-04-17T…",
  "run_seed_id": "…",
  "api_key": "…",
  "tenants": [
    {
      "id": "…",
      "users": ["…"],
      "categories": [
        {
          "id": "…",
          "subscriptions": [
            {
              "id": "…",
              "templates": [{ "id": "…", "channels": ["inbox", "email"] }]
            }
          ]
        }
      ]
    }
  ]
}
```

**k6 consumption:**

- Manifest loaded into a `SharedArray` on init (one copy per process; O(1) per-VU access, required at 100k+ VUs).
- Helpers in `lib/seed.js`: `pickTenant()`, `pickUser(tenant)`, `pickTemplate(tenant)` (walks `tenant.categories[*].subscriptions[*].templates` to choose uniformly across all templates for the tenant).
- Helpers in `lib/auth.js`: `adminHeaders()` (shared bearer), `jwtFor(user)` (HMAC-signs a JWT with the same secret the target env uses — passed in via env var).

**Per-request variance** generated at runtime (not pre-seeded): notification payload variables, UUID idempotency keys, recipient subset choice, template-vs-inline toggle.

**Local / cluster parity:** same binary. Local targets `http://localhost:8080`; cluster mode runs as a pre-test `Job` targeting the in-cluster admin service, writing the manifest to a PVC or ConfigMap shared with runner pods.

## Scaling Model

### Per-process shape (k6 script)

- `send.js`: `constant-arrival-rate`. Set `TARGET_RPS` and `preAllocatedVUs`. Rate-driven — iteration rate is held steady regardless of latency.
- `inbox-mixed.js`: `constant-vus` for the WS scenario (persistent connections), `constant-arrival-rate` for poll and co-runner.
- `soak.js`: composition of the mixed scenarios at reduced levels, longer duration.

### Horizontal fan-out (`k6-operator`)

- `TestRun.spec.parallelism = N` creates N identical runner pods.
- Operator injects `INSTANCE_ID` (0…N-1) and `INSTANCE_COUNT` (N) env vars into each pod.
- Each pod receives `TARGET_RPS / N` and `VUS / N`. Linear scale-out: 2× parallelism = 2× load, zero script changes.
- Ceilings are (a) Hermes capacity — which is what we're measuring — and (b) the node pool behind the generators.

### Sharding the seed manifest across pods

- WS scenario partitions the user list: pod *i* connects users `[i * M/N, (i+1) * M/N)`. No overlap.
- Send and poll scenarios don't need partitioning; they pick randomly from the full pool using per-VU RNG seeded with `INSTANCE_ID + __VU` so distinct pods don't degenerate to the same sequence.

### Pinning

- Dedicated EKS node pool `loadtest-generators` with a taint.
- `TestRun` pod spec tolerates the taint and node-selects onto the pool.
- Hermes pods never schedule onto load-test nodes; load-test pods never schedule onto Hermes nodes.
- Explicit resource requests/limits per runner (start: 2 vCPU / 4 GiB), so `parallelism` maps directly to capacity planning for the generator pool.

### Local mode degeneracy

Single `k6 run` with `INSTANCE_ID=0 INSTANCE_COUNT=1`. Sharding math collapses to the full range. Same script, same helpers, no branching.

## Metrics & Observability

### Generator-side → Prometheus

`k6 run --out experimental-prometheus-rw …` pushes all built-in metrics via remote-write. Every metric tagged with `scenario`, `run_id`, `instance_id`, `parallelism` for precise filtering and run-to-run comparison.

Custom metrics in `lib/metrics.js`:

| Metric | Type | Meaning |
|---|---|---|
| `send_ack_latency` | Trend | `POST /v1/send` start → 2xx |
| `ws_connect_latency` | Trend | Centrifugo WS handshake |
| `ws_push_e2e_latency` | Trend | Send-ack → push arrival on recipient WS (headline metric) |
| `inbox_list_latency` | Trend | `GET /v1/inbox/notifications` by page depth |
| `ws_connection_active` | Gauge | Current open WS count |
| `ws_connection_drops` | Counter | Unexpected disconnects |

### Server-side correlation (Datadog, unchanged)

- No new Hermes instrumentation for v1 — existing DD coverage is sufficient.
- Every load-test request carries `X-Load-Test-Run-Id: <run_id>`. This is promoted to a trace tag, letting DD queries slice to a single run.

### Deployment

- **Local:** Prometheus + Grafana added to `docker-compose.loadtest.yml`. k6 remote-writes to `http://prometheus:9090/api/v1/write`. Grafana exposed at `localhost:3001` with the load-test dashboard pre-provisioned.
- **Cluster:** Prometheus + Grafana deployed into the `loadtest` namespace via a trimmed Helm release. Dashboard JSON from `loadtest/dashboards/` mounted via ConfigMap and provisioned on Grafana startup.

### Dashboard

One main Grafana dashboard, filter by `run_id`:

- Throughput (actual vs target RPS)
- p50 / p95 / p99 latency panels per custom metric
- Error rate (`http_req_failed` rate, WS drop rate)
- End-to-end push latency distribution
- VU count vs target, connection health
- Generator resource usage (CPU / memory per pod)

## Run Lifecycle & Results Capture

### `run_id`

Short hex, generated at run start by the Makefile target. Passed into:
- k6 env (`RUN_ID`), echoed in logs and every metric tag.
- Every request as `X-Load-Test-Run-Id`.
- The output artifact directory: `artifacts/<run_id>/`.

### Results per run

- `handleSummary` in each scenario writes `summary.json` (thresholds pass/fail + percentile summary) to `artifacts/<run_id>/summary.json`.
- Makefile prints a Grafana URL with the time range and `run_id` filter pre-applied.
- CI attaches `summary.json` and the Grafana URL to the workflow run.

### Thresholds (pass/fail)

Defined in each scenario, overridable by env. Initial defaults (calibrated after first capacity run):

- `send_ack_latency: p(99)<200ms`
- `http_req_failed: rate<0.01`
- `ws_push_e2e_latency: p(95)<1000ms`
- `ws_connection_drops: rate<0.001 per connection-hour`

Threshold breach fails k6's exit code, failing the CI job.

## Infra

### Local mode

- `docker-compose.loadtest.yml` extends `docker-compose.yml` with Prometheus, Grafana, and a one-shot `loadseed` service.
- `make loadtest-local SCENARIO=send RPS=500 DURATION=5m`:
  1. `docker compose -f docker-compose.yml -f docker-compose.loadtest.yml up -d` (idempotent).
  2. Run `cmd/loadseed` if `seed-manifest.json` absent.
  3. Exec `k6 run loadtest/scenarios/$SCENARIO.js` with env (`TARGET_RPS`, `DURATION`, `RUN_ID`, `INSTANCE_ID=0`, `INSTANCE_COUNT=1`, `SEED_MANIFEST`).
  4. Write `artifacts/<run_id>/summary.json`, print Grafana URL.
- `make loadtest-local-clean`: tears Prom/Grafana down, runs `loadseed --cleanup`.

### Cluster mode

Target: separate pre-prod EKS cluster running a staging Hermes deployment, with a dedicated `loadtest` namespace.

One-time setup per cluster:

1. Install `k6-operator` (Helm) into the `loadtest` namespace.
2. Install Prometheus + Grafana (Helm, trimmed values) into `loadtest`. Dashboard provisioned from a ConfigMap built out of `loadtest/dashboards/`.
3. Create `loadtest-generators` node pool with a taint; RBAC + taint tolerations applied via `loadtest/k8s/`.

Per-run flow — `make loadtest-k8s SCENARIO=mixed PARALLELISM=10 VUS=50000 DURATION=30m`:

1. `kubectl apply` a `Job` running `loadseed` against the target admin service. Manifest written to a PVC (or ConfigMap when small) shared with runner pods.
2. `kubectl apply` a `TestRun` built from `loadtest/k8s/testrun.yaml` via `envsubst`, with scenario, `parallelism`, and env-var overrides substituted in.
3. Tail `k6-operator` logs until completion. Merge per-pod `summary.json` files into `artifacts/<run_id>/`.
4. Print Grafana URL scoped to `run_id`.

`make loadtest-k8s-clean` deletes the `TestRun`, runs the cleanup `Job`, optionally uninstalls Prom/Grafana if `--full` passed.

### CI workflow

`.github/workflows/loadtest.yml`:

- **`workflow_dispatch`** with inputs `scenario`, `parallelism`, `vus`, `duration`, `target_env` (staging only). Runs cluster-mode end-to-end.
- **`schedule` cron** nightly soak: `scenario=soak`, `duration=4h`, low `parallelism`, against staging. Summary + Grafana URL posted to a Slack webhook; `summary.json` attached to the workflow run.
- **PR smoke (opt-in, label `load-test-smoke`):** local-mode `send` at 200 RPS / 60s against a k3d Hermes. Threshold breach fails the check. Kept small; not on by default.

### Security & credentials

- Staging admin bootstrap token in a k8s Secret `hermes-loadtest-bootstrap`; read only by the `loadseed` Job's ServiceAccount.
- CI workflow authenticates to AWS via short-lived OIDC role assumption — no long-lived cluster creds in GitHub.
- Load-test requests are tagged `X-Load-Test-Run-Id`, making them trivially distinguishable from real traffic in logs and traces.

## Open Items For Implementation Plan

The following are intentional open questions to resolve during implementation, not in this spec:

- Choice of Prometheus + Grafana installer: `kube-prometheus-stack` (heavy, batteries-included) vs hand-rolled minimal manifests. Start minimal.
- Whether the seed manifest lives on a PVC or a ConfigMap in cluster mode (ConfigMap only if size stays under ~1 MiB; likely need PVC at 10 tenants × 10k users).
- Initial node pool sizing for `loadtest-generators` (TBD calibrated after the first capacity run; plan will include an initial guess + re-tune step).
- Exact thresholds — current values are starting points; calibrated from the first capacity run.

## Summary

One scenario codebase (k6 JS), two run modes (local, cluster), horizontally scalable via `k6-operator`'s `parallelism`, seeded deterministically by a small Go tool, observed through a dedicated Prometheus + Grafana stack correlated to Datadog by `run_id`. Capacity and soak are first-class in v1; regression and pre-launch validation layer on later without rework.
