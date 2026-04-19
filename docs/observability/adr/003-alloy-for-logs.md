# ADR-003: Grafana Alloy for log shipping, not the OTel Logs SDK

**Date:** 2026-04-19
**Status:** Accepted

## Context

Hermes services already emit structured JSON logs via Go's `log/slog` to stdout. Kubernetes collects stdout to `/var/log/containers/*.log`. Two options to ship these logs to Loki:

1. **OTel Logs SDK.** Install an `slog.Handler` that emits OTLP logs via the SDK to the Collector, then export to Loki.
2. **Grafana Alloy DaemonSet.** Scrape `/var/log/containers/*.log` on each node, parse the JSON, ship to Loki.

## Decision

**Alloy DaemonSet.**

A small `slog.Handler` is still installed in-app, but only to inject `trace_id` / `span_id` attributes into the JSON (for log↔trace correlation). The handler does NOT do network I/O — output goes to stdout as before.

## Consequences

### Why Alloy

- **Zero app-side network path for logs.** Services just print to stdout; they don't need to know Loki exists. If Loki is down, logs are still on disk and in DD (via the DD Agent DaemonSet, which also reads `/var/log/containers`).
- **Decouples log transport from app lifecycle.** A node-level collector restart doesn't affect app pods.
- **Already-exercised pattern.** `/var/log/containers` scraping is how the DD Agent works today. Alloy does the same thing with a different destination — no new failure mode.
- **No performance impact on services.** OTel Logs SDK buffers in-app; under sustained load that buffer grows and either blocks the app or drops logs. Stdout → disk → scrape pushes that buffer to the node.

### Why not the OTel Logs SDK

- App now holds a network queue of pending logs. Every service carries more state.
- Under partial network outages, apps either drop logs or backpressure handlers. Neither is great.
- OTel Logs SDK is the newest of the three signals — less battle-tested than Traces and Metrics.

### What we give up

- **Single-pipe dream.** A platonic OTel deployment uses one SDK for all three signals. We chose pragmatism.
- **Exact semantic conventions for logs.** OTLP logs have a defined schema; Alloy-to-Loki logs are schema-on-read via the parsing config. Practical difference is small — we set labels and structured metadata explicitly in the Alloy config.

### Log-to-trace correlation

Still works end-to-end because the `slog.Handler` in `internal/observability/slog.go` injects `trace_id` and `span_id` as JSON fields on every log line written with a context that carries an active span. Alloy's parsing config promotes those fields as structured metadata, and Grafana's Loki datasource wires a "View trace" button on any log line that has a `trace_id`.
