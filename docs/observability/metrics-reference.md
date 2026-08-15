# Metrics Reference

Every metric Hermes emits from its own code, what it means, and where it is read.

This file exists because its absence had a cost. The pipeline dashboard shipped with
panels querying `hermes_notifications_sent_total`, `hermes_notifications_delivered_total`,
`hermes_deliveries_failed_total` and `hermes_template_cache_hits_total` — four metric
names no Go file has ever emitted. Three of its four panels drew nothing, and had done
since they were written. A blank Grafana panel is indistinguishable from a healthy one
with no traffic, so nothing ever pointed it out.

**Names below are the OTel-side names.** The Prometheus exporter appends `_total` to
counters and the unit to histograms, so `hermes.delivery.result` is queried as
`hermes_delivery_result_total` and `hermes.delivery.provider.duration` as
`hermes_delivery_provider_duration_seconds_bucket`. Dots become underscores.

> Adding a metric? Add its row here in the same change, and put a query against it in a
> dashboard or an alert. An instrument nothing reads is a cost with no benefit — three
> already existed in this codebase before the audit that produced this file.

## Business outcomes

The chain a notification travels, in order. These four are what "is Hermes working"
means; everything else is a mechanism that supports them.

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `hermes.notifications.accepted` | counter | `result` (new\|duplicate) | Requests the send API took. `duplicate` is an idempotency-key replay — a success, but not throughput. |
| `hermes.notifications.dispatched` | counter | `channel` | Delivery messages published to a channel subject. Counted per notification **per channel**, so it exceeds `accepted` whenever fan-out is working. |
| `hermes.delivery.result` | counter | `channel`, `provider`, `outcome` (success\|failed) | Terminal delivery outcomes, counted once after retries are spent. A flaky provider that eventually succeeds is one `success`, not several failures and a success. |
| `hermes.eventwriter.events` | counter | — | Events durably written. The far end of the chain. |

Read by: `pipeline-overview` dashboard, `HermesDeliveryFailureRate`,
`HermesDeliveryAbsent`.

### Where notifications go missing

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `hermes.routing.drop` | counter | `channel`, `reason` | Routing declined to deliver. **Not an error** — a steady background is normal. `channel` is `""` for whole-notification drops. |
| `hermes.dispatch.failures` | counter | `channel`, `reason` (marshal\|publish) | Routing selected a channel and the publish lost it anyway. Any nonzero rate is a defect. |
| `hermes.send.publish_rejections` | counter | — | Send could not publish: NATS unreachable, or a bounded stream at its ceiling. |
| `hermes.eventwriter.dropped` | counter | `stage` (insert\|status) | Events lost to a failed batch write. **Unrecoverable** — messages are acked on entry to the batcher. |

Read by: `pipeline-overview`, `HermesRoutingDropRate`, `HermesEventWriterDropping`.

