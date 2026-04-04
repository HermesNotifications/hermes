# Helm Chart Distribution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Package Hermes as a public Helm chart so users can deploy the full notification stack with `helm install`.

**Architecture:** Single umbrella chart at `charts/hermes/` with sub-chart dependencies for Postgres, NATS, Redis, and Centrifugo. All 9 Hermes services share a common ConfigMap and Secret, wired via `envFrom`. Migrations run as a pre-install/pre-upgrade hook Job.

**Tech Stack:** Helm 3, OCI registry (GHCR), GitHub Actions for chart publishing, `helm-docs` for README generation, JSON Schema for values validation.

**Spec:** `docs/superpowers/specs/2026-04-04-helm-chart-distribution-design.md`

---

## File Map

```
charts/hermes/
  Chart.yaml                          # chart metadata + sub-chart dependencies
  values.yaml                         # all config with sensible defaults
  values.schema.json                  # JSON Schema for values validation
  templates/
    _helpers.tpl                      # shared helpers: fullname, labels, image ref, secret name
    configmap.yaml                    # HERMES_* env vars, sub-chart vs external toggle
    secret.yaml                       # inline secrets OR reference existing
    migration-job.yaml                # pre-install/pre-upgrade hook
    cleanup-cronjob.yaml              # daily event retention cleanup
    ingress.yaml                      # path-based routing to public services
    networkpolicy.yaml                # optional, off by default
    servicemonitor.yaml               # optional Prometheus ServiceMonitor
    tests/
      test-connection.yaml            # helm test: seed tenant + API key
    services/
      admin.yaml                      # Deployment + Service + optional HPA
      dispatch.yaml
      send.yaml
      inbox.yaml
      user.yaml
      worker-email.yaml
      worker-sms.yaml
      worker-inbox.yaml
      worker-events.yaml
    admin-portal/
      deployment.yaml                 # Next.js app, disabled by default
      service.yaml
.github/workflows/
  release-chart.yml                   # new workflow: package + push chart on vN.N.N tags
docs/self-hosting/
  quickstart.md
  production.md
  configuration.md
  upgrading.md
```

---

### Task 1: Chart Scaffolding and Helpers

**Files:**
- Create: `charts/hermes/Chart.yaml`
- Create: `charts/hermes/templates/_helpers.tpl`

- [ ] **Step 1: Create Chart.yaml**

```yaml
# charts/hermes/Chart.yaml
apiVersion: v2
name: hermes
description: An event-driven notification platform for delivering multi-channel notifications at scale
type: application
version: 0.1.0
appVersion: "0.1.0"
home: https://github.com/hermesnotifications/hermes
sources:
  - https://github.com/hermesnotifications/hermes
maintainers:
  - name: Hermes Notifications
    url: https://github.com/hermesnotifications
keywords:
  - notifications
  - messaging
  - email
  - sms
  - inbox
  - push
  - nats
  - event-driven

dependencies:
  - name: postgresql
    version: "~16.0"
    repository: oci://registry-1.docker.io/bitnamicharts
    condition: postgresql.enabled
  - name: redis
    version: "~20.0"
    repository: oci://registry-1.docker.io/bitnamicharts
    condition: redis.enabled
  - name: nats
    version: "~1.2"
    repository: https://nats-io.github.io/k8s/helm/charts/
    condition: nats.enabled
  - name: centrifugo
    version: "~1.0"
    repository: https://centrifugal.github.io/helm-charts
    condition: centrifugo.enabled
```

- [ ] **Step 2: Create _helpers.tpl**

```yaml
# charts/hermes/templates/_helpers.tpl
{{/*
Expand the name of the chart.
*/}}
{{- define "hermes.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "hermes.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "hermes.labels" -}}
helm.sh/chart: {{ include "hermes.chart" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: hermes
{{- end }}

{{/*
Chart label
*/}}
{{- define "hermes.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Service labels — call with (dict "root" . "service" "admin")
*/}}
{{- define "hermes.serviceLabels" -}}
{{ include "hermes.labels" .root }}
app.kubernetes.io/name: {{ .service }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
{{- end }}

{{/*
Service selector labels — call with (dict "root" . "service" "admin")
*/}}
{{- define "hermes.serviceSelectorLabels" -}}
app.kubernetes.io/name: {{ .service }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
{{- end }}

{{/*
Image reference for a Hermes service — call with (dict "root" . "service" .Values.admin)
The service value must have .image.repository and optional .image.tag
*/}}
{{- define "hermes.image" -}}
{{- $registry := .root.Values.global.image.registry -}}
{{- $tag := default .root.Values.global.image.tag .service.image.tag | default .root.Chart.AppVersion -}}
{{- printf "%s/%s:%s" $registry .service.image.repository $tag -}}
{{- end }}

{{/*
Name of the Secret to use for Hermes config.
If existingSecret is set at the top level, use that. Otherwise use the generated one.
*/}}
{{- define "hermes.secretName" -}}
{{- if .Values.hermes.existingSecret }}
{{- .Values.hermes.existingSecret }}
{{- else }}
{{- printf "%s-secrets" (include "hermes.fullname" .) }}
{{- end }}
{{- end }}

{{/*
Name of the ConfigMap for Hermes env vars.
*/}}
{{- define "hermes.configMapName" -}}
{{- printf "%s-config" (include "hermes.fullname" .) }}
{{- end }}

{{/*
Database URL — from sub-chart or external.
*/}}
{{- define "hermes.databaseUrl" -}}
{{- if .Values.postgresql.enabled }}
{{- $host := printf "%s-postgresql" .Release.Name -}}
{{- $user := .Values.postgresql.auth.username -}}
{{- $pass := .Values.postgresql.auth.password -}}
{{- $db := .Values.postgresql.auth.database -}}
{{- printf "postgres://%s:%s@%s:5432/%s?sslmode=disable" $user $pass $host $db -}}
{{- else }}
{{- .Values.externalPostgresql.url }}
{{- end }}
{{- end }}

{{/*
NATS URL — from sub-chart or external.
*/}}
{{- define "hermes.natsUrl" -}}
{{- if .Values.nats.enabled }}
{{- printf "nats://%s-nats:4222" .Release.Name -}}
{{- else }}
{{- .Values.externalNats.url }}
{{- end }}
{{- end }}

{{/*
Redis URL — from sub-chart or external.
*/}}
{{- define "hermes.redisUrl" -}}
{{- if .Values.redis.enabled }}
{{- printf "redis://%s-redis-master:6379/0" .Release.Name -}}
{{- else }}
{{- .Values.externalRedis.url }}
{{- end }}
{{- end }}

{{/*
Centrifugo API URL — from sub-chart or external.
*/}}
{{- define "hermes.centrifugoApiUrl" -}}
{{- if .Values.centrifugo.enabled }}
{{- printf "http://%s-centrifugo:8000" .Release.Name -}}
{{- else }}
{{- .Values.externalCentrifugo.apiUrl }}
{{- end }}
{{- end }}
```

- [ ] **Step 3: Verify chart lints**

Run: `helm lint charts/hermes/ 2>&1 | head -20`
Expected: May warn about missing values.yaml (created next task), but no errors in templates.

- [ ] **Step 4: Commit**

```bash
git add charts/hermes/Chart.yaml charts/hermes/templates/_helpers.tpl
git commit -m "feat(helm): add chart scaffolding and template helpers"
```

