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
*/}}
{{- define "hermes.labels" -}}
helm.sh/chart: {{ include "hermes.chart" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: hermes
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
When the bundled postgresql sub-chart is enabled, construct a DSN from its auth values.
Otherwise, use the external URL.
*/}}
{{- define "hermes.databaseUrl" -}}
{{- if .Values.postgresql.enabled -}}
{{- $host := printf "%s-postgresql" .Release.Name -}}
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
*/}}
{{- define "hermes.redisUrl" -}}
{{- if .Values.redis.enabled -}}
{{- printf "redis://%s-redis-master:6379/0" .Release.Name -}}
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
{{- .Values.externalCentrifugo.url -}}
{{- end -}}
{{- end }}
