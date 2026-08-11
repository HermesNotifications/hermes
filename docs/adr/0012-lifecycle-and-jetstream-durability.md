# ADR 0012: Drain before shutdown, and replicate the streams the cluster can lose

**Status:** Accepted
**Date:** 2026-08-10
**Author:** Daryl Robbins

> **Numbering:** this branch is cut from PR #73, whose ADR sequence ends at its own 0010 (the
> embeddable inbox contract). `main` has since landed a *different* 0010 (bounded work streams)
> and an 0011 (organization-scoped API keys). Both this ADR and 0011 will therefore need
> renumbering when #73 lands on `main` — as will #73's own 0010. Numbered against the base this
> PR actually targets, rather than guessing at how that reconciliation will go.

---

## Context

Hermes deploys three NATS servers with a `minAvailable: 2` PodDisruptionBudget and hard
hostname anti-affinity, nine services behind PDBs and HPAs, and a documented expectation that
nodes come and go. Under inspection, almost none of that survived a node actually going away,
and each defect was invisible from the outside.

**Every JetStream stream ran at one replica.** `SetupStreams` never set `Replicas`, and
JetStream's default is 1. So on a three-node cluster all four streams — including the DLQ —
lived on a single peer. `nats stream ls` reports them healthy, every publish succeeds, and the
first evidence is losing that node: publishes fail, consumers stall, and any service that
restarts crash-loops on `EnsureStreams`. The PDB and the anti-affinity rules existed to survive
precisely this and could not.

**Consumers kept pulling new work throughout shutdown.** Every worker used
`defer natsClient.Close()`, which runs *after* `bootstrap.ListenAndServe` returns — so for the
entire shutdown window the pool accepted messages it had no intention of finishing. Worse, the
`ConsumeContext` that `Consume` returns was discarded, so nothing could stop the fetcher short of
closing the connection, and `Close` never waited for handlers already running. A pod exiting
mid-handler leaves the message unacked, so JetStream redelivers it and whatever the handler had
already done — sent the email, published to Centrifugo — happens again. Every rolling restart
paid that cost.

**`AckWait` was never set**, so the 30-second default applied to handlers that call SMTP, a
customer's webhook, or SES. A slower one was redelivered *while still running*. And
`processMessage` derived its context from `context.Background()`, so a wedged provider call
occupied a pool worker forever.

**There was no readiness drain.** SIGTERM went straight to `server.Shutdown` with a hardcoded
five-second budget under a thirty-second grace period, racing endpoint removal in kube-proxy and
nginx: every in-flight and newly-routed request in that window was reset. Meanwhile
`/readyz` on inbox, user, dispatch and three workers passed *zero checks* and so reported ready
unconditionally, even with a dead database.

The conventional fix for the endpoint race — `preStop: exec: sleep` — is unavailable here.
Every Hermes image is `FROM scratch`, so there is no shell to run it in.

## Decision

**We will drain deliberately before shutting down, and we will state the replication factor
rather than inherit it.**

### Lifecycle

On SIGTERM, in this order:

1. Flip `/readyz` to 503, so the next probe removes the pod from the Service.
2. **Keep serving** for `HERMES_SHUTDOWN_DRAIN_DELAY` (default 5s) while that propagates.
3. Run shutdown callbacks — stop consuming, wait for in-flight handlers, flush batches.
4. `server.Shutdown` with `HERMES_SHUTDOWN_TIMEOUT` (default 15s).

The drain delay runs **in-process**, because the images have no shell. A `preStop.sleep`
SleepAction is available as opt-in belt-and-braces where the cluster is Kubernetes 1.30+.

`messaging.Client.Drain` retains every `ConsumeContext`, stops each one, releases blocked
hand-offs, waits on an in-flight `WaitGroup`, then flushes the connection. It is registered as
the *first* shutdown callback and replaces `defer natsClient.Close()`. In-flight work is counted
in the fetcher callback rather than in the worker, so a message that has left the fetcher but
not yet been picked up cannot be missed.

Per-consumer `AckWait` and `HandlerTimeout` are explicit, with `HandlerTimeout < AckWait`
enforced in `Subscribe`.

### Readiness

Readiness answers "will this pod serve correctly right now", not "is the world healthy".

| Dependency | Gates readiness | Why |
|---|---|---|
| Postgres | Yes | No fallback exists. |
| NATS | Yes, via local `conn.Status()` | Workers take no inbound traffic, but readiness also gates *rollout progress*, and refusing to roll forward into a broken bus is correct. |
| **Redis** | **Never** | Every read it serves falls back to the database. |
| Centrifugo, SMTP/SES, SMS | No | Per-message failures that already retry and dead-letter. |

Dependency failures are debounced by two consecutive probes; the drain flag bypasses the
debounce, and readiness `failureThreshold` drops to 1 so a draining pod leaves the endpoints on
the very next scrape.

### JetStream

`HERMES_NATS_STREAM_REPLICAS` is explicit configuration read only by `cmd/natsprovision` — 1 in
the base, local and staging; 3 in production. Not derived from the observed cluster size, because
a provisioner Job running during a NATS rolling restart could see one server and silently
downgrade every stream.