---

### Task 2: values.yaml

**Files:**
- Create: `charts/hermes/values.yaml`

- [ ] **Step 1: Create values.yaml**

```yaml
# charts/hermes/values.yaml
# -- Override the chart name
nameOverride: ""
# -- Override the full release name
fullnameOverride: ""

global:
  image:
    # -- Container image registry for all Hermes services
    registry: ghcr.io/hermesnotifications
    # -- Image tag override (defaults to chart appVersion)
    tag: ""

  # -- Domain for ingress routing
  domain: hermes.example.com

hermes:
  # -- Use an existing Secret instead of creating one (must contain all HERMES_* secret keys)
  existingSecret: ""

  jwt:
    # -- JWT signing secret (ignored if existingSecret is set)
    secret: ""
    # -- Use a specific existing Secret for the JWT secret
    existingSecret: ""
    # -- Key within the existing Secret
    existingSecretKey: "jwt-secret"

  apiKey:
    # -- HMAC secret for API key hashing (ignored if existingSecret is set)
    hmacSecret: ""
    # -- Use a specific existing Secret for the HMAC secret
    existingSecret: ""
    # -- Key within the existing Secret
    existingSecretKey: "hmac-secret"

  centrifugo:
    # -- Centrifugo API key (ignored if existingSecret is set)
    apiKey: ""
    # -- Use a specific existing Secret for the Centrifugo API key
    existingSecret: ""
    # -- Key within the existing Secret
    existingSecretKey: "centrifugo-api-key"

  email:
    # -- Email provider: smtp, ses, or sendgrid
    provider: smtp
    # -- Sender email address
    from: noreply@example.com
    smtp:
      # -- SMTP server host
      host: ""
      # -- SMTP server port
      port: 587
      # -- SMTP username
      username: ""
      # -- SMTP password (ignored if existingSecret is set)
      password: ""
    ses:
      # -- AWS SES region
      region: us-east-1
    # -- Use an existing Secret for email credentials (e.g., SMTP password)
    existingSecret: ""
    existingSecretKey: "smtp-password"

  sms:
    # -- Webhook URL for SMS delivery
    webhookUrl: ""

  events:
    # -- Number of days to retain notification events
    retentionDays: 90

  cleanup:
    # -- Enable the daily event cleanup CronJob
    enabled: true
    # -- Cron schedule for cleanup
    schedule: "0 3 * * *"

# -- Per-service configuration. All services share the same structure.

admin:
  replicas: 1
  image:
    repository: hermes-admin
    tag: ""
  port: 8080
  resources:
    requests:
      cpu: 50m
      memory: 64Mi
    limits:
      memory: 256Mi
  autoscaling:
    enabled: false
    minReplicas: 2
    maxReplicas: 10
    targetCPUUtilizationPercentage: 80
    targetMemoryUtilizationPercentage: 80
  podAnnotations: {}
  nodeSelector: {}
  tolerations: []
  affinity: {}
  topologySpreadConstraints: []

send:
  replicas: 1
  image:
    repository: hermes-send
    tag: ""
  port: 8088
  resources:
    requests:
      cpu: 50m
      memory: 64Mi
    limits:
      memory: 256Mi
  autoscaling:
    enabled: false
    minReplicas: 2
    maxReplicas: 10
    targetCPUUtilizationPercentage: 80
    targetMemoryUtilizationPercentage: 80
  podAnnotations: {}
  nodeSelector: {}
  tolerations: []
  affinity: {}
  topologySpreadConstraints: []

dispatch:
  replicas: 1
  image:
    repository: hermes-dispatch
    tag: ""
  port: 8081
  resources:
    requests:
      cpu: 50m
      memory: 64Mi
    limits:
      memory: 256Mi
  autoscaling:
    enabled: false
    minReplicas: 2
    maxReplicas: 10
    targetCPUUtilizationPercentage: 80
    targetMemoryUtilizationPercentage: 80
  podAnnotations: {}
  nodeSelector: {}
  tolerations: []
  affinity: {}
  topologySpreadConstraints: []

inbox:
  replicas: 1
  image:
    repository: hermes-inbox
    tag: ""
  port: 8086
  resources:
    requests:
      cpu: 50m
      memory: 64Mi
    limits:
      memory: 256Mi
  autoscaling:
    enabled: false
    minReplicas: 2
    maxReplicas: 10
    targetCPUUtilizationPercentage: 80
    targetMemoryUtilizationPercentage: 80
  podAnnotations: {}
  nodeSelector: {}
  tolerations: []
  affinity: {}
  topologySpreadConstraints: []

user:
  replicas: 1
  image:
    repository: hermes-user
    tag: ""
  port: 8087
  resources:
    requests:
      cpu: 50m
      memory: 64Mi
    limits:
      memory: 256Mi
  autoscaling:
    enabled: false
    minReplicas: 2
    maxReplicas: 10
    targetCPUUtilizationPercentage: 80
    targetMemoryUtilizationPercentage: 80
  podAnnotations: {}
  nodeSelector: {}
  tolerations: []
  affinity: {}
  topologySpreadConstraints: []

workerEmail:
  replicas: 1
  image:
    repository: hermes-worker-email
    tag: ""
  port: 8083
  resources:
    requests:
      cpu: 50m
      memory: 64Mi
    limits:
      memory: 256Mi
  autoscaling:
    enabled: false
    minReplicas: 2
    maxReplicas: 10
    targetCPUUtilizationPercentage: 80
    targetMemoryUtilizationPercentage: 80
  podAnnotations: {}
  nodeSelector: {}
  tolerations: []
  affinity: {}
  topologySpreadConstraints: []

workerSms:
  replicas: 1
  image:
    repository: hermes-worker-sms
    tag: ""
  port: 8084
  resources:
    requests:
      cpu: 50m
      memory: 64Mi
    limits:
      memory: 256Mi
  autoscaling:
    enabled: false
    minReplicas: 2
    maxReplicas: 10
    targetCPUUtilizationPercentage: 80
    targetMemoryUtilizationPercentage: 80
  podAnnotations: {}
  nodeSelector: {}
  tolerations: []
  affinity: {}
  topologySpreadConstraints: []

workerInbox:
  replicas: 1
  image:
    repository: hermes-worker-inbox
    tag: ""
  port: 8085
  resources:
    requests:
      cpu: 50m
      memory: 64Mi
    limits:
      memory: 256Mi
  autoscaling:
    enabled: false
    minReplicas: 2
    maxReplicas: 10
    targetCPUUtilizationPercentage: 80
    targetMemoryUtilizationPercentage: 80
  podAnnotations: {}
  nodeSelector: {}
  tolerations: []
  affinity: {}
  topologySpreadConstraints: []

workerEvents:
  replicas: 1
  image:
    repository: hermes-worker-events
    tag: ""
  port: 8082
  resources:
    requests:
      cpu: 50m
      memory: 64Mi
    limits:
      memory: 256Mi
  autoscaling:
    enabled: false
    minReplicas: 2
    maxReplicas: 10
    targetCPUUtilizationPercentage: 80
    targetMemoryUtilizationPercentage: 80
  podAnnotations: {}
  nodeSelector: {}
  tolerations: []
  affinity: {}
  topologySpreadConstraints: []

# -- Admin Portal (Next.js web UI)
adminPortal:
  enabled: false
  replicas: 1
  image:
    repository: hermes-admin-portal
    tag: ""
  port: 3000
  resources:
    requests:
      cpu: 50m
      memory: 64Mi
    limits:
      memory: 256Mi
  podAnnotations: {}
  nodeSelector: {}
  tolerations: []
  affinity: {}

# -- Migration job configuration
migration:
  image:
    repository: hermes-migrate
    tag: ""
  backoffLimit: 3
  resources:
    requests:
      cpu: 50m
      memory: 64Mi
    limits:
      memory: 256Mi

# -- Ingress configuration
ingress:
  enabled: true
  # -- Ingress class name (e.g., nginx, traefik, alb)
  className: ""
  annotations: {}
  tls: []

# -- Observability
observability:
  enabled: false
  # -- Provider: otel or datadog
  provider: otel
  otel:
    # -- OTLP collector endpoint
    endpoint: ""
    # -- Trace sampling rate (0.0 to 1.0)
    samplingRate: 0.1
  datadog:
    enabled: false

# -- Network policies (restrict traffic between services)
networkPolicy:
  enabled: false

# -- External services (used when corresponding sub-chart is disabled)

externalPostgresql:
  # -- Full PostgreSQL DSN
  url: ""
  # -- Use an existing Secret for the database URL
  existingSecret: ""
  # -- Key within the existing Secret
  existingSecretKey: "database-url"

externalNats:
  # -- Full NATS URL (e.g., nats://nats.example.com:4222)
  url: ""

externalRedis:
  # -- Full Redis URL (e.g., redis://redis.example.com:6379/0)
  url: ""
  # -- Use an existing Secret for the Redis URL
  existingSecret: ""
  # -- Key within the existing Secret
  existingSecretKey: "redis-url"

externalCentrifugo:
  # -- Centrifugo HTTP API URL
  apiUrl: ""
  # -- Centrifugo API key
  apiKey: ""
  # -- Use an existing Secret for the Centrifugo API key
  existingSecret: ""
  # -- Key within the existing Secret
  existingSecretKey: "centrifugo-api-key"

# -- Sub-chart overrides (see each chart's values for full options)

postgresql:
  enabled: true
  auth:
    username: hermes
    password: hermes
    database: hermes
  primary:
    persistence:
      size: 8Gi

redis:
  enabled: true
  architecture: standalone
  auth:
    enabled: false
  master:
    persistence:
      size: 1Gi

nats:
  enabled: true
  config:
    jetstream:
      enabled: true
      fileStore:
        pvc:
          size: 5Gi
    cluster:
      enabled: true
      replicas: 3

centrifugo:
  enabled: true
  config:
    engine: memory
    token_hmac_secret_key: ""  # must match hermes.jwt.secret
    admin: false
    presence: true
    history_size: 50
    history_ttl: "1h"
    user_subscribe_to_personal: true
    allow_user_limited_channels: true
    health: true
```

