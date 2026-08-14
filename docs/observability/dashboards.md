# Dashboards

Seed dashboards committed to `deploy/observability/base/grafana/dashboards/` and loaded into Grafana automatically by the sidecar (ConfigMap with `grafana_dashboard=1` label).

## Seed set

| Dashboard | UID | Purpose |
|---|---|---|
| **Hermes service — overview** | `hermes-service-overview` | Per-service (templated by `$service`): RPS, error rate, p50/p95/p99 latency, Go runtime (goroutines, heap) |
| **Hermes pipeline — overview** | `hermes-pipeline-overview` | End-to-end notifications: sent vs delivered rate, per-channel error rate, NATS consumer lag, template cache hit rate |
| **Hermes infra — NATS / Postgres / Redis** | `hermes-infra` | Infra dependencies: JetStream bytes + lag, Postgres conns + transaction rate, Redis ops/sec + memory |
| **Observability — stack health** | `observability-health` | Self-monitoring: Collector receive rate, exporter failures, Prometheus scrape success, Loki/Tempo ingestion rate, alerts firing |

kube-prometheus-stack ships ~20 additional dashboards (nodes, pods, deployments, Prometheus itself) — those are kept at defaults and not documented here.

## Editing dashboards

**Source of truth is git**, not the Grafana UI.

1. Edit the dashboard in Grafana → use "Share → Export → Save to file" to get the JSON.
2. Replace the file in `deploy/observability/base/grafana/dashboards/`.
3. Commit and PR. ArgoCD syncs, the sidecar reloads.

The chart is configured with `allowUiUpdates: false` so UI edits don't persist past a reload — preventing accidental drift.

## Log-to-trace pivot

Every log line with a `trace_id` attribute gets a "View trace" button in Grafana's Loki panel. Wiring is in `deploy/observability/base/grafana/datasources.yaml`:

```yaml
derivedFields:
  - datasourceName: Tempo
    matcherRegex: '"trace_id":"([a-f0-9]+)"'
    name: TraceID
    url: "${__value.raw}"
    datasourceUid: tempo
```

If your logs aren't showing the pivot button:

1. Confirm the log line is JSON and has `"trace_id": "..."` (view raw).
2. Confirm the parent ctx had an active span when the log was written.
3. Check the Grafana Loki datasource config — the regex above should match your JSON shape exactly.

## Adding a new dashboard

1. Author in Grafana, export JSON.
2. Drop the file into `deploy/observability/base/grafana/dashboards/<name>.json`.
3. Add a `configMapGenerator` entry in `deploy/observability/base/grafana/kustomization.yaml` mirroring the existing ones.
4. Add a row to the table above. **Every dashboard listed here should exist; every dashboard in the ConfigMap should be listed here.** CI check coming in Phase 2.

## Which panels drive which alerts

Cross-reference when tuning thresholds:

Rows marked *no panel yet* have an alert and a runbook but nothing to look at while triaging.
Listed rather than omitted, because the gap is the point: an alert whose runbook says "check
the dashboard" and whose dashboard has no such panel wastes the minutes the annotation exists
to save. The queries are given so a panel can be added, or so you can paste them into Explore.

| Alert rule | Panel that shows the signal |
|---|---|
| `ServiceDown` | kube-prometheus-stack default "Pods" dashboard |
| `HighErrorRate` | Hermes service overview → Error rate |
| `HighLatency` | Hermes service overview → Latency p95 |
| `SendIngestionLatency` | Hermes service overview → Latency p95, filtered to `hermes-send` |
| `NATSConsumerBacklogGrowing` | Hermes infra → NATS consumer lag |
| `NATSConsumerBacklogUnbounded` | Hermes infra → NATS consumer lag |
| `HermesWorkerPoolSaturated` | *no panel yet* — `hermes_messaging_inflight / hermes_messaging_workers_limit` |
| `HermesProbeLoss` | *no panel yet* — `hermes_probe_results_total` by `result` |
| `HermesProbeAbsent` | *no panel yet* — same series; alerts on its absence |
| `HermesProbeLatency` | *no panel yet* — `hermes_probe_e2e_duration_seconds` |
| `DBPoolSaturated` | Hermes infra → Postgres active connections |
| `PrometheusTargetDown` | Observability health → Prometheus scrape success rate |
| `DiskPressure` | kube-prometheus-stack default "Persistent Volumes" |
