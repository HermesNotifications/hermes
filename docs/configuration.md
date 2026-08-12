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
| `HERMES_NATS_STREAM_MAX_BYTES` | `536870912` (512 MiB) | Disk ceiling for **each** of the three JetStream work streams. At the ceiling, publishes are rejected rather than old messages dropped — see [ADR 0010](adr/0010-bounded-work-streams-reject-rather-than-drop.md); a rejected publish becomes a `503` with `Retry-After` from `/v1/send`. **Only the `natsprovision` Job's value has any effect** — under [ADR 0005](adr/0005-transport-security-for-infrastructure-connections.md) phase 4 it is the sole identity permitted to create or update a stream, so setting this on a service Deployment does nothing. Size it against the NATS volume: three work streams plus the 1 GiB DLQ must fit with headroom. At the 5Gi default that is 2.5 GiB used. Raising this without growing the volume re-creates the unbounded-growth failure it exists to prevent. |

### HTTP rate limiting

Every HTTP service reads the same three variables. Because each service runs as its own
Deployment, they are set **per service**, not fleet-wide.

| Variable | Default | Purpose |
|---|---|---|
| `HERMES_RATELIMIT_ENABLED` | `true` | Set `false` to disable rate limiting for that service entirely. |
| `HERMES_RATELIMIT_BURST` | _(service default)_ | Requests admitted instantaneously per caller. Unset or `0` keeps the service default. |
| `HERMES_RATELIMIT_PER_SECOND` | _(service default)_ | Sustained requests per second per caller. Unset or `0` keeps the service default. |

Per-service defaults, and what the limit is keyed on:

| Service | Burst | Per second | Keyed on |
|---|---|---|---|
| Send | 5000 | 2000 | API key ID |
| Admin | 1000 | 500 | API key ID |
| Inbox | 50 | 20 | JWT `user_id` |
| User | 50 | 20 | JWT `user_id` |

