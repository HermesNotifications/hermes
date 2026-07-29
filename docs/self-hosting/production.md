# Production Hardening

This guide covers the steps to move from an evaluation install to a production-grade Hermes deployment.

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

## External Redis

Same pattern for Redis:

```yaml
redis:
  enabled: false

externalRedis:
  url: "redis://:secret@my-elasticache.abc123.cache.amazonaws.com:6379"
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

For a managed or externally operated NATS cluster:

```yaml
nats:
  enabled: false

externalNats:
  url: "nats://my-nats-cluster.example.com:4222"
```

## Secrets Management

### Using existingSecret

Instead of passing secrets in `values.yaml`, create a Kubernetes secret and reference it:

```bash
kubectl create secret generic hermes-secrets -n hermes \
  --from-literal=jwt-secret="$(openssl rand -base64 32)" \
  --from-literal=api-key-hmac-secret="$(openssl rand -base64 32)"
```

```yaml
hermes:
  jwt:
    existingSecret: hermes-secrets
    existingSecretKey: jwt-secret
  apiKey:
    existingSecret: hermes-secrets
    existingSecretKey: api-key-hmac-secret
```

### External Secrets Operator

For secrets stored in AWS Secrets Manager, Vault, or similar:

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: hermes-secrets
  namespace: hermes
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: aws-secrets-manager
    kind: ClusterSecretStore
  target:
    name: hermes-secrets
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

