# Configuration Reference

Reference for the Hermes Helm chart's values, written against
`charts/hermes/values.yaml` and the templates that consume them.

> **Unknown keys are silently ignored.** `charts/hermes/values.schema.json` does not set
> `additionalProperties: false`, so a value the chart does not have is not rejected — the
> install succeeds and the setting has no effect. An earlier version of this page documented
> sixteen keys the chart never had (`services.admin.replicaCount`, `ingress.host`,
> `networkPolicies`, and others); every one of them installed cleanly and quietly ran on chart
> defaults. If an override appears to do nothing, check the spelling here first, and see
> [Values the chart does not have](#values-the-chart-does-not-have) at the end of this page.
>
> `helm show values oci://ghcr.io/hermesnotifications/charts/hermes` is the authority for the
> version you are installing.

## Required values

Three values must be set. The first is enforced by the schema, the other two by
`required` in `templates/secret.yaml`:

```yaml
global:
  # Required. The hostname every ingress rule is bound to. There is no ingress.host.
  domain: hermes.example.com

hermes:
  jwt:
    secret: ""            # required unless hermes.jwt.existingSecret is set
  apiKey:
    hmacSecret: ""        # required unless hermes.apiKey.existingSecret is set
```

`global.domain` has `minLength: 1` in the schema, so `helm install` fails immediately
without it rather than rendering an Ingress with an empty host.

## Environment mode

```yaml
hermes:
  # "development" (default) or "production". Schema-constrained to those two.
  env: development
```

This sets `HERMES_ENV`, which `internal/config/config.go` reads at startup. Only the exact
string `development` relaxes the startup checks; the schema's enum exists so a typo is
rejected by `helm install` instead of quietly selecting the strict path and failing later
inside the pod.

`HERMES_ENV` is written to the shared ConfigMap, so the migration Job, the NATS stream
provisioner Job and the cleanup CronJob see it too — they all run the same validation.

**With `hermes.env: production`, `Config.Validate` refuses to start any service unless
all of the following hold:**

| Requirement | Rejected values |
|---|---|
| `HERMES_DATABASE_URL` carries `sslmode=require`, `verify-ca` or `verify-full` | absent, `disable`, `allow`, `prefer` — the last two silently fall back to plaintext |
| `HERMES_REDIS_URL` uses the `rediss://` scheme | `redis://` |
| `HERMES_NATS_URL` uses the `tls://` scheme | `nats://` |
| `hermes.jwt.secret` is not the built-in default | `hermes-jwt-secret` |
| `hermes.apiKey.hmacSecret` is not the built-in default | `hermes-dev-hmac-secret` |
| `hermes.centrifugo.apiKey` is set to something | empty, which falls back to the published default `centrifugo-api-key` |

The last three are placeholder checks: those defaults are committed to a public repository,
so a deployment still using one does not have a weak secret, it has a published constant.

**Consequence: `hermes.env: production` is incompatible with the bundled sub-charts.** The
chart builds the bundled datastore URLs as `postgres://…?sslmode=disable`, `redis://…` and
`nats://…` (`templates/_helpers.tpl`), all three of which the production checks reject. A
production install must disable all four sub-charts and use the `external*` values below,
with TLS in every URL, plus an explicit `hermes.centrifugo.apiKey`. See
[Bundled infrastructure is for evaluation](#bundled-infrastructure-is-for-evaluation).

## Images

There is no top-level `image:` key and no `imagePullSecrets`.

```yaml
global:
  image:
    registry: ghcr.io/hermesnotifications   # prefix for every Hermes image
    tag: ""                                 # defaults to the chart appVersion
```

Per-service overrides take a **bare repository name**, which the chart joins to
`global.image.registry`:

```yaml
admin:
  image:
    repository: hermes-admin    # not ghcr.io/…/hermes-admin
    tag: ""                     # overrides global.image.tag for this service only
```

Pull policy is `IfNotPresent` in the templates and is not configurable.

> Three repository identities coexist in this project and only the source location is
> settled — the repository is `github.com/darylrobbins/hermes`. The chart publishes to
> `ghcr.io/hermesnotifications` while `go.mod` declares module
> `github.com/hermes-notifications/hermes`. The registry above matches the chart, which is
> what actually publishes. See finding 31.12 in
> [the 2026-07-27 review](../reviews/2026-07-27-architecture-review.md).

## Email

The email worker supports exactly two providers, `smtp` and `ses`. The schema enumerates
both; `internal/email/email.go` rejects anything else with `unknown email provider`.

**There is no `webhook` email provider.** An earlier version of this page presented
`provider: webhook` with a `hermes.email.webhook.*` block as the way to reach SES and
SendGrid. No such provider exists in the Go code, no such values exist in the chart, and
`HERMES_EMAIL_WEBHOOK_URL` is read by nothing. SES is supported natively — use `provider:
ses`. (SMS *is* webhook-based; see [SMS](#sms).)

### SMTP

```yaml
hermes:
  email:
    provider: smtp
    from: "notifications@example.com"   # top level, not under smtp
    smtp:
      host: smtp.example.com
      port: 587
      username: hermes
      password: ""                      # rendered into the chart's Secret
```

`hermes.email.smtp.password` is only rendered when `provider` is `smtp`. To keep it out of
your values file, set `hermes.existingSecret` and supply `HERMES_EMAIL_SMTP_PASSWORD`
yourself.

### Amazon SES

```yaml
hermes:
  email:
    provider: ses
    from: "notifications@example.com"
    ses:
      region: us-east-1
```

The SES provider uses the AWS SDK's default credential chain
(`internal/email/ses.go`) — there are no access-key values in the chart. On EKS, give the
email worker's ServiceAccount an IAM role via IRSA or EKS Pod Identity; elsewhere, provide credentials
through the environment or an instance profile. The chart does not create a ServiceAccount
or annotate one, so on EKS you will need to supply that yourself today.

The sender address must be a verified SES identity, and an account still in the SES sandbox
can only send to verified recipients.

### Other providers

Anything other than SMTP or SES needs a relay you operate — for example an SMTP-speaking
gateway in front of your provider, configured with `provider: smtp`. There is no generic
HTTP escape hatch for email.

## SMS

SMS delivery is genuinely webhook-based. The worker POSTs each message to one URL; there is
no `hermes.sms.provider` and no `hermes.sms.webhook` block.

```yaml
hermes:
  sms:
    webhookUrl: "https://your-sms-gateway.example.com/send"
```

The chart offers no way to set auth headers on that request. If your gateway needs
credentials, put them in the URL or terminate the webhook at something you control.

## Per-service overrides

Services are **top-level keys**, not nested under `services:`.

| Service | Values key | Default port |
|---------|-----------|--------------|
| Admin API | `admin` | 8080 |
| Send | `send` | 8088 |
| Dispatch | `dispatch` | 8081 |
| Inbox Service | `inbox` | 8086 |
| User Service | `user` | 8087 |
| Email Worker | `workerEmail` | 8083 |
| SMS Worker | `workerSms` | 8084 |
| Inbox Worker | `workerInbox` | 8085 |
| Event Writer | `workerEvents` | 8082 |

Every one of them accepts exactly these fields, and no others:

```yaml
admin:
  replicas: 1                 # the key is "replicas" — there is no replicaCount
  image:
    repository: hermes-admin
    tag: ""
  port: 8080
  resources: {}
  autoscaling:
    enabled: false
    minReplicas: 1
    maxReplicas: 5
    targetCPUUtilizationPercentage: 80
    # targetMemoryUtilizationPercentage: 80   # optional, adds a second HPA metric
  podAnnotations: {}
  nodeSelector: {}
  tolerations: []
  affinity: {}
  topologySpreadConstraints: []
```

`replicas` is ignored when `autoscaling.enabled` is true — the template omits the field so
the HPA owns it.

## Ingress

```yaml
ingress:
  enabled: true               # default true
  className: nginx
  annotations: {}
  tls: []
    # - secretName: hermes-tls
    #   hosts:
    #     - hermes.example.com
```

The host comes from `global.domain`. **`ingress.host` does not exist** — setting it changes
nothing, and the Ingress is still bound to `global.domain`.

The chart renders two Ingress resources when enabled: the API routes
(`/v1/send`, `/v1/types`, `/v1/groups`, `/v1/notifications`, `/v1/auth`, `/v1/inbox`,
`/v1/users`) and a `/realtime` route for Centrifugo WebSockets, which carries nginx
long-timeout annotations. Both use the same `global.domain` and the same `ingress.tls`.

With external Centrifugo, the realtime Ingress is only rendered if you set
`externalCentrifugo.ingressServiceName` to a Service you have created yourself.

## Install ordering: migrations and NATS streams

Two Jobs prepare the database schema and the JetStream streams. You do not run either by
hand — they are Helm `post-install`/`post-upgrade` hooks, with explicit hook weights so the
migration runs before the provisioner.

**`post`, not `pre`, and the difference is visible on a first install.** Helm creates
ordinary resources *after* pre-install hooks but *before* post-install hooks. A pre-install
Job could not work here: it consumes the release ConfigMap and Secret, which do not exist
yet at that point, and with the bundled sub-charts enabled the database itself is an ordinary
resource that has not been created either. Running the Jobs afterwards is what makes them
able to reach anything.

The visible consequence is that the service Deployments are created *before* the schema and
the streams exist, so on a first install they will `CrashLoopBackOff` for a minute or two
until the two Jobs finish. **That is expected, not a broken install.** The services fail
closed by design — `EnsureStreams` refuses to start against a bus that is not ready — and
Kubernetes' restart backoff is the convergence mechanism. They settle on their own.

```yaml
migration:
  image:
    repository: hermes-migrate
    tag: ""
  backoffLimit: 3
  resources: {}

natsProvision:
  enabled: true
  image:
    repository: hermes-natsprovision
    tag: ""
  backoffLimit: 3
  resources: {}
```

There is **no `migration.enabled`** — the migration Job always runs.

Both Jobs are named per release revision (`<release>-migrate-<revision>`,
`<release>-natsprovision-<revision>`). Kubernetes Jobs are immutable, so a stable name would
fail on the second `helm upgrade`; the revision suffix is what makes repeat upgrades work.

The NATS provisioner declares the four JetStream streams (`NOTIFICATIONS`, `DELIVERY`,
`EVENTS`, `DLQ`). Services no longer create streams themselves: `EnsureStreams`
(`internal/messaging/provision.go`) only *verifies* that the streams a service depends on
exist and refuses to start otherwise, which is what lets the runtime identities hold read-only
JetStream grants. If you disable `natsProvision` against a bus where the streams have not
been declared some other way, every service will crash-loop with:

```
stream NOTIFICATIONS is not available to hermes-send (has cmd/natsprovision run?)
```

That is the expected failure, not a bug — provision the streams, and the pods converge.

## Cleanup CronJob

Cleanup lives under `hermes.cleanup`, not at the top level.

```yaml
hermes:
  cleanup:
    enabled: false
    schedule: "0 3 * * *"
  events:
    retentionDays: 90     # the retention window; there is no cleanup.retentionDays
```

The CronJob has no resources block of its own — it reuses `migration.resources`.

## Admin Portal

```yaml
adminPortal:
  enabled: false
  replicas: 1               # not replicaCount
  image:
    repository: hermes-admin-portal   # bare name; registry comes from global.image.registry
    tag: ""
  port: 3000
  resources: {}
  podAnnotations: {}
  nodeSelector: {}
  tolerations: []
  affinity: {}
```

The portal has no ingress route in this chart — the Ingress template does not include a path
for it. Reach it by port-forward, or add your own Ingress.

## Observability

```yaml
observability:
  enabled: false
  otel:
    endpoint: "http://otel-collector-opentelemetry-collector.observability.svc:4317"
    protocol: "grpc"       # or "http/protobuf"
  resourceAttributes: "deployment.environment=production"
  serviceMonitor:
    enabled: true
```

`observability.enabled` gates the OTLP environment on every Hermes pod.
`serviceMonitor.enabled` emits one Prometheus Operator `ServiceMonitor` per long-running
service and defaults to true, preserving the older `observability.enabled=true` behaviour.
The current alert rules and dashboards depend on Prometheus scraping and remote-write, so do
not disable it unless you have a replacement plan.

The chart configures services to export OTLP to a Collector; it does not render or operate
the Collector itself. To fan out to SigNoz, configure the Collector deployment — in this
repository's Kustomize observability stack, use the opt-in overlays
`deploy/observability/overlays/{local,staging,production}-signoz`. SigNoz support is
additive and removes nothing.

## Network policy

The key is **singular**.

```yaml
networkPolicy:
  enabled: false
```

> **Enabling this is not isolation.** Every rule in
> `charts/hermes/templates/networkpolicy.yaml` specifies ports with no `to:` or `from:`
> peers, and an empty peer list in Kubernetes means *all* peers — so this restricts which
> ports are used, not who may talk to whom. Finding 41 of the
> [2026-07-27 review](../reviews/2026-07-27-architecture-review.md) covers it.

## Bundled and external infrastructure

Each dependency is a sub-chart with an `enabled` flag and a matching `external*` block used
when it is disabled.

```yaml
postgresql:
  enabled: true
  auth:
    username: hermes
    password: hermes
    database: hermes

externalPostgresql:
  url: ""                    # postgres://user:pass@host:5432/hermes?sslmode=require
  existingSecret: ""
  existingSecretKey: "HERMES_DATABASE_URL"

redis:
  enabled: true

externalRedis:
  url: ""                    # rediss://:pass@host:6379/0
  existingSecret: ""
  existingSecretKey: "HERMES_REDIS_URL"

nats:
  enabled: true

externalNats:
  url: ""                    # tls://nats.example.com:4222
  existingSecret: ""
  existingSecretKey: "HERMES_NATS_URL"

centrifugo:
  enabled: true

externalCentrifugo:
  apiUrl: ""
  existingSecret: ""                        # there is no externalCentrifugo.apiKey
  existingSecretKey: "centrifugo-api-key"
  ingressServiceName: ""
```

Note the `existingSecretKey` **defaults are the environment variable names**, not `url`. If
your Secret uses a different key — as the examples in
[production.md](production.md) do — set `existingSecretKey` explicitly to match. Getting this
wrong produces a pod stuck in `CreateContainerConfigError`, not a silent fallback.

When a sub-chart is disabled, the schema requires either `url` or `existingSecret` on the
matching `external*` block (`apiUrl` or `existingSecret` for Centrifugo), so a half-configured
external install is caught at `helm install` time.

The Centrifugo API key is supplied differently on each path: `hermes.centrifugo.apiKey` for
the bundled sub-chart, `externalCentrifugo.existingSecret` + `existingSecretKey` for an
external one.

### Bundled infrastructure is for evaluation

This is a deliberate posture, not an oversight, but it should not be discovered the hard way:

- **The bundled PostgreSQL, Redis and NATS are unauthenticated and unencrypted.** The chart
  builds their URLs as `sslmode=disable`, `redis://` and `nats://`, the bundled Redis has
  `auth.enabled: false`, and the bundled Postgres uses the committed password `hermes`.
  Anything with network reach into the namespace can read and write all of it. This is why
  `hermes.env: production` and the bundled sub-charts are mutually exclusive.
- **The bundled Centrifugo uses the in-memory engine.** Realtime push does not fan out
  across replicas: a message published through one Centrifugo pod reaches only the clients
  connected to that pod. It is fine at one replica and silently lossy above one. A
  production realtime deployment needs Centrifugo with the Redis engine, configured
  externally and pointed at through `externalCentrifugo`.

### Known gap: secured external NATS

The chart cannot currently point at a NATS cluster that requires TLS verification or NKey
authentication. `HERMES_NATS_CA_BUNDLE` and `HERMES_NATS_NKEY_SEED`
(`internal/config/config.go`) both name **files** that must be mounted into each pod, and the
chart offers no values to mount them — no volume, no Secret reference, no env var. A
`tls://` URL against a bus whose CA is in the system trust store and which accepts anonymous
connections will work; anything stricter will not. If you operate a secured NATS cluster,
you need a chart change or a post-render patch today.

## Secrets

By default the chart creates a Secret containing `HERMES_JWT_SECRET`,
`HERMES_API_KEY_HMAC_SECRET`, `HERMES_CENTRIFUGO_API_KEY`, `HERMES_DATABASE_URL` and (for
SMTP) `HERMES_EMAIL_SMTP_PASSWORD`.

```yaml
hermes:
  # Take over the whole Secret. The chart then creates none of it and expects
  # every variable above to be present in this Secret.
  existingSecret: ""

  jwt:
    existingSecret: ""
    existingSecretKey: "HERMES_JWT_SECRET"
  apiKey:
    existingSecret: ""
    existingSecretKey: "HERMES_API_KEY_HMAC_SECRET"
```

The per-secret `existingSecret` fields are finer-grained: they inject that one variable from
your Secret via `secretKeyRef` and stop the chart from rendering it into its own Secret. See
[production.md](production.md) for the External Secrets Operator pattern.

## Values the chart does not have

Because unknown keys are accepted and ignored, these are worth stating explicitly. Each was
documented on this page at some point, and none of them ever did anything:

| Documented | Actual |
|---|---|
| `services.<name>.*` | top-level `<name>.*` |
| `replicaCount` | `replicas` |
| `networkPolicies` | `networkPolicy` |
| `ingress.host` | `global.domain` |
| `image.registry` / `image.tag` / `image.pullPolicy` | `global.image.registry` / `global.image.tag`; pull policy is not configurable |
| `imagePullSecrets` | *no equivalent* |
| `hermes.logLevel` | *no equivalent — services do not read a log level* |
| `hermes.email.smtp.from` | `hermes.email.from` |
| `hermes.email.provider: webhook` + `hermes.email.webhook.*` | `provider: smtp` or `provider: ses` |
| `hermes.sms.provider` + `hermes.sms.webhook.url` | `hermes.sms.webhookUrl` |
| `cleanup.*` | `hermes.cleanup.*` |
| `cleanup.retentionDays` | `hermes.events.retentionDays` |
| `cleanup.resources` | *none — reuses `migration.resources`* |
| `migration.enabled` | *none — the Job always runs* |
| `adminPortal.replicaCount` | `adminPortal.replicas` |
| `externalCentrifugo.apiKey` | `externalCentrifugo.existingSecret` + `existingSecretKey` |
| per-service `podLabels`, `env`, `podDisruptionBudget` | *no equivalents* |
