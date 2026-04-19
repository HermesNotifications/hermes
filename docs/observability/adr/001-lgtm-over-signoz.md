# ADR-001: LGTM stack over SigNoz

**Date:** 2026-04-19
**Status:** Accepted

## Context

Hermes is introducing open-source full-stack monitoring to **supplement Datadog** (Phase 1) and set up a **migration path off Datadog** (Phase 2). Two credible open-source options emerged:

1. **LGTM stack** — Loki (logs) + Grafana (UI) + Tempo (traces) + Prometheus (metrics), coordinated by the OpenTelemetry Collector.
2. **SigNoz** — a single-product APM platform backed by ClickHouse, positioned as a direct Datadog replacement.

## Decision

**LGTM stack.**

## Consequences

### Why not SigNoz

- **Operational maturity of the backend.** ClickHouse is powerful but operationally heavier than the LGTM components — tuning merges, disk sizing, cluster mode, replication all require ClickHouse expertise we don't have in-house. LGTM's Prometheus/Loki/Tempo are each individually battle-tested in environments much larger than ours.
- **Ecosystem of integrations.** NATS exporter, postgres-exporter, redis-exporter, kube-state-metrics, node-exporter — all written against Prometheus scrape conventions. SigNoz consumes OTLP only; infra metrics would need a secondary pipeline.
- **Mix-and-match.** If Loki underperforms for our log volume, we swap it for Elastic without touching metrics or traces. SigNoz is monolithic — one underperforming subsystem forces a full platform change.
- **Operator familiarity.** The team already uses Grafana for load test dashboards (`loadtest/dashboards/load-test.json`). Reusing that muscle memory for production is a real productivity win.
- **Project age.** SigNoz (founded 2021) is younger than Grafana Labs' OSS components (Prometheus 2012, Grafana 2014, Loki 2018, Tempo 2020). For a system on the critical path, older == fewer surprises.

### What we give up

- **Single-product UX.** SigNoz offers a more unified APM experience out of the box. With LGTM we're assembling a stack and living in Grafana's "Explore" views rather than a purpose-built APM UI. Grafana's unified alerting and correlated views close most of this gap.
- **OTel-first identity.** SigNoz brands itself as OTel-first; LGTM has OTel compatibility but isn't identity-bound to it. Practically identical — OTel Collector sits in front of LGTM too.

### Reversibility

If this decision is wrong, migration **from** LGTM **to** SigNoz is low-cost: the instrumentation (OTel SDK) is shared. The OTel Collector config gets a new exporter block. No app-side changes.

## Alternatives considered

- **Keep Datadog only** — rejected for cost, lock-in, and multi-cloud portability goals.
- **Pure Prometheus + Jaeger + EFK** — rejected in favor of LGTM's tighter integration and shared Grafana UI.
- **Managed services (Grafana Cloud, AWS AMP/AMG)** — rejected for Phase 1 per the "self-hosted, in-cluster" decision; revisitable in Phase 2 if operational load is higher than expected.
