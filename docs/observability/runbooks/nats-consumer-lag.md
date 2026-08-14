# Runbook: `NATSConsumerBacklogGrowing` / `NATSConsumerBacklogUnbounded`

## What these alerts mean

Both are about a JetStream consumer that is behind. They differ in what "behind" means, and
the distinction is the point — a queue absorbing a burst and a pipeline losing a race look
identical if you only measure depth.

| alert | fires when | says |
|---|---|---|
| `NATSConsumerBacklogGrowing` | time-to-drain > 5 min for 10 min | the backlog is deep relative to how fast it clears |
| `NATSConsumerBacklogUnbounded` | backlog growing continuously for 15 min | offered rate exceeds drain rate; it will not recover on its own |

`NATSConsumerBacklogUnbounded` is the more serious of the two. A deep backlog that is draining
resolves itself; one that grows does not.

> **Do not judge this from API latency.** Send publishes to JetStream and returns in about 2ms
> whether or not anything downstream keeps up. A 12,000/s run left **1,010,571 messages**
> queued while `send_ack_latency` p95 read 2ms and `http_req_failed` was 0.00%. The write path
> can be completely stopped with every HTTP signal green — that is what
> `hermes_messaging_consumer_progress_age_seconds` and these alerts exist for.

## Immediate triage

```bash
# What the alert is computed from, in one place:
#   backlog        hermes:consumer_pending
#   drain rate     hermes:consumer_drain_rate
#   seconds behind hermes:consumer_seconds_to_drain

kubectl -n hermes port-forward svc/nats 8222:8222 &
nats consumer info <stream> <consumer>
```

Dashboard: **Hermes pipeline** → "Consumer backlog and drain rate".

The two numbers to read together are backlog and drain rate. Backlog alone tells you nothing:
5,000 messages draining at 1,242/s is four seconds; 5,000 draining at 5/s is sixteen minutes.

## Is it stalled, or just slow?

Check `HermesConsumerStalled` first. A drain rate of **exactly zero** with work pending is a
wedged consumer, not a slow one, and it has its own runbook
([consumer-stalled.md](consumer-stalled.md)) — including the instruction to capture logs and
`consumer info` *before* restarting, which is the step that was missed the first time this
happened.

## Common causes, ranked

1. **Worker pool saturated.** Every worker busy, queue growing. See
   [worker-pool-saturated.md](worker-pool-saturated.md) — this is a concurrency setting, and
   it is the most common cause on dispatch.
2. **Worker down or scaled to zero.** Check `ServiceDown` for the corresponding service.
3. **A downstream dependency is slow.** Check `hermes_messaging_handler_duration_seconds` p95
   for the consumer, and the dependency's own metrics.
4. **Upstream spike.** Legitimate surge; workers healthy but insufficient.
5. **Poison message.** A handler that cannot process message X and retries forever blocks
   nothing behind it (workers are a pool, not a queue) but will show as a rising
   `hermes_messaging_redeliveries` and eventually `HermesDeadLetterDetected`.
6. **NATS broker issue.** `nats stream info <stream>` — storage full, no leader.

## Mitigations

### If the pool is saturated

Raise worker count before adding replicas. Measured on the same hardware and storage:

| change | result |
|---|---|
| 8 → 64 workers, one replica | 2,100 → **7,907** msg/s |
| 1 → 4 replicas, 8 workers each | 1,242 → **2,006** msg/s |

Replicas buy Postgres connections faster than they buy throughput, and `max_connections`
is the limit you hit. See [worker-pool-saturated.md](worker-pool-saturated.md).

### If a worker is down

See [service-down.md](service-down.md).

### If it is an upstream spike

Confirm it is real traffic and not a retry storm — check `hermes_messaging_redeliveries` and
the publishing service's request rate. A queue absorbing a spike is the design working; watch
`NATSConsumerBacklogUnbounded` rather than intervening on depth alone.

### If it is a poison message

```bash
nats stream view <stream> --raw
```

Terminally failed messages land on the DLQ stream rather than blocking — see
[dead-letter-queue.md](dead-letter-queue.md) for inspection and replay.

### If it is NATS itself

`nats stream info <stream>` for `bytes` vs max. If full, GC or raise stream storage.
Escalate to infra on-call.

## Escalation

- Service owners for their own consumers.
- Infra for NATS-level issues (broker, storage, leadership).

## Post-incident

- If concurrency was the cause, the new value belongs in the chart values, not in a
  `kubectl set env` that the next deploy reverts.
- If a poison message caused it, add a test for that input shape.
- If the backlog was invisible until a customer noticed, that is an alerting gap — these
  rules are derived from drain rate, so a consumer with no traffic at all emits no drain
  rate and produces no ratio. Check whether the consumer should have a floor on throughput.
