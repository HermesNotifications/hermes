# Runbook: `HermesProbeLoss` / `HermesProbeAbsent` / `HermesProbeLatency`

## What these alerts mean

The synthetic prober (`cmd/prober`) continuously sends a notification to itself and waits for
the frame on a websocket. It is the only signal that measures the pipeline the way a user
experiences it, end to end.

| alert | fires when | means |
|---|---|---|
| `HermesProbeLoss` | >10% of probes never arrive | notifications are not being delivered |
| `HermesProbeAbsent` | no probe results for 15 min | the probe itself is not running or not scraped |
| `HermesProbeLatency` | e2e p95 > 2s | delivery works but is slow |

**`HermesProbeLoss` is the highest-signal alert in this repo.** Every other alert measures a
hop. This one measures the outcome, and it is the only one that fires for the failures that
leave every HTTP response at 200:

- Centrifugo on the memory engine behind more than one replica — half of all publications land
  on a pod the recipient is not connected to.
- `client.allowed_origins` refusing the browser's origin — the socket handshake succeeds and
  every subscription is refused.
- A publish to a channel nobody is subscribed to (external vs. internal user id).
- A wedged consumer that still passes its readiness probe.

## Check the prober before you check Hermes

```promql
hermes_probe_connected          # 1 while it holds a subscribed websocket
rate(hermes_probe_results_total{result="send_error"}[5m])
```

A prober that has lost its own subscription reports **100% loss on a perfectly healthy
pipeline**, and `hermes_probe_connected` is the only thing that distinguishes the two. Check it
first, every time.

If `result="send_error"` is nonzero, the failure is on the way in, not the way out — the probe
never got as far as waiting. Check the prober's logs for the status code; a 429 means it is
being rate-limited by its own credential, a 401 means its key is wrong or expired.

## `HermesProbeAbsent`

The probe is not reporting. In order of likelihood:

1. **Pod not running.** `kubectl -n hermes get pods -l app.kubernetes.io/name=hermes-prober`.
   A `CreateContainerConfigError` means the API-key Secret named by
   `prober.apiKey.existingSecret` does not exist or lacks the key — the chart validates the
   value is set but cannot see whether the Secret resolves.
2. **Telemetry not scraped.** Check the prober appears as a Prometheus target. If
   `observability.serviceMonitor` is rendering nothing, see the `force` note in values.yaml.
3. **It exited at boot.** The prober exits rather than idling if it cannot mint a token — a pod
   that runs while probing nothing is the exact failure it exists to prevent. Check the logs
   for `mint token:`.

While this is firing, `HermesProbeLoss` **cannot fire**, because its ratio has no samples.
Treat it as "the end-to-end check is off", not as a minor alert.

## `HermesProbeLoss`

Confirm it is real, then find the hop.

```promql
# Is anything being delivered at all, or is this total?
sum(rate(hermes_probe_results_total{result="received"}[10m]))

# Is the pipeline moving other people's notifications?
hermes:consumer_drain_rate
hermes:consumer_pending
```

- **Backlog growing** → this is a throughput problem, not a delivery one. See
  [nats-consumer-lag.md](nats-consumer-lag.md) and
  [worker-pool-saturated.md](worker-pool-saturated.md). The probe is queued behind real work.
- **Backlog flat, drain rate healthy, probes still lost** → the notification is being processed
  and not arriving. That is the realtime tier. Check `centrifugo_node_num_clients` across
  replicas, and confirm the engine:

  ```bash
  kubectl -n hermes get cm -l app.kubernetes.io/name=centrifugo -o yaml | grep -A3 engine
  ```

  More than one replica on `"type": "memory"` is the classic cause. The chart refuses that
  combination at render time now, but a cluster installed before that, or one running the
  kustomize overlays, can still be in it.
- **`worker-inbox` erroring** → check its handler duration and error rate; a failing Centrifugo
  publish nacks the message and it retries, so this also shows as rising redeliveries.

## `HermesProbeLatency`

Split ingestion from the rest:

```promql
histogram_quantile(0.95, sum by (le) (rate(hermes_probe_send_duration_seconds_bucket[10m])))
histogram_quantile(0.95, sum by (le) (rate(hermes_probe_e2e_duration_seconds_bucket[10m])))
```

If send is flat and e2e has risen, the delay is downstream of ingestion — consumer backlog
first, then the realtime tier. If both rose together, it is send: auth, idempotency or the
JetStream publish.

For reference, this path measured **11ms p95** while the system held 250,000 concurrent
websocket connections. A p95 of seconds is not a busy system; it is a broken one.

## What this alert does not tell you

The probe exercises **one organization, one user, the inbox channel**. It cannot see a failure
scoped to email or SMS delivery, to a specific template, or to a particular organization's
configuration. A green probe means the realtime path works, not that every notification works.

## Escalation

Platform on-call. `HermesProbeLoss` sustained is a customer-visible outage even when no other
alert is firing — that is the entire reason it exists.

## Post-incident

If the probe caught something no other alert did, that gap is the finding. Consider whether the
failure has a cheaper leading indicator worth alerting on directly, since the probe tells you
*that* delivery broke and rarely *why*.
