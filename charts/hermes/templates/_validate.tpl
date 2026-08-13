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
{{/*
The admin portal image does not exist anywhere.

`web/admin` is a Next.js app with no Dockerfile in this repository. The Tiltfile runs it as
a `local_resource` (`pnpm dev`), and .github/workflows/cd.yml builds Go services only —
every entry in its matrix goes through `deploy/docker/Dockerfile` with `--build-arg
SERVICE`, which has no meaning for a pnpm workspace. So `adminPortal.enabled=true` on chart
defaults produces a Deployment referencing ghcr.io/hermesnotifications/hermes-admin-portal,
which nobody publishes and nothing in this repository can build.

Unlike hermes-cleanup — which was the same defect and was fixable by adding one line to the
cd.yml matrix — this one needs a Dockerfile that does not exist. Rather than ship a value
that guarantees ImagePullBackOff, the chart refuses it and tells you what to override. If
you have built and pushed your own portal image, point adminPortal.image.repository at it
and this passes.
*/}}
{{- define "hermes.validateAdminPortal" -}}
{{- if .Values.adminPortal.enabled -}}
{{-   if eq .Values.adminPortal.image.repository "hermes-admin-portal" -}}
{{-     fail "adminPortal.enabled=true, but no hermes-admin-portal image is published and this repository contains no Dockerfile that could build one (web/admin is a Next.js app; .github/workflows/cd.yml builds Go services only). Build and push your own portal image, then set adminPortal.image.repository to it." -}}
{{-   end -}}
{{- end -}}
{{- end }}

{{/*
The bundled Centrifugo's two secrets must name the Secret this release actually creates.

They are written out in values.yaml because a parent chart cannot template a sub-chart's
values, which means they are a hardcoded string that a differently-named release, or
hermes.existingSecret, silently invalidates. The consequences are both quiet:

  * a wrong CENTRIFUGO_HTTP_API_KEY reference and every publish is rejected 401, while the
    notification is already in Postgres -- so the inbox is right on reload and only live
    push is missing;
  * a wrong CENTRIFUGO_CLIENT_TOKEN_HMAC_SECRET_KEY reference and Centrifugo can verify no
    token at all, refusing every browser while the websocket handshake still succeeds.

Neither produces an error anywhere that names Centrifugo, so this is checked at render time.
*/}}
{{- define "hermes.validateCentrifugoSecrets" -}}
{{- if .Values.centrifugo.enabled -}}
{{-   $want := include "hermes.secretName" . -}}
{{-   $seen := dict -}}
{{-   range (.Values.centrifugo.envSecret | default list) -}}
{{-     $ref := .secretKeyRef | default dict -}}
{{-     $seen = set $seen .name true -}}
{{-     if ne (get $ref "name") $want -}}
{{-       fail (printf "centrifugo.envSecret entry %s references Secret %q, but this release creates %q. Realtime fails silently when this is wrong: publishes are rejected 401 and browsers cannot authenticate, while the websocket route itself still answers." .name (get $ref "name") $want) -}}
{{-     end -}}
{{-   end -}}
{{-   if not (hasKey $seen "CENTRIFUGO_HTTP_API_KEY") -}}
{{-     fail (printf "the bundled centrifugo has no CENTRIFUGO_HTTP_API_KEY. Centrifugo 6 requires API authentication, so every publish from the inbox worker would be rejected 401 -- notifications would still be stored and would simply never arrive live. Set centrifugo.envSecret to read it from %s." $want) -}}
{{-   end -}}
{{-   if not (hasKey $seen "CENTRIFUGO_CLIENT_TOKEN_HMAC_SECRET_KEY") -}}
{{-     fail (printf "the bundled centrifugo has no CENTRIFUGO_CLIENT_TOKEN_HMAC_SECRET_KEY. The token the widget presents is one Hermes minted with HERMES_JWT_SECRET; without the same secret here Centrifugo can verify no token and refuses every browser. Set centrifugo.envSecret to read it from %s." $want) -}}
{{-   end -}}
{{/*
The reference resolving to the right Secret is not enough -- the KEY has to be in it.

hermes.jwt.existingSecret moves HERMES_JWT_SECRET out of the release Secret, leaving the name
matching and the key gone. The result is not a render error but a Centrifugo pod stuck in
CreateContainerConfigError, because an unresolvable secretKeyRef fails when the kubelet builds
the container rather than when the chart renders. `helm template` cannot see it; only an actual
install can, which is how it reached CI rather than a local check.
*/}}
{{-   if .Values.hermes.jwt.existingSecret -}}
{{-     fail (printf "hermes.jwt.existingSecret moves HERMES_JWT_SECRET out of %s, but the bundled centrifugo reads its token verification key from there -- the pod would sit in CreateContainerConfigError. Either point centrifugo.envSecret's CENTRIFUGO_CLIENT_TOKEN_HMAC_SECRET_KEY at %s instead, or disable the bundled centrifugo and use externalCentrifugo." $want .Values.hermes.jwt.existingSecret) -}}
{{-   end -}}
{{- end -}}
{{- end }}

