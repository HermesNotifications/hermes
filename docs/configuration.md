# Configuration

Every Hermes service is configured entirely through environment variables with the `HERMES_`
prefix, loaded by `config.Load()` in `internal/config/config.go`. There is no config file. The
defaults target local development (Docker Compose / k3d), so a freshly started stack needs no
configuration at all.

> **Deploying with Helm?** The chart exposes these as structured values rather than raw env
> vars. See [self-hosting/configuration.md](self-hosting/configuration.md) for the values
> reference; this page documents the underlying variables the binaries actually read.

## Reference

| Variable | Default | Purpose |
|---|---|---|
| `HERMES_HTTP_PORT` | `8080` | Port the service's HTTP server binds. Each service sets its own default via deployment config (see [services.md](services.md)). |
| `HERMES_DATABASE_URL` | `postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable` | Postgres DSN. |
| `HERMES_NATS_URL` | `nats://localhost:4222` | NATS JetStream server. |
| `HERMES_REDIS_URL` | `redis://localhost:6379/0` | Redis (cache, idempotency, Centrifugo engine). |
| `HERMES_JWT_SECRET` | `hermes-jwt-secret` | Secret used to sign/verify Hermes-issued JWTs. |
| `HERMES_API_KEY_HMAC_SECRET` | `hermes-dev-hmac-secret` | HMAC key for hashing/verifying API-key secrets. |
| `HERMES_CENTRIFUGO_API_URL` | `http://localhost:8000` | Centrifugo HTTP API base URL (real-time push). |
| `HERMES_CENTRIFUGO_API_KEY` | `centrifugo-api-key` | Centrifugo HTTP API key. |
| `HERMES_SMS_WEBHOOK_URL` | `http://localhost:9090/sms` | Webhook the SMS worker POSTs to. |
| `HERMES_EVENT_RETENTION_DAYS` | `90` | Age threshold for `cmd/cleanup` to delete `notification_events`. |

### Email (`worker-email`)

| Variable | Default | Purpose |
|---|---|---|
| `HERMES_EMAIL_PROVIDER` | `smtp` | `smtp` or `ses`. |
| `HERMES_EMAIL_FROM` | `noreply@example.com` | Default From address. |
| `HERMES_EMAIL_SMTP_HOST` | `localhost` | SMTP host (local dev uses Mailpit). |
| `HERMES_EMAIL_SMTP_PORT` | `1025` | SMTP port. |
| `HERMES_EMAIL_SMTP_USERNAME` | _(empty)_ | SMTP username. |
| `HERMES_EMAIL_SMTP_PASSWORD` | _(empty)_ | SMTP password. |
| `HERMES_EMAIL_SES_REGION` | `us-east-1` | AWS region when `HERMES_EMAIL_PROVIDER=ses`. |
| `HERMES_EMAIL_LAYOUT_PATH` | _(empty)_ | Path to an HTML layout template wrapping email bodies. |

## Production note

The defaults for `HERMES_JWT_SECRET` and `HERMES_API_KEY_HMAC_SECRET` are **development
placeholders**. Always override them with strong, unique secrets in any shared or production
environment — rotating `HERMES_API_KEY_HMAC_SECRET` invalidates every existing API key, and
rotating `HERMES_JWT_SECRET` invalidates every issued JWT.

## Observability variables

Telemetry is configured via standard OpenTelemetry environment variables (e.g.
`OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_RESOURCE_ATTRIBUTES`) and, during Phase 1, Datadog `DD_*`
variables — not `HERMES_*`. See
[observability/instrumentation-guide.md](observability/instrumentation-guide.md) and
[observability/local-dev.md](observability/local-dev.md).
