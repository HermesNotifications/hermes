# Datadog Instrumentation Design

**Date:** 2026-03-27
**Status:** Draft

## Goal

Add full observability to Hermes: distributed tracing, runtime profiling, log correlation, and data streams monitoring. All instrumentation is compiled in but inert when no Datadog Agent is reachable (`DD_AGENT_HOST` unset). The agent infrastructure is opt-in per environment.

## Approach

**Orchestrion compile-time instrumentation** for libraries with built-in support (chi, pgx, go-redis, AWS SDK, net/http, slog). **Manual instrumentation** only for NATS, which Orchestrion does not support. A thin `internal/tracing` package handles tracer lifecycle and NATS propagation. No service-level code changes except evolving the `messaging.Client` API to thread context.

## Section 1: Build Toolchain

### orchestrion.tool.go

Created at repo root by `orchestrion pin`. Selects integrations matching Hermes's dependency stack:

```go
//go:build tools

package tools

import (
    _ "github.com/DataDog/dd-trace-go/contrib/go-chi/chi.v5/v2"
    _ "github.com/DataDog/dd-trace-go/contrib/jackc/pgx.v5/v2"
    _ "github.com/DataDog/dd-trace-go/contrib/redis/go-redis.v9/v2"
    _ "github.com/DataDog/dd-trace-go/contrib/aws/aws-sdk-go-v2/v2/aws"
    _ "github.com/DataDog/dd-trace-go/contrib/database/sql/v2"
    _ "github.com/DataDog/dd-trace-go/contrib/net/http/v2"
    _ "github.com/DataDog/dd-trace-go/contrib/log/slog/v2"
    _ "github.com/DataDog/orchestrion"
)
```

### Dockerfile

Builder stage installs Orchestrion and uses it for compilation:

```dockerfile
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

RUN apk --no-cache add tzdata ca-certificates
RUN go install github.com/DataDog/orchestrion@latest

ARG SERVICE
ARG VERSION=dev
ARG TARGETARCH
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} \
    orchestrion go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /service ./cmd/${SERVICE}/
```

Runtime stage remains `scratch` — Orchestrion is compile-time only.

### Makefile

```makefile
build-%:
	orchestrion go build -o bin/$*/service ./cmd/$*/
```

## Section 2: Tracer Lifecycle

New package `internal/tracing/tracing.go`:

```go
package tracing

import (
    "os"

    "gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
    "gopkg.in/DataDog/dd-trace-go.v1/profiler"
)

// Start initializes the DD tracer and profiler.
// No-ops if DD_AGENT_HOST is not set.
func Start() {
    if os.Getenv("DD_AGENT_HOST") == "" {
        return
    }

    tracer.Start()

    profiler.Start(
        profiler.WithProfileTypes(
            profiler.CPUProfile,
            profiler.HeapProfile,
            profiler.GoroutineProfile,
        ),
    )
}

// Shutdown flushes and stops the tracer and profiler.
func Shutdown() {
    profiler.Stop()
    tracer.Stop()
}
```

Wired into `internal/bootstrap/serve.go` — `ListenAndServe` calls `tracing.Start()` with a deferred `tracing.Shutdown()`. Every service gets tracing automatically, no changes to `cmd/*/main.go`.

```go
func ListenAndServe(addr string, handler http.Handler, logger *slog.Logger, onShutdown ...func()) {
    tracing.Start()
    defer tracing.Shutdown()

    // ... existing server code unchanged ...
}
```

`DD_SERVICE`, `DD_ENV`, `DD_VERSION` are read automatically by dd-trace-go from environment variables — no code references needed.

## Section 3: NATS Trace + Data Streams Propagation

### messaging.Client API Evolution

The current API does not support context or message headers:

```go
// Current
func (c *Client) Publish(subject string, data []byte) error
func (c *Client) Subscribe(subject, consumer string, handler func(data []byte) error) error
```

Evolve to context-aware signatures:

```go
// New
func (c *Client) Publish(ctx context.Context, subject string, data []byte) error
func (c *Client) Subscribe(subject, consumer string, handler func(ctx context.Context, data []byte) error) error
```

`Publish` creates a produce span + DSM checkpoint and injects trace/pathway context into NATS message headers before publishing. `Subscribe` extracts trace/pathway context from incoming message headers, creates a consume span + DSM checkpoint, and passes the enriched context to the handler.

