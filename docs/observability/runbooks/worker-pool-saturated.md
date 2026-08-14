# Runbook: `HermesWorkerPoolSaturated`

## What this alert means

Every worker in a consumer's pool has been busy for 10 minutes **while messages are waiting**.
The service is not failing and not slow per message — it has no free worker to hand the next
message to.

```promql
sum by (stream, consumer) (hermes_messaging_inflight)
  / on (stream, consumer)
sum by (stream, consumer) (hermes_messaging_workers_limit)
```

The backlog condition is deliberate. A pool at 100% with an empty queue is a pool sized
correctly and working; it is only a problem when work is queued behind it.

## Why this alert exists

Because the answer is a configuration change, and nothing previously said so. `cmd/dispatchbench`,
run inside a cluster against the same Longhorn-backed Postgres:

| dispatch workers | msgs/s |
|---:|---:|
| **8 (the default)** | 2,100 |
| 16 | 3,511 |
| 32 | 5,534 |
| 64 | **7,907** |

3.8× throughput from one value, on storage that had been diagnosed as the wall. Postgres
amortises concurrent commits into shared flushes, and eight workers do not give it enough
concurrency to amortise. The pipeline was bounded by its own pool size, not by the disk.

Adding replicas does **not** substitute:

| dispatch replicas | sustained drain | per replica |
|---:|---:|---:|
| 1 | ~1,242/s | 1,242/s |
| 4 | ~2,006/s | ~500/s |

Four times the replicas bought 1.6× the throughput, because extra replicas add workers
competing for the same fsync queue — and consume `max_connections` while doing it.

## Immediate triage

```bash
# Which consumer, and how saturated?
#   hermes_messaging_inflight / hermes_messaging_workers_limit
# Is work actually waiting behind it?
#   hermes:consumer_pending
# Is it getting worse?
#   deriv(hermes:consumer_pending[15m])
```

If saturation is high but the backlog is flat or falling, you are at capacity and keeping up.
Note it and move on — this does not need a fix at 3am.

## Mitigation

### Raise the pool, and the connection pool with it

For dispatch, two knobs that **must** move together:

```yaml
dispatch:
  concurrency: 32          # HERMES_DISPATCH_CONCURRENCY
postgresql:
  maxConnections: "200"    # the server-side limit these draw from
```

Worker count is clamped to the pgx pool size by `dispatch.ClampWorkersToPool`, so raising
concurrency alone does nothing except log:

```
HERMES_DISPATCH_CONCURRENCY exceeds the database pool size; clamping to pool size
```

`templates/_validate.tpl` does the connection arithmetic at render time when dispatch is
tuned above the built-in pool size.

### Watch `max_connections`

The bundled server defaults to 100. Every Hermes service holds its own pgx pool of 10, so
nine services at one replica is already 90 before dispatch asks for more. Exhaustion does not
fail where you caused it — Postgres answers `sorry, too many clients already` to whichever
service connects next, so the symptom surfaces in inbox or admin while dispatch runs fine.

Raise `postgresql.resources` alongside it: each connection slot costs a few hundred KB of
shared memory plus a backend process when used.

### What not to reach for

- **More replicas.** Measured at 1.6× for 4×, and each one takes connections from the same
  `max_connections` budget.
- **Explicit insert batching.** Already implemented, measured, and rejected: −26% at 32
  workers, because funnelling inserts through one goroutine and one connection destroys
  exactly the commit concurrency that group commit was using. Preserved on
  `experiment/dispatch-insert-batching`; re-measure before trusting it.

## If raising concurrency does not help

Then the pool is not the constraint and the workers are genuinely waiting on something.
Check, in order:

1. `hermes_messaging_handler_duration_seconds` p95 by consumer — how long each message takes.
2. `hermes_db_pool_acquire_waits` — workers queued for a connection rather than for work.
3. `pg_stat_wal` mean fsync latency (`wal_sync_time / wal_sync`) and `pg_stat_activity` wait
   events for `WALSync` — the storage question, answered from a dashboard rather than from
   `pg_test_fsync` on a shell.
4. `/debug/pprof/block` on the service, if `HERMES_DEBUG_PORT` is set — where it is parked.

## Escalation

Service owner. This is a capacity-tuning conversation, not usually an incident, unless
`NATSConsumerBacklogUnbounded` is firing alongside it.

## Post-incident

Put the new concurrency in chart values. A `kubectl set env` is reverted by the next deploy,
and this is exactly the class of setting that gets re-discovered by benchmark a quarter later.