{{/*
dispatch.concurrency and dispatch.database.maxConns describe one thing, and every way of
getting them out of step fails quietly.

cmd/dispatch caps the worker pool at the database pool size at startup -- each worker holds a
connection for as long as it is processing, so workers past the pool only add contention. The
cap is right, and its consequence is that raising concurrency against an unchanged pool changes
throughput by exactly nothing. The only evidence is one Warn line at boot, in a service whose
logs nobody reads until something is already wrong. That is why hermes.dispatchDatabaseMaxConns
derives the pool instead of defaulting it, and why the three cases the derivation cannot cover
are refused here.

The last of the three is the server side. Connections are a fixed, shared, cluster-wide budget:
every service holds its own pool and Postgres ships with max_connections=100. Nothing in
dispatch fails when that runs out -- Postgres refuses whichever service connects next, so the
damage lands on inbox or admin while dispatch runs happily at the concurrency you asked for.
The arithmetic is checkable for the bundled server (the chart knows its max_connections) and
not for an external one, so it is checked in the first case and documented in both.
*/}}
{{- define "hermes.validateDispatchPool" -}}
{{- $workers := include "hermes.dispatchConcurrency" . | int -}}
{{- $pool := include "hermes.dispatchDatabaseMaxConns" . | int -}}
{{- $explicit := get (.Values.dispatch.database | default dict) "maxConns" -}}
{{/*
1. Set by hand, below the worker count. Clamped at runtime to $pool, so the install would run
   narrower than the values say and nothing but a log line would disagree.
*/}}
{{- if and $explicit (lt $pool $workers) -}}
{{-   fail (printf "dispatch.database.maxConns is %d but dispatch.concurrency is %d. cmd/dispatch clamps the worker pool to the database pool at startup, so dispatch would run %d wide while these values claim %d -- reported only as one warning in the dispatch log. Leave dispatch.database.maxConns empty to derive it (concurrency + 2 = %d), or set it to at least %d." $pool $workers $pool $workers (add $workers 2) $workers) -}}
{{- end -}}
{{/*
2. pool_max_conns in an inline external URL. database.NewPoolWithConfig lets URL pool_* win over
   HERMES_DATABASE_MAX_CONNS -- deliberately, so cmd/dispatchbench can sweep pool sizes -- which
   means a URL carrying it overrides what this chart renders, for every service that shares the
   URL. Only an inline url can be inspected; externalPostgresql.existingSecret is opaque at
   render time and pretending otherwise would be worse than not checking (see below).
*/}}
{{- if and (not .Values.postgresql.enabled) (not .Values.externalPostgresql.existingSecret) -}}
{{-   $url := .Values.externalPostgresql.url | default "" -}}
{{-   if contains "pool_max_conns" $url -}}
{{-     if $explicit -}}
{{-       fail (printf "externalPostgresql.url carries pool_max_conns and dispatch.database.maxConns is set to %v. The URL wins (database.NewPoolWithConfig gives URL pool_* parameters precedence over HERMES_DATABASE_MAX_CONNS), so the chart value would be rendered and ignored. Remove pool_max_conns from the URL -- note it sizes every service's pool, not just dispatch's -- and size dispatch here." $explicit) -}}
{{-     end -}}
{{-     $found := regexFind "pool_max_conns=[0-9]+" $url -}}
{{-     if $found -}}
{{-       $urlPool := $found | trimPrefix "pool_max_conns=" | int -}}
{{-       if lt $urlPool $workers -}}
{{-         fail (printf "externalPostgresql.url sets pool_max_conns=%d, below dispatch.concurrency (%d). URL pool_* parameters win over HERMES_DATABASE_MAX_CONNS, so dispatch would clamp its worker pool to %d and the chart's own pool value could not raise it. Remove pool_max_conns from the URL and let dispatch.database.maxConns size the pool, or raise it to at least %d." $urlPool $workers $urlPool $workers) -}}
{{-       end -}}
{{-     end -}}
{{-   end -}}
{{- end -}}
{{/*
3. The bundled server's connection budget, checked only once dispatch has been tuned past the
   built-in pool size.

   Gated on that deliberately. At chart defaults the fleet already sits at 9 x 10 = 90 of the
   image's 100, so an unconditional check would either refuse installs that work today or pick
   a threshold loose enough to be meaningless. Gating it means the arithmetic appears to the
   person who changed the number that makes it matter, and an install that never touches these
   values renders exactly as it did before.
*/}}
{{- if and .Values.postgresql.enabled (gt $pool 10) -}}
{{/*
ROT: 10 is database.DefaultPoolConfig.MaxConns, the pool every service other than dispatch gets
because the chart sets HERMES_DATABASE_MAX_CONNS for dispatch alone. If that default moves in
internal/database, this arithmetic quietly under-counts.
*/}}
{{-   $defaultPool := 10 -}}
{{-   $total := 0 -}}
{{-   $lines := list -}}
{{-   range $svc := list "admin" "send" "dispatch" "inbox" "user" "workerEmail" "workerSms" "workerInbox" "workerEvents" -}}
{{-     $block := index $.Values $svc -}}
{{-     $auto := $block.autoscaling | default dict -}}
{{/*
Worst case, not steady state: under an HPA the ceiling is maxReplicas, and a budget computed
from minReplicas is a budget that holds until the first time it matters. `int` because YAML
numbers arrive as float64 and printf %d cannot render one.
*/}}
{{-     $replicas := ternary (get $auto "maxReplicas" | default 1) ($block.replicas | default 1) (eq (get $auto "enabled") true) | int -}}
{{-     $conns := ternary $pool $defaultPool (eq $svc "dispatch") -}}
{{-     $total = add $total (mul $replicas $conns) -}}
{{-     $lines = append $lines (printf "%s %dx%d" $svc $replicas $conns) -}}
{{-   end -}}
{{/*
10 held back for superuser_reserved_connections (3 by default), the migration and bootstrap
Jobs, the cleanup CronJob and whatever psql an operator opens while debugging this.
*/}}
{{-   $reserved := 10 -}}
{{-   $max := .Values.postgresql.maxConnections | default 100 | int -}}
{{-   $budget := sub $max $reserved -}}
{{-   if gt $total $budget -}}
{{-     fail (printf "dispatch is tuned to a pool of %d, which puts the fleet's worst-case Postgres connections at %d (%s) against a budget of %d -- the bundled server's max_connections of %d less %d held back for the migration/bootstrap Jobs, the cleanup CronJob and superuser_reserved_connections. Over that ceiling Postgres refuses whichever service connects next, so this surfaces as inbox or admin failing while dispatch runs fine. Raise postgresql.maxConnections (and postgresql.resources with it), lower dispatch.concurrency, or move to externalPostgresql -- the bundled Postgres is a single unreplicated evaluation instance (ADR 0009) and is not where throughput work belongs." $pool $total (join ", " $lines) $budget $max $reserved) -}}
{{-   end -}}
{{- end -}}
{{- end }}

