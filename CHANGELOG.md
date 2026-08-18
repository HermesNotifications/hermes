# Changelog

Notable changes to Hermes, newest first.

The version is one number for the whole project: the Helm chart's `version` and `appVersion`
and every image tag are the same string, cut from a `vX.Y.Z` git tag. A chart-only fix
therefore still needs a full version and republishes every image. That is a deliberate
simplification for 0.x; when it stops paying, the chart gets its own `chart-vX.Y.Z` tags.

`.github/workflows/release.yml` refuses to release unless the tag, `charts/hermes/Chart.yaml`'s
`version` and its `appVersion` are all the same, and it builds the GitHub Release notes from
the section below matching the version. A missing section fails the release.

## 0.2.3

One fix, to the component whose job is to tell you when something else is broken. Upgrade if you
run the synthetic prober; nothing else changes.

### The prober now refreshes its connection token

`hermes-prober` minted its Centrifugo connection token once at startup and set it statically.
Tokens from `POST /v1/auth/token` live 4h ± 10%; the prober runs for weeks. When the token
expired the websocket stopped delivering, and the prober reported **100% probe loss until it was
restarted** — in the instance that found this, for 41 hours.

Nothing about that was visible from outside. The pod stayed `Running` and `Ready`, the socket
stayed open, and the only symptom was an unbroken drip of identical `probes lost` warnings —
which is exactly what a total pipeline outage looks like. If you alert on
`hermes.probe.results{result="lost"}`, an alert firing after roughly four hours of uptime was
more likely this bug than a real outage.

centrifuge-go had diagnosed it immediately and correctly, raising
`ConfigurationError{"GetToken must be set to handle expired token"}` on every reconnect attempt.
No `OnError` handler was registered, so that went nowhere.

The fix supplies a token-refresh callback, registers `OnError` and `OnDisconnected` handlers so
the SDK's own diagnosis reaches a log, and escalates to `ERROR` once when probe loss becomes
continuous — naming the prober's own subscription as a suspect rather than leaving every reader
to assume the pipeline is down.

**Readiness deliberately still does not gate on probe results.** The probe result stream remains
the signal, as before; a prober that fails its own health check would only hide the evidence.

## 0.2.2

One chart addition, and nothing to do on upgrade unless you want it.

### An evaluation install can now turn its logging down

`hermes.logLevel` sets `HERMES_LOG_LEVEL` for every service, and for the migration Job, the
cleanup CronJob and the stream provisioner alongside them. Empty stays the default.

It exists because a bundled install could not stop logging every request, and neither half of
that was wrong on its own. Per-request and per-notification records live at Debug on purpose —
that is what keeps steady-state volume proportional to incidents rather than to traffic. And
the level defaults to Debug when `hermes.env` is `development`, which is right for a laptop.

But `development` is not a choice on the bundled datastores: `Config.Validate()` requires TLS on
Postgres, Redis and NATS outside it, and the chart refuses that combination rather than letting
nine services crash-loop. So a bundled install was pinned to `development`, therefore pinned to
Debug, with no value to escape through.

**If you run the bundled stack under real traffic, set `hermes.logLevel: info`.** A
100,000-connection load test had every service writing a line per request, which put more
pressure on the log pipeline than on the notification pipeline. At `info` the per-request records
go away and warnings, errors and the slow-request records remain — which are the ones worth
having during a run.

A mistyped level is refused at install time. `bootstrap.logLevel()` deliberately ignores an
unrecognised value rather than exiting, so without the schema check `logLevel: verbose` would
have quietly left Debug in place.

## 0.2.1

Three fixes to 0.2.0's own telemetry, each found by reading what it actually produced in
production rather than what its tests said. Nothing to do on upgrade.

### Span names carry the route again

0.2.0 stopped naming spans from the request path — right, because that put notification
IDs in span names — but its replacement never ran on the chi-routed services (send, inbox,
admin). Every span was named for its method alone, so all endpoints collapsed under `POST`.

otelhttp names a span when it starts it, before routing, and re-invokes the name formatter
afterwards only when `Request.Pattern` is set. chi never sets it, so the second call never
came and the pre-routing name was the one that stuck. The route is now applied directly
once the router has matched, which also puts **`http.route` on the span as an attribute**
— it was never there, so traces could not be filtered by endpoint at all.

