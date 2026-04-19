# ADR-002: OTel Collector as the single fan-out point

**Date:** 2026-04-19
**Status:** Accepted

## Context

During Phase 1 we need telemetry to land in **both** our new OSS backends (Tempo, Prometheus, Loki) **and** Datadog. Three approaches were considered:

1. **Dual SDKs in-app.** Keep `dd-trace-go` running, add OTel SDK alongside. App emits to two agents.
2. **Datadog Agent as OTLP endpoint.** App uses OTel SDK only; DD Agent receives OTLP and also ingests to OSS via its sidecar-like capability.
3. **OTel Collector as fan-out.** App uses OTel SDK only; Collector receives OTLP and fans out to Tempo/Prometheus (OSS exporters) **and** Datadog (via the `datadog` exporter).

## Decision

**Option 3: OTel Collector as the single fan-out point.**

## Consequences

### Why not dual SDKs

- Doubled compiled-in telemetry code, doubled config surface, two sources of bugs.
- When Phase 2 removes Datadog, we have to untangle the second SDK from every service. Deletes a second time.
- Sampling coordination across SDKs is hard — you can end up double-sampling or missing traces.

### Why not the DD Agent as OTLP endpoint

- The DD Agent's OTLP receiver ships everything to Datadog — it isn't a fan-out point. We'd still need a second agent/collector for the OSS side.
- When Datadog is removed (Phase 2), the OTLP endpoint disappears with the DD Agent, forcing an infrastructure change on top of a removal.

### Why the Collector wins

- **Single fan-out point.** All backend changes — adding Tempo, adding SigNoz if we ever switch, removing Datadog — are a config edit to `deploy/observability/base/otel-collector/values.yaml`. Zero app changes.
- **OTel is the app-side identity.** Forever. Backends come and go; the SDK stays the same.
- **Built-in processing.** memory_limiter, batch, resource injection, cardinality stripping, filtering — all handled at the Collector, not in 9 services.
- **Operational isolation.** If the Collector has a bad deploy, apps don't — they emit OTLP, and if no one receives, they back off. Apps never link against backend libraries directly.

### What we give up

- **One more component to operate.** The Collector itself can fail, back-pressure, OOM. Mitigations: 2+ replicas, `memory_limiter` processor, self-monitoring dashboard (`observability-health`).
- **Direct DD-to-app debugging.** Before, dd-trace-go logged agent errors in-app. Now those errors are on the Collector side. Operators need to know to look there.

## Migration path

Phase 1 exit state: every service talks OTLP to the Collector; `dd-trace-go` is removed from `go.mod`; Datadog data flows via Collector's `datadog` exporter.

Phase 2 cutover: delete the `datadog` exporter block from the pipeline configs, `ExternalSecret` for `DD_API_KEY`, and the DD Agent DaemonSet. The app stack is unchanged.