- [ ] **Step 2: Verify chart lints**

Run: `helm lint charts/hermes/`
Expected: No errors (warnings about missing deps are OK until `helm dep update` is run).

- [ ] **Step 3: Commit**

```bash
git add charts/hermes/values.yaml
git commit -m "feat(helm): add values.yaml with full configuration surface"
```

---

### Task 3: ConfigMap and Secret Templates

**Files:**
- Create: `charts/hermes/templates/configmap.yaml`
- Create: `charts/hermes/templates/secret.yaml`

- [ ] **Step 1: Create configmap.yaml**

```yaml
# charts/hermes/templates/configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ include "hermes.configMapName" . }}
  labels:
    {{- include "hermes.labels" . | nindent 4 }}
data:
  HERMES_NATS_URL: {{ include "hermes.natsUrl" . | quote }}
  HERMES_CENTRIFUGO_API_URL: {{ include "hermes.centrifugoApiUrl" . | quote }}
  HERMES_REDIS_URL: {{ include "hermes.redisUrl" . | quote }}
  HERMES_EMAIL_PROVIDER: {{ .Values.hermes.email.provider | quote }}
  HERMES_EMAIL_FROM: {{ .Values.hermes.email.from | quote }}
  {{- if eq .Values.hermes.email.provider "smtp" }}
  HERMES_EMAIL_SMTP_HOST: {{ .Values.hermes.email.smtp.host | quote }}
  HERMES_EMAIL_SMTP_PORT: {{ .Values.hermes.email.smtp.port | quote }}
  HERMES_EMAIL_SMTP_USERNAME: {{ .Values.hermes.email.smtp.username | quote }}
  {{- end }}
  {{- if eq .Values.hermes.email.provider "ses" }}
  HERMES_EMAIL_SES_REGION: {{ .Values.hermes.email.ses.region | quote }}
  {{- end }}
  {{- if .Values.hermes.sms.webhookUrl }}
  HERMES_SMS_WEBHOOK_URL: {{ .Values.hermes.sms.webhookUrl | quote }}
  {{- end }}
  HERMES_EVENT_RETENTION_DAYS: {{ .Values.hermes.events.retentionDays | quote }}
```

- [ ] **Step 2: Create secret.yaml**

```yaml
# charts/hermes/templates/secret.yaml
{{- if not .Values.hermes.existingSecret }}
apiVersion: v1
kind: Secret
metadata:
  name: {{ include "hermes.fullname" . }}-secrets
  labels:
    {{- include "hermes.labels" . | nindent 4 }}
type: Opaque
stringData:
  {{- if not .Values.hermes.jwt.existingSecret }}
  HERMES_JWT_SECRET: {{ required "hermes.jwt.secret is required (or set hermes.jwt.existingSecret)" .Values.hermes.jwt.secret | quote }}
  {{- end }}
  {{- if not .Values.hermes.apiKey.existingSecret }}
  HERMES_API_KEY_HMAC_SECRET: {{ required "hermes.apiKey.hmacSecret is required (or set hermes.apiKey.existingSecret)" .Values.hermes.apiKey.hmacSecret | quote }}
  {{- end }}
  {{- if not .Values.hermes.centrifugo.existingSecret }}
  {{- if .Values.hermes.centrifugo.apiKey }}
  HERMES_CENTRIFUGO_API_KEY: {{ .Values.hermes.centrifugo.apiKey | quote }}
  {{- end }}
  {{- end }}
  {{- if and (not .Values.postgresql.enabled) (not .Values.externalPostgresql.existingSecret) }}
  HERMES_DATABASE_URL: {{ required "externalPostgresql.url is required when postgresql.enabled is false" .Values.externalPostgresql.url | quote }}
  {{- end }}
  {{- if .Values.postgresql.enabled }}
  HERMES_DATABASE_URL: {{ include "hermes.databaseUrl" . | quote }}
  {{- end }}
  {{- if and (eq .Values.hermes.email.provider "smtp") .Values.hermes.email.smtp.password (not .Values.hermes.email.existingSecret) }}
  HERMES_EMAIL_SMTP_PASSWORD: {{ .Values.hermes.email.smtp.password | quote }}
  {{- end }}
{{- end }}
```

- [ ] **Step 3: Verify templates render**

Run: `helm template test charts/hermes/ --set hermes.jwt.secret=test --set hermes.apiKey.hmacSecret=test 2>&1 | head -60`
Expected: ConfigMap and Secret render with correct values. May fail on missing deps — that's OK for now.

