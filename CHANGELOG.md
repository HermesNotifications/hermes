# Changelog

Notable changes to Hermes, newest first.

The version is one number for the whole project: the Helm chart's `version` and `appVersion`
and every image tag are the same string, cut from a `vX.Y.Z` git tag. A chart-only fix
therefore still needs a full version and republishes every image. That is a deliberate
simplification for 0.x; when it stops paying, the chart gets its own `chart-vX.Y.Z` tags.

`.github/workflows/release.yml` refuses to release unless the tag, `charts/hermes/Chart.yaml`'s
`version` and its `appVersion` are all the same, and it builds the GitHub Release notes from
the section below matching the version. A missing section fails the release.

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