**Changing the replication factor of an existing stream is a maintenance operation, not a
deploy.** `upsertStream` refuses a mismatch unless `HERMES_NATS_STREAM_REPLICAS_ALLOW_CHANGE` is
set.

Work streams gain `MaxBytes` (default 512 MiB) with **`Discard: DiscardNew`**. On a work queue
the messages are jobs nobody has done yet, so discarding the oldest silently drops accepted
notifications; rejecting the publish surfaces as a 503 the caller can retry. The DLQ keeps
`DiscardOld` for the opposite reason: a rejected dead letter is a message destroyed with no
trace.

> **This half was decided independently and first.** While this work was in progress, `main`
> landed *its* ADR 0010, "Bound the JetStream work streams and reject new work when they fill",
> reaching the same conclusion — `MaxBytes` with `DiscardNew`, same 512 MiB default, same
> reasoning about accepted work never being silently destroyed. It goes further than this ADR
> does, mapping a rejected publish to `503` with `Retry-After` and a
> `hermes.send.publish_rejections` counter.
>
> This branch is cut from PR #73, which predates that, so the two implementations arrived at the
> same policy through different APIs: `main` takes the ceiling as a `WithStreamMaxBytes` connect
> option, this branch as a field on `StreamOptions` alongside `Replicas`. **That is a genuine
> conflict to reconcile when #73 lands on `main`**, and `main`'s shape should win for the
> ceiling — it is the ratified one and has the send-side handling. What is *not* duplicated, and
> should survive the reconciliation, is everything about replication: `main` still creates every
> stream at R=1.

### Enforcement

The budget spans a manifest field and three environment variables, which is exactly the pairing
that drifts. `scripts/check_shutdown_budget.py` and `scripts/check_nats_stream_replicas.py` fail
the render, and both run over the Kustomize overlays *and* the Helm chart.

## Consequences

- A rolling restart no longer resets in-flight requests, and no longer re-executes the side
  effects of in-flight messages.
- **The six NATS-consuming services need `terminationGracePeriodSeconds: 60`** (5 + 30 + 15 = 50).
  The three that hold no NATS client state `HERMES_NATS_DRAIN_TIMEOUT=0s` and stay at 30.
- **Pods take longer to go away.** A scale-down that used to complete in seconds now takes up to
  a minute. That is the cost of not abandoning work, and it is the right trade.
- **A drain that times out is reported, not swallowed.** `ErrDrainTimeout` means the messages are
  safe (unacked, so redelivered) but their side effects will be repeated — worth alerting on.
- **Redis will never remove a pod from service.** A Redis outage degrades the cache and the
  idempotency window; it does not stop a pod answering correctly. Gating readiness on it would
  turn a degradation users would barely notice into a total outage.
- **Raising an existing cluster to R=3 is a deliberate, watched operation.** It migrates whole
  file-backed streams between peers. The guard makes that a decision rather than a side effect.
- `MaxBytes` means a sustained spike can now be *rejected*. That is the intent: an unbounded
  stream that fills its volume fails every publish, including the dead-letter publishes meant to
  be the safety net.
- The Helm chart gains PDBs (it had none), grace periods, preStop plumbing and non-empty resource
  requests — bringing it to the posture the Kustomize overlays already had.
- **Flagged, not fixed:** `internal/eventwriter` acks a message as soon as it joins the in-memory
  batch, before the database write. That is at-most-once for events even with a perfect drain. It
  needs its own change.

## Alternatives considered

**Deriving the replication factor from the running cluster.** Removes a config value that can
disagree with reality. Rejected because the observation is not trustworthy at the moment it is
made: the provisioner Job runs during deploys, which is exactly when a NATS pod may be
restarting, and the failure is silent — streams quietly downgraded to R=1 with everything
reporting healthy. `check_nats_stream_replicas.py` catches the disagreement instead.

**Defaulting `HERMES_NATS_STREAM_REPLICAS` to 3.** Safer for clustered deployments, but asking
for three replicas on a one-node server fails outright, so it would break every single-node
install — `make infra-up`, the local overlay, staging, and every evaluation. The default has to
be the value that always works; the gate covers the case where a clustered deployment forgets.

**`preStop: exec: ["/bin/sh", "-c", "sleep 5"]`.** The conventional answer, and impossible here:
`deploy/docker/Dockerfile` builds `FROM scratch`. Switching to a distroless base with a shell to
enable it would add attack surface to nine images to avoid ten lines of Go.

**`preStop.sleep` (SleepAction) alone.** Needs Kubernetes 1.30 (beta in 1.29) and would make the
chart un-installable on older clusters. Offered as opt-in on top of the in-process delay rather
than instead of it.

**Letting `CreateOrUpdateStream` apply replica changes.** Simplest code, and how the streams were
already declared. Rejected in the reverse direction more than the forward one: pointing a
single-node staging config at production would strip every stream to one replica, report success,
and leave no trace until a node was lost.

**Gating readiness on Redis.** Superficially consistent — it is a dependency, so check it. It
inverts the blast radius: one blip on a dependency that every service shares would pull *all*
replicas of *all* services out of their Services simultaneously, for a fault none of them
actually needed to stop serving for.