- [ ] **Step 4: Commit**

```bash
git add charts/hermes/templates/configmap.yaml charts/hermes/templates/secret.yaml
git commit -m "feat(helm): add configmap and secret templates"
```

---

### Task 4: Service Template (One Service as Pattern)

Build the admin service template first. This becomes the pattern for all other services.

**Files:**
- Create: `charts/hermes/templates/services/admin.yaml`

- [ ] **Step 1: Create admin.yaml**

```yaml
# charts/hermes/templates/services/admin.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "hermes.fullname" . }}-admin
  labels:
    {{- include "hermes.serviceLabels" (dict "root" . "service" "admin") | nindent 4 }}
spec:
  {{- if not .Values.admin.autoscaling.enabled }}
  replicas: {{ .Values.admin.replicas }}
  {{- end }}
  selector:
    matchLabels:
      {{- include "hermes.serviceSelectorLabels" (dict "root" . "service" "admin") | nindent 6 }}
  template:
    metadata:
      annotations:
        checksum/config: {{ include (print $.Template.BasePath "/configmap.yaml") . | sha256sum }}
        checksum/secret: {{ include (print $.Template.BasePath "/secret.yaml") . | sha256sum }}
        {{- with .Values.admin.podAnnotations }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
      labels:
        {{- include "hermes.serviceSelectorLabels" (dict "root" . "service" "admin") | nindent 8 }}
    spec:
      serviceAccountName: default
      securityContext:
        runAsNonRoot: true
        runAsUser: 65534
      containers:
        - name: admin
          image: {{ include "hermes.image" (dict "root" . "service" .Values.admin) }}
          imagePullPolicy: IfNotPresent
          ports:
            - name: http
              containerPort: {{ .Values.admin.port }}
              protocol: TCP
          env:
            - name: HERMES_HTTP_PORT
              value: {{ .Values.admin.port | quote }}
            {{- if .Values.hermes.jwt.existingSecret }}
            - name: HERMES_JWT_SECRET
              valueFrom:
                secretKeyRef:
                  name: {{ .Values.hermes.jwt.existingSecret }}
                  key: {{ .Values.hermes.jwt.existingSecretKey }}
            {{- end }}
            {{- if .Values.hermes.apiKey.existingSecret }}
            - name: HERMES_API_KEY_HMAC_SECRET
              valueFrom:
                secretKeyRef:
                  name: {{ .Values.hermes.apiKey.existingSecret }}
                  key: {{ .Values.hermes.apiKey.existingSecretKey }}
            {{- end }}
            {{- if .Values.externalPostgresql.existingSecret }}
            - name: HERMES_DATABASE_URL
              valueFrom:
                secretKeyRef:
                  name: {{ .Values.externalPostgresql.existingSecret }}
                  key: {{ .Values.externalPostgresql.existingSecretKey }}
            {{- end }}
            {{- if .Values.externalRedis.existingSecret }}
            - name: HERMES_REDIS_URL
              valueFrom:
                secretKeyRef:
                  name: {{ .Values.externalRedis.existingSecret }}
                  key: {{ .Values.externalRedis.existingSecretKey }}
            {{- end }}
          envFrom:
            - configMapRef:
                name: {{ include "hermes.configMapName" . }}
            {{- if not .Values.hermes.existingSecret }}
            - secretRef:
                name: {{ include "hermes.fullname" . }}-secrets
                optional: true
            {{- else }}
            - secretRef:
                name: {{ .Values.hermes.existingSecret }}
            {{- end }}
          livenessProbe:
            httpGet:
              path: /healthz
              port: http
            initialDelaySeconds: 5
            periodSeconds: 10
          readinessProbe:
            httpGet:
              path: /readyz
              port: http
            initialDelaySeconds: 3
            periodSeconds: 5
          resources:
            {{- toYaml .Values.admin.resources | nindent 12 }}
          securityContext:
            readOnlyRootFilesystem: true
            allowPrivilegeEscalation: false
            capabilities:
              drop:
                - ALL
          volumeMounts:
            - name: tmp
              mountPath: /tmp
      volumes:
        - name: tmp
          emptyDir:
            sizeLimit: 10Mi
      {{- with .Values.admin.nodeSelector }}
      nodeSelector:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.admin.affinity }}
      affinity:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.admin.tolerations }}
      tolerations:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.admin.topologySpreadConstraints }}
      topologySpreadConstraints:
        {{- toYaml . | nindent 8 }}
      {{- end }}
---
apiVersion: v1
kind: Service
metadata:
  name: {{ include "hermes.fullname" . }}-admin
  labels:
    {{- include "hermes.serviceLabels" (dict "root" . "service" "admin") | nindent 4 }}
spec:
  type: ClusterIP
  ports:
    - port: {{ .Values.admin.port }}
      targetPort: http
      protocol: TCP
      name: http
  selector:
    {{- include "hermes.serviceSelectorLabels" (dict "root" . "service" "admin") | nindent 4 }}
---
{{- if .Values.admin.autoscaling.enabled }}
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: {{ include "hermes.fullname" . }}-admin
  labels:
    {{- include "hermes.serviceLabels" (dict "root" . "service" "admin") | nindent 4 }}
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: {{ include "hermes.fullname" . }}-admin
  minReplicas: {{ .Values.admin.autoscaling.minReplicas }}
  maxReplicas: {{ .Values.admin.autoscaling.maxReplicas }}
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: {{ .Values.admin.autoscaling.targetCPUUtilizationPercentage }}
    - type: Resource
      resource:
        name: memory
        target:
          type: Utilization
          averageUtilization: {{ .Values.admin.autoscaling.targetMemoryUtilizationPercentage }}
{{- end }}
```

- [ ] **Step 2: Verify template renders**

Run: `helm template test charts/hermes/ --set hermes.jwt.secret=test --set hermes.apiKey.hmacSecret=test -s templates/services/admin.yaml 2>&1`
Expected: Valid Deployment, Service, and (when autoscaling enabled) HPA YAML.

- [ ] **Step 3: Commit**

```bash
git add charts/hermes/templates/services/admin.yaml
git commit -m "feat(helm): add admin service template as reference pattern"
```

---

### Task 5: Remaining Service Templates

Create the remaining 8 service templates following the exact same pattern as admin. Each file differs only in the service name, values key, and port.

**Files:**
- Create: `charts/hermes/templates/services/send.yaml`
- Create: `charts/hermes/templates/services/dispatch.yaml`
- Create: `charts/hermes/templates/services/inbox.yaml`
- Create: `charts/hermes/templates/services/user.yaml`
- Create: `charts/hermes/templates/services/worker-email.yaml`
- Create: `charts/hermes/templates/services/worker-sms.yaml`
- Create: `charts/hermes/templates/services/worker-inbox.yaml`
- Create: `charts/hermes/templates/services/worker-events.yaml`

- [ ] **Step 1: Create all 8 service templates**

Each template is identical to `admin.yaml` with these substitutions:

| File | `service` label | Values key | Container name |
|------|----------------|------------|----------------|
| `send.yaml` | `send` | `.Values.send` | `send` |
| `dispatch.yaml` | `dispatch` | `.Values.dispatch` | `dispatch` |
| `inbox.yaml` | `inbox` | `.Values.inbox` | `inbox` |
| `user.yaml` | `user` | `.Values.user` | `user` |
| `worker-email.yaml` | `worker-email` | `.Values.workerEmail` | `worker-email` |
| `worker-sms.yaml` | `worker-sms` | `.Values.workerSms` | `worker-sms` |
| `worker-inbox.yaml` | `worker-inbox` | `.Values.workerInbox` | `worker-inbox` |
| `worker-events.yaml` | `worker-events` | `.Values.workerEvents` | `worker-events` |

For each file, copy the admin.yaml template and replace:
- All occurrences of `"admin"` (the label/name string) with the service label name
- All occurrences of `.Values.admin` with the correct Values key from the table above
- The container `name:` field with the container name from the table above

- [ ] **Step 2: Verify all templates render**

Run: `helm template test charts/hermes/ --set hermes.jwt.secret=test --set hermes.apiKey.hmacSecret=test --show-only 'templates/services/*' 2>&1 | grep "kind:" | sort | uniq -c`
Expected: 9 Deployments, 9 Services (no HPAs since autoscaling is off by default).

- [ ] **Step 3: Commit**

```bash
git add charts/hermes/templates/services/
git commit -m "feat(helm): add remaining 8 service templates"
```

---

### Task 6: Migration Job and Cleanup CronJob

**Files:**
- Create: `charts/hermes/templates/migration-job.yaml`
- Create: `charts/hermes/templates/cleanup-cronjob.yaml`

- [ ] **Step 1: Create migration-job.yaml**

```yaml
# charts/hermes/templates/migration-job.yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: {{ include "hermes.fullname" . }}-migrate-{{ .Release.Revision }}
  labels:
    {{- include "hermes.labels" . | nindent 4 }}
    app.kubernetes.io/component: migration
  annotations:
    "helm.sh/hook": pre-install,pre-upgrade
    "helm.sh/hook-weight": "0"
    "helm.sh/hook-delete-policy": hook-succeeded
spec:
  backoffLimit: {{ .Values.migration.backoffLimit }}
  template:
    metadata:
      labels:
        {{- include "hermes.labels" . | nindent 8 }}
        app.kubernetes.io/component: migration
    spec:
      restartPolicy: Never
      securityContext:
        runAsNonRoot: true
        runAsUser: 65534
      containers:
        - name: migrate
          image: {{ include "hermes.image" (dict "root" . "service" .Values.migration) }}
          args: ["-migrations-path", "/migrations"]
          env:
            {{- if .Values.externalPostgresql.existingSecret }}
            - name: HERMES_DATABASE_URL
              valueFrom:
                secretKeyRef:
                  name: {{ .Values.externalPostgresql.existingSecret }}
                  key: {{ .Values.externalPostgresql.existingSecretKey }}
            {{- end }}
          envFrom:
            {{- if not .Values.hermes.existingSecret }}
            - secretRef:
                name: {{ include "hermes.fullname" . }}-secrets
                optional: true
            {{- else }}
            - secretRef:
                name: {{ .Values.hermes.existingSecret }}
            {{- end }}
          resources:
            {{- toYaml .Values.migration.resources | nindent 12 }}
          securityContext:
            readOnlyRootFilesystem: true
            allowPrivilegeEscalation: false
            capabilities:
              drop:
                - ALL
```

- [ ] **Step 2: Create cleanup-cronjob.yaml**

```yaml
# charts/hermes/templates/cleanup-cronjob.yaml
{{- if .Values.hermes.cleanup.enabled }}
apiVersion: batch/v1
kind: CronJob
metadata:
  name: {{ include "hermes.fullname" . }}-cleanup
  labels:
    {{- include "hermes.labels" . | nindent 4 }}
    app.kubernetes.io/component: cleanup
spec:
  schedule: {{ .Values.hermes.cleanup.schedule | quote }}
  concurrencyPolicy: Forbid
  successfulJobsHistoryLimit: 3
  failedJobsHistoryLimit: 3
  jobTemplate:
    spec:
      backoffLimit: 3
      template:
        metadata:
          labels:
            {{- include "hermes.labels" . | nindent 12 }}
            app.kubernetes.io/component: cleanup
        spec:
          restartPolicy: Never
          securityContext:
            runAsNonRoot: true
            runAsUser: 65534
          containers:
            - name: cleanup
              image: {{ include "hermes.image" (dict "root" . "service" (dict "image" (dict "repository" "hermes-cleanup" "tag" ""))) }}
              env:
                {{- if .Values.externalPostgresql.existingSecret }}
                - name: HERMES_DATABASE_URL
                  valueFrom:
                    secretKeyRef:
                      name: {{ .Values.externalPostgresql.existingSecret }}
                      key: {{ .Values.externalPostgresql.existingSecretKey }}
                {{- end }}
              envFrom:
                {{- if not .Values.hermes.existingSecret }}
                - secretRef:
                    name: {{ include "hermes.fullname" . }}-secrets
                    optional: true
                {{- else }}
                - secretRef:
                    name: {{ .Values.hermes.existingSecret }}
                {{- end }}
                - configMapRef:
                    name: {{ include "hermes.configMapName" . }}
              resources:
                requests:
                  cpu: 50m
                  memory: 64Mi
                limits:
                  memory: 256Mi
              securityContext:
                readOnlyRootFilesystem: true
                allowPrivilegeEscalation: false
                capabilities:
                  drop:
                    - ALL
{{- end }}
```

- [ ] **Step 3: Verify both render**

Run: `helm template test charts/hermes/ --set hermes.jwt.secret=test --set hermes.apiKey.hmacSecret=test -s templates/migration-job.yaml -s templates/cleanup-cronjob.yaml 2>&1`
Expected: A Job with hook annotations and a CronJob.

- [ ] **Step 4: Commit**

```bash
git add charts/hermes/templates/migration-job.yaml charts/hermes/templates/cleanup-cronjob.yaml
git commit -m "feat(helm): add migration hook job and cleanup cronjob"
```

---

### Task 7: Ingress Template

**Files:**
- Create: `charts/hermes/templates/ingress.yaml`

- [ ] **Step 1: Create ingress.yaml**

