# ADR-004: Accepting the loss of Datadog Data Streams Monitoring

**Date:** 2026-04-19
**Status:** Accepted

## Context

Hermes currently uses Datadog's **Data Streams Monitoring (DSM)** on NATS messages. DSM adds lightweight checkpoints to message headers that let Datadog compute end-to-end pipeline latency (from first publish to final consume) as a first-class metric, per pipeline.

The current implementation is in `internal/tracing/nats.go` — the `NATSHeaderCarrier` embeds DSM checkpoints alongside APM trace context.

DSM is a **proprietary Datadog feature**. There is no OpenTelemetry equivalent and no open-source replacement. When the app migrates to OTel, DSM data stops flowing to Datadog.

## Decision

**Accept the loss of DSM.** Replace it with trace-based NATS latency views in Tempo.

## Consequences

### What we lose

- Datadog's dedicated Data Streams tab.
- Pipeline-level p50/p95/p99 latency metrics computed by DSM's aggregation.
- Long-term retention of stream-level statistics in DD.

### What we keep

- **Per-message end-to-end latency via traces.** Every notification carries an OTel trace context across NATS subjects. Tempo's service graph and trace view shows `publish → consume → process` spans with timing. The information is the same; the UI and query model are different.
- **NATS consumer lag metrics.** The nats-exporter exposes `nats_jetstream_consumer_num_pending` — the most operationally important DSM signal. This goes to Prometheus/Grafana.
- **Per-stream throughput, bytes, message counts.** Also via nats-exporter.

### Why not replicate DSM with custom instrumentation

- DSM's value is the backend aggregation, not the header format. Reproducing the aggregation would mean writing our own ClickHouse-like pipeline analysis system.
- The operational questions DSM answers — "where is the bottleneck?" — are answerable with trace search + consumer lag metrics. Less polished UX, same diagnostic power.
- Don't build something because Datadog had it. Build it if we discover we actually need it.

### When this would need revisiting

If, post-migration, we find ourselves frequently asking "what's the end-to-end p99 from send to delivery?" and can't easily derive it from Tempo + Prom, we'd consider:

1. A custom **latency-at-each-stage metric** emitted by each service (cheap, ~3 lines per stage).
2. A **TraceQL metrics generator rule** in Tempo that extracts stage timings into Prometheus metrics automatically.

Start without, add only when the need is measured.
