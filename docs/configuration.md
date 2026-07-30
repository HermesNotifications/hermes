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
| `HERMES_NATS_URL` | `nats://localhost:4222` | NATS JetStream server. Must be `tls://` outside development — see [ADR 0005](adr/0005-transport-security-for-infrastructure-connections.md). |
| `HERMES_NATS_CA_BUNDLE` | _(empty)_ | PEM file of the roots that verify the NATS server certificate. cert-manager signs `nats.hermes.svc` with a private CA that is in no system trust store, so in a real deployment this is how the connection can be verified at all; the deployment mounts it at `/etc/nats-certs/ca.crt`. Empty means "use the system pool", which is the local setting — `make infra-up` runs NATS without TLS. A path that is set but missing **fails the connection** rather than downgrading it. |
| `HERMES_NATS_NKEY_SEED` | _(empty)_ | File holding this service's NATS NKey seed. The seed selects the service's user in `deploy/k8s/base/infra/nats-accounts.conf`, and with it the subjects that service may publish and subscribe to (see [NATS authorization](#nats-authorization) below). The deployment mounts it at `/etc/nats-nkey/seed.nk`. Empty connects anonymously, which only works against a server with no accounts — the local overlay. It is not a silent downgrade: a server that defines accounts answers an unauthenticated connection with an authorization violation, so a deployment that forgets the seed fails to start. Re-read on every reconnect, so a rotated Secret needs no restart. |
| `HERMES_REDIS_URL` | `redis://localhost:6379/0` | Redis (cache, idempotency, Centrifugo engine). |
| `HERMES_JWT_SECRET` | `hermes-jwt-secret` | Secret used to sign/verify Hermes-issued JWTs. |
| `HERMES_API_KEY_HMAC_SECRET` | `hermes-dev-hmac-secret` | HMAC key for hashing/verifying API-key secrets. |
| `HERMES_CENTRIFUGO_API_URL` | `http://localhost:8000` | Centrifugo HTTP API base URL (real-time push). |
| `HERMES_CENTRIFUGO_API_KEY` | `centrifugo-api-key` | Centrifugo HTTP API key. |
| `HERMES_SMS_WEBHOOK_URL` | `http://localhost:9090/sms` | Webhook the SMS worker POSTs to. |
| `HERMES_EVENT_RETENTION_DAYS` | `90` | Age threshold for `cmd/cleanup` to delete `notification_events`. |
| `HERMES_DISPATCH_CONCURRENCY` | `8` | Size of the **dispatch** worker pool — how many `notification.send` messages are processed in parallel. Distinct notifications are independent (status rollup is monotonic downstream), so raising this lifts dispatch throughput. The default of 8 is from the June 2026 load-test sweep ([docs/loadtest/dispatch-tuning-2026-06.md](loadtest/dispatch-tuning-2026-06.md)): throughput scales to ~16 workers, but 8 is the balanced point that leaves DB-pool headroom. Dispatch is I/O-bound, so the useful ceiling is the database pool, not CPU cores: the value is **clamped to the Postgres pool size** (`pool_max_conns`, default `max(4, NumCPU)`) and a warning is logged if set higher — raise `pool_max_conns` in `HERMES_DATABASE_URL` (and this value, toward 16) to push throughput further. |
| `HERMES_DISPATCH_PREFETCH` | `64` | Dispatch fetcher's in-flight buffer (NATS `PullMaxMessages`) feeding the worker pool. Decouples fetching from processing so the pull pipeline stays full without one consumer hoarding the backlog. Auto-raised to at least `concurrency + 1`; the consumer's server-side `MaxAckPending` is raised to at least `prefetch + concurrency`. Tune against load tests. |

### Store backend

| Variable | Default | Purpose |
|---|---|---|
| `HERMES_ENV` | `development` | Anything other than the exact string `development` enables the startup validation in [ADR 0005](adr/0005-transport-security-for-infrastructure-connections.md): TLS is required on every datastore connection and the built-in placeholder secrets are rejected. A service failing validation **refuses to start** rather than connecting in the clear. A misspelling takes the strict path, so a typo cannot silently disable the checks. |
| `HERMES_DYNAMO_ENDPOINT` | _(empty)_ | Enables the DynamoDB-model store for the hot notification and event path ([ADR 0001](adr/0001-dynamodb-model-via-extenddb.md)). Empty keeps everything on Postgres. Read by admin, dispatch, inbox, user and worker-events. **Postgres remains required** — the Dynamo store delegates to it and every service connects unconditionally. See the caveats in [architecture.md](architecture.md#the-dual-store): cursors are not portable between backends, `cmd/cleanup` does not run on this path, and no backup mechanism for the table exists in this repository. |
| `HERMES_DYNAMO_REGION` | `us-east-1` | AWS region for the DynamoDB store. Ignored when `HERMES_DYNAMO_ENDPOINT` is empty. |

Transport security is expressed in the connection strings themselves rather than in separate
flags, because two settings that can disagree are worse than one that cannot. Outside
development, `HERMES_DATABASE_URL` needs `sslmode=require` or stricter (`allow` and `prefer`
are rejected — both silently fall back to plaintext), `HERMES_REDIS_URL` must use `rediss://`,
and `HERMES_NATS_URL` must use `tls://`.

### NATS authorization

TLS encrypts the bus; it authorises nothing. Authorization is a second layer, added in
[ADR 0005](adr/0005-transport-security-for-infrastructure-connections.md) phase 3: the server
defines a single `HERMES` account with **one NKey user per service**, each scoped to the
subjects that service actually uses. The permissions live in
`deploy/k8s/base/infra/nats-accounts.conf`, which both the base and staging server
configurations `include`.

| Service | Publishes | Consumes (consumer name) |
|---|---|---|
| `hermes-send` | `notification.send` | — |
| `hermes-dispatch` | `delivery.email`, `delivery.sms`, `delivery.inbox`, `notification.events`, `dlq.notification.send` | `notification.send` (`dispatch`) |
| `hermes-worker-email` | `notification.events`, `dlq.delivery.email` | `delivery.email` (`worker-email`) |
| `hermes-worker-sms` | `notification.events`, `dlq.delivery.sms` | `delivery.sms` (`worker-sms`) |
| `hermes-worker-inbox` | `notification.events`, `dlq.delivery.inbox` | `delivery.inbox` (`worker-inbox`) |
| `hermes-worker-events` | `dlq.notification.events` | `notification.events` (`event-writer`) |

`hermes-admin`, `hermes-user` and `hermes-inbox` do not connect to NATS and have no credential.

Three things follow that are easy to trip over:

- **A subject added in code needs a permission added here**, or the service fails at runtime
  rather than at deploy. `TestAccounts_ConfCoversEverySubjectTheCodeUses` in
  `internal/messaging` is the guard: it fails if a subject in `messaging.Streams` has no grant.
- **JetStream is a separate permission surface.** Publishing to a stream subject is not enough;
  declaring a stream needs `$JS.API.STREAM.CREATE|UPDATE.<STREAM>`, and consuming needs
  `$JS.API.CONSUMER.CREATE`, `$JS.API.CONSUMER.MSG.NEXT` and `$JS.ACK` for that one stream and
  consumer name. A missing JetStream grant looks like a broken stream, not a permissions
  problem — the request simply never gets a reply.
- **Each connection's reply inbox is scoped to `_INBOX.<service>`** by
  `messaging.WithIdentity`. JetStream delivers pulled messages to the client's inbox, so a user
  permitted to subscribe to `_INBOX.>` would receive copies of every other service's messages
  and read `delivery.*` without any `delivery.*` permission. The narrow subscribe lists and the
  client-side prefix are one mechanism; changing either alone breaks it.

Generate a matched key set with `go run ./cmd/natskeys` (`-format json` for the payload the
staging and production ExternalSecrets read). Each key's public half becomes a
`$HERMES_NKEY_*` variable in the NATS server's environment; the seed is mounted into that one
service's pod. Rotating one half of a pair alone locks that service out of the bus.

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