```yaml
# charts/hermes/templates/ingress.yaml
{{- if .Values.ingress.enabled }}
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: {{ include "hermes.fullname" . }}
  labels:
    {{- include "hermes.labels" . | nindent 4 }}
  {{- with .Values.ingress.annotations }}
  annotations:
    {{- toYaml . | nindent 4 }}
  {{- end }}
spec:
  {{- if .Values.ingress.className }}
  ingressClassName: {{ .Values.ingress.className }}
  {{- end }}
  {{- if .Values.ingress.tls }}
  tls:
    {{- range .Values.ingress.tls }}
    - hosts:
        {{- range .hosts }}
        - {{ . | quote }}
        {{- end }}
      secretName: {{ .secretName }}
    {{- end }}
  {{- end }}
  rules:
    - host: {{ .Values.global.domain | quote }}
      http:
        paths:
          - path: /v1/send
            pathType: Prefix
            backend:
              service:
                name: {{ include "hermes.fullname" . }}-send
                port:
                  number: {{ .Values.send.port }}
          - path: /v1/types
            pathType: Prefix
            backend:
              service:
                name: {{ include "hermes.fullname" . }}-admin
                port:
                  number: {{ .Values.admin.port }}
          - path: /v1/groups
            pathType: Prefix
            backend:
              service:
                name: {{ include "hermes.fullname" . }}-admin
                port:
                  number: {{ .Values.admin.port }}
          - path: /v1/notifications
            pathType: Prefix
            backend:
              service:
                name: {{ include "hermes.fullname" . }}-admin
                port:
                  number: {{ .Values.admin.port }}
          - path: /v1/auth
            pathType: Prefix
            backend:
              service:
                name: {{ include "hermes.fullname" . }}-admin
                port:
                  number: {{ .Values.admin.port }}
          - path: /v1/inbox
            pathType: Prefix
            backend:
              service:
                name: {{ include "hermes.fullname" . }}-inbox
                port:
                  number: {{ .Values.inbox.port }}
          - path: /v1/users
            pathType: Prefix
            backend:
              service:
                name: {{ include "hermes.fullname" . }}-user
                port:
                  number: {{ .Values.user.port }}
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: {{ include "hermes.fullname" . }}-realtime
  labels:
    {{- include "hermes.labels" . | nindent 4 }}
  annotations:
    {{- with .Values.ingress.annotations }}
    {{- toYaml . | nindent 4 }}
    {{- end }}
    nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "3600"
    nginx.ingress.kubernetes.io/rewrite-target: /$2
spec:
  {{- if .Values.ingress.className }}
  ingressClassName: {{ .Values.ingress.className }}
  {{- end }}
  {{- if .Values.ingress.tls }}
  tls:
    {{- range .Values.ingress.tls }}
    - hosts:
        {{- range .hosts }}
        - {{ . | quote }}
        {{- end }}
      secretName: {{ .secretName }}
    {{- end }}
  {{- end }}
  rules:
    - host: {{ .Values.global.domain | quote }}
      http:
        paths:
          - path: /realtime(/|$)(.*)
            pathType: ImplementationSpecific
            backend:
              service:
                {{- if .Values.centrifugo.enabled }}
                name: {{ .Release.Name }}-centrifugo
                {{- else }}
                name: {{ include "hermes.fullname" . }}-centrifugo
                {{- end }}
                port:
                  number: 8000
{{- end }}
```

- [ ] **Step 2: Verify ingress renders**

Run: `helm template test charts/hermes/ --set hermes.jwt.secret=test --set hermes.apiKey.hmacSecret=test -s templates/ingress.yaml 2>&1`
Expected: Two Ingress resources — one for API routes, one for realtime with rewrite annotations.

- [ ] **Step 3: Commit**

```bash
git add charts/hermes/templates/ingress.yaml
git commit -m "feat(helm): add ingress templates for API and realtime routes"
```

---

### Task 8: Admin Portal Template

**Files:**
- Create: `charts/hermes/templates/admin-portal/deployment.yaml`
- Create: `charts/hermes/templates/admin-portal/service.yaml`

- [ ] **Step 1: Create deployment.yaml**

```yaml
# charts/hermes/templates/admin-portal/deployment.yaml
{{- if .Values.adminPortal.enabled }}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "hermes.fullname" . }}-admin-portal
  labels:
    {{- include "hermes.serviceLabels" (dict "root" . "service" "admin-portal") | nindent 4 }}
spec:
  replicas: {{ .Values.adminPortal.replicas }}
  selector:
    matchLabels:
      {{- include "hermes.serviceSelectorLabels" (dict "root" . "service" "admin-portal") | nindent 6 }}
  template:
    metadata:
      {{- with .Values.adminPortal.podAnnotations }}
      annotations:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      labels:
        {{- include "hermes.serviceSelectorLabels" (dict "root" . "service" "admin-portal") | nindent 8 }}
    spec:
      containers:
        - name: admin-portal
          image: {{ include "hermes.image" (dict "root" . "service" .Values.adminPortal) }}
          imagePullPolicy: IfNotPresent
          ports:
            - name: http
              containerPort: {{ .Values.adminPortal.port }}
              protocol: TCP
          livenessProbe:
            httpGet:
              path: /
              port: http
            initialDelaySeconds: 5
            periodSeconds: 10
          readinessProbe:
            httpGet:
              path: /
              port: http
            initialDelaySeconds: 3
            periodSeconds: 5
          resources:
            {{- toYaml .Values.adminPortal.resources | nindent 12 }}
      {{- with .Values.adminPortal.nodeSelector }}
      nodeSelector:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.adminPortal.affinity }}
      affinity:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.adminPortal.tolerations }}
      tolerations:
        {{- toYaml . | nindent 8 }}
      {{- end }}
{{- end }}
```

- [ ] **Step 2: Create service.yaml**

```yaml
# charts/hermes/templates/admin-portal/service.yaml
{{- if .Values.adminPortal.enabled }}
apiVersion: v1
kind: Service
metadata:
  name: {{ include "hermes.fullname" . }}-admin-portal
  labels:
    {{- include "hermes.serviceLabels" (dict "root" . "service" "admin-portal") | nindent 4 }}
spec:
  type: ClusterIP
  ports:
    - port: {{ .Values.adminPortal.port }}
      targetPort: http
      protocol: TCP
      name: http
  selector:
    {{- include "hermes.serviceSelectorLabels" (dict "root" . "service" "admin-portal") | nindent 4 }}
{{- end }}
```

- [ ] **Step 3: Verify templates are skipped by default**

Run: `helm template test charts/hermes/ --set hermes.jwt.secret=test --set hermes.apiKey.hmacSecret=test -s templates/admin-portal/deployment.yaml 2>&1`
Expected: Empty output (adminPortal.enabled is false by default).

- [ ] **Step 4: Verify templates render when enabled**

Run: `helm template test charts/hermes/ --set hermes.jwt.secret=test --set hermes.apiKey.hmacSecret=test --set adminPortal.enabled=true -s templates/admin-portal/deployment.yaml -s templates/admin-portal/service.yaml 2>&1`
Expected: Deployment and Service for admin-portal.

- [ ] **Step 5: Commit**

```bash
git add charts/hermes/templates/admin-portal/
git commit -m "feat(helm): add optional admin portal templates"
```

---

### Task 9: Network Policy and ServiceMonitor Templates

**Files:**
- Create: `charts/hermes/templates/networkpolicy.yaml`
- Create: `charts/hermes/templates/servicemonitor.yaml`

- [ ] **Step 1: Create networkpolicy.yaml**

```yaml
# charts/hermes/templates/networkpolicy.yaml
{{- if .Values.networkPolicy.enabled }}
# Default deny all
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: {{ include "hermes.fullname" . }}-default-deny
  labels:
    {{- include "hermes.labels" . | nindent 4 }}
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/instance: {{ .Release.Name }}
  policyTypes:
    - Ingress
    - Egress
  egress:
    # Allow DNS
    - ports:
        - port: 53
          protocol: UDP
        - port: 53
          protocol: TCP
---
# Allow ingress to public-facing services
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: {{ include "hermes.fullname" . }}-allow-ingress
  labels:
    {{- include "hermes.labels" . | nindent 4 }}
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/instance: {{ .Release.Name }}
  policyTypes:
    - Ingress
  ingress:
    - ports:
        - port: {{ .Values.admin.port }}
        - port: {{ .Values.send.port }}
        - port: {{ .Values.inbox.port }}
        - port: {{ .Values.user.port }}
---
# Allow all Hermes pods to reach NATS and Centrifugo
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: {{ include "hermes.fullname" . }}-allow-internal
  labels:
    {{- include "hermes.labels" . | nindent 4 }}
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/instance: {{ .Release.Name }}
  policyTypes:
    - Egress
  egress:
    # NATS
    - ports:
        - port: 4222
          protocol: TCP
    # Centrifugo
    - ports:
        - port: 8000
          protocol: TCP
    # PostgreSQL
    - ports:
        - port: 5432
          protocol: TCP
    # Redis
    - ports:
        - port: 6379
          protocol: TCP
    # External HTTPS (webhooks)
    - ports:
        - port: 443
          protocol: TCP
    # SMTP
    - ports:
        - port: 587
          protocol: TCP
        - port: 465
          protocol: TCP
        - port: 25
          protocol: TCP
{{- end }}
```

