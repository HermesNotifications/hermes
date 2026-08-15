# Runbook: `HermesDeliveryFailureRate` / `HermesDeliveryAbsent`

## What these alerts mean

**`HermesDeliveryFailureRate` (page):** more than 5% of terminal delivery outcomes on
one channel/provider pair are failures. Terminal means retries are already spent — these
notifications will never arrive. A provider blipping and recovering does not appear here
at all, so this is not a transient-error alert.

**`HermesDeliveryAbsent` (critical):** send is accepting work and no channel has produced
a single terminal outcome for 15 minutes. This is the more serious of the two — while it
is firing, `HermesDeliveryFailureRate` **cannot** fire, because its denominator is zero.

## Why the pipeline alerts can be green

`pipeline.rules.yaml` watches the mechanism: backlog, drain rate, pool saturation, dead
letters. A provider rejecting every request keeps all of those healthy — messages are
consumed promptly, the queue stays shallow, and dead letters do not appear until retries
are exhausted (~10 minutes per notification). Delivery outcomes are the only signal that
leads the mechanism here.

`HermesProbeLoss` is end-to-end but only covers **inbox**, the channel the prober
subscribes to. Email and SMS have no synthetic coverage.

## Immediate triage

The alert labels carry `channel` and `provider`. The first question is whether the fault
is ours or theirs:

```promql
# Is a sibling provider on the same channel healthy?
sum by (channel, provider) (rate(hermes_delivery_result_total[10m]))

# Failing fast, or timing out? These want different fixes.
histogram_quantile(0.95,
  sum by (channel, provider, le) (rate(hermes_delivery_provider_duration_seconds_bucket{outcome="failed"}[10m]))
)
```

- **One provider failing, siblings fine** → provider-side. Check their status page and
  your account standing (quota, billing, suspended sender identity).
- **Fast failures** (p95 well under a second) → the provider is rejecting, not timing
  out. Almost always credentials, a quota, or a sender identity that stopped verifying.
- **Slow failures** (p95 at or near the client timeout) → the provider is unreachable or
  overloaded. Check egress: NetworkPolicy, DNS, and any outbound proxy.
- **Every provider on every channel failing** → look at Hermes, not the providers. Egress
  or DNS for the whole namespace.

Then read the actual error, which the counter cannot carry:

```
{k8s.namespace.name="hermes"} |= "delivery failed" | json | last_attempt="true"
```

## Dead letters

Every terminal failure is also captured to the DLQ stream, so nothing is lost while you
fix the cause. Once the provider is healthy again, replay per
[dead-letter-queue.md](dead-letter-queue.md) — replay is idempotent at every pipeline
stage.

## `HermesDeliveryAbsent` specifically

Accepted traffic with no delivery outcomes at all means the break is between ingestion
and the workers, not in a provider. Work outward from send:

1. **Is dispatch consuming?** `hermes:consumer_pending` for the `NOTIFICATIONS` stream.
   A growing backlog with no drain is a stalled consumer — see
   [consumer-stalled.md](consumer-stalled.md).
2. **Is dispatch producing?** `rate(hermes_notifications_dispatched_total[5m])`. Zero
   here with dispatch consuming means routing is dropping everything — check
   `hermes_routing_drop_total` and see [routing-drops.md](routing-drops.md).
3. **Are the workers running at all?** `up{job=~"hermes-worker-.+"}`. If dispatch is
   publishing and `delivery.*` is backing up, the workers are down or wedged.

## Post-incident

- A provider that fails this way repeatedly deserves its own health signal ahead of the
  retry window, so the page arrives before notifications are lost rather than after.
- `internal/delivery/worker.go` treats every provider error as transient, because
  `Provider.Send` gives no way to distinguish a 4xx rejection from a connection refused.
  A permanent rejection therefore burns the full retry budget before dead-lettering.
  Per-provider error classification is the fix and is tracked as separate work.