```
before:  POST                  http.route absent
after:   POST /v1/send         http.route = /v1/send
```

The ServeMux services (dispatch, the workers, the prober) were unaffected throughout.

### Centrifugo's tracing was never on

The chart has set `CENTRIFUGO_OPENTELEMETRY` since telemetry was introduced. Centrifugo v6
nests that option, so the variable it wants is **`CENTRIFUGO_OPENTELEMETRY_ENABLED`** — and
the old spelling is not an error, just a `unknown var in environment` warning at startup
and tracing quietly off. If you saw no Centrifugo spans, that log line was the tell.

Fixed, along with a render-time check that now refuses to install when
`centrifugo.env.OTEL_EXPORTER_OTLP_ENDPOINT` disagrees with `observability.otel.endpoint`
(or their protocols do). That combination also fails as silence: Centrifugo exports every
span to an address that may not resolve, reports success, and is simply absent from the
trace list — indistinguishable from tracing being switched off.

**If you set `observability.otel.endpoint`, set `centrifugo.env.OTEL_EXPORTER_OTLP_ENDPOINT`
to match**, or the install will now stop and tell you to. It cannot be inherited: a parent
chart cannot template a sub-chart's values.

Centrifugo traces its server API only — the publish calls Hermes makes to it — not
websocket client sessions.

### A redelivered message no longer joins a stale trace

JetStream hands back the original headers on a redelivery, so a retry became a child of the
publish span that produced it — one that had ended long before. With retry backoff capped
at 240s and ten delivery attempts, a trace could show a root finishing in milliseconds and
a child starting a quarter of an hour later.

A redelivery now starts its own trace with a **link** back to the publish, and carries
`messaging.attempt`. First deliveries are unchanged and still parent normally, which is
what keeps the pipeline reading as one trace.

The tradeoff, stated because it is real: a linked retry is harder to reach from the
original trace than a child would be, since not every backend follows links. To find the
retries for a message, search on its notification ID rather than expecting them nested
inside the first attempt.

## 0.2.0

An observability release. 0.1.3 turned telemetry on; this is the release that makes it
answer the question you actually have during an incident, which is whether notifications
are arriving.

