# Runbook: `HighLatency` / `SendIngestionLatency`

## What this alert means

p95 latency on the service's HTTP endpoints is above threshold for 10 minutes. Users are experiencing noticeable slowness.

| alert | covers | threshold |
|---|---|---|
| `HighLatency` | every service except `hermes-send` | p95 > 1s |
| `SendIngestionLatency` | `hermes-send` only | p95 > 100ms |

> **Neither alert can tell you whether notifications are being delivered.** Send publishes to
> JetStream and returns, so it answers in ~2ms whether or not anything downstream keeps up. At
> 6,000/s offered with **764,173** messages backed up behind it, send's p95 read 60ms and its
> error rate was 0.00%. Send has its own tighter threshold precisely because 1s was
> unreachable for it — but even at 100ms, this is a statement about *ingestion*, not delivery.
>
> For delivery, see [nats-consumer-lag.md](nats-consumer-lag.md). Alert on consumer backlog,
> never on ingestion latency.

## Immediate triage

1. Dashboard: **Hermes service — overview** → "Latency p50/p95/p99" panel.
   - Is p95 climbing while p50 is stable? → tail-latency problem (usually a specific slow path).
   - Are p50/p95/p99 all climbing together? → systemic (downstream slow, saturation).
2. Pivot to traces: Explore → Tempo, search for spans with `duration > 1s` in the service. Look for the long-pole span.
3. Check downstream dashboards: NATS lag, DB pool, Redis latency.

## Common causes (ranked by frequency)

1. **Slow DB query.** Missing index, query plan regression, table bloat. Check Postgres slow query log and the `Hermes infra` dashboard's transaction rate.
2. **Downstream saturation.** NATS consumer falling behind → publish blocks. Redis under memory pressure → command queue grows.
3. **Recent traffic spike.** Concurrency-bounded service running out of headroom. Check RPS panel against baseline.
4. **GC pauses.** Go heap grew, major GC stalls. `go_gc_duration_seconds` panel on the service dashboard.
5. **DNS resolution lag in the cluster.** Less common but shows up as episodic tail-latency increases on outbound calls.

## Mitigations

### If slow DB query

- Identify the query from span: `db.statement` attribute.
- `EXPLAIN ANALYZE` it (read-replica or local copy, never prod).
- Add an index or rewrite. Ship as hotfix if severity warrants.

### If downstream saturation

- Check `NATSConsumerBacklogGrowing` / `NATSConsumerBacklogUnbounded` — if firing, see
  [nats-consumer-lag.md](nats-consumer-lag.md).
- Check `HermesWorkerPoolSaturated` — see [worker-pool-saturated.md](worker-pool-saturated.md).
- Check `DBPoolSaturated` — if firing, see that runbook.

### If traffic spike

- HPA should kick in. If it's not, check why (metrics-server healthy? HPA target metric reporting?).
- Manual scale via `kubectl scale deployment` as short-term.

### If GC

- Usually means a memory leak or recently added unbounded allocation. Check heap trend over the past 24h.

### Profiling

Requires `HERMES_DEBUG_PORT` set on the service (off by default; the port is deliberately not
in any Service, so reaching it needs a port-forward). Images are `FROM scratch` with no shell,
so fetch the profile and analyse it locally against the binary:

```bash
kubectl -n hermes port-forward deploy/hermes-<service> 6060:6060

go tool pprof -http=:8090 bin/<service>/service http://localhost:6060/debug/pprof/heap
go tool pprof -http=:8091 bin/<service>/service 'http://localhost:6060/debug/pprof/profile?seconds=30'
```

**A CPU profile is usually the wrong first look here.** At 250,000 connections Hermes sat at
11–24% node CPU with the inbox pod at 50m — the time goes on waiting, not computing, and only
the block and mutex profiles record waiting. Those need `HERMES_BLOCK_PROFILE_RATE` /
`HERMES_MUTEX_PROFILE_FRACTION` set as well:

```bash
go tool pprof -http=:8092 bin/<service>/service http://localhost:6060/debug/pprof/block
```

For a service that has stopped doing anything rather than slowed down, capture the full
goroutine dump **before** restarting the pod — it is the artifact that was lost the first time
a consumer wedged:

```bash
curl -s 'localhost:6060/debug/pprof/goroutine?debug=2' > wedge.txt
```

## Escalation

- Single service, obvious cause: the service's owning team.
- Unclear or systemic: platform on-call.

## Post-incident

- If a DB query caused it, codify the EXPLAIN pattern in CI (slow-query test).
- If HPA didn't react in time, tune target utilization or add a predictive scaler.