This keeps tracing concerns inside the messaging package. Callers just thread `ctx` — no direct dependency on `internal/tracing` from service code.

### internal/tracing/nats.go

Low-level helpers used by `messaging.Client`:

```go
package tracing

import (
    "context"

    "github.com/nats-io/nats.go"
    "gopkg.in/DataDog/dd-trace-go.v1/datastreams"
    "gopkg.in/DataDog/dd-trace-go.v1/ddtrace"
    "gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

// NATSHeaderCarrier adapts nats.Header for DD propagation (APM + DSM).
type NATSHeaderCarrier nats.Header

func (c NATSHeaderCarrier) Set(key, val string) { nats.Header(c).Set(key, val) }
func (c NATSHeaderCarrier) ForeachKey(handler func(key, val string) error) error {
    for k, vals := range nats.Header(c) {
        for _, v := range vals {
            if err := handler(k, v); err != nil {
                return err
            }
        }
    }
    return nil
}

// InjectNATS creates a produce span, sets a DSM checkpoint,
// and injects both trace and pathway context into message headers.
// Returns the span (caller must Finish) and enriched context.
func InjectNATS(ctx context.Context, msg *nats.Msg) (context.Context, ddtrace.Span) {
    span, ctx := tracer.StartSpanFromContext(ctx, "nats.publish",
        tracer.ResourceName(msg.Subject),
        tracer.SpanType("queue"),
    )

    ctx, _ = tracer.SetDataStreamsCheckpoint(ctx,
        "direction:out", "type:nats", "topic:"+msg.Subject,
    )

    if msg.Header == nil {
        msg.Header = nats.Header{}
    }
    carrier := NATSHeaderCarrier(msg.Header)
    tracer.Inject(span.Context(), carrier)
    datastreams.InjectToBase64Carrier(ctx, carrier)

    return ctx, span
}

// ExtractNATS extracts trace and pathway context from a received message,
// sets a DSM consume checkpoint, and returns a context with an active span.
// Caller must call span.Finish() when processing is done.
func ExtractNATS(ctx context.Context, msg *nats.Msg) (context.Context, ddtrace.Span) {
    carrier := NATSHeaderCarrier(msg.Header)

    opts := []tracer.StartSpanOption{
        tracer.ResourceName(msg.Subject),
        tracer.SpanType("queue"),
    }
    if sctx, err := tracer.Extract(carrier); err == nil {
        opts = append(opts, tracer.ChildOf(sctx))
    }
    span, ctx := tracer.StartSpanFromContext(ctx, "nats.consume", opts...)

    ctx = datastreams.ExtractFromBase64Carrier(ctx, carrier)
    ctx, _ = tracer.SetDataStreamsCheckpoint(ctx,
        "direction:in", "type:nats", "topic:"+msg.Subject,
    )

    return ctx, span
}
```

### Usage in messaging.Client

```go
func (c *Client) Publish(ctx context.Context, subject string, data []byte) error {
    msg := &nats.Msg{Subject: subject, Data: data}
    _, span := tracing.InjectNATS(ctx, msg)
    defer span.Finish()

    _, err := c.js.PublishMsg(ctx, msg)
    return err
}

func (c *Client) Subscribe(subject, consumer string, handler func(ctx context.Context, data []byte) error) error {
    // ... existing stream/consumer setup ...

    _, err = cons.Consume(func(msg jetstream.Msg) {
        // Note: exact method to access underlying *nats.Msg from jetstream.Msg
        // to be verified during implementation (may need msg.Headers() or similar).
        ctx, span := tracing.ExtractNATS(context.Background(), rawMsg)
        defer span.Finish()

        if err := handler(ctx, msg.Data()); err != nil {
            _ = msg.Nak()
            return
        }
        _ = msg.Ack()
    })
    return err
}
```

### Call Site Updates

All callers of `Publish` and `Subscribe` need context threading:

**Publishers (add `ctx` argument):**
- `internal/admin/handler_send.go` — `s.nats.Publish(ctx, "notification.send", msgBytes)`
- `internal/dispatch/dispatch.go` — `d.nats.Publish(ctx, subject, dmBytes)` and `d.nats.Publish(ctx, "notification.events", emBytes)`
- `internal/delivery/worker.go` — `w.nats.Publish(ctx, "notification.events", evtBytes)`

