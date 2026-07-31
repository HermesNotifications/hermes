# ADR 0009: Ship the bundled evaluation Postgres and Redis as plain manifests on Docker Official Images

**Status:** Accepted
**Date:** 2026-07-31
**Author:** unit-chart-bitnami

---

## Context

`charts/hermes/` bundles datastores so that `helm install hermes` gives an evaluator a
working system without provisioning anything first. Until now Postgres and Redis were
Bitnami sub-chart dependencies:

```yaml
- name: postgresql
  version: "~16.0"
  repository: oci://registry-1.docker.io/bitnamicharts
- name: redis
  version: "~20.0"
  repository: oci://registry-1.docker.io/bitnamicharts
```

In August 2025 Broadcom withdrew Bitnami's free image catalogue from Docker Hub. The
versioned tags those chart versions reference no longer exist, so a default `helm install`
could not start either datastore — it is not a degraded install, it is an install where two
of the four bundled components are `ImagePullBackOff` from the first second.

The first attempt at a fix was to bump to current Bitnami majors, on the assumption that
current charts reference images that are still published. **That assumption is false in the
way that matters**, and the evidence is worth recording because it is not obvious from the
outside and the next person to reach for a Bitnami chart will otherwise repeat the
investigation. Measured with `docker manifest inspect` on 2026-07-31:

| Reference | Result |
|---|---|
| `docker.io/bitnami/postgresql:17.0.0-debian-12-r9` (what `~16.0` pulled) | `no such manifest` |
| `docker.io/bitnami/postgresql:18.4.0` | `no such manifest` |
| `docker.io/bitnami/postgresql:latest` | resolves |
| `docker.io/bitnami/redis:latest` | resolves |
| `docker.io/bitnamilegacy/postgresql:17.0.0-debian-12-r9` | resolves |

The current charts (`postgresql` 18.x, `redis` 27.x) declare their images as
`registry-1.docker.io/bitnami/<name>:latest` — the *only* tag left in the free namespace.
Versioned and hardened tags now sit behind the paid Bitnami Secure Images offering, and the
withdrawn versioned tags were relocated to the archived `bitnamilegacy` namespace, which
receives no security updates.

So the reachable positions were: a floating `:latest` for the bundled datastores, an
archived namespace with no patches, or a paid subscription. All three are positions that
decay, and none is a reasonable default to hand a self-hoster.

The wider point is that the failure was structural, not incidental. Depending on a
third-party chart means depending on whatever image its maintainer chooses to point it at,
and on that maintainer's licensing decisions. Swapping to a different vendor's Postgres
chart would re-acquire exactly the same exposure under a different name.

## Decision

**We will remove the `postgresql` and `redis` sub-chart dependencies entirely and render
the bundled datastores as plain manifests in `charts/hermes/templates/`, on Docker Official
Images (`docker.io/library/postgres`, `docker.io/library/redis`).**

Each is a StatefulSet, a Service, and — for Postgres — a Secret, in
`templates/postgresql.yaml` and `templates/redis.yaml`. Image repository and tag are
values, so an operator can repoint or pin them without patching the chart.

The default tags are `postgres:16-alpine` and `redis:7-alpine`, chosen to match
`docker-compose.yml`, so the bundled datastore is the same major the integration and E2E
suites actually exercise Hermes against.

**We will not repoint at `bitnamilegacy`.** It is archived and receives no security
updates; pointing a self-hoster's default datastores at it trades a loud failure for a
silent one.

The value keys `postgresql.enabled`, `postgresql.auth.*` and `redis.enabled` are kept
under their existing names. This is deliberate: `templates/_validate.tpl` keys off
`.Values.postgresql.enabled` and `.Values.redis.enabled` to refuse `hermes.env=production`
alongside bundled infrastructure, and that guard must keep tripping. The evaluation-only
posture established by the parity work is unchanged by this ADR.

## Consequences

**The bundled datastores no longer depend on any vendor's repackaging.** Docker Official
Images are versioned, freely pullable, and maintained by the upstream projects together
with the Docker Official Images programme. This ends the exposure rather than deferring it,
which was the explicit goal.

**The chart now owns roughly 260 lines of datastore manifest it did not own before.**
Probes, `fsGroup`, `PGDATA`, and the StatefulSet/PVC wiring are now this repository's
problem. That is a real maintenance cost and the honest counterweight to the decision. It
is bounded — a single-replica evaluation datastore has no replication, failover or backup
logic — but it is not zero, and it is the reason a sub-chart was attractive in the first
place.

**Two details in those manifests are load-bearing and were found by installing, not by
review.** `fsGroup: 999` is required for the official images to own their PVC; without it
the server exits on a lock-file error on any storage class that does not happen to mount
world-writable. `PGDATA` must point at a *subdirectory* of the mount, because the official
Postgres entrypoint refuses to initialise into a non-empty directory and every PVC arrives
carrying a `lost+found`.

