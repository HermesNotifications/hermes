# Helm Chart Distribution Design

## Overview

Package Hermes as a public Helm chart so platform engineers can deploy the full notification stack on their own Kubernetes infrastructure with a single `helm install`. The chart bundles all infrastructure dependencies (Postgres, NATS, Redis, Centrifugo) by default for quick evaluation, with toggles to point at external/managed instances for production use.

## Target Audience

DevOps and platform engineers comfortable with Kubernetes who want to run Hermes on their own clusters. They know Helm, manage their own infrastructure, and expect a `values.yaml`-driven configuration experience.

## Chart Structure

Single umbrella chart with infrastructure sub-chart dependencies:

```
charts/hermes/
  Chart.yaml
  values.yaml
  values.schema.json
  templates/
    _helpers.tpl
    configmap.yaml
    secrets.yaml
    migration-job.yaml
    services/
      admin.yaml
      dispatch.yaml
      send.yaml
      inbox.yaml
      user.yaml
      worker-email.yaml
      worker-sms.yaml
      worker-inbox.yaml
      worker-events.yaml
    admin-portal/
      deployment.yaml
      service.yaml
    ingress.yaml
    networkpolicy.yaml
    servicemonitor.yaml
  charts/
```

Each service template contains a Deployment, Service, and optional HPA. All follow the same shape for consistency.

## Sub-Chart Dependencies

| Dependency | Chart | Condition | Default |
|---|---|---|---|
| PostgreSQL | bitnami/postgresql | `postgresql.enabled` | true |
| NATS | nats/nats | `nats.enabled` | true |
| Redis | bitnami/redis | `redis.enabled` | true |
| Centrifugo | centrifugal/centrifugo | `centrifugo.enabled` | true |

When a sub-chart is disabled, the user provides connection details via `external<Service>` values instead.

## Configuration Surface

### Minimal Config (Evaluation)

```yaml
global:
  domain: hermes.example.com

hermes:
  jwt:
    secret: "change-me-in-production"
  apiKey:
    hmacSecret: "change-me-in-production"
```

### Full Values Structure

**Global settings:**

```yaml
global:
  image:
    registry: ghcr.io/yourorg
    tag: ""  # defaults to chart appVersion
  domain: hermes.example.com
```

All services share the same registry and tag by default (same monorepo, same release). Per-service image overrides are available.

**Core Hermes config:**

```yaml
hermes:
  jwt:
    secret: ""
    existingSecret: ""
    existingSecretKey: "jwt-secret"
  apiKey:
    hmacSecret: ""
    existingSecret: ""
    existingSecretKey: "hmac-secret"
  email:
    provider: smtp             # smtp | ses | sendgrid
    from: noreply@example.com
    smtp:
      host: mailpit
      port: 1025
    ses:
      region: ""
    existingSecret: ""
  sms:
    webhookUrl: ""
  events:
    retentionDays: 90
```

Every secret supports both inline values (for evaluation) and `existingSecret` references (for production).

**Per-service config (same shape for all 9 services):**

```yaml
admin:
  replicas: 1
  port: 8080
  image:
    repository: hermes-admin
    tag: ""                    # override global tag
  resources:
    requests: { cpu: 50m, memory: 128Mi }
    limits: { memory: 256Mi }
  autoscaling:
    enabled: false
    minReplicas: 2
    maxReplicas: 10
    targetCPU: 80
  podAnnotations: {}
  nodeSelector: {}
  tolerations: []
  affinity: {}
```

**Optional components:**

```yaml
adminPortal:
  enabled: false
  replicas: 1
  image:
    repository: hermes-admin-portal
    tag: ""
  resources: {}
```

**Ingress:**

```yaml
ingress:
  enabled: true
  className: ""
  annotations: {}
  tls: []
```

**Observability:**

```yaml
observability:
  enabled: false
  provider: otel               # otel | datadog
  otel:
    endpoint: ""
    samplingRate: 0.1
  datadog:
    enabled: false
```

Off by default. OTel is the vendor-neutral option; Datadog supported for users who want it.

**External services (used when sub-chart is disabled):**

```yaml
externalPostgresql:
  url: ""
  existingSecret: ""
  existingSecretKey: "database-url"

externalNats:
  url: ""

externalRedis:
  url: ""
  existingSecret: ""
  existingSecretKey: "redis-url"

externalCentrifugo:
  apiUrl: ""
  apiKey: ""
  existingSecret: ""
```

### Values Schema Validation

`values.schema.json` validates at install time. Key validations:

- If `postgresql.enabled: false`, then `externalPostgresql.url` or `externalPostgresql.existingSecret` must be set (same for all sub-charts)
- `hermes.jwt.secret` or `hermes.jwt.existingSecret` must be set
- `hermes.apiKey.hmacSecret` or `hermes.apiKey.existingSecret` must be set
- `hermes.email.provider` must be one of `smtp`, `ses`, `sendgrid`