- [ ] **Step 2: Create servicemonitor.yaml**

```yaml
# charts/hermes/templates/servicemonitor.yaml
{{- if and .Values.observability.enabled (eq .Values.observability.provider "otel") }}
{{- $services := list "admin" "send" "dispatch" "inbox" "user" "worker-email" "worker-sms" "worker-inbox" "worker-events" }}
{{- $root := . }}
{{- range $svc := $services }}
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: {{ include "hermes.fullname" $root }}-{{ $svc }}
  labels:
    {{- include "hermes.serviceLabels" (dict "root" $root "service" $svc) | nindent 4 }}
spec:
  selector:
    matchLabels:
      {{- include "hermes.serviceSelectorLabels" (dict "root" $root "service" $svc) | nindent 6 }}
  endpoints:
    - port: http
      path: /metrics
      interval: 30s
---
{{- end }}
{{- end }}
```

- [ ] **Step 3: Verify network policies are skipped by default**

Run: `helm template test charts/hermes/ --set hermes.jwt.secret=test --set hermes.apiKey.hmacSecret=test -s templates/networkpolicy.yaml 2>&1`
Expected: Empty output.

- [ ] **Step 4: Commit**

```bash
git add charts/hermes/templates/networkpolicy.yaml charts/hermes/templates/servicemonitor.yaml
git commit -m "feat(helm): add optional network policy and service monitor templates"
```

---

### Task 10: Helm Test Pod

**Files:**
- Create: `charts/hermes/templates/tests/test-connection.yaml`

- [ ] **Step 1: Create test-connection.yaml**

```yaml
# charts/hermes/templates/tests/test-connection.yaml
apiVersion: v1
kind: Pod
metadata:
  name: {{ include "hermes.fullname" . }}-test
  labels:
    {{- include "hermes.labels" . | nindent 4 }}
  annotations:
    "helm.sh/hook": test
    "helm.sh/hook-delete-policy": before-hook-creation
spec:
  restartPolicy: Never
  containers:
    - name: test
      image: curlimages/curl:latest
      command: ['sh', '-c']
      args:
        - |
          echo "Testing Hermes services..."
          echo ""

          echo "--- Admin Service ---"
          curl -sf http://{{ include "hermes.fullname" . }}-admin:{{ .Values.admin.port }}/healthz && echo " OK" || exit 1

          echo "--- Send Service ---"
          curl -sf http://{{ include "hermes.fullname" . }}-send:{{ .Values.send.port }}/healthz && echo " OK" || exit 1

          echo "--- Dispatch Service ---"
          curl -sf http://{{ include "hermes.fullname" . }}-dispatch:{{ .Values.dispatch.port }}/healthz && echo " OK" || exit 1

          echo "--- Inbox Service ---"
          curl -sf http://{{ include "hermes.fullname" . }}-inbox:{{ .Values.inbox.port }}/healthz && echo " OK" || exit 1

          echo "--- User Service ---"
          curl -sf http://{{ include "hermes.fullname" . }}-user:{{ .Values.user.port }}/healthz && echo " OK" || exit 1

          echo ""
          echo "All services healthy!"
```

- [ ] **Step 2: Verify test pod renders**

Run: `helm template test charts/hermes/ --set hermes.jwt.secret=test --set hermes.apiKey.hmacSecret=test -s templates/tests/test-connection.yaml 2>&1`
Expected: Pod spec with curl commands testing each service.

- [ ] **Step 3: Commit**

```bash
git add charts/hermes/templates/tests/
git commit -m "feat(helm): add helm test pod for health checks"
```

---

### Task 11: values.schema.json

**Files:**
- Create: `charts/hermes/values.schema.json`

- [ ] **Step 1: Create values.schema.json**

Create a JSON Schema that validates the critical fields. Focus on the most common misconfiguration errors — not exhaustive validation of every field.

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "properties": {
    "global": {
      "type": "object",
      "properties": {
        "image": {
          "type": "object",
          "properties": {
            "registry": { "type": "string" },
            "tag": { "type": "string" }
          }
        },
        "domain": { "type": "string" }
      },
      "required": ["domain"]
    },
    "hermes": {
      "type": "object",
      "properties": {
        "jwt": {
          "type": "object",
          "properties": {
            "secret": { "type": "string" },
            "existingSecret": { "type": "string" },
            "existingSecretKey": { "type": "string" }
          }
        },
        "apiKey": {
          "type": "object",
          "properties": {
            "hmacSecret": { "type": "string" },
            "existingSecret": { "type": "string" },
            "existingSecretKey": { "type": "string" }
          }
        },
        "email": {
          "type": "object",
          "properties": {
            "provider": {
              "type": "string",
              "enum": ["smtp", "ses", "sendgrid"]
            },
            "from": { "type": "string" }
          }
        },
        "events": {
          "type": "object",
          "properties": {
            "retentionDays": { "type": "integer", "minimum": 1 }
          }
        }
      }
    },
    "ingress": {
      "type": "object",
      "properties": {
        "enabled": { "type": "boolean" },
        "className": { "type": "string" }
      }
    },
    "observability": {
      "type": "object",
      "properties": {
        "enabled": { "type": "boolean" },
        "provider": {
          "type": "string",
          "enum": ["otel", "datadog"]
        }
      }
    },
    "postgresql": {
      "type": "object",
      "properties": {
        "enabled": { "type": "boolean" }
      }
    },
    "nats": {
      "type": "object",
      "properties": {
        "enabled": { "type": "boolean" }
      }
    },
    "redis": {
      "type": "object",
      "properties": {
        "enabled": { "type": "boolean" }
      }
    },
    "centrifugo": {
      "type": "object",
      "properties": {
        "enabled": { "type": "boolean" }
      }
    }
  },
  "required": ["global", "hermes"]
}
```

- [ ] **Step 2: Validate the schema file**

Run: `jq . charts/hermes/values.schema.json > /dev/null`
Expected: No errors (valid JSON).

- [ ] **Step 3: Commit**

```bash
git add charts/hermes/values.schema.json
git commit -m "feat(helm): add values.schema.json for install-time validation"
```

---

### Task 12: Build Dependencies and Full Lint

**Files:**
- Modify: `charts/hermes/Chart.yaml` (version pinning adjustments if needed)

- [ ] **Step 1: Build chart dependencies**

Run: `helm dependency build charts/hermes/`
Expected: All 4 sub-charts downloaded to `charts/hermes/charts/`.

- [ ] **Step 2: Full lint with dependencies**

Run: `helm lint charts/hermes/ --set hermes.jwt.secret=test --set hermes.apiKey.hmacSecret=test`
Expected: No errors. Warnings about default values are acceptable.

- [ ] **Step 3: Full template render test**

Run: `helm template test charts/hermes/ --set hermes.jwt.secret=test --set hermes.apiKey.hmacSecret=test 2>&1 | grep "^kind:" | sort | uniq -c`
Expected: ConfigMap, CronJob, Deployment (x9+), Ingress (x2), Job, Secret, Service (x9+).

- [ ] **Step 4: Template render with external services**

Run: `helm template test charts/hermes/ --set hermes.jwt.secret=test --set hermes.apiKey.hmacSecret=test --set postgresql.enabled=false --set externalPostgresql.url=postgres://ext:5432/db --set nats.enabled=false --set externalNats.url=nats://ext:4222 --set redis.enabled=false --set externalRedis.url=redis://ext:6379 --set centrifugo.enabled=false --set externalCentrifugo.apiUrl=http://ext:8000 2>&1 | grep "^kind:" | sort | uniq -c`
Expected: Same Hermes resources but no sub-chart resources (no PostgreSQL StatefulSet, no NATS, etc.).

