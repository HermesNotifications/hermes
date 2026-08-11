# ADR 0010: Bound the JetStream work streams and reject new work when they fill

**Status:** Accepted  
**Date:** 2026-08-10  
**Author:** Daryl Robbins

---

## Context

`messaging.SetupStreams` created the three work streams — NOTIFICATIONS, DELIVERY, EVENTS —
with `WorkQueuePolicy`, `FileStorage` and a 7-day `MaxAge`, and **no `MaxMsgs`, `MaxBytes` or
`Discard`**. Under WorkQueue retention a message is removed when it is acknowledged, so in
steady state the streams stay near empty and the omission is invisible. It stops being
invisible the moment a consumer falls behind: dispatch crashlooping, a worker wedged on a slow
provider, a bad deploy. The backlog then grows without bound until the NATS volume (5Gi,
`deploy/k8s/base/infra/nats.yaml`) fills, at which point JetStream fails writes for **every**
stream on that volume, including the DLQ that exists to capture the failures.

The DLQ stream, declared five lines below in the same function, already sets `MaxBytes: 1 GiB`
and `Discard: DiscardOld`. Someone reasoned about limits there and not here.

There was also nothing to react to. "Backpressure" needs a signal, and an unbounded stream
never produces one — it absorbs everything until the disk is gone, which is the least useful
failure mode available.

## Decision

We will bound each work stream with `MaxBytes` (default 512 MiB, `DefaultStreamMaxBytes`,
overridable per deployment via `HERMES_NATS_STREAM_MAX_BYTES`) and set
**`Discard: DiscardNew`**.

`DiscardNew` means that when a stream is at its ceiling, JetStream **rejects the publish** and
returns an error to the publisher, rather than deleting the oldest message to make room.

Send maps a failed publish to `503 Service Unavailable` **with a `Retry-After` header**, and
increments `hermes.send.publish_rejections`.

## Consequences

**Send can now be told "no".** This is the substantive change and the reason this is an ADR
rather than a config tweak. Before, `POST /v1/send` could fail only if NATS was unreachable;
it can now also fail because the pipeline is saturated. Integrators must handle 503 and honour
`Retry-After`. This is documented in the integration guide.

**Accepted work is never silently destroyed.** The alternative failure mode — dropping the
oldest queued notification — would discard a notification for which the API already returned
`202 Accepted`. A caller told "accepted" and then silently ignored is worse than a caller told
"try again", because only the second one can do something about it.

**The failure surfaces at the edge instead of on a volume.** A full stream produces 503s and a
rising `hermes.send.publish_rejections` counter, which is an alertable signal that names the
problem. A full volume produces write failures across every stream at once, including the DLQ,
and looks like NATS itself broke.

**The ceiling has to be sized against the volume.** Three work streams at 512 MiB plus the
1 GiB DLQ is 2.5 GiB of the 5Gi PVC. An operator who raises `HERMES_NATS_STREAM_MAX_BYTES`
without growing the volume re-creates the original failure with extra steps; an operator who
grows the volume without raising the ceiling leaves capacity unused. Both are documented in
`docs/configuration.md`.

**Only the provisioner's value takes effect.** Under [ADR 0005](0005-transport-security-for-infrastructure-connections.md)
phase 4 the `natsprovision` Job is the sole identity permitted to create or update a stream, so
`HERMES_NATS_STREAM_MAX_BYTES` is read there and ignored everywhere else. Setting it on a
service Deployment does nothing, which is a sharp edge worth knowing about.

**Not yet done:** nothing drives autoscaling from consumer lag, so the system rejects work
before it scales to absorb it. That is the natural follow-up and is deliberately out of scope
here — it needs a lag metric and an HPA custom-metrics path, neither of which exists yet.

## Alternatives considered

**Leave the streams unbounded.** The status quo. Rejected: it converts every consumer stall
into a volume-exhaustion incident that takes out the DLQ too, and it offers no signal to act
on before that point.

**`Discard: DiscardOld`, matching the DLQ.** Rejected. The DLQ's contents are evidence — nobody
is waiting on a dead letter, so retaining the oldest is right there. A work stream's contents
are obligations the API has already accepted. Dropping the oldest silently breaks the promise
made by the `202`, and does so invisibly: no error, no counter, no log at the point of loss.

**`MaxMsgs` instead of `MaxBytes`.** Rejected as the primary bound: what exhausts is the
volume, measured in bytes, and message sizes vary with template payloads. A message count that
is safe for small notifications is unsafe for large ones. `MaxBytes` bounds the thing that
actually runs out.

**Return 429 rather than 503 when a stream is full.** Rejected. 429 tells the caller their
request rate is the problem, and the usual cause of a full work stream is a stalled consumer,
not a fast publisher. Blaming the caller for a backlog they did not create sends them into a
backoff that cannot help. 503 with `Retry-After` says what is true: the service cannot accept
this right now.

**Bound the streams but keep returning a bare 503.** Rejected as a half-measure: without
`Retry-After` each client invents its own backoff, and the ones that invent badly retry hardest
exactly when the pipeline is furthest behind.