Minor rather than patch because three things change what your existing queries return.
Nothing to do on upgrade, no configuration is required, and no chart values changed —
but if you run your own dashboards or alerts against Hermes, read
[What changes for existing queries](#what-changes-for-existing-queries) below.

### The pipeline could look healthy while nothing was delivered

Backlog, drain rate, pool saturation and dead letters were all instrumented. Whether
anything arrived was not. A provider rejecting every send keeps every one of those
signals green — messages are consumed promptly, the queue stays shallow, no dead letters
appear until retries are exhausted — so the pipeline reads healthy while nothing reaches
anyone. `HermesProbeLoss` covered this end to end, but only for the inbox channel the
prober subscribes to. Email and SMS had nothing.

Three of the four panels on the pipeline dashboard had, since it was written, queried
metric names no Go file has ever emitted. A panel with no series renders exactly like a
healthy service with no traffic.

New instruments: `hermes.notifications.accepted{result}`,
`hermes.notifications.dispatched{channel}`, `hermes.dispatch.failures{channel,reason}`,
`hermes.delivery.provider.duration`, `hermes.eventwriter.{flush.duration,batch.size,events,dropped}`,
`hermes.auth.result{scheme,reason}`, `hermes.cache.result{op=template}`, plus
`hermes.delivery.result` and `hermes.routing.drop`. `hermes.delivery.result` gains a
`provider` label, because "email is failing" is a different incident depending on whether
one provider is failing or all of them are.

New alerts, each with a runbook: `HermesDeliveryFailureRate`, `HermesDeliveryAbsent` (the
0/0 case a ratio alert structurally cannot see), `HermesRoutingDropRate`,
`HermesEventWriterDropping`, `HermesAuthFailureRate`.

`scripts/check_metric_references.py` now fails the build when a dashboard or alert queries
a `hermes_*` metric nothing emits.

### Log volume tracks incidents rather than traffic

Steady-state logging scaled with throughput: two notifications through the E2E pipeline
produced ten INFO records, every one per-request, per-notification or per-message. There
was no lever to turn it down, either — `bootstrap.NewLogger` passed nil handler options,
so the level was hardwired to Info and nothing logged at Debug at all.

**`HERMES_LOG_LEVEL` is new** (`debug` / `info` / `warn` / `error`). It defaults to `debug`
when `HERMES_ENV=development` and `info` otherwise, so local output is unchanged. An
unrecognised value falls back to the default rather than failing startup.

Records were demoted only where the same fact is already recorded more durably elsewhere:
"delivery succeeded" sits one line above the event that persists `<channel>.sent` with
strictly more detail; "published to delivery" duplicates `routing.dispatched`; "flushed
events" duplicates the flush span's `batch.size`.

Two levels were wrong rather than merely loud. Dispatch raised **Warn** for routine
routing outcomes — a recipient with no phone number, a category the user opted out of.
Those are the rules working, and a warning nobody can act on trains people to ignore
warnings. Delivery logged **every retry attempt at Error**, so one flaky provider call
raised up to `maxDeliveries` errors and any error-rate alert read a recovered notification
as a burst of failures; it is now Warn while retrying and Error on exhaustion.

The access log is levelled by outcome: 5xx Error, 4xx Warn, over 1s Warn, otherwise Debug.

`observability.LogThrottle` covers the inverse failure. The rate limiter and the inbox
unread-count cache raised Warn per request when Redis was sick, so an outage became a log
flood at the worst possible moment. They now emit once per 30s carrying `suppressed=N`.

### Two span trees never joined the trace they belonged to

Trace context has crossed NATS correctly since 0.1.3 — verified here against production
rather than assumed: a sampled trace runs unbroken from `POST /v1/send` through both NATS
hops to the event writer, and over an hour of traffic not one consumer span was orphaned
across ~450,000 messages. Two *other* span trees were detached, and had been all along.

**Event writes.** The event writer batches, so a flush cannot be a child of any single
notification; it recorded its inputs as span links, which is correct OpenTelemetry and
relies on the backend walking links to relate the traces. Not every backend does. The
result was 121 orphan traces an hour: every Postgres write recording a notification's
final status sat in a trace of its own, so "did this notification's status get written?"
could not be answered from the notification's trace. The flush keeps its links and stays a
root, and each event now also gets a `notification.event.persist` span on its own
originating trace.

**API key validation.** `hermes-send` emitted one orphan root trace per request — exactly
1:1 with `POST /v1/send` — because the API-key validator took no context and fell back to
a background one, putting every Redis lookup for the key cache outside the request span.

Also: outbound HTTP (webhook delivery, Centrifugo, the prober) now propagates trace
context, so a trace no longer stops at the last hop; `template.resolve`, `channel.resolve`
and `delivery.send` spans cover steps that were previously invisible; and the propagator is
installed even when no OTLP endpoint is configured, so a service that exports nothing still
forwards an inbound `traceparent` instead of severing its neighbours' traces.

**Note for webhook users:** deliveries now carry a W3C `traceparent` header to your
endpoint. It contains a random trace identifier and nothing else.

### Trace sampling can be turned down

`OTEL_TRACES_SAMPLER` and `OTEL_TRACES_SAMPLER_ARG` are now honoured. They were silently
ignored: the SDK was given a hardcoded always-on sampler in code, which overrides the
environment, so there was no way to reduce trace volume short of a code change. The default
is unchanged (`parentbased_always_on`). Dispatch has been measured at ~7,900 msg/s, so if
the added spans above cost more than you want:

```
OTEL_TRACES_SAMPLER=parentbased_traceidratio
OTEL_TRACES_SAMPLER_ARG=0.1
```

Use `parentbased_*`. A plain `traceidratio` samples each service independently and shreds
cross-service traces into fragments.

### What changes for existing queries

Only if you run your own dashboards or alerts:

- **`http_route` is now populated** on the chi-routed services. It was empty, so every HTTP
  series collapsed into one and per-route panels drew a single line. Existing series will
  split.
- **Span names no longer contain URL paths.** The old formatter used `r.URL.Path`, which
  put notification IDs into span names — unbounded cardinality, and against this repo's own
  conventions. Queries matching on those names need updating.
- **Several INFO log records are now DEBUG**, as described above. If you alerted or counted
  on `delivery succeeded`, `published to delivery` or `flushed events`, use
  `hermes.delivery.result` and the `<channel>.sent` event instead — both carry more, and
  neither is a restatement.

The dashboards and alert rules shipped in `deploy/observability/` were updated in the same
changes.

## 0.1.4

Two fixes, both found by running 0.1.3 at scale rather than by reading it.

### A full rate limiter no longer makes users refuse each other

Each service keeps one token bucket per caller, and the map is bounded at 50,000. Past the cap
every further caller shared **one** bucket — so on a deployment with more than 50,000 active
users, people started receiving 429s for traffic that was not theirs.

Measured on a 100,000-connection run: polling the inbox at 100 rps across 100,000 users puts
every user near 0.001 rps against a 20/s limit, and **6,705 requests were still refused in four
minutes**. The response carried `RateLimit-*` headers describing a per-user limit the user never
approached, and nothing logged an overflow.

The shared bucket is the right answer for the pre-authentication per-IP limiter, whose key space
a caller chooses — a `/16` scan mints 65,000 keys on demand and joint throttling is what stops
it. It is the wrong answer once a request is authenticated, where a key past the cap means the
cap is smaller than your user base. Those callers are now **admitted without a bucket** instead,
reported as `hermes_http_rate_limit_decisions_total{decision="overflow_admitted"}`.

**This is a deliberate loss of enforcement, not a silent one.** If that counter is non-zero, the
per-user limit is not being applied to part of your traffic. Raise
`rateLimit.maxEntries` (`HERMES_RATELIMIT_MAX_ENTRIES`, new in this release) past your active
user count, or enable distributed rate limiting, which keeps no local map at all. See
[ADR 0024](docs/adr/0024-a-full-rate-limiter-fails-open-for-credentials.md) for the alternatives
that were rejected, including simply refusing.

Callers already holding a bucket are limited exactly as before. Nothing changes below the cap.

### The synthetic prober can start

`prober.enabled: true` could not work in 0.1.3. The chart shipped
`prober.organizationID: "hermes-synthetic"`, and organizations are keyed by
`organizations.id UUID PRIMARY KEY`, so the row could never be inserted: `/v1/auth/token`
answered 500 and the pod crash-looped on `mint token: auth/token returned 500`, which reads as a
broken admin service rather than a bad value. The default is now a UUID, and a non-UUID is
refused at render time by both the values schema and a chart guard.

If you set your own `prober.organizationID`, it must be a UUID.

## 0.1.3

### Telemetry works, and ships on

`observability.enabled` now defaults to **true**. It defaulted to false, and not as a sizing
decision: `resource.WithProcess()` includes a process-owner detector that calls
`os/user.Current()`, which cannot work in a CGO-disabled distroless image, so every service
crash-looped on

```
observability init failed: build resource: error detecting resource:
user: Current requires cgo or $USER set in environment
```

`enabled: false` was the workaround, and it held long enough to become the default. What that
cost is on the record: the entire 250,000-connection scaling exercise in
[docs/loadtest/websocket-scale-2026-08.md](docs/loadtest/websocket-scale-2026-08.md) ran with
Hermes telemetry off, so every figure in it was read by hand from kubeletstats and `curl`, with
no Hermes spans or metrics for the window at all.

`buildResource` now spells out `WithProcess()` minus `WithProcessOwner()`, and a detector error
degrades rather than kills the process. **If you set `observability.enabled: false` to work
around this, remove it.** Turning it on where no collector exists is a no-op rather than an
error — `Init` returns early when `OTEL_EXPORTER_OTLP_ENDPOINT` is unset — so the new default is
safe on an evaluation install.

Two scrape targets that produced an endpoint and no time series are fixed: the Redis exporter
sidecar and the Centrifugo metrics port both now ship with the ServiceMonitor they needed.
Enabling the Redis sidecar previously gave you an endpoint nothing scraped.

New, and optional: a **synthetic prober** (`prober.enabled`, default `false`) that sends through
the real pipeline and waits for the socket, measuring end-to-end delivery continuously rather
than the health of individual hops. It ships as a new `hermes-prober` image.

### Dispatch throughput is settable, and it is the largest lever in the chart

`dispatch.concurrency` and `dispatch.database.maxConns` did not appear anywhere in the chart
before, so **every documented install ran at 8 workers whatever the hardware**. Measured by
`cmd/dispatchbench` inside a cluster, against Longhorn-backed Postgres:

| workers | msgs/s |
|---:|---:|
| 8 | 2,100 |
| 16 | 3,511 |
| 32 | 5,534 |
| 64 | **7,907** |

3.8x from configuration alone. Dispatch is I/O-bound: Postgres amortises concurrent commits into
shared WAL flushes, and the amortisation scales with how many are in flight.

The two are one knob with two halves, and getting it wrong used to be silent — the worker pool is
clamped to the connection pool at startup, so raising concurrency alone changed throughput by
exactly nothing and said so in a single log line. Leave `maxConns` empty and it is derived;
setting it below `concurrency` is now refused at render time, and the chart will not install a
fleet that cannot fit in `postgresql.maxConnections`.

The default stays at 8. Raise it deliberately.

### Liveness follows consumer progress

`/healthz` now fails when a NATS consumer holds work and settles none of it for
`HERMES_NATS_CONSUMER_STALL_TIMEOUT` (default 10m), and `HermesConsumerStalled` fires at half
that window so an operator reaches a wedged pod before the kubelet destroys the evidence. See
[ADR 0022](docs/adr/0022-liveness-follows-consumer-progress.md).

Backlog alerting changed shape with it. A static depth threshold carries no information about
severity — at 6,000/s offered, pending reached 764,173, and a healthy burst crosses any line you
pick. `NATSConsumerLag` is replaced by rules on **time-to-drain** and **sustained growth**, which
tell a queue absorbing a burst apart from a pipeline losing a race.

### Correctness

- Dispatch no longer re-upserts the same organization and user on every send.
- Two races in consumer shutdown: `Drain` could read the in-flight count before its fetchers had
  settled, and `Add`/`Wait` could interleave.
- The bundled Postgres gets a `/dev/shm` it can plan against.

### Upgrading

Nothing here requires a migration. Two things to know:

- **Telemetry is on unless you turn it off.** If your cluster has a collector at
  `observability.otel.endpoint`, services begin exporting on upgrade.
- **`hermes-prober` is a new package.** New GHCR packages are private on first push, so if you
  set `prober.enabled: true` before that package is made public, you get `ImagePullBackOff`.

## 0.1.2

### Realtime delivery works

0.1.1 claimed to fix this and did not. There were four faults, not three; it fixed the first
three. The fourth was that the chart never set `allow_user_limited_channels`.

Hermes puts each user on `user#<internal id>`, and Centrifugo honours that convention only when
that option is enabled. Without it every subscription was refused `103: permission denied` —
while the connection itself succeeded, so a widget connected, authenticated, and then received
nothing at all.

**If you are on 0.1.0 or 0.1.1, realtime has never worked.** Nothing was lost: notifications
were always stored and appear on reload. Only the live push was missing.

Verified end to end on a real cluster this time, not by a websocket handshake. `tests/realtime`
is the check that does it — it subscribes, sends through the full pipeline, and waits for the
publication. A 101 handshake proves the ingress route and nothing more, which is exactly the
evidence that let three of these faults be reported as fixed while delivery was still broken.

### Upgrading from 0.1.0 with the bundled NATS

`hermes.nats.streamReplicasAllowChange` makes the R1-to-R3 stream migration something you opt
into, rather than something an upgrade does to you. Without it, upgrading an existing bundled
install failed, rolled back and retried — churning the datastores each cycle.

See [Upgrading](docs/self-hosting/upgrading.md#011) for the two routes: pin your current replica
count and migrate nothing, or set the flag for a single upgrade and move to replicated streams
deliberately.

## 0.1.1

### Realtime delivery now works

**In 0.1.0 it did not, at all.** Notifications were accepted, dispatched and stored, and
appeared in the inbox on the next page load — but nothing ever arrived live. Three faults,
each sufficient on its own:

- Publishes went to Centrifugo's port 8000, which serves websocket and the fallback transports.
  Its HTTP API is on 9000. Every publish returned 404.
- Centrifugo's `http_api.key` was never configured, though the chart set the matching key on
  the clients. Even at the right port, every publish would have been rejected 401.
- `client.token.hmac_secret_key` was never configured, so Centrifugo could not verify the token
  the widget presents and would refuse every browser — while the websocket handshake itself
  still succeeded, which is why the route looked healthy.

Anyone on 0.1.0 relying on realtime should upgrade. Nothing was lost: the notifications are in
the database and appear on reload.

### Production is now possible with the bundled datastores

`tls.enabled=true` issues cert-manager certificates for the bundled PostgreSQL, Redis and NATS,
and generates URLs `hermes.env: production` accepts. Previously the chart refused that
combination, which meant a defensible install required operating three external datastores.
See [Bundled datastores over TLS](docs/self-hosting/production.md#bundled-datastores-over-tls)
and the 2026-08-12 amendment to
[ADR 0005](docs/adr/0005-transport-security-for-infrastructure-connections.md).

**This is encryption, not authentication.** The bundled Redis takes no password and the bundled
NATS has no NKey accounts, so any pod in the namespace reaching those ports still has full
access. It raises the bar from "anything on the network" to "anything in this namespace".

### Also fixed

- **JetStream streams are now replicated to the bundled cluster size.** They were R1 on a
  three-node bus, so losing one node stopped the pipeline on a cluster sized to survive that.
- **Traefik is supported.** The realtime route used an nginx regex path, which Traefik v3
  matches literally — so `/realtime` 404'd on every k3s cluster while the API worked.
  `ingress.controller` now selects the dialect.
- `imagePullSecrets` and a configurable `pullPolicy`, for private registries and air-gapped
  mirrors.
- `app.kubernetes.io/version` on every resource.
- `helm test` no longer pulls `curlimages/curl:latest`.

### Known limitations, unchanged from 0.1.0

Still no admin UI. The bundled Centrifugo still cannot be used in production — it runs the
in-memory engine, so a publication reaches only the users connected to one pod. The chart still
cannot present an NKey identity to a secured external NATS bus.

**New in this release:** the chart installs cleanly only as a release named `hermes`. The
bundled Centrifugo's secret references cannot be templated by the parent chart, so a different
release name is refused at render time rather than silently losing realtime. Tracked as
[#131](https://github.com/HermesNotifications/hermes/issues/131).

## 0.1.0

First public release. Nothing was published before this — no image, no chart, no tag — so
there is no upgrade path and nothing to migrate from. Install fresh.

### What you get

- Nine services (admin, send, dispatch, inbox, user, and the email/SMS/inbox/events workers)
  as a single Helm chart, with bundled PostgreSQL, Redis, NATS and Centrifugo for evaluation.
- Multi-architecture images (`linux/amd64`, `linux/arm64`), cross-compiled rather than
  emulated, on `scratch`.
- A first API key created for you at install time and written to a Kubernetes Secret, so a
  fresh install is usable without reaching into the database.

### Known limitations, stated plainly

- **No admin UI.** `adminPortal.enabled=true` is refused at render time: no image exists and
  this repository contains no Dockerfile that could build one.
- **Bundled datastores are for evaluation only.** They are unencrypted and unauthenticated,
  the PostgreSQL password is the committed string `hermes`, and the bundled Centrifugo uses
  the in-memory engine, so realtime push does not fan out past one replica. Production
  requires external datastores over TLS and `hermes.env: production`, which the bundled
  sub-charts cannot satisfy. See [Production Hardening](docs/self-hosting/production.md).
- **The chart cannot present an NKey identity to a secured NATS bus.**
  `HERMES_NATS_CA_BUNDLE` and `HERMES_NATS_NKEY_SEED` name files, and the chart mounts
  neither. An ADR 0005-style bus remains a `deploy/k8s/` (Kustomize) deployment only.
- **Expect about a minute of `CrashLoopBackOff` on a first install.** The migration and
  stream-provisioning Jobs are ordinary resources rather than Helm hooks
  ([ADR 0008](docs/adr/0008-helm-chart-provisioning-jobs-are-not-hooks.md)); the services
  start before the schema and streams exist, exit, and settle once the Jobs finish. Pass
  `--wait --wait-for-jobs` if you would rather the install block.
