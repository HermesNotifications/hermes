# Instrumentation Guide

Everything developer-facing about emitting telemetry from a Hermes service.

## The rules

1. **Every service calls `observability.Init` at startup** and defers `shutdown()` on exit. No service-specific tracer setup.
2. **Use the package loggers and meters.** Don't create a global `MeterProvider` — use `otel.GetMeterProvider().Meter("hermes.<service>")`.
3. **Trace context comes from `context.Context`.** Always propagate `ctx`. Never stash spans in package-level variables.
4. **Logs carry trace_id / span_id automatically** through the slog handler installed by `observability.Init`. You don't need to add them yourself.
5. **Follow the naming conventions.** See [semantic-conventions.md](semantic-conventions.md). Code review rejects violations.

## Initializing OTel in a service

In `cmd/<service>/main.go`:

```go
import "github.com/hermesnotifications/hermes/internal/observability"

func main() {
    ctx := context.Background()
    shutdown, err := observability.Init(ctx, "hermes-send")
    if err != nil {
        log.Fatalf("observability init: %v", err)
    }
    defer shutdown(context.Background())

    // ... rest of startup
}
```

`Init` reads configuration from standard OTel env vars:

| Var | Purpose |
|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Collector address, e.g. `otel-collector-opentelemetry-collector.observability.svc:4317` |
| `OTEL_SERVICE_NAME` | Overrides the service name passed to `Init` |
| `OTEL_RESOURCE_ATTRIBUTES` | Comma-separated extras, e.g. `deployment.environment=staging,service.version=abc123` |
| `OTEL_TRACES_SAMPLER` | Sampler. Default `parentbased_always_on` — every trace is kept |
| `OTEL_TRACES_SAMPLER_ARG` | Sampler argument, e.g. `0.1` with `parentbased_traceidratio` |

Two things about `Init` are easy to get wrong:

**Without `OTEL_EXPORTER_OTLP_ENDPOINT` it exports nothing, but it still installs
the propagator.** That is deliberate: a service shipping no telemetry of its own
must still forward an inbound `traceparent`, or it severs a trace that the services
either side of it are recording.

**Sampling is env-driven, so do not pass a sampler in code.** `Init` deliberately
omits `sdktrace.WithSampler`, because passing one silently overrides
`OTEL_TRACES_SAMPLER` and takes the dial away. Dispatch has been measured at
~7,900 msg/s, so head sampling is the lever that exists today if trace volume
needs to come down:

```
OTEL_TRACES_SAMPLER=parentbased_traceidratio
OTEL_TRACES_SAMPLER_ARG=0.1
```

`parentbased_*` matters — a plain `traceidratio` samples each service
independently, which shreds cross-service traces into fragments.

## Adding a counter

```go
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/metric"
)

var meter = otel.Meter("hermes.send")

var acceptedCounter, _ = meter.Int64Counter(
    "hermes.notifications.accepted",
    metric.WithDescription("Total notifications accepted for delivery."),
    metric.WithUnit("1"),
)

func handleSend(ctx context.Context, n *Notification) {
    acceptedCounter.Add(ctx, 1, metric.WithAttributes(
        attribute.String("result", "new"),
    ))
}
```

**Then wire it to something.** A metric nothing queries is a cost with no benefit, and
this codebase has shipped several: `hermes.delivery.result` — the one metric that says
whether notifications reach anyone — went unread by any alert or dashboard for its whole
life. In the same change, add the metric to
[metrics-reference.md](metrics-reference.md) and put it on a dashboard panel or in an
alert rule.

Check both directions. The pipeline dashboard spent just as long querying four metric
names that were never emitted, which looks identical to a healthy system with no traffic.

