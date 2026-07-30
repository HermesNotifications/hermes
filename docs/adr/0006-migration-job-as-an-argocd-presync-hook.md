# ADR 0006: Run the migration Job as an ArgoCD `PreSync` hook, and gate the sync on it

**Status:** Accepted
**Date:** 2026-07-30
**Author:** unit-argocd (finding 11 remediation)

---

## Context

`deploy/k8s/base/migration-job.yaml` shipped with its ArgoCD hook annotations commented out
behind a `TODO: re-enable once Crossplane has provisioned the database`:

```yaml
# argocd.argoproj.io/hook: PreSync
# argocd.argoproj.io/hook-delete-policy: BeforeHookCreation
```

With them commented out, ArgoCD applied the Job as an ordinary tracked resource. Three things
followed:

1. It ran exactly once, at the first sync, and never again. `docs/deployment-guide.md`'s claim
   that migrations run on every sync was false.
2. `deploy/kargo/stages/{staging,production}.yaml` rewrite the `hermes-migrate` image tag on
   every promotion via `kustomize-set-image`. `Job.spec.template` is immutable, so the second
   promotion could not apply.
3. Because it was applied in the `Sync` phase alongside everything else, its failure did not
   stop the rest of the sync.

Reproduced against ArgoCD v3 on k3s with the staging Application's own `syncPolicy`
(`ServerSideApply=true`, `automated` with `prune`/`selfHeal`, `retry.limit: 3`) and the real
`cmd/migrate` binary against a real Postgres. Rewriting the tag the way a promotion does
produced:

```
Job.batch "hermes-migrate" is invalid: spec.template: Invalid value: {...}: field is immutable
```

and the Application settled at `OutOfSync` / `phase: Failed`
(`retried 3 times`) — while the Secret and ConfigMap in the same sync *did* apply. So the
steady-state failure mode was: **new images roll out, migrations do not, and the Application
is permanently red.**

The competing force is the one the original TODO named. A `PreSync` hook runs before the
`Sync` phase, so it runs before anything the `Sync` phase creates — including the
ExternalSecret that materialises `hermes-secrets`, which holds `HERMES_DATABASE_URL`. On a
first sync into an empty namespace the migration therefore has no database URL at all, and
because the failed hook blocks the `Sync` phase, the ExternalSecret is never created either.
Verified: the hook pod exits with
`database-url is required (or set HERMES_DATABASE_URL)` and the Application never leaves
`PreSync`. That is a genuine bootstrap deadlock, not a hypothetical.

## Decision

**We will run the migration Job as an ArgoCD `PreSync` hook with
`hook-delete-policy: BeforeHookCreation`, and accept that a failing migration blocks the
sync.**