**Subscribers (add `ctx` to handler):**
- `internal/dispatch/dispatch.go` — `d.nats.Subscribe("notification.send", "dispatch", func(ctx context.Context, data []byte) error { ... })`
- `internal/eventwriter/writer.go` — same pattern
- `internal/delivery/worker.go` — same pattern

## Section 4: Infrastructure

### Kustomize Overlays

The Datadog Agent is an opt-in overlay, not part of the base manifests:

```
deploy/k8s/overlays/
├── local/
│   └── datadog/
│       └── kustomization.yaml
├── staging/
│   └── datadog/
│       └── kustomization.yaml
└── production/
    └── datadog/
        └── kustomization.yaml
```

### DaemonSet

Minimal agent configuration:

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: datadog-agent
spec:
  selector:
    matchLabels:
      app: datadog-agent
  template:
    metadata:
      labels:
        app: datadog-agent
    spec:
      containers:
        - name: agent
          image: gcr.io/datadoghq/agent:7
          env:
            - name: DD_API_KEY
              valueFrom:
                secretKeyRef:
                  name: datadog-secret
                  key: api-key
            - name: DD_APM_ENABLED
              value: "true"
            - name: DD_APM_NON_LOCAL_TRAFFIC
              value: "true"
            - name: DD_DATA_STREAMS_ENABLED
              value: "true"
          ports:
            - containerPort: 8126
              name: apm
```

### Service Environment Variables

Patched onto all Hermes pods via the datadog overlay:

```yaml
env:
  - name: DD_AGENT_HOST
    valueFrom:
      fieldRef:
        fieldPath: status.hostIP
  - name: DD_SERVICE
    value: hermes-<service-name>
  - name: DD_ENV
    valueFrom:
      fieldRef:
        fieldPath: metadata.labels['env']
  - name: DD_VERSION
    valueFrom:
      fieldRef:
        fieldPath: metadata.labels['version']
```

### Local Dev

Tiltfile gets an optional flag, off by default:

```python
config.define_bool("datadog", args=True, usage="Enable Datadog tracing")
cfg = config.parse()

if cfg.get("datadog", False):
    # apply datadog overlay
```

Opt in with `tilt up -- --datadog`.

### Staging/Production

Datadog overlay always applied. `DD_API_KEY` sourced from K8s secret managed via Terraform (external-secrets referencing AWS Secrets Manager).

## Section 5: Log Correlation & Profiling

### Log Correlation

Handled entirely by Orchestrion's `log/slog` integration. When the tracer is active, every slog record gets `dd.trace_id`, `dd.span_id`, and `dd.service` fields injected automatically. When inactive, logs remain unchanged.

`bootstrap.NewLogger()` already uses `slog.NewJSONHandler` — no changes needed. Datadog's log intake parses these fields from JSON logs automatically.

### Runtime Profiling

Covered by `tracing.Start()` — CPU, heap, and goroutine profiles sent to the agent. Mutex and block profiles excluded (non-trivial overhead).

### Unified Service Tagging

`DD_SERVICE`, `DD_ENV`, `DD_VERSION` from pod env vars tie traces, logs, profiles, and DSM data together in the Datadog UI. Read automatically by dd-trace-go from environment.

## Summary: Automatic vs Manual

| Concern | Mechanism | Code Changes |
|---|---|---|
| HTTP spans (chi) | Orchestrion | None |
| Postgres spans (pgx) | Orchestrion | None |
| Redis spans | Orchestrion | None |
| AWS SDK spans | Orchestrion | None |
| Log correlation (slog) | Orchestrion | None |
| NATS trace + DSM | `internal/tracing` helpers | Evolve `messaging.Client` API, update call sites |
| Profiling | `tracing.Start()` | Two lines in `ListenAndServe` |
| Tracer lifecycle | `tracing.Start/Shutdown` | Two lines in `ListenAndServe` |

## Not in Scope

- Custom business metrics (can be added later with `statsd` client)
- Centrifugo or go-mail tracing (no Orchestrion support, low priority)
- Synthetic monitoring or RUM
- Log forwarding pipeline (assumes Datadog Agent collects container stdout)