**Do NOT** put `user_id`, `notification_id`, `organization_id`, or any other unbounded-cardinality value on metric attributes. See [semantic-conventions.md](semantic-conventions.md#forbidden-high-cardinality-labels).

## Adding a histogram

```go
var sendLatency, _ = meter.Float64Histogram(
    "hermes.notifications.send.duration",
    metric.WithDescription("Time from send request to NATS ack."),
    metric.WithUnit("s"),
    metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10),
)

start := time.Now()
// ... do work ...
sendLatency.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(
    attribute.String("channel", channel),
))
```

## Creating a span in business code

Most span creation is automatic (otelchi for HTTP, otelpgx for DB, redisotel for Redis). Add explicit spans for domain-level operations:

```go
var tracer = otel.Tracer("hermes.send")

func publishToNATS(ctx context.Context, msg *Message) error {
    ctx, span := tracer.Start(ctx, "notification.publish",
        trace.WithAttributes(
            attribute.String("messaging.system", "nats"),
            attribute.String("messaging.destination", msg.Subject),
            attribute.String("messaging.operation", "publish"),
        ),
    )
    defer span.End()

    if err := nc.Publish(msg.Subject, msg.Payload); err != nil {
        span.RecordError(err)
        span.SetStatus(codes.Error, err.Error())
        return err
    }
    return nil
}
```

Span name convention: `<noun>.<verb>`, lowercase, dots as separators. E.g. `notification.publish`, `template.resolve`, `user.fetch-preferences`.

## Propagating trace context over NATS

**You do not need to do anything.** `messaging.Client` handles both ends, and every
publish and consume in Hermes goes through it:

- `Client.Publish` calls `observability.InjectNATS`, which starts a producer span
  and writes `traceparent` into the message headers.
- `Client.Subscribe`'s internal `processMessage` calls `observability.ExtractNATS`
  and hands your handler a `ctx` already parented to the publisher's span.

So the only rule is the general one: **use the `ctx` your handler is given**, and
pass it down. Anything you start from `context.Background()` instead becomes a
detached root trace.

```go
err := client.Subscribe(cfg, func(ctx context.Context, data []byte, info messaging.DeliveryInfo) error {
    // ctx is already inside the publisher's trace -- just use it.
    return doWork(ctx, data)
})
```

`NATSHeaderCarrier`, `InjectNATS` and `ExtractNATS` in
`internal/observability/nats.go` are the mechanism behind this. Call them directly
only if you are writing a new transport that does not go through
`messaging.Client`.

### A redelivery is a new trace, linked back

Worth knowing before you go looking for one. On the **first** delivery the consumer span
is a child of the publish, so the pipeline reads as a single trace. On a **redelivery**
it is a new root with a link back to the publish, and `messaging.attempt` on the span.

The reason is that JetStream hands back the original headers, so a retry would otherwise
graft onto a span that ended long ago — with `retryDelay` capped at 240s and
`maxDeliveries` at 10, a trace could show a root that finished in milliseconds with a
child starting a quarter of an hour later. That is a false claim about time, not a
display quirk.

The cost is that a retry is harder to find from the original trace, since not every
backend walks links. To find the retries for a message, search on the notification ID
rather than expecting them inside its first trace.

### Batching breaks the parent relationship — link *and* span

When one operation serves many messages, it cannot be a child of any single one.
Record links to the inputs, but do not stop there: also emit a span on each input's
own trace. A link expresses the batch relationship; it does not make the work
visible from an individual message's trace, and not every backend traverses links.
`internal/eventwriter/writer.go` is the worked example, and
[ADR-004](adr/004-accepting-dsm-loss.md#amendment-2026-08-15-links-alone-did-not-deliver-this)
records what went wrong when only the links were there.

## Correlating logs with traces

The `slog.Handler` installed by `observability.Init` automatically adds `trace_id` and `span_id` to every log record that carries a non-empty `SpanContext` in `ctx`:

```go
slog.InfoContext(ctx, "notification accepted",
    slog.String("channel", channel),
    slog.String("recipient_hash", hashRecipient(r)),
)
// JSON output:
// {"time":"...", "level":"INFO", "msg":"notification accepted",
//  "service":"hermes-send", "channel":"email", "recipient_hash":"...",
//  "trace_id":"4bf92f3577b34da6a3ce929d0e0e4736",
//  "span_id":"00f067aa0ba902b7"}
```

Grafana's Loki datasource is wired to extract `trace_id` and offer a "View trace" button that jumps straight to Tempo. See [dashboards.md](dashboards.md#log-to-trace-pivot).

## What NOT to do

- **Don't** create your own global `TracerProvider` or `MeterProvider`. Use the one `Init` registers.
- **Don't** attach user-identifying data to metric attributes. Put it on spans as attributes (unbounded cardinality is fine there, it's stored per-trace).
- **Don't** log PII in plaintext. Use hashes or redaction. Logs land in Loki, which has no access-level gating beyond Grafana auth.
- **Don't** spawn spans in tight inner loops. If you're recording >1000 spans/sec from a single handler, you're tracing too fine-grained — use metrics.
- **Don't** call `span.End()` without a `defer`. Forgotten ends make debugging awful.