**We will not** solve the bootstrap-ordering problem by leaving the hook off. Instead, the
secret plumbing (`secret-store.yaml`, `external-secrets.yaml`) is applied out of band before
the Hermes Application is created for the first time, and the Crossplane claims must be
Healthy first so that the database URL exists in Secrets Manager to be read. This is
documented as an ordered bootstrap step in
[`docs/deployment-guide.md`](../deployment-guide.md#database-migrations).

`BeforeHookCreation` is chosen over `HookSucceeded` deliberately. `HookSucceeded` leaves a
*failed* Job in place; the next sync then applies over it and reproduces the exact
immutable-field error this ADR exists to remove — verified, including that a forced re-sync
with the manifest unchanged never re-runs the Job (its UID does not change) and the
Application stays `Failed` until someone deletes the Job by hand. `BeforeHookCreation` also
recovers a namespace already stuck in that state.

## Consequences

**A failing migration now blocks the release.** That is the point — no pod rolls out against a
schema the code does not expect — but it is a real behavioural change from "the migration
failure was ignored". The operator-visible sequence was measured, not inferred:

| Stage | What the Application shows |
|---|---|
| Job retrying (`backoffLimit: 3`) | `phase: Running`, `waiting for completion of hook batch/Job/hermes-migrate` |
| Between ArgoCD retries | `waiting for deletion of hook batch/Job/hermes-migrate` |
| Terminal | `phase: Failed`, `one or more synchronization tasks completed unsuccessfully (retried 3 times)`, hook result `hookPhase: Failed` / `Job has reached the specified backoff limit` |

Nothing from the new revision is applied — the `Sync` phase never runs — so the previous
release keeps serving. Kargo's `argocd-update` step fails with it and the Freight does not
advance.

**The failed Job's logs do not survive.** `BeforeHookCreation` deletes the Job, and with it
the pod, before each retry; after the retry budget is exhausted the Job is gone entirely
(`NotFound`). The Application status says only that the Job hit its backoff limit — the
actual `migration failed: …` line is only in the container's stdout. **This makes log
shipping load-bearing for migration diagnosis**, not merely nice to have: the runbook must
point at Loki (`{namespace="hermes", container="migrate"}`), and a migration failure is
undiagnosable in an environment where the Collector is broken. Finding 10 (OTLP egress
blocked) is therefore a prerequisite for this decision being operable, not an unrelated bug.

**A hung migration pins the sync indefinitely.** The Job sets `backoffLimit: 3` but no
`activeDeadlineSeconds`, and ArgoCD applies no timeout of its own to hook completion. A
migration blocked on a lock holds the Application in `waiting for completion of hook` with no
terminal state. Observed indirectly: an attempt whose database TCP connect black-holed took
~2 minutes per pod attempt and pushed a single sync past ten minutes. Deliberately **not**
fixed here — a bound low enough to catch a hang could also abort a legitimately long
migration midway, which is worse. Tracked as a follow-up.

**Migrations run on every sync, including `selfHeal` syncs.** `cmd/migrate` calls
golang-migrate's `Up()`, which is a no-op on a current schema, so this is cheap — but it does
mean the Job is created and deleted repeatedly, and the migration is not gated behind "the
schema changed".

**Rollback is unchanged and still manual.** `cmd/migrate` has no `down` direction, so a
promotion that applies a destructive migration and is then rolled back leaves the schema
ahead of the code. The `PreSync` gate does not help here; it only ensures the schema is not
*behind*.

## Alternatives considered

**Leave the annotations commented out and run migrations by hand** — the status quo. Rejected:
it is not a stable state. Every promotion after the first fails the sync outright, so the
Application is permanently red and the manual step is invisible against that noise. It also
means the schema and the deployed code are only ever coincidentally in step.

**`hook-delete-policy: HookSucceeded`** (or `BeforeHookCreation,HookSucceeded`). Attractive
because a failed Job survives for `kubectl logs`. Rejected on evidence: with `HookSucceeded`
alone, the surviving failed Job makes the next sync fail with the original
`spec.template: … field is immutable` error, so the fix does not hold. Combining it with
`BeforeHookCreation` does not restore log durability, because `BeforeHookCreation` is the half
that does the deleting. Log durability has to come from log shipping.

**Give the Job a revision-scoped name**, as `charts/hermes/templates/migration-job.yaml`
already does with `-migrate-{{ .Release.Revision }}`. This sidesteps immutability without any
hook, since each sync creates a differently-named Job. Rejected for the kustomize path: there
is no ArgoCD equivalent of a Helm release revision to interpolate, the image tag would be the
only available discriminator (so a re-sync at an unchanged tag would not re-run), and it
leaves a growing pile of Jobs behind. It also gives up the `PreSync` gate, which is the
half of this decision that has actual value.

**Order the migration behind the ExternalSecret with `argocd.argoproj.io/sync-wave` instead of
a phase**, keeping everything in the `Sync` phase. Rejected: sync waves inside a phase are
ordered but ArgoCD does not treat a wave failure as a hard stop for prior waves already
applied, and it would put the migration in the same phase as the Deployments it is supposed to
gate.

## Open question — folding the secret plumbing into `PreSync`

The bootstrap deadlock could plausibly be closed inside the Application rather than by an
out-of-band step, by annotating the SecretStore and ExternalSecret as `PreSync` hooks at an
earlier `argocd.argoproj.io/sync-wave` than the migration Job.

The **mechanism** was verified on a virgin namespace: with the Secret annotated
`argocd.argoproj.io/hook: PreSync` and `sync-wave: "-1"`, ArgoCD applied it before the
wave-0 migration Job, the migration found its `HERMES_DATABASE_URL`, and the first sync went
straight to `Synced`/`Healthy`.

What is **not** verified, and is why this is not being adopted here: that stand-in was a plain
`Secret`, which is Healthy the instant it is applied. A real `ExternalSecret` is reconciled
*asynchronously* by ESO, so the ordering only holds if ArgoCD blocks on the ExternalSecret's
health until ESO has populated the target Secret. Whether ArgoCD's health assessment for
`external-secrets.io/ExternalSecret` is strong enough for that was not tested (ESO is not
installed on the verification cluster). If it is not, annotating it would convert a
deterministic deadlock into a race, which is worse.

**Revisit trigger:** before the next fresh-environment bootstrap, or as part of the staging
soak for finding 11 — install ESO on a test cluster, annotate the ExternalSecret as a
wave `-1` `PreSync` hook, and check whether ArgoCD waits for `status.conditions[Ready]`
before starting wave 0. If it does, adopt it and this ADR gets an amendment; if it does not,
record that the out-of-band bootstrap step is permanent.
