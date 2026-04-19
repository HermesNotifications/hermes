# Runbook: `HighLatency`

## What this alert means

p95 latency on the service's HTTP endpoints is above 1s for 10 minutes. Users are experiencing noticeable slowness.

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

- Check `NATSConsumerLag` alert — if firing, see that runbook.
- Check `DBPoolSaturated` — if firing, see that runbook.

### If traffic spike

- HPA should kick in. If it's not, check why (metrics-server healthy? HPA target metric reporting?).
- Manual scale via `kubectl scale deployment` as short-term.

### If GC

- Usually means a memory leak or recently added unbounded allocation. Check heap trend over the past 24h.
- Go profiling: `pprof` at `:6060/debug/pprof/heap`. Phase 2 wires continuous profiling.

## Escalation

- Single service, obvious cause: the service's owning team.
- Unclear or systemic: platform on-call.

## Post-incident

- If a DB query caused it, codify the EXPLAIN pattern in CI (slow-query test).
- If HPA didn't react in time, tune target utilization or add a predictive scaler.
