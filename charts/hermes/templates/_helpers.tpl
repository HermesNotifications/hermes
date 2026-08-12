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
Create chart name and version as used by the chart label.
*/}}
{{- define "hermes.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels shared across all resources.

app.kubernetes.io/version belongs HERE and not in hermes.serviceSelectorLabels. A Deployment's
`spec.selector` is immutable, so a version in the selector would make every upgrade fail with
"field is immutable" and need the Deployment deleted by hand. It is here so that `kubectl get
deploy -L app.kubernetes.io/version` answers "what is actually running?" -- which nothing on a
live cluster could answer before, since the binaries do not report a version either.
*/}}
{{- define "hermes.labels" -}}
helm.sh/chart: {{ include "hermes.chart" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: hermes
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
{{- end }}

{{/*
Labels for a specific service. Call with (dict "root" . "service" "admin").
Includes common labels plus app name and instance.
*/}}
{{- define "hermes.serviceLabels" -}}
{{ include "hermes.labels" .root }}
{{ include "hermes.serviceSelectorLabels" . }}
{{- end }}

{{/*
Selector labels for a specific service. Call with (dict "root" . "service" "admin").
*/}}
{{- define "hermes.serviceSelectorLabels" -}}
app.kubernetes.io/name: {{ include "hermes.name" .root }}-{{ .service }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
{{- end }}

{{/*
Build a container image reference. Call with (dict "root" . "service" .Values.admin).
Merges global registry/tag with per-service overrides.
*/}}
{{- define "hermes.image" -}}
{{- $registry := .root.Values.global.image.registry -}}
{{- $repository := .service.image.repository -}}
{{- $tag := default .root.Values.global.image.tag .service.image.tag | default .root.Chart.AppVersion -}}
{{- if $registry -}}
{{- printf "%s/%s:%s" $registry $repository $tag -}}
{{- else -}}
{{- printf "%s:%s" $repository $tag -}}
{{- end -}}
{{- end }}

{{/*
Image pull policy. Call with (dict "root" . "service" .Values.admin) -- `service` may be a
block with no `image` key, so this tolerates its absence.

Was hardcoded IfNotPresent in twelve templates, which is right for the immutable semver tags
this chart defaults to and wrong for anyone tracking a mutable tag or loading images into a
kind/k3d node by hand: the node keeps the stale copy forever and the deploy silently does
nothing.
*/}}
{{- define "hermes.imagePullPolicy" -}}
{{- $service := .service | default dict -}}
{{- $image := get $service "image" | default dict -}}
{{- get $image "pullPolicy" | default .root.Values.global.image.pullPolicy -}}
{{- end }}

{{/*
imagePullSecrets for a pod spec, rendered as a complete key (or nothing at all).

Emits the key only when there is at least one secret, because `imagePullSecrets: []` and an
absent key are not the same to some admission controllers. Call with the root context.
*/}}
{{- define "hermes.imagePullSecrets" -}}
{{- with .Values.global.imagePullSecrets }}
imagePullSecrets:
{{- range . }}
  - name: {{ . }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Return the secret name. Uses existingSecret if set, otherwise generates one.
*/}}
{{- define "hermes.secretName" -}}
{{- if .Values.hermes.existingSecret -}}
{{- .Values.hermes.existingSecret -}}
{{- else -}}
{{- include "hermes.fullname" . }}-secrets
{{- end -}}
{{- end }}

{{/*
Return the configmap name.
*/}}
{{- define "hermes.configMapName" -}}
{{- include "hermes.fullname" . }}-config
{{- end }}

{{/*
Build the database URL.
When the bundled Postgres is enabled, construct a DSN from its auth values. Otherwise, use
the external URL.

The host must match the Service rendered by templates/postgresql.yaml. It is built from
hermes.fullname, not .Release.Name: those differ whenever the release name does not already
contain the chart name (release `hv` gives fullname `hv-hermes`), and the bundled Postgres
was a sub-chart until ADR 0009, which named its Service after the release instead. Getting
this wrong produces a URL pointing at nothing, which renders and lints perfectly and fails
only at connect time.
*/}}
{{- define "hermes.databaseUrl" -}}
{{- if .Values.postgresql.enabled -}}
{{- $host := printf "%s-postgresql" (include "hermes.fullname" .) -}}
{{- $port := "5432" -}}
{{- $user := .Values.postgresql.auth.username -}}
{{- $pass := .Values.postgresql.auth.password -}}
{{- $db   := .Values.postgresql.auth.database -}}
{{- printf "postgres://%s:%s@%s:%s/%s?sslmode=disable" $user $pass $host $port $db -}}
{{- else -}}
{{- .Values.externalPostgresql.url -}}
{{- end -}}
{{- end }}

{{/*
Return the NATS URL.
*/}}
{{- define "hermes.natsUrl" -}}
{{- if .Values.nats.enabled -}}
{{- printf "nats://%s-nats:4222" .Release.Name -}}
{{- else -}}
{{- .Values.externalNats.url -}}
{{- end -}}
{{- end }}

{{/*
Return the Redis URL.

`-redis`, not `-redis-master`: the `-master` suffix was the bitnami/redis chart's naming for
a replication topology this chart never used. templates/redis.yaml renders a single Service
named after hermes.fullname (see hermes.databaseUrl for why fullname rather than the release
name).
*/}}
{{- define "hermes.redisUrl" -}}
{{- if .Values.redis.enabled -}}
{{- printf "redis://%s-redis:6379/0" (include "hermes.fullname" .) -}}
{{- else -}}
{{- .Values.externalRedis.url -}}
{{- end -}}
{{- end }}

{{/*
Return the Centrifugo API URL.
*/}}
{{- define "hermes.centrifugoApiUrl" -}}
{{- if .Values.centrifugo.enabled -}}
{{- printf "http://%s-centrifugo:8000" .Release.Name -}}
{{- else -}}
{{- .Values.externalCentrifugo.apiUrl -}}
{{- end -}}
{{- end }}

{{/*
Render per-service OpenTelemetry service identity. Call with
(dict "root" . "service" "admin").
*/}}
{{- define "hermes.otelServiceEnv" -}}
{{- if .root.Values.observability.enabled -}}
- name: OTEL_SERVICE_NAME
  value: hermes-{{ .service }}
{{- end }}
{{- end }}

{{/*
Render the HTTP rate-limit env for one service. Call with a dict carrying the
root context and the service's own values block, e.g.
  (include "hermes.rateLimitEnv" (dict "root" . "svc" .Values.send))

The per-credential knobs are per service because each service is its own
Deployment. The per-IP and reconciliation knobs are global, under
.Values.rateLimit, because they describe the deployment rather than one service.

By default enforcement is PER REPLICA: the cluster-wide ceiling is the configured
rate times the replica count, so size these per pod. With
.Values.rateLimit.distributed.enabled the check runs in Redis and the configured
rate becomes the cluster-wide ceiling instead. An empty burst or perSecond keeps
the service's compiled-in default.
*/}}
{{- define "hermes.rateLimitEnv" -}}
{{- with .svc.rateLimit }}
{{- if hasKey . "enabled" }}
- name: HERMES_RATELIMIT_ENABLED
  value: {{ .enabled | quote }}
{{- end }}
{{- if .burst }}
- name: HERMES_RATELIMIT_BURST
  value: {{ .burst | quote }}
{{- end }}
{{- if .perSecond }}
- name: HERMES_RATELIMIT_PER_SECOND
  value: {{ .perSecond | quote }}
{{- end }}
{{- end }}
{{- with .root.Values.rateLimit }}
{{- with .perIP }}
{{- if hasKey . "enabled" }}
- name: HERMES_RATELIMIT_IP_ENABLED
  value: {{ .enabled | quote }}
{{- end }}
{{- if .burst }}
- name: HERMES_RATELIMIT_IP_BURST
  value: {{ .burst | quote }}
{{- end }}
{{- if .perSecond }}
- name: HERMES_RATELIMIT_IP_PER_SECOND
  value: {{ .perSecond | quote }}
{{- end }}
{{- end }}
{{- if .trustedProxyCIDRs }}
- name: HERMES_TRUSTED_PROXY_CIDRS
  value: {{ join "," .trustedProxyCIDRs | quote }}
{{- end }}
{{- with .distributed }}
{{- if hasKey . "enabled" }}
- name: HERMES_RATELIMIT_DISTRIBUTED_ENABLED
  value: {{ .enabled | quote }}
{{- end }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Termination grace period for a Hermes pod. Call with (dict "root" . "service" "inbox").

The grace period has to cover the whole shutdown sequence -- the optional preStop sleep, the
in-process drain delay, the NATS drain, and the HTTP shutdown -- or the kubelet SIGKILLs the
process partway through. That is worse than not draining at all: the process has already
stopped accepting work and now abandons what it was finishing, so those messages are
redelivered with their side effects repeated. scripts/check_shutdown_budget.py enforces it.

Services that drain a NATS consumer need materially longer than ones that do not, which is why
each service may override the shared default.
*/}}
{{- define "hermes.terminationGracePeriod" -}}
{{- $svc := index .root.Values .service -}}
{{- $grace := $svc.terminationGracePeriodSeconds | default .root.Values.hermes.shutdown.terminationGracePeriodSeconds -}}
terminationGracePeriodSeconds: {{ $grace }}
{{- end }}

{{/*
Container preStop hook, rendered only when the operator opts in. Call with the root context.

Every Hermes image is FROM scratch (deploy/docker/Dockerfile), so there is no shell and the
conventional `preStop: exec: ["/bin/sh", "-c", "sleep 5"]` cannot run. The drain delay is
therefore in-process (HERMES_SHUTDOWN_DRAIN_DELAY) and works everywhere; this SleepAction is
belt and braces on top, and is opt-in because it needs Kubernetes 1.30+ (beta in 1.29).
*/}}
{{- define "hermes.preStop" -}}
{{- if .Values.hermes.shutdown.preStopSleepSeconds }}
lifecycle:
  preStop:
    sleep:
      seconds: {{ .Values.hermes.shutdown.preStopSleepSeconds }}
{{- end }}
{{- end }}
