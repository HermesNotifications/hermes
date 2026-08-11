# Runbook: `MessagingRedeliverySpike`

## What this alert means

More than 10% of the messages a consumer handled in the last 10 minutes had been delivered
before.

Redeliveries are normal in small numbers — a nacked message is a redelivery, and that is the
retry mechanism working. A *spike* means something else, and this alert exists to separate the
two, because in the logs a retry and an ack-deadline expiry look identical.

The distinguishing question is whether handler failures rose too:

- **Redeliveries up, failures up** → a downstream fault. The handler is genuinely failing and
  retrying. This alert is a symptom; find the real one.
- **Redeliveries up, failures flat** → **`AckWait` is too short.** JetStream is redelivering
  messages whose handlers are still running, so the same work is being done twice concurrently
  and every side effect — an email, a webhook call, a Centrifugo publish — happens twice.

Before ADR 0012, `AckWait` was never set at all, so the JetStream default of 30s applied to
handlers that call SMTP and customer webhooks. This was a live duplicate source with no metric
that could show it.

## Immediate triage

```bash
# Handler duration against the configured AckWait — the core comparison.
#   histogram_quantile(0.99,
#     rate(hermes_messaging_handler_duration_seconds_bucket{consumer="worker-email"}[10m]))
# If p99 is anywhere near AckWait, that is your answer.

# Are handlers actually failing, or just slow?
#   rate(hermes_messaging_handler_duration_seconds_count{result="retry"}[10m])

# What is the consumer's ack deadline?
kubectl -n hermes exec -it deploy/hermes-worker-email -- \
  nats consumer info DELIVERY worker-email 2>/dev/null | grep -i 'ack wait'
```

Current settings, from the `SubscribeConfig` in each package:

| Consumer | HandlerTimeout | AckWait |
|---|---|---|
| delivery workers (email, sms, inbox) | 30s | 60s |
| dispatch | 30s | 60s |
| event-writer | 10s | 30s |

`Subscribe` enforces `HandlerTimeout < AckWait`, so the handler is always cancelled before the
deadline expires — *provided the handler respects its context*.

## Common causes (ranked by frequency)

1. **A slow provider.** SES throttling, a customer webhook that got slower, an SMTP server
   sitting on connections. Handler duration rises, and once it crosses `AckWait` the
   redeliveries begin.
2. **A handler that ignores its context.** `HandlerTimeout` cancels the context, but a call made
   without passing `ctx` runs to completion regardless — so the handler outlives its deadline
   and the guarantee that it finishes before `AckWait` is void.
3. **A pod being SIGKILLed mid-handler.** Check for `UngracefulShutdown` at the same time; a
   drain that does not complete leaves messages unacked and they come back.
4. **Genuine retries.** Look at `result="retry"` — if it moved with the redeliveries, this is
   the retry mechanism working and the fault is downstream.

## Mitigations

- **Slow provider:** fix the provider, or lower its client-side timeout so the handler fails
  fast and retries with backoff rather than sitting on the ack deadline.
- **Handler ignoring context:** thread `ctx` into the provider call. This is a bug, not a tuning
  problem.
- **Raising `AckWait`:** possible, but it is the last resort rather than the first. A longer
  deadline also means a *crashed* pod's messages stay invisible for that much longer before
  redelivery. Raise `HandlerTimeout` and `AckWait` together, keeping the former below the
  latter, and remember `AckWait` interacts with the shutdown budget in ADR 0012.

## Escalation

- One channel only → that provider's owner.
- Every consumer at once → platform on-call; suspect the bus or a cluster-wide event rather than
  any individual handler.

## Post-incident

- Duplicate side effects are user-visible. Establish how many notifications went out twice.
- Ask whether the handler could be idempotent. `internal/delivery/inbox.go` guards its Redis
  increment on the notification ID for exactly this reason; the same pattern applies wherever a
  redelivery would repeat an effect.
