# Production Hardening

This guide covers the steps to move from an evaluation install to a production-grade Hermes deployment.

## Turn on production mode first

```yaml
hermes:
  env: production
```

This sets `HERMES_ENV`, and every Hermes process — services, the migration Job, the NATS
stream provisioner Job and the cleanup CronJob — then validates its own configuration at
startup and **exits rather than running insecurely**. Only the literal string `development`
relaxes those checks; the chart's schema constrains the value to `development` or
`production` so a typo is caught by `helm install` instead of surfacing later as a confusing
runtime failure.

Production mode is not a switch you flip at the end. It defines what the rest of this page
has to deliver:

| Check | What it requires |
|---|---|
| `HERMES_DATABASE_URL` | `sslmode=require`, `verify-ca` or `verify-full`. `allow` and `prefer` are rejected because both silently fall back to plaintext. |
| `HERMES_REDIS_URL` | the `rediss://` scheme |
| `HERMES_NATS_URL` | the `tls://` scheme |
| `hermes.jwt.secret` | anything other than the built-in `hermes-jwt-secret` |
| `hermes.apiKey.hmacSecret` | anything other than the built-in `hermes-dev-hmac-secret` |
| `hermes.centrifugo.apiKey` | must be set — leaving it empty falls back to the published default `centrifugo-api-key` |

The last three defaults are committed to this public repository. A deployment still using one
does not have a weak secret; it has a published constant.