## Migration Strategy

Database migrations run as a Helm pre-install/pre-upgrade hook:

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: {{ include "hermes.fullname" . }}-migrate
  annotations:
    "helm.sh/hook": pre-install,pre-upgrade
    "helm.sh/hook-weight": "0"
    "helm.sh/hook-delete-policy": hook-succeeded
spec:
  backoffLimit: 3
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: migrate
          image: {{ registry }}/hermes-migrate:{{ tag }}
          env:
            - name: HERMES_DATABASE_URL
              valueFrom: secretKeyRef
```

Services only become ready after migrations complete. Failed migrations block the deploy entirely — no partial upgrades with schema mismatches.

## Service Wiring

A single ConfigMap provides `HERMES_` environment variables to all services. Template logic handles the sub-chart vs. external toggle:

```yaml
# templates/configmap.yaml
data:
  HERMES_NATS_URL: >-
    {{ if .Values.nats.enabled }}
    nats://{{ .Release.Name }}-nats:4222
    {{ else }}
    {{ .Values.externalNats.url }}
    {{ end }}
```

Each service Deployment uses `envFrom` to load the shared ConfigMap and Secret, plus service-specific env vars (like `HERMES_HTTP_PORT`).

## Ingress

Single Ingress resource with path-based routing:

| Path | Service |
|---|---|
| `/admin/v1` | admin |
| `/inbox/v1` | inbox |
| `/user/v1` | user |
| `/send/v1` | send |
| `/centrifugo` | centrifugo |

Internal-only services (dispatch, workers, event-writer) have no ingress path — they communicate only via NATS.

## Distribution

### OCI Registry (Primary)

```bash
helm install hermes oci://ghcr.io/yourorg/charts/hermes --version 1.0.0
```

### GitHub Pages (Fallback)

```bash
helm repo add hermes https://yourorg.github.io/hermes
helm install hermes hermes/hermes
```

### Versioning

- Chart version and appVersion stay in sync
- All service images share the same tag (built from the same monorepo commit)
- Semver contract: patch = bugfixes, minor = new features/config options, major = breaking `values.yaml` changes

### CI Pipeline

A new GitHub Actions workflow triggered on version tags:

```yaml
on:
  push:
    tags: ["v*"]

jobs:
  release-chart:
    steps:
      - helm package charts/hermes --version $TAG --app-version $TAG
      - helm push hermes-$TAG.tgz oci://ghcr.io/yourorg/charts
      - # update GitHub Pages index
```

Container images are tagged with the semver version (in addition to existing SHA tags for internal use):

```
ghcr.io/yourorg/hermes-admin:1.0.0
ghcr.io/yourorg/hermes-dispatch:1.0.0
...
```

### Internal Deployments

Internal staging/production deployments continue using Kustomize + ArgoCD + Kargo with SHA-based tags. The Helm chart is for external consumers — no migration of the internal workflow.

## Documentation

| Doc | Purpose |
|---|---|
| `charts/hermes/README.md` | Auto-generated values reference via `helm-docs` |
| `docs/self-hosting/quickstart.md` | 5-minute eval deployment |
| `docs/self-hosting/production.md` | Production hardening: external DB, secrets management, TLS, resources, HA |
| `docs/self-hosting/configuration.md` | Full values reference with examples |
| `docs/self-hosting/upgrading.md` | Version-to-version upgrade notes |

## Getting Started Experience

```bash
# 1. Add the repo
helm repo add hermes https://yourorg.github.io/hermes

# 2. Install with defaults (bundles Postgres, NATS, Redis, Centrifugo)
helm install hermes hermes/hermes \
  --set global.domain=hermes.example.com \
  --set hermes.jwt.secret=my-secret \
  --set hermes.apiKey.hmacSecret=my-hmac-secret

# 3. Seed a dev tenant + API key
helm test hermes

# 4. Verify
curl https://hermes.example.com/admin/v1/healthz
```

`helm test` runs a pod that creates a dev tenant, generates an API key, and prints it — so users have a working API key immediately after install.

## Network Policies

Optional (`networkpolicy.enabled: false` by default). When enabled:

- Public-facing services (admin, inbox, user, send) accept ingress traffic
- Internal services (dispatch, workers, event-writer) only communicate via NATS
- Infrastructure pods only accept connections from Hermes services

## What This Design Does NOT Include

- **Kubernetes Operator**: Not needed today. The Helm chart covers the use case. An operator can be added later if lifecycle management requirements grow.
- **Multi-tenancy isolation at the K8s level**: Hermes handles multi-tenancy in-app. The chart deploys a single instance.
- **Backup/restore tooling**: Users manage their own database backups (especially when using external/managed Postgres).
- **Service mesh integration**: Users can layer on Istio/Linkerd themselves via pod annotations.
