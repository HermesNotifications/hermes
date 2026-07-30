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
(`natsProvision.*`) that declares `NOTIFICATIONS`, `DELIVERY`, `EVENTS` and `DLQ` as a Helm
`pre-install`/`pre-upgrade` hook, so ordering is automatic.

If you disable it against a bus where the streams have not been declared some other way,
every service crash-loops with `stream NOTIFICATIONS is not available to hermes-send (has
cmd/natsprovision run?)`. That is the intended failure — a service never runs against a bus
that is not ready.

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

