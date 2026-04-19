# Runbook: `HighErrorRate`

## What this alert means

The service is returning HTTP 5xx on more than 1% of requests over a 5-minute window. Either the service is broken, a downstream is broken, or a recent deploy introduced a regression.

## Immediate triage

1. Dashboard: **Hermes service — overview**, select the firing service from `$service`.
2. Check the "Error rate (5xx)" panel — which route(s) are failing?
3. Pivot to logs: Grafana → Explore → Loki, query `{service="$service"} |= "ERROR"` for the last 15 minutes.
4. Pivot to traces: in Loki, click the "View trace" button on a failing log line to see the span tree.

## Common causes (ranked by frequency)

1. **Downstream dependency is slow/down.** Check spans — if the time is spent in a DB/Redis/NATS span, the issue is there, not the service itself.
2. **Recent deploy regression.** Correlate alert start time with last deploy. The "HighErrorRate" span tag `deployment.version` should show the new SHA.
3. **Poison message.** One message in a NATS queue that the worker can't process. Worker enters a retry loop. Check for repeated identical errors.
4. **Rate limit / quota hit.** Upstream provider (email, SMS) returning 429. Service surfaces as 5xx.
5. **Config drift.** An env var changed, pointing at a stale dependency.

## Mitigations

### If downstream is slow

- Check the DB pool dashboard. If saturated, see `db-pool-saturated.md`.
- Check the downstream dependency's own dashboards (kube-prom-stack includes NATS/Postgres/Redis panels).
- Consider short-term rate limiting upstream to let the downstream catch up.

### If recent deploy

Roll back via Kargo/ArgoCD. Stabilize first, investigate after.

### If poison message

- Exec into the worker pod, inspect the NATS consumer state: `nats consumer info <stream> <consumer>`.
- If a single message is stuck, manually ack it (`nats consumer next <stream> <consumer> --ack`) and open a bug to handle the case in code.

### If upstream quota

Confirm with provider dashboards (SendGrid/Twilio/etc.). If quota was hit, short-term throttle the worker. Long-term, look at spend patterns.

## Escalation

- Worker services: platform on-call.
- User-facing services (admin, inbox, user): product on-call.
- Multiple services at once: infrastructure issue, platform on-call.

## Post-incident

- If the root cause was a deploy regression, add a test.
- If it was a downstream, check whether our dashboards showed the downstream problem clearly — if not, add a panel or alert.
