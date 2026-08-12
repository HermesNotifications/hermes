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

## Adding a counter

```go
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/metric"
)

var meter = otel.Meter("hermes.send")

var sentCounter, _ = meter.Int64Counter(
    "hermes.notifications.sent",
    metric.WithDescription("Total notifications accepted for delivery."),
    metric.WithUnit("1"),
)

func handleSend(ctx context.Context, n *Notification) {
    sentCounter.Add(ctx, 1, metric.WithAttributes(
        attribute.String("channel", n.Channel),
    ))
}
```

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

Use the carrier from `internal/observability/nats.go`:

```go
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/propagation"
    obsnats "github.com/hermesnotifications/hermes/internal/observability"
)

// Publisher
msg := &nats.Msg{Subject: subj, Data: payload, Header: nats.Header{}}
otel.GetTextMapPropagator().Inject(ctx, obsnats.NATSHeaderCarrier(msg.Header))
nc.PublishMsg(msg)

// Consumer
func handle(msg *nats.Msg) {
    ctx := otel.GetTextMapPropagator().Extract(
        context.Background(),
        obsnats.NATSHeaderCarrier(msg.Header),
    )
    ctx, span := tracer.Start(ctx, "notification.consume",
        trace.WithSpanKind(trace.SpanKindConsumer),
    )
    defer span.End()
    // process
}
```

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