## Latency

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `hermes.delivery.provider.duration` | histogram (s) | `channel`, `provider`, `outcome` | The provider call alone — the part Hermes does not control. Recorded per attempt, not per terminal outcome. Compare against the consumer's `AckWait`. |
| `hermes.messaging.handler.duration` | histogram (s) | `stream`, `consumer`, `result` | Whole message handler. Its `_count` is the drain rate, which `hermes:consumer_drain_rate` is built from. |
| `hermes.eventwriter.flush.duration` | histogram (s) | — | One batch write. `_count` is the flush rate. Timed on the failure paths too. |
| `hermes.eventwriter.batch.size` | histogram (1) | — | Events per flush. Against the configured max of 100 it says which trigger is binding — size or the 500ms timer. |
| `hermes.probe.e2e.duration` | histogram (s) | — | Synthetic send → websocket. The only true end-to-end measurement. |
| `hermes.probe.send.duration` | histogram (s) | — | The prober's send call. Flat while e2e rises means the delay is downstream of ingestion. |
| `http.server.request.duration` | histogram (s) | `http_route`, `http_request_method`, `http_response_status_code` | From otelhttp. `http_route` is the **templated** pattern; see [the route note](#the-http_route-label). |

## Messaging

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `hermes.messaging.redeliveries` | counter | `stream`, `consumer` | Messages arriving with `NumDelivered > 1`. A spike **without** matching handler failures is an `AckWait` problem, not a downstream one. |
| `hermes.messaging.inflight` | up/down counter | `stream`, `consumer` | Messages handed to a worker and not finished. Meaningless without the next row. |
| `hermes.messaging.workers.limit` | up/down counter | `stream`, `consumer` | Pool size. `inflight / limit` is saturation. |
| `hermes.messaging.consumer.progress.age` | gauge (s) | `stream`, `consumer` | Seconds since this consumer last finished work while work was waiting. Per pod; a stalled consumer keeps emitting it. |
| `hermes.messaging.dead_letters` | counter | `stream`, `consumer`, `reason` | Messages given up on and captured to the DLQ stream. |
| `hermes.messaging.dlq_publish_failures` | counter | `stream`, `consumer` | The safety net itself failing. |

## Dependencies

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `hermes.cache.result` | counter | `op`, `result` | Cache consultations. `op=template` from dispatch, inbox ops from the read path. `result=error` is the one that matters — Redis does not gate readiness, so a failing cache is otherwise invisible. |
| `hermes.db.pool.connections` | gauge | — | Open pool connections. |
| `hermes.db.pool.max` | gauge | — | The ceiling, so the ratio is computable. |
| `hermes.db.pool.acquire.waits` | counter | — | Waits for a connection. Nonzero means the pool is the bottleneck. |
| `hermes.http.rate_limit_decisions` | counter | — | Limiter decisions. |
| `hermes.http.rate_limit_backend_failures` | counter | — | The shared limiter's backend failing — it fails open, so this is the only signal. |

## Auth

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `hermes.auth.result` | counter | `scheme` (api_key\|jwt), `reason` | Every authentication decision. `reason=ok` for success; see [auth-failures.md](runbooks/auth-failures.md) for the rest. |

Deliberately carries **no** key, organization, or caller identifier: an attacker chooses
those values on a failing request, which would make it an unbounded label anyone on the
internet could inflate.

## Lifecycle

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `hermes.shutdown.duration` | histogram (s) | `service` | Time from SIGTERM to exit. |
| `hermes.shutdown.ungraceful` | counter | `service` | Shutdowns that hit the timeout. Each one dropped in-flight work. |
| `hermes.probe.results` | counter | `result` (received\|lost\|send_error) | Synthetic probe verdicts. |
| `hermes.probe.connected` | up/down counter | — | Prober websocket state. Check first when probe loss fires: a prober that lost its own subscription reports total loss on a healthy pipeline. |

## Not emitted by Hermes

Available in Prometheus, but from elsewhere — do not look for these in Go code:

- `nats_jetstream_*` — `prometheus-nats-exporter`. Note it labels consumers
  `stream_name`/`consumer_name` while Hermes uses `stream`/`consumer`; the recording
  rules in `pipeline.rules.yaml` normalize them, so **join through
  `hermes:consumer_pending`** rather than the raw exporter series.
- `go_*`, `process_*` — the Go runtime collector.
- `redis_*`, `pg_*` — infrastructure exporters.
- Redis client spans and metrics — `redisotel`.
- Database query spans — `otelpgx`. Traces only; the pool gauges above are ours.

## The `http_route` label

`http_route` is the **templated** route (`/v1/notifications/{id}`), never the requested
path. This matters and is easy to break:

- ServeMux-routed services (dispatch, the workers, prober) get it free — otelhttp reads
  `http.Request.Pattern`, which ServeMux assigns.
- chi-routed services (send, inbox) need `observability.ChiRoute` wrapping the router,
  plus `observability.WithHTTPRoute` outside the otelhttp handler. See
  `internal/observability/httproute.go` for why a plain middleware cannot do this.

Before that plumbing existed the label was empty on the chi services, so every series
collapsed into one and `sum by (http_route)` drew a single line. If a per-route panel
goes flat, check that wiring first.
