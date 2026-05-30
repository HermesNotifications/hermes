# Hermes Load Testing

End-to-end load-testing system for the Hermes notification platform. Scales from a local docker-compose smoke test to a 100k+ VU cluster run via the same scenario code.

**Design spec:** [`docs/superpowers/specs/2026-04-17-load-testing-design.md`](../docs/superpowers/specs/2026-04-17-load-testing-design.md)

## Quick start — local

Requires `make infra-up` plus Hermes services running (admin, inbox, send, Centrifugo).

```bash
make loadseed                              # seed default dataset (10 tenants × 10k users)
make loadtest-local SCENARIO=send TARGET_RPS=100 DURATION=30s
# → artifacts/<run_id>/summary.json
# → Grafana at http://localhost:3001
make loadtest-local-clean                  # teardown + cleanup seed
```

## Scenarios

| Scenario | Purpose | Key env |
|---|---|---|
| `send` | Write-path capacity | `TARGET_RPS`, `DURATION`, `CHANNEL_WEIGHTS` |
| `inbox-mixed` | WS + REST read path + driving send | `VUS`, `SEND_RPS`, `POLL_RPS`, `DURATION` |
| `soak` | Long-duration stability | `VUS`, `SEND_RPS`, `POLL_RPS`, `DURATION` (default 4h) |

All scenarios honor `RUN_ID`, `INSTANCE_ID`, `INSTANCE_COUNT` (the last two set by `k6-operator`), `ADMIN_URL`, `SEND_URL`, `INBOX_URL`, `CENTRIFUGO_URL`, `HERMES_JWT_SECRET`.

## Cluster runs

One-time install:

```bash
aws eks update-kubeconfig --name <staging-cluster>
make loadtest-k8s-install
```

Per run:

```bash
LOADSEED_IMAGE=ghcr.io/hermes-notifications/loadseed:latest \
make loadtest-k8s SCENARIO=inbox-mixed PARALLELISM=10 VUS=50000 DURATION=30m
```

Linear scale-out: double `PARALLELISM` to double the load. The scenario code sees `INSTANCE_COUNT` and divides its per-pod rate accordingly.

## Metrics

Generator-side metrics go to the dedicated `loadtest` Prometheus. Hermes service-side metrics continue to flow to Datadog. Correlate by `run_id` (appears as a metric tag on the Prom side and as the `X-Load-Test-Run-Id` trace tag on the DD side).

Custom k6 metrics:

- `send_ack_latency` — `POST /v1/send` ack latency
- `ws_connect_latency`, `ws_connections_opened`, `ws_connections_closed`, `ws_connection_drops` (active = opened − closed)
- `ws_push_e2e_latency` — send-ack → WS push received (headline e2e metric)
- `inbox_list_latency`

## Non-goals (v1)

No SMS scenarios. No multi-region generators. No chaos/failure injection. No production-target runs.

## Files

- `scenarios/*.js` — k6 scenario entrypoints
- `lib/*.js` — shared helpers (seed, auth, metrics, payloads, Centrifugo WS)
- `k8s/` — namespace, TestRun template, Helm values for Prom+Grafana+k6-operator
- `dashboards/` — Grafana JSON
- `scripts/` — `run-local.sh`, `run-k8s.sh`
- `../cmd/loadseed/` — Go seeder (direct-to-DB inserts)
