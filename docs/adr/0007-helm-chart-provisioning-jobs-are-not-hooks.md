# ADR 0007: Run the Helm chart's provisioning Jobs as plain resources, not Helm hooks

**Status:** Accepted
**Date:** 2026-07-31
**Author:** unit-chart-parity

---

## Context

`charts/hermes/` runs two provisioning Jobs: `hermes-migrate` (database schema) and
`hermes-natsprovision` (the four JetStream streams, added in this change per ADR 0005
phase 4). Both must have run before the services can serve traffic. Since PR #71 the
dependency is hard rather than soft: `bootstrap.MustEnsureStreams` calls `os.Exit(1)` when
a stream is missing, and six of the nine services call it at boot, *before* they ever
serve a readiness probe. `cmd/migrate`'s absence is equally fatal in practice — without
the schema, admin exits on `relation "jwt_signing_keys" does not exist`.

The obvious mechanism is a Helm lifecycle hook, and `migration-job.yaml` was already
written as a `pre-install,pre-upgrade` hook. Neither hook phase works, and the reasons are
structural rather than incidental. All of the following was measured on k3s v1.34 with
Helm v3.16.4, against the chart's default (bundled sub-chart) posture.

**A `pre-install` hook cannot see the release.** Helm creates a release's regular resources
only *after* its pre-install hooks have completed. Two consequences, both observed:

1. The Jobs read configuration from the release ConfigMap and Secret via `envFrom`. Those
   do not exist yet, so the pod sits in `CreateContainerConfigError` and the install dies
   with `failed pre-install: timed out waiting for the condition`. Confirmed in isolation
   with a pod whose `envFrom` named a nonexistent ConfigMap.
2. Fixing (1) with hook-scoped copies of the config exposes the deeper problem: with
   `postgresql.enabled` and `nats.enabled` — the chart defaults, and the evaluation posture
   this chart is for — the datastores being provisioned are themselves regular resources.
   The migration Job then failed with
   `migration failed: create migrator: failed to open database: dial tcp: lookup
   hv-postgresql on 10.43.0.10:53: no such host`, `BackoffLimitExceeded`.

(2) is not fixable inside the phase. This is the same bootstrap deadlock
[ADR 0006](0006-migration-job-as-an-argocd-presync-hook.md) documented for ArgoCD's
`PreSync`, where the ExternalSecret carrying `HERMES_DATABASE_URL` had not been created
when the hook ran. There it was closed by an out-of-band bootstrap step; a Helm chart
installed by a self-hoster has no equivalent out-of-band step to lean on.

**A `post-install` hook deadlocks under `--wait` and `--atomic`.** Helm waits for every
regular resource to become Ready *before* running post-install hooks. The stream consumers
cannot become Ready until the provisioner has run. Measured on this chart with
`helm install --wait --timeout 4m`:

| | Observed |
|---|---|
| `helm` | `Error: INSTALLATION FAILED: context deadline exceeded` after 243s |
| Jobs | **none created** — neither Job ever ran |
| Streams | none |
| Pods | all nine Hermes services in `CrashLoopBackOff` |

`--atomic` did the same and then rolled the release back and uninstalled it. The mechanism
was confirmed independently on a four-resource throwaway chart with no Hermes code in it: a
Deployment that exits non-zero until a marker exists, plus a Job that creates the marker.
As a post-install hook the Job was never created; as a plain resource it completed in 4s
while Helm was still waiting on the Deployment.

This matters more than the flag names suggest. `--wait`/`--atomic` are the normal shape of
an operator's install script, and **Flux's `HelmRelease` and ArgoCD's Helm integration both
effectively wait** — so the deployment paths most likely to be used in anger are exactly the
ones that deadlock.

## Decision

**We will run both provisioning Jobs as ordinary tracked resources with revision-scoped
names, and not as Helm hooks in any phase.**

`{{ include "hermes.fullname" . }}-migrate-{{ .Release.Revision }}` and
`…-natsprovision-{{ .Release.Revision }}`. The revision suffix is what makes re-application
safe: `Job.spec.template` is immutable, so a stable name fails on the second install. Helm
prunes the previous revision's Job on upgrade because it is absent from the new manifest.

The Jobs are applied in the same pass as everything else and retry until their datastore is
reachable, while the services crash-loop and converge behind them. `backoffLimit` is 6 on
both (raised from 3 for migrate): Kubernetes backs off 10s, 20s, 40s… between attempts, so
3 gave roughly 70 seconds — less than a bundled Postgres takes to accept connections.

`scripts/check_helm_render.py` enforces the *annotation*, which is what a render-time check
can actually see: the provisioner Job must carry no `helm.sh/hook`. The deadlock itself is a
runtime property and the gate does not pretend to observe it.

**We will not** weaken the fail-closed startup to make this easier. ADR 0005 phase 4
deliberately made services exit rather than run against a bus that is not ready, and the
crash-loop is the convergence mechanism that decision chose.

