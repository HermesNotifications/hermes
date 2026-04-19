# Runbook: `DBPoolSaturated`

## What this alert means

Active Postgres connections are >90% of `max_connections` for 5 minutes. Services are queuing for pool slots, which cascades into latency and error-rate alerts.

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

- Increase service-side pool size (in HERMES_DATABASE_MAX_CONNS or equivalent).
- If already at cluster `max_connections` limit, tune that (requires Postgres restart or parameter reload).
- Phase 2: consider pgBouncer in transaction pooling mode.

## Escalation

- Service-specific leak → that service's team.
- Systemic / DB-level → platform on-call.

## Post-incident

- Every connection leak should end with a regression test that counts connections before/after a test scenario.
- Review: do we have adequate alerting on per-service pool saturation, not just cluster-wide?
