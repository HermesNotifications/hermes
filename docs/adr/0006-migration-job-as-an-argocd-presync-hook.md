# ADR 0006: Run the migration Job as an ArgoCD `PreSync` hook, and gate the sync on it

**Status:** Accepted (amended 2026-07-31: extends the decision to `hermes-natsprovision`, which
gets `hook: Sync` with sync-wave ordering rather than `PreSync` — see
[Amendment](#amendment-2026-07-31--the-second-job-hermes-natsprovision))
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

---

## Amendment 2026-07-31 — the second Job, `hermes-natsprovision`

### Why this is an amendment and not a new ADR

PR #71 and PR #72 were developed in parallel and neither saw the other. #71 added
`deploy/k8s/base/nats-provision-job.yaml` (ADR 0005 phase 4) with its hook annotations
commented out and this justification:

> Left commented to match hermes-migrate rather than enabling it for one Job and not the other.

#72 — this ADR — then made `hermes-migrate` a `PreSync` hook. The justification was stale on
arrival: the two Jobs no longer matched, and `hermes-natsprovision` was left as an ordinary
tracked resource carrying **exactly the defect this ADR exists to remove**, in a second place.

This does not reverse anything decided above. `hermes-migrate` stays `PreSync`. It extends the
same decision to a second Job of the same class, with a different mechanism, and it corrects
one sentence in *Alternatives considered*. Per `docs/adr/README.md` that is a clarification,
so it amends in place.

### The defect, reproduced

Same cluster and same method as the original: ArgoCD **v3.4.5** on k3s, an Application with
the staging `syncPolicy` (`ServerSideApply=true`, `automated` with `prune`/`selfHeal`,
`retry.limit: 3`), the real `cmd/natsprovision` binary against a real NATS with JetStream, and
a promotion simulated by making the same image-tag rewrite `kustomize-set-image` makes.

First sync: clean. Second sync, tag rewritten:

```
Job.batch "hermes-natsprovision" is invalid: spec.template: Invalid value: {...}:
field is immutable
```

Terminal state `sync: OutOfSync`, `operationState.phase: Failed`, `retryCount: 3`. Every other
resource in the same sync applied successfully — the two Deployments and the StatefulSet all
reported `serverside-applied`.

**One thing is worse here than in the migrate case.** Throughout the failure the Application
reported **`health: Healthy`**. A migration failure at least eventually shows as an unhealthy
workload; a stream-provisioning failure on a cluster whose streams already exist shows as
nothing at all. The steady state was: new images roll out, streams are never re-declared, and
the only signal is a red sync status on an application that reports itself healthy.

### Why not `PreSync`, and why not `PostSync`

`PreSync` works for `hermes-migrate` because Aurora is external, provisioned by Crossplane, and
present before any sync. `hermes-natsprovision`'s dependency is the NATS StatefulSet **that this
same Application creates during the `Sync` phase**. A `PreSync` hook has nothing to connect to
on a virgin bootstrap.

`PostSync` deadlocks, and this was verified rather than assumed. ArgoCD runs `PostSync` hooks
only once the `Sync` phase is healthy; the six stream-consuming services call
`bootstrap.MustEnsureStreams` and `os.Exit(1)` at boot, before serving any readiness probe. On a
virgin namespace with the Job annotated `PostSync`:

| t | Observed |
|---|---|
| 0s | NATS and both stand-in services created together. **No provisioning Job.** |
| 1s | Both services `Error` — no streams |
| 3m18s | Terminal: `health: Degraded`, `phase: Failed`, services at 5 restarts, `Deployment "…" exceeded its progress deadline` |

The provisioning Job **was never created at all** — it does not appear in the Application's
resource list. The services cannot become healthy without the streams, and the hook that would
create the streams cannot run until they are healthy.

`hook: Sync` with the Job **alone** at `sync-wave: "1"` deadlocks identically, for the same
reason: wave 0 then contains the services, which never go healthy, so wave 1 never starts. Also
verified on a virgin namespace — no Job pod in 3 minutes, both services at 5 restarts. Ordering
the provisioner *after* something that depends on it is not ordering.

### Decision

**`hermes-natsprovision` runs as an `argocd.argoproj.io/hook: Sync` at `sync-wave: "0"` with
`hook-delete-policy: BeforeHookCreation`, and the six services that fail closed on streams —
`send`, `dispatch`, `worker-email`, `worker-sms`, `worker-inbox`, `worker-events` — carry
`sync-wave: "1"`.**

`admin`, `inbox` and `user` do not call `MustEnsureStreams` and stay at the default wave.

The NATS StatefulSet is deliberately **not** moved to an earlier wave. It cannot be: its config
Secret comes from a kustomize `secretGenerator` and its TLS certificate from a cert-manager
`Certificate`, both at wave 0, so a StatefulSet at wave `-1` would wait forever for a config
that arrives a wave later. The Job therefore *races* NATS inside wave 0 rather than being
ordered behind it, and the Job's `backoffLimit` is what absorbs the race — see below.

### Verified

Two consecutive image-tag rewrites (`v2`→`v3`, `v3`→`v4`), each its own commit and sync: both
`Succeeded`, no immutable-field error. The Job is deleted and recreated each time — visible as
the hook going `Healthy` → `Progressing` → `Succeeded` with a new pod UID — which is exactly
what `BeforeHookCreation` buys and what the current manifest cannot do.

Virgin namespace, nothing in it, syncing from scratch:

| t | Observed |
|---|---|
| 0s | NATS pod and provisioning Job pod created. **No service pods.** |
| 2s | Provisioner attempt 1 exits: `nats connect: dial tcp: lookup nats on 10.43.0.10:53: no such host` |
| 10s | Attempt 2 created |
| 11s | Provisioner `Completed` |
| 13s | Service pods created |
| 14s | Both `Running` — **0 restarts** |

The same bootstrap without the wave annotations produced **2 restarts per service**, and with
`PostSync` or a wave-1 Job, 5 and climbing. This is the answer to the question the design rests
on: **ArgoCD does wait for wave 0 to be healthy — including a `Sync`-phase hook Job reaching
completion — before applying wave 1.**

### Consequences

**Crash-looping pods stop being normal.** That is the point. A deploy that always shows six
crash-looping pods trains operators to ignore crash-looping pods, and this repository has a
long run of defects that survived because a broken thing looked like the usual thing.

**When the provisioner fails, the operator sees an unambiguous signal.** Verified by pointing
the Job at an unreachable bus on a virgin namespace:

| What | Value |
|---|---|
| Application | `sync: OutOfSync`, `phase: Running`, `health: Healthy` |
| Message | `waiting for completion of hook batch/Job/hermes-natsprovision` |
| Service Deployments | `OutOfSync` — **never created, zero pods** |
| Provisioner pods | one per backoff attempt, all `Error`, logs readable |

"No service pods at all, plus *waiting for completion of hook*" is distinguishable from a real
application fault at a glance, which "six pods in CrashLoopBackOff" is not.

**`backoffLimit: 6` is now load-bearing, not a round number.** `cmd/natsprovision` has no
connect retry — `bootstrap.MustConnectNATS` exits on first failure — so the Job's backoff is
the only thing that lets it start before NATS does. Measured pod-creation deltas were 11s, 20s,
40s, confirming Kubernetes' 10s doubling, so 6 gives 7 attempts across ≈10.5 minutes. Lowering
it shortens the bootstrap window by more than it looks.

**`optional: true` on the `nats-server-tls` and `nats-nkeys` volumes is kept, deliberately.**
The intuition that a pod which starts without its credential is harder to diagnose than one
that will not start turns out to be false here, and it was measured rather than argued. Both
paths already fail closed with the missing file named on one line:

```
nats nkey seed /etc/nats-nkey/seed.nk: nats: open /etc/nats-nkey/seed.nk: no such file or directory
nats connect: nats: error loading or parsing rootCA file: open /etc/nats-certs/ca.crt: no such file or directory
```

Dropping `optional` would instead pin the pod in `ContainerCreating` indefinitely. That
consumes none of the `backoffLimit`, so the hook would never complete *or* fail, and the sync
would hang with no terminal state — the cost this ADR already names as its worst. Fast and
terminal with a precise message beats hung with a precise message.

**The log-durability cost carries over, with one refinement.** `BeforeHookCreation` destroys
the failed Job and its pods, so the error text must come from Loki
(`{namespace="hermes", container="natsprovision"}`). The refinement measured here: within a
single Job's own backoff the failed pods **do** persist and `kubectl logs` works. Only an
ArgoCD-level retry, which recreates the hook, destroys them. The window is real but not
something to rely on.

**A hook's content does not make the Application `OutOfSync` — latent, and NOT fixed here.**
ArgoCD excludes hook resources from the desired-state diff, so a commit that changes *only*
this Job's image tag leaves the Application reporting `Synced` at the new revision, and no sync
is triggered at all. Verified: the Application reported `Synced` at a revision whose only change
was this Job's tag, with `operationState` still showing the *previous* sync's timestamps.

This is **currently masked, not handled**. A Kargo promotion rewrites all eleven image tags in
one commit and then runs an explicit `argocd-update` step, so a sync does happen and the hook
does run. Remove either of those properties — promote a single image, or drive ArgoCD by polling
alone — and the Job silently stops re-running while the Application reports itself in sync.

**This applies equally to `hermes-migrate` and was not noticed when that hook landed**, two
commits before this amendment. It is a latent defect in the fix this ADR already accepted, left
unfixed deliberately: closing it means either giving hook Jobs a revision-scoped name (rejected
above, for reasons that still hold) or making the promotion path's `argocd-update` step
load-bearing by contract rather than by accident. Whoever picks it up should know it is one
decision covering both Jobs, not two.

**The transition sync does not run the hook — expected, and NOT a bug to chase.** On the one
sync where these annotations are first added, ArgoCD sees the previously-tracked Job as absent
from the desired state and **prunes** it (`status: Pruned`); the hook runs on the *next* sync,
not that one. Observed directly. Harmless where the streams already exist, which is true of any
cluster this change can land on — but an operator watching the first sync after this merges will
see the provisioning Job disappear and no replacement run, and should not go looking for a
fault. "Deploy the annotation change" and "re-provision the streams" are two syncs.

**Wave 1 widens the blast radius of a wave-0 failure.** Anything at wave 0 that cannot become
healthy — NATS, Centrifugo, the migration's downstream effects — now also blocks the six
services from being applied at all, where previously they would have been applied and left to
crash-loop. This is the deliberate trade: a deterministic stop instead of an ambiguous one.

### Relationship to ADR 0008 — why the two deployment paths differ

[ADR 0008](0008-helm-chart-provisioning-jobs-are-not-hooks.md) reached the *opposite-looking*
answer for `charts/hermes/` on the same day: both provisioning Jobs there are **plain tracked
resources with revision-scoped names, not hooks in any phase**, and the services crash-loop and
converge behind them. A reader comparing the two could reasonably ask which one is wrong.
Neither is, and the difference is not stylistic.

The shared root cause is identical, and both ADRs refuse the same shortcut: services fail closed
at boot (ADR 0005 phase 4) and neither Helm nor ArgoCD can order a provisioner between two
things it deploys in one shot without splitting the deployment. **Neither path weakens
`MustEnsureStreams` to make the ordering easier** — that is not on the table in either record.

What differs is what each tool gives you to split with:

| | Helm (ADR 0008) | ArgoCD (here) |
|---|---|---|
| Ordering primitive | hook phases only — `pre-install` runs before the release's own resources exist, `post-install` (with `--wait`) blocks on Deployments that cannot go Ready | `sync-wave`, which orders *within* the Sync phase and gates on health |
| Both hook phases | deadlock | `PreSync` and `PostSync` both deadlock |
| Escape | none — so: plain resources, revision-scoped names, converge by crash-loop | waves — so: order the consumers a wave behind the provisioner |
| Immutability handled by | a new Job name per release revision | `hook-delete-policy: BeforeHookCreation` |

ArgoCD has a third option; Helm does not. Where ADR 0008 accepts crash-loop convergence because
nothing better exists, this ADR spends eleven annotations to remove it, because something better
does. The `backoffLimit: 6` reasoning is the same in both records and was arrived at
independently: Kubernetes doubles from 10s, so 3 buys only ~70 seconds.

### Correction to *Alternatives considered*

The rejection of "order the migration behind the ExternalSecret with
`argocd.argoproj.io/sync-wave` instead of a phase" says:

> sync waves inside a phase are ordered but ArgoCD does not treat a wave failure as a hard stop
> for prior waves already applied

That is true as written and remains the right call for the migration, but it is easy to read as
"waves do not gate anything", which is wrong and would have led away from the fix here.
Measured: a failing wave-0 hook **does** stop wave 1 from being applied at all — the wave-1
Deployments were never created. What survives a wave failure is what wave 0 *already applied*,
not the waves after it.
