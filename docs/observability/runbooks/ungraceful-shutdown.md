# Runbook: `UngracefulShutdown`

## What this alert means

A pod's drain budget expired while message handlers were still running, so the process exited
with work in flight.

**Nothing is lost.** Those messages were never acked, so JetStream redelivers them after
`AckWait`. What *is* lost is exactly-once-ish behaviour: whatever the handler had already done
before it was abandoned — sent the email, called the SMS webhook, published to Centrifugo —
happens again on redelivery. One duplicate notification per abandoned message, roughly.

This should be zero. Before ADR 0012 it was the normal case on every rolling restart, because
consumers kept pulling new work throughout shutdown and nothing waited for handlers at all.

## Immediate triage

```bash
# Which service, and how often?
# Dashboard: Hermes service → "Shutdown" row.

# Where is the time going? Compare the phases against the pod's grace period.
#   hermes.shutdown.duration{phase="nats_drain"}   ← usually the one that grows
#   hermes.shutdown.duration{phase="http"}

# What is the pod actually allowed?
kubectl -n hermes get deploy <service> \
  -o jsonpath='{.spec.template.spec.terminationGracePeriodSeconds}{"\n"}'

# What does it think its budget is?
kubectl -n hermes set env deploy/<service> --list | grep -E 'SHUTDOWN|DRAIN'
```

## Common causes (ranked by frequency)

1. **A slow provider.** The drain waits for in-flight handlers, and a delivery handler waits on
   SMTP, SES or a customer's webhook. If that endpoint is degraded, handlers sit at their full
   `HandlerTimeout` (30s for the delivery workers) and the drain waits with them. Check whether
   a provider alert is firing at the same time — this alert is often a symptom, not a cause.
2. **The budget no longer fits the grace period.** Someone raised `HERMES_NATS_DRAIN_TIMEOUT` or
   `HERMES_SHUTDOWN_TIMEOUT` without raising `terminationGracePeriodSeconds`.
   `scripts/check_shutdown_budget.py` fails the render on this, so it usually means a value was
   set outside the manifests — a `kubectl set env`, or an overlay the gate is not pointed at.
3. **A handler that ignores its context.** `HandlerTimeout` cancels the context, but a handler
   that does not check it runs to completion anyway. Look for a provider call made without
   passing `ctx` through.
4. **Genuinely too much in flight.** A worker holding `MaxAckPending` messages when SIGTERM
   arrives has more to finish than one that was idle.

## Mitigations

- **Provider degraded:** fix or fail out the provider. Do not raise the drain budget to
  compensate — that lengthens every shutdown to cover an exceptional case.
- **Budget mismatch:** raise `terminationGracePeriodSeconds`, keeping
  `preStop + drain delay + NATS drain + HTTP shutdown` under ~90% of it, and re-run
  `make verify-manifests`.
- **Handler ignoring context:** a bug in that provider; the fix is threading `ctx` to the call.

## Escalation

- Repeated on one service → that service's owner.
- Across all services at once → platform on-call; look for a shared dependency (the bus, or a
  node draining faster than the grace period allows).

## Post-incident

- If the cause was a provider, ask whether that provider's own timeout should be shorter than
  `HandlerTimeout` so the handler fails fast and retries rather than blocking the drain.
- Duplicate side effects are user-visible. Check whether the affected notifications need an
  apology, and whether the handler could be made idempotent — `internal/delivery/inbox.go`'s
  `MarkUnreadCounted` guard is the pattern.
