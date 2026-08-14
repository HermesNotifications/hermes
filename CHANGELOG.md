# Changelog

Notable changes to Hermes, newest first.

The version is one number for the whole project: the Helm chart's `version` and `appVersion`
and every image tag are the same string, cut from a `vX.Y.Z` git tag. A chart-only fix
therefore still needs a full version and republishes every image. That is a deliberate
simplification for 0.x; when it stops paying, the chart gets its own `chart-vX.Y.Z` tags.

`.github/workflows/release.yml` refuses to release unless the tag, `charts/hermes/Chart.yaml`'s
`version` and its `appVersion` are all the same, and it builds the GitHub Release notes from
the section below matching the version. A missing section fails the release.

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