**The generated Service names changed, and with them the connection URLs.** The Bitnami
charts named their Services after the release (`<release>-postgresql`,
`<release>-redis-master`); the bundled manifests name them after `hermes.fullname`
(`<release>-hermes-postgresql`, `<release>-hermes-redis`), consistent with every other
Service in the chart. `hermes.databaseUrl` and `hermes.redisUrl` in `_helpers.tpl` were
updated to match. **This class of change is silent** — a URL pointing at a Service that
does not exist renders, lints and passes `helm template` perfectly, and fails only when
something tries to connect. It is the reason this change was verified by connecting from
inside the cluster rather than by reading the diff.

**The bundled datastores are now subject to the default-deny NetworkPolicy.** The Bitnami
pods carried no `app.kubernetes.io/part-of: hermes` label, so the chart's default-deny
policy never selected them and traffic reached them because no policy applied at all. The
bundled manifests use `hermes.labels` and therefore *are* selected, so
`templates/networkpolicy.yaml` gained explicit ingress rules admitting Hermes pods to 5432
and 6379. Net effect is a stronger posture than before — the datastores are now
default-denied to everything except Hermes pods — but the rules are required for basic
function, not optional hardening.

**Anyone with an existing PVC from the Bitnami chart does not carry their data forward.**
The new StatefulSets use different names, so they provision new, empty PVCs
(`data-<release>-hermes-postgresql-0`) and the old Bitnami PVCs are left untouched and
orphaned. Nothing is deleted — `helm` does not remove PVCs created from
`volumeClaimTemplates` — so this is not destructive, but it is not an upgrade either. **No
migration path is provided, deliberately.** A bundled evaluation datastore with no backups,
no replication and no TLS is not somewhere anyone should be keeping data they want, and
shipping a migration path would imply otherwise. Operators with real data are on
`externalPostgresql` / `externalRedis`, which this change does not touch.

**Storage defaults are unchanged at 8Gi for each**, matching what the Bitnami charts
requested, so replacing them does not silently alter what a fresh install provisions.

**The bundled Redis has no authentication, and the `redis.auth` value key is gone.** It was
already effectively unused (`auth.enabled: false`), and implementing it would have made
things worse rather than better: `HERMES_REDIS_URL` is a ConfigMap value, so a password set
here would be written to the ConfigMap in plaintext beside it. The bundled instance is
reachable only in-cluster and is NetworkPolicy-restricted to Hermes pods. Anyone needing an
authenticated Redis is past the evaluation posture and uses `externalRedis.url`, which
`_validate.tpl` already forces for `hermes.env=production`.

**Redis 7.4+ is under RSALv2/SSPL rather than an OSI licence.** For a datastore a
self-hoster runs for themselves this permits use; it restricts offering Redis as a managed
service. This does not bind Hermes or its users in the bundled-evaluation case, but it is
the one licensing edge in this decision and it is why the image is a value: an operator who
wants a BSD-licensed drop-in can set `redis.image.repository` to `valkey/valkey` without
patching the chart.

**`values.schema.json` closes both blocks (`additionalProperties: false`).** While they
forwarded to sub-charts they had to stay open, because unknown keys were the sub-chart's to
validate. There is no downstream schema now, so an unrecognised key is a silent no-op and
closing the blocks turns a typo into an error.

**NATS and Centrifugo are untouched.** Both were checked as part of this work and neither
is affected: they publish their own images (`docker.io/nats`, `docker.io/natsio/*`,
`docker.io/centrifugo/centrifugo`), all of which were confirmed to resolve, and neither
depends on Bitnami. They remain sub-chart dependencies.

## Alternatives considered

**Bump the Bitnami sub-charts to current majors.** The original plan, and the reason this
ADR exists. Rejected on the evidence in Context: "current majors reference images that are
still published" is true only for `:latest`. Adopting it would have given the bundled
datastores a floating tag whose major version can change under an operator between two
pulls of the same chart version, with no reproducible redeploy — a consequential change to
what a self-hoster gets, smuggled in as a version bump.

**Repoint at the `bitnamilegacy` namespace.** Rejected before evaluation began, and the
finding supports it: the namespace is archived and receives no security updates. It also
only defers the problem, since nothing commits Broadcom to keeping it.

**Adopt a different third-party Postgres/Redis chart** (CloudNativePG, the Zalando operator,
a community Redis chart). Rejected because it re-acquires the exposure this ADR exists to
remove — another maintainer, another set of image and licensing decisions, and in the
operator cases a CRD-installing dependency that is heavyweight for something whose entire
job is to let someone try Hermes out. An operator is the right answer for *production*
Postgres, and that path already exists and is preferred: `postgresql.enabled=false` with
`externalPostgresql.url`.

**Drop the bundled datastores and require external ones.** Rejected: it removes the
one-command evaluation the bundle exists to provide, and pushes every evaluator into
provisioning a database before they can see whether Hermes is worth provisioning one for.

**Digest-pin the images.** Rejected as the default. It buys reproducibility but freezes the
datastore at a digest that stops receiving patches, and the maintenance burden of moving it
is real work nobody has committed to. A floating patch level within a fixed major
(`16-alpine`) picks up security patches while keeping the major stable; operators who need
byte-reproducibility can set `postgresql.image.tag` to a digest-bearing pin themselves.
