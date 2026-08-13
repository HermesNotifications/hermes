# ADR 0022: Make liveness follow consumer progress

**Status:** Accepted  
**Date:** 2026-08-13  
**Author:** Platform

---

## Context

Dispatch was found idle at 1m of CPU with 133,472 messages pending on its JetStream consumer and
its last log line 2h58m old. Throughout that window the pod reported `Ready=true` with zero
restarts. A rollout restart resolved it instantly — CPU went to 1,159m and the backlog drained.

The write path had been completely stopped for three hours and every health signal was green,
because of what the signals actually asked:

- `/healthz` was a hardcoded `200`. It asked whether the process was running. It was.
- `/readyz` ([ADR 0015](0015-lifecycle-and-jetstream-durability.md)) asked whether Postgres and the
  NATS connection were reachable. They were.
- `NATSConsumerLag` (warn, `num_pending > 1000` for 5m) asked whether a backlog existed. A backlog
  also exists during a legitimate traffic spike, so the alert cannot distinguish "busy" from
  "wedged" and does not page.

Nothing asked whether the consumer was still taking work. The trigger was never identified — the
leading hypothesis, a `nats stream purge` wedging the attached consumer, was tested directly and
disproved, and the pod's logs were lost with the pod. This ADR therefore records a decision about
**detection**, not about a cause.

The constraint that makes this non-trivial is that an idle consumer is indistinguishable from a
wedged one by progress alone. A correctly working dispatch with an empty queue makes no progress
for hours, and any check that restarts on "no progress" would restart the fleet every quiet night.

## Decision

We will fail **liveness**, not readiness, when a NATS consumer has work waiting and has settled
none of it for `HERMES_NATS_CONSUMER_STALL_TIMEOUT` (default 10 minutes).

The signal is maintained in `internal/messaging` (`stall.go`) and applies to every consuming
service — dispatch, the three delivery workers, the event writer — rather than to dispatch alone.
Three rules define it:

1. **Work waiting** is `NumPending + NumAckPending`, read once per 30s with `CONSUMER.INFO`. The
   ack-pending term is required: every worker wedged inside a handler with an empty stream behind
   them shows `NumPending == 0`, and a pending-only check would call that consumer idle forever.
2. **Progress** is a message *settled* — acked, nak'd or terminated — not a message succeeded. A
   consumer failing everything is retrying, which is a different problem with its own alerts, and a
   restart would not help it.
3. **Not knowing counts as healthy.** An unreachable bus, a failed poll, a missing `CONSUMER.INFO`
   grant and a draining pod all report healthy. A check that failed on any of them would turn a
   NATS blip into a fleet-wide restart — a worse outage than the one being detected.

The window resets whenever the queue is observed empty, so a backlog arriving after an idle night
gets a full window of its own, and the window is seeded at subscribe time, which is the whole of
the startup grace period.

The corresponding metric `hermes.messaging.consumer.progress.age` and the `HermesConsumerStalled`
alert fire at half the window (5m), before the restart, so an operator can reach a live wedged
process. `0` disables detection entirely.

## Consequences

- A wedged consumer is now detected in ~10 minutes and self-heals by restart, instead of running
  until a human notices. That is the whole point.
- **Liveness can now restart pods.** This is the cost, and it is a real one: a liveness probe that
  is wrong restart-loops an entire Deployment. Everything above — the ack-pending term, the
  10-minute window against a 240s backoff ceiling, "unknown means healthy", the kubelet's own
  `failureThreshold` on top — is spent buying margin against that.
- `HERMES_NATS_CONSUMER_STALL_TIMEOUT` must only ever be **raised**. A value under the retry
  backoff ceiling can call a legitimately backing-off consumer stalled.
- Each consuming service gains `$JS.API.CONSUMER.INFO.<stream>.<consumer>` on its own consumer
  ([ADR 0005](0005-transport-security-for-infrastructure-connections.md) phase 3/4 scoping is
  preserved: one subject, read-only, own consumer only). A cluster running an older accounts file
  keeps working with detection silently off, which is why it logs `stall detection paused` and why
  `TestAccounts_EveryConsumerMayInspectItsOwnConsumer` asserts the grant on the wire.
- New obligation: the `HermesConsumerStalled` runbook
  ([docs/observability/runbooks/consumer-stalled.md](../observability/runbooks/consumer-stalled.md))
  leads with *capture the pod before it restarts*, because the root cause is still open and the
  automatic remedy destroys the evidence.

## Alternatives considered

- **Readiness instead of liveness.** Rejected: dispatch and the workers receive no inbound traffic,
  so removing them from Service endpoints changes nothing about their consuming. A stalled pod
  would keep consuming nothing while looking correctly handled — and rollouts would stall, which is
  the one visible effect and the wrong one.
- **Alert only, no probe.** Legitimate, and we did add the alert. Rejected as the *only* measure
  because it leaves recovery at human latency for a failure whose remedy is a single restart, and
  because the existing lag alert had already been in place for months without closing this gap.
- **A cause-specific fix.** Not available. The cause is unknown and the one hypothesis was
  disproved; a guessed fix would have been untestable and would have removed the pressure to keep
  looking.
- **Progress alone, without querying the server.** Rejected: without knowing whether work is
  waiting, an idle consumer and a wedged one are the same observation, and this is exactly the
  check that must not be wrong.
- **A shorter window (5 minutes).** Rejected: the retry backoff in `internal/messaging` caps at
  240s, during which a healthy consumer can legitimately settle nothing while holding ack-pending
  work. Five minutes spends the entire margin to save five minutes of an outage that ran for three
  hours.
