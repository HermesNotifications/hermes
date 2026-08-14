# Runbook: `DBPoolSaturated`

## What this alert means

A service has held more than 90% of its own pool for 5 minutes. Requests are close to queuing on
acquire, which surfaces as latency with no slow query to blame it on, and cascades into the
latency and error-rate alerts.

> This runbook predates both the alert and the fix it recommends. Until ADR 0012 there was no
> metric that could detect the condition (`otelpgx` traces queries, not the pool) and
> `HERMES_DATABASE_MAX_CONNS` did not exist — pgx sized the pool from `runtime.NumCPU()`, which
> reads the *node*, so the same image opened anywhere from 4 to 16 connections depending on
> where the scheduler put it. Both now exist.

The companion `DBPoolAcquireWaits` fires earlier and at lower severity: it means requests are
*already* waiting, before saturation is sustained enough to trip this one.

## Immediate triage

```bash
# See top connection sources
kubectl -n hermes exec -it postgres-0 -- \
  psql -U hermes -c "SELECT application_name, count(*) FROM pg_stat_activity GROUP BY 1 ORDER BY 2 DESC;"

# See long-running queries
kubectl -n hermes exec -it postgres-0 -- \
  psql -U hermes -c "SELECT pid, now() - query_start AS duration, query FROM pg_stat_activity WHERE state = 'active' ORDER BY duration DESC LIMIT 10;"
```

Dashboard: **Hermes infra** → "Postgres — Active connections".

## Common causes (ranked by frequency)

1. **Connection leak in a service.** Usually a recent change that forgot to `Release()` or `Rollback()`. Look at the service dashboards — one will show abnormally high pool usage.
2. **Long-running query holding a slot.** Check the triage query above — if something has been running >60s, it's your culprit.
3. **Traffic spike without scaling pool config.** All services suddenly need more connections than the configured max allows.
4. **pgBouncer / proxy misconfiguration.** If one is in front of Postgres, confirm it's healthy and not itself saturated.

## Mitigations

### If connection leak

- Roll back the last deploy of the offending service.
- File a bug; the owner team fixes pool usage.

### If long-running query

- `SELECT pg_cancel_backend(<pid>);` — cancel the query.
- If it's ETL / analytics, reschedule to off-hours.
- If it's app code, open a bug — queries on this DB should return in <5s.

### If traffic spike

- Raise `HERMES_DATABASE_MAX_CONNS` for the affected service (default 10; set per-service in the
  Deployment's `env`). Note the connection-string parameter `pool_max_conns` overrides it if
  present — `cmd/dispatchbench` relies on that, and a URL that carries it will ignore the
  variable.
- On the Helm chart, dispatch is the one service with a value for this: `dispatch.concurrency`
  sizes the worker pool and the connection pool follows it (`concurrency + 2`). Raising the pool
  there without raising `concurrency` buys dispatch nothing — the clamp runs the other way.
- Check the arithmetic first: `scripts/check_db_pool_budget.py` sums `maxReplicas × MAX_CONNS`
  across the render. Raising one service's pool without re-running it is how the cluster total
  finds Postgres' `max_connections` the hard way.
- If already at the cluster `max_connections` limit, tune that (requires a Postgres restart or
  parameter reload).

### Consider a connection pooler — with the caveat

pgBouncer in transaction pooling mode, or RDS Proxy, is the next step, but not a free one: pgx
v5 defaults to `QueryExecModeCacheStatement`, i.e. implicit prepared statements. Transaction-
pooled pgBouncer needs ≥1.21 to tolerate that, and RDS Proxy *pins* the session when prepared
statements are used, which removes the multiplexing benefit entirely. Adopting either means
forcing `default_query_exec_mode=exec` and giving up the statement cache.

**Revisit trigger:** p95 `hermes.db.pool.acquire.duration` above 50ms, or the budget gate's total
exceeding 60% of `max_connections` at maximum HPA replicas.

## Escalation

- Service-specific leak → that service's team.
- Systemic / DB-level → platform on-call.

## Post-incident

- Every connection leak should end with a regression test that counts connections before/after a test scenario.
- Review: do we have adequate alerting on per-service pool saturation, not just cluster-wide?
