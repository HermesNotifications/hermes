{{/*
Refuse to render a combination of values that cannot start.

internal/config/config.go Validate() fails closed outside `development`: it demands
sslmode=require (or stricter) on Postgres, rediss:// on Redis, tls:// on NATS, and rejects
each of the three built-in placeholder secrets by value. Those checks run in every service,
in the migration Job, in the cleanup CronJob and in the stream provisioning Job.

The bundled sub-charts cannot satisfy any of them. _helpers.tpl builds their URLs as
`?sslmode=disable`, `nats://` and `redis://`, deliberately — that is the evaluation
posture ADR 0005 keeps out of scope for the chart. So `hermes.env=production` with bundled
infrastructure is not a configuration that merely performs badly; it is one where all nine
workloads exit at startup and stay in CrashLoopBackOff.

Producing that outcome is a choice the chart makes at render time with full knowledge, so
it makes it here instead: one `helm install` error naming the specific sub-chart, rather
than six crash-looping pods and a log dive. Production installs use the external* settings.

Called from configmap.yaml, which always renders.
*/}}
{{- define "hermes.validateEnvironment" -}}
{{- if ne .Values.hermes.env "development" -}}
{{-   $bundled := list -}}
{{-   if .Values.postgresql.enabled -}}
{{-     $bundled = append $bundled "postgresql (built as ?sslmode=disable; Validate() requires require/verify-ca/verify-full)" -}}
{{-   end -}}
{{-   if .Values.redis.enabled -}}
{{-     $bundled = append $bundled "redis (built as redis://; Validate() requires rediss://)" -}}
{{-   end -}}
{{-   if .Values.nats.enabled -}}
{{-     $bundled = append $bundled "nats (built as nats://; Validate() requires tls://)" -}}
{{-   end -}}
{{-   if .Values.centrifugo.enabled -}}
{{-     $bundled = append $bundled "centrifugo (bundled instance uses the memory engine and no API key)" -}}
{{-   end -}}
{{-   if $bundled -}}
{{-     fail (printf "hermes.env=%s cannot be used with the bundled sub-charts. Every Hermes workload calls config.Validate() at startup and would exit immediately. Disable and replace: %s. Set the matching external* values (externalPostgresql/externalRedis/externalNats/externalCentrifugo) to TLS endpoints you control." .Values.hermes.env (join "; " $bundled)) -}}
{{-   end -}}
{{-   if or (not .Values.hermes.centrifugo.apiKey) (eq .Values.hermes.centrifugo.apiKey "centrifugo-api-key") -}}
{{-     if not .Values.externalCentrifugo.existingSecret -}}
{{-       fail (printf "hermes.env=%s requires a real hermes.centrifugo.apiKey (or externalCentrifugo.existingSecret). Leaving it unset leaves HERMES_CENTRIFUGO_API_KEY at the built-in default \"centrifugo-api-key\", which config.go rejects by value because it is committed to a public repository." .Values.hermes.env) -}}
{{-     end -}}
{{-   end -}}
{{- end -}}
{{- end }}
