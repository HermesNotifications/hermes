# Hermes Observability

Open-source full-stack monitoring for Hermes: **metrics**, **traces**, and **logs** via the **LGTM stack** (Loki, Grafana, Tempo, Prometheus) with **OpenTelemetry** as the instrumentation API.

Phase 1 runs the OSS stack **alongside** Datadog — everything is dual-emitted so we can validate parity before cutting over.

## I want to…

| Goal | Start here |
|---|---|
| **Understand the stack** | [architecture.md](architecture.md) |
| **Add a metric, trace, or log to a service** | [instrumentation-guide.md](instrumentation-guide.md) |
| **Look up what a metric means** | [metrics-reference.md](metrics-reference.md) |
| **Check naming / label conventions** | [semantic-conventions.md](semantic-conventions.md) |
| **Find a dashboard** | [dashboards.md](dashboards.md) |
| **Handle a firing alert** | [runbooks/](runbooks/) |
| **Deploy, upgrade, or debug the stack** | [operations.md](operations.md) |
| **Run it locally (k3d/Tilt)** | [local-dev.md](local-dev.md) |
| **Migrate a service off dd-trace-go** | [migration-guide.md](migration-guide.md) |
| **Understand why a decision was made** | [adr/](adr/) |

## The one-paragraph version

Every Hermes service initializes OpenTelemetry at startup (`internal/observability.Init`). Traces, metrics, and OTLP logs go out as OTLP/gRPC to the **OTel Collector** (gateway pattern) in the `observability` namespace. The Collector fans out to **Tempo** (traces), **Prometheus** (metrics via remote-write), **Datadog** (all three, for parity during Phase 1), and optionally **SigNoz** when an environment enables that exporter. Container stdout is scraped by the **Alloy** DaemonSet and shipped to **Loki**; a small `slog.Handler` injects `trace_id` / `span_id` into every log line so Grafana can pivot from a log to its trace and back. Alertmanager runs with rules codified but **routing is deliberately silent** — Phase 2 wires Slack/PagerDuty once signal-to-noise is tuned.

## Phase status

- **Phase 1 (current):** dual-emit to OSS + Datadog, local PVs for Prom/Loki/Tempo, 100% trace sampling, silent alert routing.
- **Phase 2 (future):** S3-backed storage via Crossplane, HA replicas, tail-based sampling, live alert destinations, Datadog removal.
