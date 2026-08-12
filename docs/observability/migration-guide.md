# Migration Guide: `dd-trace-go` → OpenTelemetry

Playbook for migrating a service's instrumentation. Used during Phase 1 rollout, preserved for future service additions and the eventual DD removal in Phase 2.

## Migration order (Phase 1 rollout)

Services are migrated smallest-blast-radius first. Follow this order:

1. **`send`** (pilot — thinnest service, minimal DB/Redis use)
2. `worker-email`
3. `worker-sms`
4. `worker-inbox`
5. `worker-events`
6. `inbox`
7. `user`
8. `dispatch`
9. `admin` (heaviest — migrated last after the pattern is proven)

After the last service migrates, the `dd-trace-go/v2` module import is removed from `go.mod` and `internal/tracing/` is deleted.

## Per-service steps

### 1. Add OTel dependencies to `go.mod`

```bash
go get \
  go.opentelemetry.io/otel/sdk \
  go.opentelemetry.io/otel/sdk/metric \
  go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc \
  go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc \
  go.opentelemetry.io/contrib/instrumentation/runtime \
  github.com/riandyrn/otelchi \
  github.com/exaring/otelpgx \
  go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-sdk-go-v2/otelaws
```

Redis go-redis has its OTel hook in the extras module: `github.com/redis/go-redis/extra/redisotel/v9`.

### 2. Library swap table

| Current (dd-trace-go) | Replacement |
|---|---|
| `chitrace.Middleware(chitrace.WithServiceName(...))` | `otelchi.Middleware(serviceName)` |
| `sqltrace.Register(...)` + `sqltrace.Open(...)` | `otelpgx.NewTracer()` + `cfg.ConnConfig.Tracer = tracer` |
| `redistrace.WrapClient(...)` | `redisotel.InstrumentTracing(client)` + `redisotel.InstrumentMetrics(client)` |
| `awstrace.AppendMiddleware(...)` | `otelaws.AppendMiddlewares(&cfg.APIOptions)` |
| `tracer.StartSpanFromContext(ctx, "op")` | `tracer.Start(ctx, "op")` — note arg order change |
| `span.SetTag("k", v)` | `span.SetAttributes(attribute.String("k", v))` |
| `span.Finish()` | `span.End()` |
| Custom NATS `dd-trace-go` carrier | `obsnats.NATSHeaderCarrier(headers)` + `otel.GetTextMapPropagator().Inject/Extract` |

### 3. `cmd/<service>/main.go`

Replace:

```go
import "github.com/hermesnotifications/hermes/internal/tracing"

func main() {
    tracing.Start("hermes-send")
    defer tracing.Stop()
    // ...
}
```

with:

```go
import "github.com/hermesnotifications/hermes/internal/observability"

func main() {
    ctx := context.Background()
    shutdown, err := observability.Init(ctx, "hermes-send")
    if err != nil {
        log.Fatalf("observability init: %v", err)
    }
    defer shutdown(context.Background())
    // ...
}
```

### 4. NATS carrier

Old (`internal/tracing/nats.go`):

```go
carrier := tracing.NATSHeaderCarrier(msg.Header)
tracer.Inject(span.Context(), carrier)
```

New (`internal/observability/nats.go`):

```go
carrier := obsnats.NATSHeaderCarrier(msg.Header)
otel.GetTextMapPropagator().Inject(ctx, carrier)
```

Extract side mirrors: `Extract(ctx, carrier)` returns a new `ctx` with the remote span context attached.

### 5. Deployment manifests

In the service's `deploy/k8s/base/services/<name>.yaml`:

**Remove:**

```yaml
- name: DD_AGENT_HOST
  valueFrom:
    fieldRef:
      fieldPath: status.hostIP
- name: DD_SERVICE
  value: hermes-send
- name: DD_ENV
  value: production
- name: DD_VERSION
  valueFrom: ...
- name: DD_PROFILING_ENABLED
  value: "true"
- name: DD_APM_ENABLED
  value: "true"
```

**Add:**

```yaml
- name: OTEL_EXPORTER_OTLP_ENDPOINT
  value: http://otel-collector-opentelemetry-collector.observability.svc:4317
- name: OTEL_EXPORTER_OTLP_PROTOCOL
  value: grpc
- name: OTEL_SERVICE_NAME
  value: hermes-send
- name: OTEL_RESOURCE_ATTRIBUTES
  value: deployment.environment=$(ENVIRONMENT),service.version=$(IMAGE_TAG)
```

`$(ENVIRONMENT)` and `$(IMAGE_TAG)` come from existing ConfigMaps/patches in the overlays.

### 6. Verification per-service

After rolling the deployment:

1. `curl <service>/<route>` — hit a known endpoint.
2. Grafana → Explore → Tempo: search `service.name = hermes-<name>`, confirm trace appears.
3. Datadog APM: confirm the **same trace** appears (dual-emit gate — if only one side shows it, something is wrong).
4. Grafana → Explore → Prometheus: `http_server_request_duration_seconds_count{service="hermes-<name>"}` > 0.
5. Grafana → Explore → Loki: `{service="hermes-<name>"}` shows logs with `trace_id` matching the Tempo trace.
6. `make test` and `make test-e2e` — all pass.

**Dual-emit parity is the gate.** A service is considered migrated only when both sides (OSS and DD) show the same data for the same request.

## After the last service

1. Delete `internal/tracing/` entirely.
2. `go mod tidy` removes `github.com/DataDog/dd-trace-go/v2` from `go.mod`.
3. Remove `orchestrion` tooling from Tiltfile (`datadog_enabled` branch of the compile step).
4. Commit + PR with title `chore(observability): remove dd-trace-go after full OTel migration`.

The Datadog Agent DaemonSet and the Collector's `datadog` exporter stay in place — they're how Datadog continues to receive data post-migration. Removing those is Phase 2.

## Adding a new service (post-migration)

New services added after Phase 1 complete should never touch dd-trace-go. Start from OTel:

1. `internal/observability.Init` in `main.go`.
2. Chi middleware: `otelchi.Middleware(serviceName)`.
3. DB/Redis/AWS clients: OTel instrumentation per the table above.
4. NATS: `obsnats.NATSHeaderCarrier`.
5. Deployment env: `OTEL_EXPORTER_OTLP_ENDPOINT` + `OTEL_SERVICE_NAME` + `OTEL_RESOURCE_ATTRIBUTES`.

No DD env vars. No `dd-trace-go` import. If anything in your stack templates DD env or imports, the template is out of date — update it.
