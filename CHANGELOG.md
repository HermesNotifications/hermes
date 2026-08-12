# Changelog

Notable changes to Hermes, newest first.

The version is one number for the whole project: the Helm chart's `version` and `appVersion`
and every image tag are the same string, cut from a `vX.Y.Z` git tag. A chart-only fix
therefore still needs a full version and republishes every image. That is a deliberate
simplification for 0.x; when it stops paying, the chart gets its own `chart-vX.Y.Z` tags.

`.github/workflows/release.yml` refuses to release unless the tag, `charts/hermes/Chart.yaml`'s
`version` and its `appVersion` are all the same, and it builds the GitHub Release notes from
the section below matching the version. A missing section fails the release.

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
