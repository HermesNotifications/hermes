# Configuration Reference

Complete reference for Hermes Helm chart values with examples.

## Global Settings

```yaml
hermes:
  # JWT secret for user-facing APIs (Inbox, User services)
  jwt:
    secret: ""                    # Required (or use existingSecret)
    existingSecret: ""            # Name of existing K8s secret
    existingSecretKey: "jwt-secret"

  # API key HMAC secret for server-to-server Admin API
  apiKey:
    hmacSecret: ""                # Required (or use existingSecret)
    existingSecret: ""            # Name of existing K8s secret
    existingSecretKey: "api-key-hmac-secret"

  # Log level for all services (debug, info, warn, error)
  logLevel: info
```

## Image Configuration

```yaml
image:
  registry: ghcr.io/hermesnotifications
  tag: ""                         # Defaults to chart appVersion
  pullPolicy: IfNotPresent

imagePullSecrets: []
```

## Email Configuration

### SMTP

```yaml
hermes:
  email:
    provider: smtp
    smtp:
      host: smtp.example.com
      port: 587
      username: hermes
      password: ""
      from: "notifications@example.com"
```

### Amazon SES (via webhook worker)

Configure the email worker to call your SES sending endpoint:

```yaml
hermes:
  email:
    provider: webhook
    webhook:
      url: "https://your-ses-proxy.example.com/send"
      authHeader: "Authorization"
      authToken: "Bearer your-token"
```

### SendGrid (via webhook worker)

```yaml
hermes:
  email:
    provider: webhook
    webhook:
      url: "https://api.sendgrid.com/v3/mail/send"
      authHeader: "Authorization"
      authToken: "Bearer SG.your-api-key"
```

## SMS Configuration

```yaml
hermes:
  sms:
    provider: webhook
    webhook:
      url: "https://your-sms-gateway.example.com/send"
      authHeader: "Authorization"
      authToken: "Bearer your-token"
```

## Observability

### OpenTelemetry

```yaml
hermes:
  otel:
    enabled: true
    endpoint: "otel-collector.monitoring:4317"
    insecure: true
    samplingRatio: 0.1             # Sample 10% of traces
```

### Datadog

```yaml
hermes:
  datadog:
    enabled: true
    # Uses DD_AGENT_HOST and DD_ENTITY_ID from the Datadog admission controller
    # No additional configuration needed if using the Datadog Operator
```

### Prometheus (ServiceMonitor)

```yaml
serviceMonitor:
  enabled: true
  namespace: monitoring            # Namespace where Prometheus Operator watches
  interval: 30s
  labels:
    release: prometheus
```

## Per-Service Overrides

Each service under `services.*` supports the following fields:

```yaml
services:
  admin:
    replicaCount: 1
    port: 8080
    resources:
      requests:
        cpu: 100m
        memory: 128Mi
      limits:
        cpu: 500m
        memory: 256Mi
    nodeSelector: {}
    tolerations: []
    affinity: {}
    topologySpreadConstraints: []
    podAnnotations: {}
    podLabels: {}
    env: []                        # Additional env vars
    autoscaling:
      enabled: false
      minReplicas: 1
      maxReplicas: 10
      targetCPUUtilizationPercentage: 80
    podDisruptionBudget:
      enabled: false
      minAvailable: 1
```

### Available Services

| Service | Key | Default Port | Description |
|---------|-----|------|-------------|
| Admin API | `services.admin` | 8080 | Tenant management, notification sending |
| Send | `services.send` | 8088 | Lightweight ingestion layer |
| Dispatch | `services.dispatch` | 8081 | Template resolution, channel fan-out |
| Event Writer | `services.workerEvents` | 8082 | Batch event persistence |
| Email Worker | `services.workerEmail` | 8083 | Email delivery |
| SMS Worker | `services.workerSms` | 8084 | SMS delivery |
| Inbox Worker | `services.workerInbox` | 8085 | In-app notification delivery |
| Inbox Service | `services.inbox` | 8086 | User inbox API |
| User Service | `services.user` | 8087 | User preferences API |

## Ingress

```yaml
ingress:
  enabled: false
  className: nginx
  host: hermes.example.com
  annotations: {}
  tls: []
```

## External Services

### PostgreSQL

```yaml
postgresql:
  enabled: true                    # Set to false for external DB

externalPostgresql:
  url: ""                          # Direct URL
  existingSecret: ""               # Or reference a secret
  existingSecretKey: "url"
```

### NATS

```yaml
nats:
  enabled: true                    # Set to false for external NATS

externalNats:
  url: ""
```

### Redis

```yaml
redis:
  enabled: true                    # Set to false for external Redis

externalRedis:
  url: ""
  existingSecret: ""
  existingSecretKey: "url"
```

### Centrifugo

```yaml
centrifugo:
  enabled: true

externalCentrifugo:
  apiUrl: ""
  apiKey: ""
```

## Migration Job

```yaml
migration:
  enabled: true
  backoffLimit: 3
  resources:
    requests:
      cpu: 100m
      memory: 128Mi
```

## Cleanup CronJob

```yaml
cleanup:
  enabled: false
  schedule: "0 3 * * *"           # Daily at 3 AM
  retentionDays: 90
  resources:
    requests:
      cpu: 100m
      memory: 128Mi
```

## Admin Portal

```yaml
adminPortal:
  enabled: false
  replicaCount: 1
  image:
    repository: ghcr.io/hermesnotifications/hermes-admin-portal
    tag: ""
  port: 3000
```

## Network Policies

```yaml
networkPolicies:
  enabled: false
```

When enabled, creates NetworkPolicy resources that restrict ingress and egress traffic to only what each service requires.