{{- define "hermes.validateEnvironment" -}}
{{- if ne .Values.hermes.env "development" -}}
{{/*
Bundled datastores are legal outside development NOW, provided tls.enabled and each one has a
certificate. Before that existed this block refused the combination outright, and the refusal
was correct: the URLs were built as ?sslmode=disable, redis:// and nats://, which every
workload rejects at startup.

What is checked here is that the *encryption* exists. What is deliberately not checked, and
must be said rather than implied: TLS is not authentication. The bundled Redis takes no
password, and the bundled NATS has no NKey accounts, so any pod in this namespace that can
reach either port has full access. Encryption raises the bar from anything on the network to
anything in the namespace. NOTES.txt says so too.
*/}}
{{-   $plaintext := list -}}
{{-   if and .Values.postgresql.enabled (not .Values.tls.enabled) -}}
{{-     $plaintext = append $plaintext "postgresql (built as ?sslmode=disable; Validate() requires require/verify-ca/verify-full)" -}}
{{-   end -}}
{{-   if and .Values.redis.enabled (not .Values.tls.enabled) -}}
{{-     $plaintext = append $plaintext "redis (built as redis://; Validate() requires rediss://)" -}}
{{-   end -}}
{{-   if and .Values.nats.enabled (not .Values.tls.enabled) -}}
{{-     $plaintext = append $plaintext "nats (built as nats://; Validate() requires tls://)" -}}
{{-   end -}}
{{-   if $plaintext -}}
{{-     fail (printf "hermes.env=%s with bundled datastores requires tls.enabled=true. Without it these are plaintext and every Hermes workload exits at startup on config.Validate(): %s. Either set tls.enabled=true with tls.issuer.name pointing at a cert-manager issuer (or per-store tls.existingSecret), or disable them and point the external* values at TLS endpoints you operate." .Values.hermes.env (join "; " $plaintext)) -}}
{{-   end -}}
{{-   if .Values.centrifugo.enabled -}}
{{/*
Centrifugo is still refused, and for a different reason than the other three: this is not
about transport. The bundled instance runs the MEMORY engine, so a publication reaches only
the users connected to the pod that received it. At one replica that is invisible; at two it
silently delivers to half your users. No amount of TLS changes that, so there is nothing to
enable here -- it needs an external Centrifugo on the Redis engine.
*/}}
{{-     fail (printf "hermes.env=%s cannot be used with the bundled centrifugo: it runs the in-memory engine, so a publication reaches only the users connected to the pod that received it. Set centrifugo.enabled=false and point externalCentrifugo at an instance using the Redis engine." .Values.hermes.env) -}}
{{-   end -}}
{{-   if or (not .Values.hermes.centrifugo.apiKey) (eq .Values.hermes.centrifugo.apiKey "centrifugo-api-key") -}}
{{-     if not .Values.externalCentrifugo.existingSecret -}}
{{-       fail (printf "hermes.env=%s requires a real hermes.centrifugo.apiKey (or externalCentrifugo.existingSecret). Leaving it unset leaves HERMES_CENTRIFUGO_API_KEY at the built-in default \"centrifugo-api-key\", which config.go rejects by value because it is committed to a public repository." .Values.hermes.env) -}}
{{-     end -}}
{{-   end -}}
{{/*
An external endpoint whose URL is plaintext fails exactly as a bundled one would, and the
chart could see it and did not: it rendered happily and the operator found out from nine
crash-looping pods. Only inline URLs can be checked -- the contents of an existingSecret are
not visible at render time, and pretending otherwise would be worse than not checking.
*/}}
{{-   if and (not .Values.postgresql.enabled) .Values.externalPostgresql.url -}}
{{-     if not (or (contains "sslmode=require" .Values.externalPostgresql.url) (contains "sslmode=verify-ca" .Values.externalPostgresql.url) (contains "sslmode=verify-full" .Values.externalPostgresql.url)) -}}
{{-       fail (printf "hermes.env=%s requires externalPostgresql.url to carry sslmode=require, verify-ca or verify-full. config.Validate() rejects anything else, and rejects `allow` and `prefer` specifically because libpq falls back to plaintext without reporting it." .Values.hermes.env) -}}
{{-     end -}}
{{-   end -}}
{{-   if and (not .Values.redis.enabled) .Values.externalRedis.url -}}
{{-     if not (hasPrefix "rediss://" .Values.externalRedis.url) -}}
{{-       fail (printf "hermes.env=%s requires externalRedis.url to begin rediss://. config.Validate() rejects redis:// outside development." .Values.hermes.env) -}}
{{-     end -}}
{{-   end -}}
{{-   if and (not .Values.nats.enabled) .Values.externalNats.url -}}
{{-     if not (hasPrefix "tls://" .Values.externalNats.url) -}}
{{-       fail (printf "hermes.env=%s requires externalNats.url to begin tls://. config.Validate() rejects nats:// outside development." .Values.hermes.env) -}}
{{-     end -}}
{{-   end -}}
{{- end -}}
{{/*
Independent of hermes.env: tls.enabled with nothing to sign with.

Checked separately because the failure is the same in development -- the Certificates are
created, cert-manager cannot issue them, the Secrets never appear, and the datastore pods sit
in ContainerCreating waiting for a volume that will not arrive. That reads as a storage
problem, so it is worth naming here.
*/}}
{{- if .Values.tls.enabled -}}
{{/*
The bundled bus needs sub-chart values this chart cannot template.

A parent chart cannot reach a sub-chart's values -- they are merged before rendering -- so the
three secretName references have to be supplied by hand, and this asserts they were and that
they name the certificate we actually issue.

Without the assertion the failure is: NATS serves plaintext, the ConfigMap advertises tls://,
every workload exits at startup, and nothing in any log connects the two. With it, the error
names the file to pass.
*/}}
{{-   if .Values.nats.enabled -}}
{{-     $want := include "hermes.natsTLSSecret" . -}}
{{-     $natsTLS := .Values.nats.config.nats.tls | default dict -}}
{{-     $clusterTLS := .Values.nats.config.cluster.tls | default dict -}}
{{-     $tlsCA := .Values.nats.tlsCA | default dict -}}
{{-     if not (and (get $natsTLS "enabled") (get $clusterTLS "enabled") (get $tlsCA "enabled")) -}}
{{-       fail (printf "tls.enabled=true with the bundled NATS, but the NATS sub-chart's own TLS is not switched on. A parent chart cannot set a sub-chart's values, so these must be supplied: apply charts/hermes/values-production-bundled.yaml with -f, or set nats.tlsCA.enabled, nats.config.nats.tls.enabled and nats.config.cluster.tls.enabled to true with secretName %q on all three." $want) -}}
{{-     end -}}
{{-     $names := list (get $natsTLS "secretName") (get $clusterTLS "secretName") (get $tlsCA "secretName") -}}
{{-     range $names -}}
{{-       if ne . $want -}}
{{-         fail (printf "the NATS sub-chart references TLS secret %q but this chart issues %q. All three of nats.tlsCA.secretName, nats.config.nats.tls.secretName and nats.config.cluster.tls.secretName must name the certificate the chart creates, or NATS mounts a Secret that does not exist and never starts." . $want) -}}
{{-       end -}}
{{-     end -}}
{{-   end -}}
{{-   if and (not .Values.tls.issuer.name) (not .Values.tls.issuer.create) -}}
{{-     $needsIssuer := list -}}
{{-     if and .Values.postgresql.enabled (not .Values.postgresql.tls.existingSecret) -}}
{{-       $needsIssuer = append $needsIssuer "postgresql" -}}
{{-     end -}}
{{-     if and .Values.redis.enabled (not .Values.redis.tls.existingSecret) -}}
{{-       $needsIssuer = append $needsIssuer "redis" -}}
{{-     end -}}
{{-     if .Values.nats.enabled -}}
{{-       $needsIssuer = append $needsIssuer "nats" -}}
{{-     end -}}
{{-     if $needsIssuer -}}
{{-       fail (printf "tls.enabled=true but no issuer is configured, and these stores have no certificate of their own: %s. The chart cannot mint certificates itself -- Helm's genCA regenerates on every render, so `helm template` would disagree with `install` and every GitOps reconcile would churn the material. Set tls.issuer.name to a cert-manager Issuer or ClusterIssuer you already have (requires cert-manager), or supply <store>.tls.existingSecret with tls.crt, tls.key and ca.crt%s." (join ", " $needsIssuer) (ternary "" " -- note that nats has no existingSecret option and always needs an issuer" (not .Values.nats.enabled))) -}}
{{-     end -}}
{{-   end -}}
{{- end -}}
{{- end }}