## Consequences

**`--wait`, `--wait-for-jobs` and `--atomic` all work.** Measured on a fresh install with
empty PVCs:

| Invocation | Result |
|---|---|
| `--wait --timeout 6m` | exit 0 in **69s**; 9/9 services Running, both Jobs Complete, 4 streams |
| `--atomic --timeout 6m` | exit 0 in **69s**; same end state, no rollback |
| `--wait --wait-for-jobs --timeout 6m` | exit 0 in **68s**; same |
| `helm upgrade --wait` | revision 2 deployed; revision-1 Jobs pruned |

**`helm install` with no flags reports success before provisioning has necessarily
succeeded.** This is the real cost, and it is a regression against what a working hook would
have given. Without `--wait`, Helm returns as soon as the resources are applied; if the
migration then fails, the release still says `deployed` and the operator sees crash-looping
pods rather than a failed command. Mitigation is documentation, not machinery: `NOTES.txt`
and `values.yaml` tell operators to use `--wait` (or `--wait --wait-for-jobs`), and those now
work. **`--wait` catches a missing provisioner**: installing with
`natsProvision.enabled=false` failed with `context deadline exceeded` and exit 1, with
exactly the six stream consumers in `CrashLoopBackOff`.

**`--wait` alone does not guarantee the Jobs finished.** Helm's `--wait` does not wait for
Jobs unless `--wait-for-jobs` is also passed. On a *first* install this does not matter — the
services cannot become Ready until the Jobs have done their work, so waiting on them is
transitive. On an *upgrade* it does: the services are already Ready, so `helm upgrade --wait`
returned while the new revision's migration Job was still `ContainerCreating`. An operator
who needs the schema migrated before Helm returns must pass `--wait-for-jobs`.

**Services crash-loop for roughly a minute on a first install.** Three restarts each in the
measured runs. It is noisy, it is expected, and `NOTES.txt` says so before it happens —
otherwise it reads as a broken install.

**Failed Job pods survive for diagnosis.** An improvement over `hook-delete-policy:
hook-succeeded`, which deletes the Job and its logs. ADR 0006 called that loss out as a
genuine cost of the ArgoCD path and made log shipping load-bearing for migration diagnosis;
the Helm path does not inherit it. The retry attempts are visible as `Error` pods alongside
the `Completed` one.

**Completed Jobs accumulate within a revision.** They are pruned on the next upgrade, not
on completion. No `ttlSecondsAfterFinished` is set, deliberately — a TTL short enough to
tidy up is also short enough to delete the logs of the failure someone is trying to read.

**The two Jobs are no longer ordered relative to each other.** Hook weights gave an explicit
order; plain resources have none. They are genuinely independent — one touches Postgres, one
touches NATS, neither reads the other's output — so this costs nothing today. It would matter
if a future Job depended on the schema.

## Alternatives considered

**`pre-install`/`pre-upgrade` hook** — the status quo for `hermes-migrate`. Rejected on
evidence: it has never worked, in two independent ways (see Context). Worth stating plainly,
because the file has looked correct since it was written and nothing in CI could tell:
`helm template` renders it, `helm lint` passes it, and no test installed the chart.

**`post-install`/`post-upgrade` hook** — what this change first implemented, and what was
verified working *without* `--wait`. Rejected once `--wait` was tested: it converts a
working-but-noisy install into a hard deadlock on precisely the paths Flux and ArgoCD use.
The failure is also maximally confusing, because the thing Helm is waiting for and the thing
that would unblock it are in the same release.

**Keep the hooks and declare `--wait`/`--atomic` unsupported.** Honest, and cheap to
document. Rejected because a fix exists that costs only the no-flag loud-failure property,
and because "unsupported" would in practice mean "unsupported with Flux and ArgoCD", which is
most of the audience for a published chart.

**Give the services an initContainer that blocks until the streams exist.** Does not help:
`--wait` waits for the Deployment to be Available, which requires the initContainer to
finish, which requires the Job that `--wait` is preventing from running. The deadlock is
unchanged, only its symptom moves from `CrashLoopBackOff` to `Init:0/1`.

**Make `MustEnsureStreams` retry instead of exiting**, so services become Ready and heal
later. Rejected: it reverses ADR 0005 phase 4's fail-closed property, which is not in scope
here and was chosen deliberately. It would also make a genuinely misconfigured bus look like
a slow one.

**Hook-scoped copies of the ConfigMap and Secret** (implemented and then removed). This
fixes the first pre-install failure only, and it was worth building to establish that the
second failure was real and separate rather than a consequence of the first. It has its own
cost — the copies are hook resources, so Helm neither diffs them on upgrade nor removes them
on uninstall.

## Revisit trigger

If Helm ever gains a way to order a hook phase *between* resource creation and the readiness
wait — the phase this chart actually wants — revisit. That single primitive would restore the
loud-failure property for flagless installs without reintroducing either deadlock.