- [ ] **Step 5: Add charts/ lock file, commit**

```bash
echo "charts/hermes/charts/" >> .gitignore
git add charts/hermes/Chart.lock .gitignore
git commit -m "feat(helm): add chart dependency lock file"
```

---

### Task 13: Chart Release GitHub Actions Workflow

**Files:**
- Create: `.github/workflows/release-chart.yml`

- [ ] **Step 1: Create release-chart.yml**

```yaml
# .github/workflows/release-chart.yml
name: Release Helm Chart

on:
  push:
    tags:
      - "v*"

permissions:
  contents: read
  packages: write

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Set up Helm
        uses: azure/setup-helm@v4

      - name: Extract version from tag
        id: version
        run: echo "version=${GITHUB_REF_NAME#v}" >> "$GITHUB_OUTPUT"

      - name: Login to GHCR
        run: echo "${{ secrets.GITHUB_TOKEN }}" | helm registry login ghcr.io -u "${{ github.actor }}" --password-stdin

      - name: Build dependencies
        run: helm dependency build charts/hermes/

      - name: Package chart
        run: |
          helm package charts/hermes/ \
            --version "${{ steps.version.outputs.version }}" \
            --app-version "${{ steps.version.outputs.version }}"

      - name: Push to GHCR
        run: helm push hermes-${{ steps.version.outputs.version }}.tgz oci://ghcr.io/hermesnotifications/charts

      - name: Lint packaged chart
        run: |
          helm lint charts/hermes/ \
            --set hermes.jwt.secret=test \
            --set hermes.apiKey.hmacSecret=test
```

- [ ] **Step 2: Commit**

```bash
git add .github/workflows/release-chart.yml
git commit -m "ci: add Helm chart release workflow for GHCR"
```

---

### Task 14: Update CD Workflow for Semver Image Tags

**Files:**
- Modify: `.github/workflows/cd.yml`

- [ ] **Step 1: Read current cd.yml**

Read `.github/workflows/cd.yml` to understand the current image tagging logic.

- [ ] **Step 2: Add semver tag on version tag pushes**

Add a conditional step: when the trigger is a version tag (`v*`), also tag images with the semver version (in addition to the existing SHA tag). This ensures `ghcr.io/hermesnotifications/hermes-admin:1.0.0` exists when a chart release is cut.

The exact change depends on the current workflow structure — the key addition is:

```yaml
- name: Tag with version
  if: startsWith(github.ref, 'refs/tags/v')
  run: |
    VERSION=${GITHUB_REF_NAME#v}
    # Re-tag and push with semver version
```

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/cd.yml
git commit -m "ci: add semver image tags on version tag pushes"
```

---

### Task 15: Self-Hosting Documentation

**Files:**
- Create: `docs/self-hosting/quickstart.md`
- Create: `docs/self-hosting/production.md`
- Create: `docs/self-hosting/configuration.md`
- Create: `docs/self-hosting/upgrading.md`

- [ ] **Step 1: Create quickstart.md**

```markdown
# Quick Start

Deploy Hermes with all dependencies bundled (PostgreSQL, NATS, Redis, Centrifugo).

## Prerequisites

- Kubernetes cluster (1.26+)
- Helm 3.12+
- An ingress controller installed (e.g., ingress-nginx)

## Install

helm install hermes oci://ghcr.io/hermesnotifications/charts/hermes \
  --set global.domain=hermes.example.com \
  --set hermes.jwt.secret=<your-jwt-secret> \
  --set hermes.apiKey.hmacSecret=<your-hmac-secret> \
  --create-namespace --namespace hermes

## Verify

helm test hermes -n hermes
kubectl get pods -n hermes

## Next Steps

- Point your DNS at your ingress controller's external IP
- See [Production Guide](production.md) for hardening
- See [Configuration Reference](configuration.md) for all options
```

- [ ] **Step 2: Create production.md**

Cover: external database, external secrets, TLS, resource tuning, HA replicas, network policies. Use concrete `values.yaml` examples for each topic.

- [ ] **Step 3: Create configuration.md**

Full values reference with examples for common scenarios: external Postgres, SMTP email, SES email, custom ingress annotations, autoscaling.

- [ ] **Step 4: Create upgrading.md**

Template for version-to-version upgrade notes. Include the general upgrade process:

```bash
helm repo update
helm upgrade hermes oci://ghcr.io/hermesnotifications/charts/hermes --version <new-version> -n hermes
```

- [ ] **Step 5: Commit**

```bash
git add docs/self-hosting/
git commit -m "docs: add self-hosting guides (quickstart, production, config, upgrading)"
```

---

### Task 16: Makefile Target for Chart Linting

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Read current Makefile structure**

Read the Makefile to find where to add the new target (near other lint/validation targets).

- [ ] **Step 2: Add helm-lint target**

Add a target that lints the chart and validates the schema:

```makefile
.PHONY: helm-lint
helm-lint: ## Lint the Helm chart
	helm lint charts/hermes/ --set hermes.jwt.secret=test --set hermes.apiKey.hmacSecret=test
	jq . charts/hermes/values.schema.json > /dev/null
```

- [ ] **Step 3: Verify the target works**

Run: `make helm-lint`
Expected: Lint passes, JSON schema is valid.

- [ ] **Step 4: Commit**

```bash
git add Makefile
git commit -m "build: add helm-lint Makefile target"
```

---

### Task 17: Add Chart Lint to CI

**Files:**
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Read current ci.yml**

Read `.github/workflows/ci.yml` to understand the job structure.

- [ ] **Step 2: Add helm-lint job**

Add a new job that runs `make helm-lint` (or the equivalent helm commands). This should run on PRs that touch `charts/**`.

```yaml
helm-lint:
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v4
    - uses: azure/setup-helm@v4
    - run: helm dependency build charts/hermes/
    - run: helm lint charts/hermes/ --set hermes.jwt.secret=test --set hermes.apiKey.hmacSecret=test
    - run: helm template test charts/hermes/ --set hermes.jwt.secret=test --set hermes.apiKey.hmacSecret=test > /dev/null
```

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: add Helm chart lint to CI pipeline"
```
