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

## Network Policies

Enable network policies to restrict traffic between services:

```yaml
networkPolicy:
  enabled: true
```

This creates policies that only allow traffic between Hermes services and their required infrastructure (PostgreSQL, NATS, Redis). See the [configuration reference](configuration.md) for details.

