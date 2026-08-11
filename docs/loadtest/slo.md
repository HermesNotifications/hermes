# Load-test objectives

Where the numbers in the k6 `thresholds` blocks come from, and — more usefully — which of them
are measured and which are still guesses.

That distinction matters more than the numbers. A threshold nobody can justify gets raised the
first time it fails, which is the same as not having one. Each entry below says what it is
based on, so the next person can tell whether a failure means the system regressed or the
target was always wrong.

## Objectives

| SLI | Objective | Scenario | Basis |
|---|---|---|---|
| `send_ack_latency` | p99 < 200ms | send, inbox-mixed | **Measured.** Send is a thin ingestion layer — authenticate, dedupe, publish. It does no template or channel work. |
| `send_ack_latency` (churn) | p99 < 1000ms | churn | **Derived.** Allows for a pod being out and a retry landing elsewhere. |
| `inbox_list_latency` | p95 < 150ms, p99 < 400ms | inbox-mixed | **Estimated.** One keyset-paginated query plus a cached count. Set after ADR 0011 removed the uncached `COUNT(*)` from the path; needs a baseline run to confirm. |
| `inbox_list_latency` (churn) | p95 < 500ms, p99 < 2000ms | churn | **Derived** from the above, allowing for a drain in progress. |
| `ws_connect_latency` | p95 < 500ms | inbox-mixed | **Estimated.** TLS handshake, JWT verification, subscribe round trip. |
| `ws_push_e2e_latency` | p95 < 1s | inbox-mixed | **Measured**, but see the caveat below. |
| `ws_push_e2e_latency` (churn) | p95 < 3s | churn | **Derived.** |
| `ws_reconnect_duration` | p95 < 5s | churn | **Estimated** from centrifuge-js backoff (500ms min, 20s max, jittered). |
| `http_req_failed` | rate < 1% | inbox-mixed | Existing. |
| `http_req_failed` | rate < 0.1% **during a rolling restart** | churn | **The point of the churn scenario.** See below. |
| `http_req_failed` | rate < 0.5% | soak | Existing. |

> **Caveat on `ws_push_e2e_latency`:** until ADR 0012's change to `loadtest/lib/centrifugo.js`,
> the driver read `payload.notification_id` from every publication. An arrival is a
> `notification.new`, whose id field is `id`; only `inbox.updated` uses `notification_id`, and
> the load test generates none. So this metric recorded **nothing** while appearing in every
> summary as a configured threshold. Any historical figure for it is not evidence of anything.

## The churn criterion

`http_req_failed: rate < 0.001` during a rolling restart is the assertion ADR 0012 exists to
satisfy, and it is deliberately an order of magnitude stricter than the steady-state 1%. The
claim is not that Hermes mostly survives a restart; it is that a rolling restart is invisible to
callers.

Run `loadtest/scripts/run-churn.sh` against a build without ADR 0012's changes and it fails:
there was no readiness drain, so SIGTERM raced endpoint removal and every in-flight and
newly-routed request in that window was reset. Run it after and it passes. That delta is the
test — a churn run in isolation says much less than a churn run compared against a baseline, so
capture both when investigating a regression.

The script fails if no restart completed during the run. A churn scenario in which nothing
churned passes trivially, which is the failure mode most likely to go unnoticed.

## Baselines that do not exist yet

Several thresholds above are marked estimated because nothing has measured them. They are
cheap to establish with the existing harness and each one is an input to a scaling decision:

1. **Inbox read QPS per pod** at 50m/64Mi — sets the HPA target and the replica count.
2. **Websocket connections per Centrifugo pod**, by memory — sets a connection-count HPA target,
   which is the right autoscaling signal for that tier (an idle-but-connected socket costs
   almost no CPU, so a CPU HPA under-provisions until a push storm and, worse, scales *down*
   while pods hold thousands of live sockets).
3. **End-to-end push latency against connection count** — tells you where the tier degrades
   rather than merely that it does.
4. **Reconnect throughput ceiling** across ingress, Centrifugo and Redis — bounds how fast a
   restarted pod's sockets can come back, which is what makes a scale-down policy safe.

Until (2) exists, Centrifugo autoscaling is deliberately not enabled: a scale-down disconnects
every socket on the pod it removes, so a policy tuned by guesswork manufactures the reconnect
storm it was meant to survive.

## Running

```bash
make infra-up            # or a cluster, for the churn scenario

# Steady state
k6 run loadtest/scenarios/inbox-mixed.js

# With disruption — needs a cluster, and restarts pods in $NAMESPACE
DURATION=10m VUS=200 ./loadtest/scripts/run-churn.sh
```

Cluster-scale runs go through the k6 operator; see [loadtest/README.md](../../loadtest/README.md).
