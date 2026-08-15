# ADR-004: Accepting the loss of Datadog Data Streams Monitoring

**Date:** 2026-04-19
**Status:** Accepted (amended 2026-08-15 — see [Amendment](#amendment-2026-08-15-links-alone-did-not-deliver-this))

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

## Amendment 2026-08-15: links alone did not deliver this

The decision stands — DSM stays gone, and traces remain the replacement. One
assumption in it was wrong, and this records the correction rather than reversing
anything.

**What was assumed.** "Every notification carries an OTel trace context across NATS
subjects. Tempo's service graph and trace view shows `publish → consume → process`
spans with timing."

**What was true.** The first sentence holds and was verified: a sampled production
trace runs unbroken from `POST /v1/send` through both NATS hops to
`hermes-worker-events`, and over an hour of traffic not one `nats.consume` span was
orphaned across ~450k dispatch messages. The NATS propagation in
`internal/observability/nats.go` does its job.

The second sentence did not. The event writer batches, so one flush serves many
notifications and cannot be a child of any of them; it recorded its inputs as
**span links** and started from `context.Background()`. That is correct OTel
semantics, and it relies on the backend walking links to relate the traces. Tempo
does. The backend actually holding this telemetry is SigNoz, which does not — so
every Postgres write recording a notification's final status sat in a trace of its
own, 121 orphan roots per hour. "Did this notification's status get written?" could
not be answered from the notification's trace, which is exactly the class of
question this ADR promised traces would answer.

**Correction.** `eventwriter.flush` keeps its links and stays a root: batch timing
is a batch property and reparenting it onto one arbitrary member would misrepresent
it. Each item additionally gets a short `notification.event.persist` span started
from *its own* originating context and linked back to the flush. The batch keeps one
span; each notification gains the one fact it was missing. Cost is roughly one extra
span per event on the highest-call service in the fleet.

**Carried forward.** Do not assume a backend traverses span links. Where a fan-in
must remain queryable from an individual input's trace, emit a span on that input's
trace as well; treat the link as the batch-level relationship, not as the only one.
