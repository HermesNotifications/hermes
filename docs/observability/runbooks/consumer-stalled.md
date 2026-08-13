# Runbook: `HermesConsumerStalled`

## What this alert means

A NATS consumer has work waiting and has finished none of it for five minutes. Not "slow" — a
consumer that acks, naks or terminates *anything* resets the clock, so this is a consumer that has
stopped taking work altogether while work is queued for it.

The pod is almost certainly still reporting `Ready`, still holding its connections, and still
answering `/readyz`. That is what makes this failure mode expensive: every other signal is green.

**A restart is coming.** `/healthz` fails once the age passes `HERMES_NATS_CONSUMER_STALL_TIMEOUT`
(10 minutes by default) and the kubelet restarts the container. This alert fires at half that, so
you have roughly five minutes to look at the wedged process before it is replaced. If you want
anything from it — a goroutine dump, its logs, its connection state — take it now.

**Nothing is lost either way.** Whatever the consumer was holding was never acked, so JetStream
redelivers it after `AckWait`.

## Background

This exists because dispatch was found idle at 1m of CPU with 133,472 messages pending on its
consumer, its last log line 2h58m old, `Ready=true`, zero restarts. The write path had been stopped
for three hours and nothing had noticed. A rollout restart fixed it instantly — CPU went to
1,159m and the backlog drained.

The trigger was never identified. The leading hypothesis (a `nats stream purge` wedging the
attached consumer) was tested directly and disproved, and the pod's logs were lost with the pod. So
treat the cause as open: if you catch a live one, capture it.

## Immediate triage

```bash
# Which pod? The alert labels carry stream and consumer; the metric is per pod.
kubectl -n hermes get pods -l app.kubernetes.io/component=dispatch

# 1. Is it doing anything at all? A wedged consumer sits at ~1m CPU.
kubectl -n hermes top pod <pod>

# 2. What does the process say about itself? This is the line the original incident lacked.
kubectl -n hermes logs <pod> --tail=100 | grep -E 'consumer is stalled|stall detection paused'

# 3. What does the server think the consumer is doing?
kubectl -n hermes port-forward svc/nats 8222:8222 &
nats consumer info <stream> <consumer>
#   num_pending      → work queued and never delivered
#   num_ack_pending  → work delivered and never settled (handlers wedged)
#   num_waiting      → pull requests outstanding; zero here means the client stopped asking

# 4. CAPTURE BEFORE IT RESTARTS — the thing the last incident could not do.
kubectl -n hermes logs <pod> > /tmp/<pod>.log
kubectl -n hermes describe pod <pod> > /tmp/<pod>.describe
```

## Reading `num_pending` against `num_ack_pending`

The two halves point at different bugs, and the alert fires on their sum:

- **`num_pending` high, `num_ack_pending` ~0** — the client has stopped *fetching*. The pull
  consumer is not asking for messages. This is the shape of the original incident.
- **`num_ack_pending` at `MaxAckPending`, `num_pending` anything** — the client fetched and the
  handlers never returned. Every pool worker is stuck inside one message. Look for a provider call
  made without a context, or a lock held across an I/O call: `HandlerTimeout` only cancels a
  context, it cannot interrupt a handler that ignores it.
- **Both moving, alert firing anyway** — should not happen; a settled message resets the clock.
  If you see it, suspect the clock rather than the consumer and check for a paused detector
  (below).

## Mitigations

### Let it restart, or restart it now

`kubectl -n hermes rollout restart deploy/<service>` — after capturing the evidence above. This is
what cleared the incident. Redelivery covers the in-flight work.

### If every replica is stalled at once

Suspect the bus rather than the pods. Check `nats stream info <stream>`, the NATS pods' own health,
and whether a stream maintenance operation was running. Restarting the whole Deployment into a
broken bus achieves nothing, and the pods will restart-loop until NATS recovers.

Note that a NATS *outage* cannot cause this alert: an unanswerable poll counts as no evidence and
resets the window (`internal/messaging/stall.go`). If every consumer is stalled while NATS looks
healthy, the bus is doing something subtler than being down.

### If you need the probe off

`HERMES_NATS_CONSUMER_STALL_TIMEOUT=0` disables stall detection entirely, and the service returns
to a `/healthz` that is a constant 200. Do this only to stop a restart loop you cannot otherwise
break — it restores the blindness that let the original incident run for three hours.

## `stall detection paused` in the logs

Different problem, same file. The monitor could not read `CONSUMER.INFO` — usually a missing
`$JS.API.CONSUMER.INFO.<stream>.<consumer>` grant in `deploy/k8s/base/infra/nats-accounts.conf`.
Detection is off for that consumer until it is fixed, and `HermesConsumerStalled` cannot fire for
it. The permissions test
(`TestAccounts_EveryConsumerMayInspectItsOwnConsumer`) covers the committed file, so this points
at a cluster running an older or hand-edited accounts file.

## Escalation

- One service, repeatedly → that service's owner. It is a bug in a handler until proven otherwise.
- All consumers at once → platform on-call, treat as a bus incident.

## Post-incident

- **Attach whatever you captured to the open investigation.** The root cause is still unknown, and
  a single captured wedged process is worth more than the detection itself.
- If the cause was a handler that ignored its context, that provider call needs `ctx` threaded
  through — same fix as the `UngracefulShutdown` runbook's third cause.
- If the stall was legitimate backpressure (a consumer genuinely unable to keep up while every
  message sat in retry backoff), the window is too short for that workload: raise
  `HERMES_NATS_CONSUMER_STALL_TIMEOUT` rather than lowering it. A liveness probe that restarts
  a backing-off consumer is worse than the bug it detects.