A key may also carry **its own** limit, which overrides the service default for that credential
alone — see [per-credential limits](#per-credential-limits) below.

> **By default the limit is enforced per replica, not per cluster.** Each pod holds its own
> in-memory buckets, so the cluster-wide ceiling is the configured rate **times the replica
> count** — and under an [HPA](deployment-guide.md#autoscaling) that ceiling moves with the
> autoscaler. At the production defaults, send's 2000/s is 6,000/s across 3 replicas and
> 40,000/s if it scales to 20. Turn on [distributed limiting](#distributed-rate-limiting) to
> make the configured rate the actual cluster-wide ceiling.

Rate limiting runs **after** authentication, so an unauthenticated flood is rejected with 401
before it reaches the credential limiter and never allocates a bucket. The
[per-IP limiter](#pre-authentication-per-ip-limiting) exists to bound that work.
`/healthz` and `/readyz` are never limited.

See [the integration guide](integration-guide.md#rate-limits) for the client-facing contract,
and [ADR 0016](adr/0016-distributed-rate-limiting-with-local-fallback.md) for why the design is
shaped this way.

### Pre-authentication per-IP limiting

A second limiter runs **before** authentication, keyed by source address. It is a flood bound,
not a quota: it exists so an invalid-credential flood is shed before it costs an HMAC and a
Redis lookup per request. It runs inside the Go services, so unlike the nginx ingress
annotations it works on any ingress controller, and on none.

| Variable | Default | Purpose |
|---|---|---|
| `HERMES_RATELIMIT_IP_ENABLED` | `true` | Set `false` to disable per-IP limiting. |
| `HERMES_RATELIMIT_IP_BURST` | _(service default)_ | Requests admitted instantaneously per address. |
| `HERMES_RATELIMIT_IP_PER_SECOND` | _(service default)_ | Sustained requests per second per address. |
| `HERMES_TRUSTED_PROXY_CIDRS` | _(empty)_ | Comma-separated CIDRs or IPs whose `X-Forwarded-For` may be believed. |

Per-service defaults: Send 5000/2000 and Admin 1000/500, matching their per-credential ceilings
so a legitimate caller behind one egress IP can still reach its documented limit. Inbox and User
default to 500/200 — far above their 20/s per-user rate, because many users share one address
behind a corporate NAT or a mobile carrier and a bound at the per-user rate would throttle a
whole office as one person.

> **`HERMES_TRUSTED_PROXY_CIDRS` needs setting behind a proxy, and must never be `0.0.0.0/0`.**
> Empty means trust no forwarding headers, so every request is attributed to the address of the
> immediate peer — behind an ingress controller that is the controller itself, and all callers
> collapse into a single bucket. Set it to your ingress controller's pod CIDR to get real client
> addresses. Trusting *every* peer is worse than not limiting at all: `X-Forwarded-For` is
> caller-supplied, so anyone could then pick their own bucket on every request.

The forwarded chain is walked from the **right**, taking the first address that is not itself a
trusted proxy — the leftmost entry is whatever the client chose to send.

### Distributed rate limiting

With this enabled the per-credential admission check runs **in Redis**, so the configured rate is
the cluster-wide ceiling rather than a per-pod one.

| Variable | Default | Purpose |
|---|---|---|
| `HERMES_RATELIMIT_DISTRIBUTED_ENABLED` | `false` | Run the per-credential check in Redis. Requires Redis. |

The algorithm is GCRA, evaluated inside a single Lua script, so the whole check is one round trip
and is atomic across replicas — there is no window in which two pods both believe they hold the
last token. The limit is exact; there is no approximation to size around.

Three properties worth knowing:

- **It costs one Redis round trip per authenticated request.** On the Send path that is roughly a
  50% increase in Redis operations, since authentication and idempotency already make one or two.
- **A Redis outage degrades, it does not reject.** The call is bounded at 100ms. On timeout or
  error the request is decided by the replica's **local bucket** instead, so behaviour falls back
  to per-replica enforcement — what you get with this setting off — rather than failing requests
  or removing the limit. Each fallback increments `hermes.http.rate_limit_backend_failures`.
- **That counter is worth alerting on.** While it is non-zero the advertised limit is no longer
  cluster-wide, and nothing else surfaces that: requests are still being served normally.

The [per-IP limiter](#pre-authentication-per-ip-limiting) is **never** sent to Redis, whatever
this is set to. It is a flood bound whose key space an attacker chooses, and forwarding it would
turn an address scan into Redis load.

### Per-credential limits

An API key may carry its own limit, overriding the service default for that credential alone.
Set it at creation, or afterwards through the Admin API:

```http
POST /v1/apikeys
{"name": "Acme", "rate_limit_per_second": 500, "rate_limit_burst": 1000}

PUT /v1/apikeys/{id}/rate-limit
{"per_second": 50, "burst": 100}
```

`PUT .../rate-limit` **replaces** the whole limit rather than patching it: omitted fields reset
to the service default, so `{}` clears the override entirely. That is deliberate — once
unmarshalled, JSON cannot distinguish an absent field from an explicit null, so a PATCH could
never tell "leave this alone" from "clear this".

Both values must be at least 1 if present. There is no "zero means unlimited" — omit the field
instead. The current values appear on `GET /v1/apikeys`.

Underneath, these are nullable `rate_limit_per_second` and `rate_limit_burst` columns on
`api_keys`. Unset for every existing key, so behaviour is unchanged until you set one.

Two timing properties worth knowing:

- **The limit rides the API key cache**, so it costs no extra lookup on the request path. The
  `PUT` endpoint invalidates that entry, so a change is visible immediately; a change made with
  direct SQL is not, and waits out the 5-minute TTL.
- **A caller's limit is pinned when their bucket is created** and held for that bucket's
  lifetime (30 minutes idle). A change therefore applies to the next new bucket rather than
  retuning one in use — it does not discard tokens a caller has already accrued.

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
| `hermes-natsprovision` | — (declares streams only) | — |
| `centrifugo` | `centrifugo.>` | `centrifugo.>` |

`hermes-admin`, `hermes-user` and `hermes-inbox` do not connect to NATS and have no credential.

Two of those rows are not services:

- **`hermes-natsprovision`** is a run-to-completion Job (ADR 0005 phase 4) and the **only**
  identity that may create or update a stream. Services hold `$JS.API.STREAM.INFO.<STREAM>` for
  the streams in `messaging.StreamsForService` and call `messaging.EnsureStreams` at boot, which
  exits non-zero if a stream is missing. Streams are provisioned the way the database schema is:
  by a Job that runs first. `STREAM.DELETE`, `STREAM.PURGE`, `CONSUMER.DELETE`, `DIRECT.GET`,
  `STREAM.MSG.GET`, `STREAM.LIST` and `$JS.API.INFO` are granted to **nobody**.
- **`centrifugo`** is the one **password** user on the bus, because `centrifugo:v5` exposes no
  NKey setting in any form — the credential can only travel in `nats_url`'s userinfo. Its
  password comes from `nats-nkeys/HERMES_CENTRIFUGO_NATS_PASSWORD` for the server and from
  `hermes-secrets/HERMES_CENTRIFUGO_NATS_URL` for Centrifugo; `go run ./cmd/natskeys` emits both
  from one value because nothing in the cluster checks they match. Centrifugo's TLS is configured
  by `nats_tls` in `centrifugo-config.json` — a **map**, not a bool, and absent from
  `centrifugo --help` entirely.

Three things follow that are easy to trip over:

- **A subject added in code needs a permission added here**, or the service fails at runtime
  rather than at deploy. `TestAccounts_ConfCoversEverySubjectTheCodeUses` in
  `internal/messaging` is the guard: it fails if a subject in `messaging.Streams` has no grant.
  `TestAccounts_StreamInfoGrantsMatchStreamsForService` and
  `TestAccounts_OnlyTheProvisionerMayDeclareStreams` guard the stream grants in both directions.
- **JetStream is a separate permission surface.** Publishing to a stream subject is not enough;
  declaring a stream needs `$JS.API.STREAM.CREATE|UPDATE.<STREAM>` (provisioner only), reading
  one needs `$JS.API.STREAM.INFO.<STREAM>`, and consuming needs `$JS.API.CONSUMER.CREATE`,
  `$JS.API.CONSUMER.MSG.NEXT` and `$JS.ACK` for that one stream and consumer name. A missing
  JetStream grant looks like a broken stream, not a permissions problem — the request simply
  never gets a reply.
- **Each connection's reply inbox is scoped to `_INBOX.<service>`** by
  `messaging.WithIdentity`. JetStream delivers pulled messages to the client's inbox, so a user
  permitted to subscribe to `_INBOX.>` would receive copies of every other service's messages
  and read `delivery.*` without any `delivery.*` permission. The narrow subscribe lists and the
  client-side prefix are one mechanism; changing either alone breaks it.

Generate a matched key set with `go run ./cmd/natskeys` (`-format json` for the payload the
staging and production ExternalSecrets read). Each key's public half becomes a
`$HERMES_NKEY_*` variable in the NATS server's environment; the seed is mounted into that one
service's pod. Rotating one half of a pair alone locks that service out of the bus.

#### Setting `HERMES_CENTRIFUGO_NATS_PASSWORD` by hand

**The first character must be an ASCII letter.** If you rotate this password yourself rather
than taking what `cmd/natskeys` emits, this is not a style preference — get it wrong and
**nats-server will not start**.

`nats-accounts.conf` reads the password as an unquoted `$VARIABLE`, and nats-server resolves
such a reference by *re-parsing the value as a configuration document*. So the value has to
lex as a bare conf value, and these do not:

| Password | What nats-server does |
|---|---|
| `-Xk3f…` | `Parse error: 'Expected a digit but got 'X''` — a leading `-` starts a negative number |
| `12-Xk3f…` | `All ISO8601 dates must be in full Zulu form` — a leading digit starts a number, and a later `-` makes it a date |
| `2p2Xk3f…` | `Expected a top-level value to end…` — a size suffix (`kKmMgGtTpPeE`) then a digit ends the number early |
| `1234567890`, `true`, `false` | No parse error at all: the value reaches the server as an integer or a bool instead of a string |

A leading letter avoids every one of these, whatever follows it — `-` and `_` later in the
value are fine. `cmd/natskeys` guarantees it by redrawing; nothing stops a hand-written
`kubectl create secret` from ignoring it. About 2.3% of unconstrained 43-character base64url
values hit a failing shape, which is intermittent enough to look like anything but the cause.

Do **not** try to fix this by quoting the reference in `nats-accounts.conf`. NATS only treats
an *unquoted* token as a variable, so quoting stops the lookup happening and sets Centrifugo's
password to the literal string `$HERMES_CENTRIFUGO_NATS_PASSWORD` — no parse error, no log
line, and a credential that is committed in git.
`TestAccounts_CentrifugoPasswordReferenceMustNotBeQuoted` fails if anyone tries.

Also: an **unset** variable is a parse error, but an **empty** one is not. Setting this to `""`
starts the server and lets anyone connect as the `centrifugo` user with no credential. Verified
on the wire; see `TestCentrifugoPassword_EmptyVariableIsAcceptedAndAuthenticates`.

**On Kubernetes, the NATS StatefulSet now refuses to start rather than accepting either
mistake.** An initContainer (`require-centrifugo-password` in
`deploy/k8s/base/infra/nats.yaml`) checks the variable before nats-server runs and fails the pod
with a message naming the rule — so a half-provisioned cluster stays down instead of serving
while unauthenticated. It rejects an empty or unset value, a first character that is not an
ASCII letter, and the two values a leading letter does *not* save, `true` and `false`. The local
overlay deletes the container by name: it drops `-c nats.conf`, never reads this file, and
legitimately has no password. This does not help you outside Kubernetes — the constraint is
still yours to honour if you run nats-server directly.

### Centrifugo allowed origins

**Realtime does not work in a browser until this is set.** It is not a Hermes variable — it is
Centrifugo's own, and it is the one piece of realtime configuration whose absence is invisible
from every direction except a real browser.

| Where | Setting |
|---|---|
| `deploy/k8s/overlays/local` | `allowed_origins` in `centrifugo-config.json` (top-level: the v5 image) |
| `deploy/k8s/overlays/{staging,production}` | `CENTRIFUGO_ALLOWED_ORIGINS` on the Centrifugo Deployment, space-separated, substituted at deploy time like `DOMAIN_PLACEHOLDER` |
| Helm chart | `centrifugo.config.client.allowed_origins` (nested under `client`: the sub-chart ships v6) |

The inbox widget is embedded in **your** application, so the browser presents your origin while
the socket lives on the Hermes domain. Every connection is cross-origin by construction, and
Centrifugo answers `403` at the websocket handshake to any origin not listed.

Since [ADR 0017](adr/0017-realtime-transport-ladder.md) this one setting covers two mechanisms.
The WebSocket handshake is exempt from CORS and Centrifugo enforces the list as its own `Origin`
check; the `http_stream` and `sse` fallbacks are ordinary CORS-governed requests whose preflights
Centrifugo answers from the same list. A correct value therefore makes the entire ladder work
cross-origin with no CORS middleware on any Hermes service — and a missing one now fails all three
transports, presenting as a CORS error on the fallbacks and an opaque handshake failure on the
websocket.

What makes it expensive to diagnose is the asymmetry: Centrifugo **permits connections that
carry no `Origin` header at all**, "as they typically originate from non-browser environments".
So `/health` returns 200, `curl` connects, every server-side client connects, the pods are Ready
and stay Ready — and no browser can connect. The service looks perfect and serves nobody.

Two gates now hold this:

- `scripts/check_centrifugo_origins.py`, run by `make verify-manifests` against all three
  overlays, fails when the key is absent or empty. Pass `--forbid-placeholder` in a deploy
  pipeline to also reject an unsubstituted `ALLOWED_ORIGINS_PLACEHOLDER`.
- `tests/browser/global-setup.ts` performs a real handshake carrying an `Origin` before any spec
  runs, so the live suite reports this in seconds rather than as a wall of widget failures.

Note the version split: the kustomize overlays run `centrifugo/centrifugo:v5`, where the option
is top-level and the env var is `CENTRIFUGO_ALLOWED_ORIGINS`. The Helm sub-chart ships
Centrifugo 6.6.2, where it moved under `client` and the env var became
`CENTRIFUGO_CLIENT_ALLOWED_ORIGINS`. Config written for one is silently ignored by the other.

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