> **Production mode and the bundled sub-charts are mutually exclusive.** The chart builds the
> bundled datastore URLs as `postgres://…?sslmode=disable`, `redis://…` and `nats://…`, and
> the bundled Redis runs with `auth.enabled: false`. All three fail the checks above. Moving
> to production therefore means disabling all four sub-charts and completing the `external*`
> sections below — it is not an incremental hardening step. See
> [Bundled infrastructure is for evaluation](configuration.md#bundled-infrastructure-is-for-evaluation).

## External PostgreSQL

Disable the bundled PostgreSQL sub-chart and point Hermes at your managed database (e.g., RDS, Cloud SQL, Azure Database for PostgreSQL):

```yaml
postgresql:
  enabled: false

externalPostgresql:
  url: "postgres://hermes:secret@my-rds-instance.abc123.us-east-1.rds.amazonaws.com:5432/hermes?sslmode=require"
```

Or reference an existing Kubernetes secret:

```yaml
postgresql:
  enabled: false

externalPostgresql:
  existingSecret: hermes-db-credentials
  existingSecretKey: url
```

> `existingSecretKey` defaults to `HERMES_DATABASE_URL`, not `url`. The example above works
> because it sets the key explicitly to match the Secret you created. If you omit
> `existingSecretKey`, your Secret must have a key named `HERMES_DATABASE_URL` — otherwise the
> pod is stuck in `CreateContainerConfigError`. The same applies to `externalRedis`
> (`HERMES_REDIS_URL`), `externalNats` (`HERMES_NATS_URL`) and `externalCentrifugo`
> (`centrifugo-api-key`).

## External Redis

Same pattern for Redis. Note the `rediss://` scheme — under `hermes.env: production`
a plain `redis://` URL is rejected at startup:

```yaml
redis:
  enabled: false

externalRedis:
  url: "rediss://:secret@my-elasticache.abc123.cache.amazonaws.com:6379"
```

Or with an existing secret:

```yaml
redis:
  enabled: false

externalRedis:
  existingSecret: hermes-redis-credentials
  existingSecretKey: url
```

## External NATS

For a managed or externally operated NATS cluster. The `tls://` scheme is required under
`hermes.env: production`:

```yaml
nats:
  enabled: false

externalNats:
  url: "tls://my-nats-cluster.example.com:4222"
```

> **The chart cannot currently reach a NATS cluster that verifies a private CA or requires
> NKey authentication.** `HERMES_NATS_CA_BUNDLE` and `HERMES_NATS_NKEY_SEED` name *files* that
> have to be mounted into every pod, and the chart exposes no values to mount them — no
> volume, no Secret reference. A `tls://` URL works when the server certificate chains to a
> root already in the system trust store and the server accepts anonymous connections;
> anything stricter needs a chart change or a post-render patch. This is a known gap, not a
> configuration you have got wrong. (The Kustomize deployment under `deploy/k8s/` does mount
> both, so the two paths are not at parity.)

### NATS stream provisioning

Hermes services do not create JetStream streams. `EnsureStreams`
(`internal/messaging/provision.go`) only verifies that the streams a service depends on exist
and refuses to start otherwise, which is what allows the runtime identities to hold read-only
JetStream grants rather than `STREAM.CREATE`. The chart ships a provisioner Job
(`natsProvision.*`) that declares `NOTIFICATIONS`, `DELIVERY`, `EVENTS` and `DLQ`. You do not
run it by hand.

It is an ordinary tracked resource, **not a Helm hook** — as a `pre-install` hook it could
not see the release ConfigMap or the bundled NATS it provisions, and as a `post-install` hook
it would never run at all under `--wait` or `--atomic`, because Helm blocks waiting for the
very services this Job unblocks. [ADR 0008](../adr/0008-helm-chart-provisioning-jobs-are-not-hooks.md)
records the decision and the measurements behind it.

Being applied alongside the Deployments rather than before them, on a **first install the
services will `CrashLoopBackOff` for a minute or two** with:

```
stream NOTIFICATIONS is not available to hermes-send (has cmd/natsprovision run?)
```

**That is expected and self-correcting**, not a failed install. The services fail closed
rather than running against a bus that is not ready, and Kubernetes' restart backoff carries
them across until the Job finishes. Wait before you start debugging. The same message is a
real failure only if it persists — for example if you disabled `natsProvision` against a bus
where the streams were never declared some other way.

Both this Job and the migration Job are named per release revision
(`<release>-natsprovision-<revision>`). A Job's pod template is immutable, so a stable name
would fail on the second install; with no hook machinery to delete the previous one, the
revision suffix is what makes repeat upgrades work.

For a production install, use `helm install --wait` (or `--atomic`, which additionally rolls
back on failure). Both are supported, and `--wait` is what makes the install fail on a broken
migration rather than returning successfully and leaving you to notice later. On subsequent
upgrades add `--wait-for-jobs` as well — see
[the flag notes](configuration.md#--wait-and---atomic) for why that is upgrade-specific.

## External Centrifugo

The bundled Centrifugo sub-chart runs the **in-memory engine**. A message published through
one Centrifugo pod reaches only the clients connected to that pod, so realtime push does not
fan out across replicas — correct at one replica, and silently lossy above one. That is an
evaluation posture, not something to tune; a production realtime deployment needs Centrifugo
with the Redis engine, operated outside this chart.

```yaml
centrifugo:
  enabled: false

externalCentrifugo:
  apiUrl: "https://centrifugo.example.com"
  existingSecret: hermes-centrifugo
  existingSecretKey: centrifugo-api-key
  # Name of a Service you create yourself, so the chart can route /realtime to it.
  ingressServiceName: centrifugo-external
```

There is no `externalCentrifugo.apiKey` — the key always comes from a Secret on this path.
(`hermes.centrifugo.apiKey` is the bundled-sub-chart equivalent, and is one of the values
`hermes.env: production` refuses to leave at its default.)

Centrifugo must be configured to validate the same tokens Hermes issues: its token secret has
to equal `HERMES_JWT_SECRET`.

### What your Centrifugo has to be configured with

Most of these are load-bearing in a way that is invisible when they are missing: the connection
succeeds, the widget reports itself connected, and notifications are quietly lost. Configure all
of them.

| Setting | Why the client needs it |
|---|---|
| Redis engine + a broker | Hermes publishes through whichever Centrifugo node it reaches. Without a shared engine, a publication is delivered only to clients connected to *that* node. |
| `allow_user_limited_channels` | Every subscription is to `user#<internal-user-id>`. Without this, Centrifugo does not treat the `user#` prefix as a user-limited channel and the subscription is refused. |
| `http_stream` + `sse` | The two fallback rungs of the client's transport ladder. Their absence is invisible to everyone whose network permits websockets — which includes you, your CI and your monitoring. It shows up only as a user on a corporate proxy whose inbox loads once and never updates again. Enabling either also enables the `/emulation` endpoint they both need, which your ingress must route. |
| `allowed_origins` | Centrifugo answers `403` at the websocket handshake to any browser origin not listed — while permitting connections that carry no `Origin` at all, so health checks and `curl` succeed and only real browsers are refused. It also governs CORS for the two fallback transports above, so a wrong value now breaks all three. An embedded widget lives on *your* origin, not the Hermes one. |

`presence`, `history_size` and `history_ttl` are conventional rather than load-bearing. The SDK
deliberately does **not** subscribe with `recoverable`/`positioned`: Hermes publishes over a NATS
broker, which is at-most-once and keeps no history, so a replay request would be accepted and
silently do nothing. Gap repair happens a layer up instead — the client refetches from the Hermes
API on every reconnect, which is also what covers a fallback transport dropping and re-establishing.

Values for the v6 configuration schema (the v5 keys are flat and differ — check
`centrifugo defaultconfig` against the image you are running):

```yaml
engine:
  type: redis
  redis:
    address: "rediss://:PASSWORD@your-redis:6379/1"
client:
  allowed_origins: ["https://app.example.com"]
  token:
    hmac_secret_key: "<the same value as HERMES_JWT_SECRET>"
# Fallback transports. Either one also enables /emulation, which carries the
# client->server half and which your ingress must route alongside /connection.
http_stream:
  enabled: true
sse:
  enabled: true
channel:
  without_namespace:
    presence: true
    history_size: 50
    history_ttl: "1h"
    allow_user_limited_channels: true
http_api:
  key: "<the same value as HERMES_CENTRIFUGO_API_KEY>"
```

Run at least three replicas behind a PodDisruptionBudget. Do not add session affinity: with a
shared engine any node can serve any user, and pinning would concentrate reconnects on one pod.
That holds for the fallback transports too — each client→server command is an independent POST to
`/emulation` that any node can answer, which is precisely why these replaced SockJS, whose
long-polling did require sticky sessions. Do make sure your proxy does not buffer responses:
`http_stream` and `sse` are single responses the server never finishes writing, and a buffering
proxy holds each publication waiting for an end that never comes.
Do give it a generous `terminationGracePeriodSeconds` — every socket on a terminating pod
reconnects at once, and a graceful exit is what makes clients back off instead of hot-looping.

## Secrets Management

### Using existingSecret

Instead of passing secrets in `values.yaml`, create a Kubernetes secret and reference it:

```bash
kubectl create secret generic hermes-app-secrets -n hermes \
  --from-literal=jwt-secret="$(openssl rand -base64 32)" \
  --from-literal=api-key-hmac-secret="$(openssl rand -base64 32)"
```

```yaml
hermes:
  jwt:
    existingSecret: hermes-app-secrets
    existingSecretKey: jwt-secret
  apiKey:
    existingSecret: hermes-app-secrets
    existingSecretKey: api-key-hmac-secret
```

> **Do not name it `<release>-secrets`.** The chart creates a Secret of that name itself —
> `hermes-secrets` for a release called `hermes` — and it does so even when every value inside
> it has been redirected to an `existingSecret`. Creating your own Secret with the same name
> makes `helm install` fail with *"Secret hermes-secrets exists and cannot be imported into
> the current release"*. Earlier revisions of this page used exactly that name. Any other
> name works.

### External Secrets Operator

For secrets stored in AWS Secrets Manager, Vault, or similar:

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: hermes-app-secrets
  namespace: hermes
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: aws-secrets-manager
    kind: ClusterSecretStore
  target:
    # Must not be <release>-secrets — see the warning above.
    name: hermes-app-secrets
  data:
    - secretKey: jwt-secret
      remoteRef:
        key: hermes/production
        property: jwt-secret
    - secretKey: api-key-hmac-secret
      remoteRef:
        key: hermes/production
        property: api-key-hmac-secret
    - secretKey: database-url
      remoteRef:
        key: hermes/production
        property: database-url
```

## TLS

Enable TLS on the ingress:

```yaml
global:
  domain: hermes.example.com

ingress:
  enabled: true
  tls:
    - secretName: hermes-tls
      hosts:
        - hermes.example.com
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
```

## Resource Tuning

Set resource requests and limits per service based on your expected load:

```yaml
admin:
  resources:
    requests:
      cpu: 250m
      memory: 256Mi
    limits:
      cpu: "1"
      memory: 512Mi

dispatch:
  resources:
    requests:
      cpu: 500m
      memory: 256Mi
    limits:
      cpu: "2"
      memory: 512Mi

workerEmail:
  resources:
    requests:
      cpu: 100m
      memory: 128Mi
    limits:
      cpu: 500m
      memory: 256Mi
```

Adjust based on your notification volume. The dispatch and worker services are the most CPU-intensive under load.

## High Availability

### Autoscaling

Enable horizontal pod autoscaling for high-throughput services:

```yaml
admin:
  autoscaling:
    enabled: true
    minReplicas: 2
    maxReplicas: 10
    targetCPUUtilizationPercentage: 70

dispatch:
  autoscaling:
    enabled: true
    minReplicas: 2
    maxReplicas: 20
    targetCPUUtilizationPercentage: 70

workerEmail:
  autoscaling:
    enabled: true
    minReplicas: 2
    maxReplicas: 10
    targetCPUUtilizationPercentage: 70
```

### Topology Spread

Spread pods across availability zones:

```yaml
admin:
  topologySpreadConstraints:
    - maxSkew: 1
      topologyKey: topology.kubernetes.io/zone
      whenUnsatisfiable: DoNotSchedule
      labelSelector:
        matchLabels:
          app.kubernetes.io/name: hermes-admin
```

### Multiple Replicas (without autoscaling)

For simpler setups, set a fixed replica count:

```yaml
admin:
  replicas: 3

dispatch:
  replicas: 3
```

## Backup and Restore

Finding 40 of the [2026-07-27 review](../reviews/2026-07-27-architecture-review.md) noted
this page had no backup section at all. What follows is the honest state rather than a
procedure that has been rehearsed — **none of it has been tested against a restore**, and a
backup you have not restored from is a hypothesis.

**What holds state:**

| Store | Contains | Losing it means |
|---|---|---|
| Postgres | Everything durable: organizations, users, contact points, subscriptions, templates, notifications, events, API keys, JWT signing keys | Total loss |
| Redis | Cache, idempotency dedup window, Centrifugo presence | Duplicate sends inside the dedup window; no permanent loss |
| NATS JetStream | In-flight messages and the DLQ | Notifications accepted but not yet delivered; dead letters awaiting replay |
| DynamoDB (if enabled) | The hot notification and event path | Recent notifications and events |

**Postgres** is the one that matters. Take periodic `pg_dump` output and store it off-cluster:

```bash
pg_dump "$HERMES_DATABASE_URL" --format=custom --file=hermes-$(date +%F).dump
# Restore into an empty database, then run migrations to confirm the version matches:
pg_restore --dbname="$HERMES_DATABASE_URL" --clean --if-exists hermes-2026-07-29.dump
```

Two things to verify on any restore, because neither is obvious:

- `jwt_signing_keys` is restored with it. The internal signing key is written once on first
  startup and never overwritten, so a restore that loses that row and a service that boots
  with a different `HERMES_JWT_SECRET` will silently invalidate every issued JWT. See
  [architecture.md](../architecture.md).
- `schema_migrations` must match the deployed image. Restoring an older dump under a newer
  image leaves migrations unapplied — `cmd/migrate` only migrates forward.

**NATS JetStream** persistence depends on your deployment's `store_dir` volume. The DLQ uses
Limits retention and is the one stream worth preserving deliberately: it holds messages that
failed and are awaiting replay. Losing it discards them silently.

**Redis** needs no backup by design. Losing it re-opens the idempotency dedup window, so a
client retry inside that window can produce a duplicate notification.

**DynamoDB**, if enabled, has **no backup mechanism in this repository** — not a
point-in-time-recovery setting, not an export. Note also that `cmd/cleanup` does not run on
that path, so retention is entirely TTL-driven and unverified. If you enable the DynamoDB
store in production, treat backup as work you still have to do.

## Network Policies

Enable network policies to restrict traffic between services:

```yaml
networkPolicy:
  enabled: true
```

> **The chart's NetworkPolicy is far more permissive than it appears.** Every rule in
> `charts/hermes/templates/networkpolicy.yaml` specifies ports with no `to:` or `from:`
> peers, and an empty peer list in Kubernetes means *all* peers — so enabling this restricts
> which ports are used, not who may talk to whom. Finding 41 of the review covers it. Do not
> read `enabled: true` as isolation.

This creates policies that only allow traffic between Hermes services and their required infrastructure (PostgreSQL, NATS, Redis). See the [configuration reference](configuration.md) for details.

