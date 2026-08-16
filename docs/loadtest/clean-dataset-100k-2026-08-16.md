# 100,000 connections against a clean dataset

Third 100,000-connection run on the OVH cluster, 2026-08-16, 01:16–01:33 UTC. This is step 2 of
the plan in [limiter-fix-verification-2026-08-15.md](limiter-fix-verification-2026-08-15.md):
reset the dataset and re-run, so that latency can be attributed to the code rather than to table
growth.

**The dataset hypothesis was right, and it was not the whole story.** Cleaning the data recovered
most of the previous run's regression. What remains is a read path that saturates the database
node on its own, and it did so while every stage of the delivery pipeline stayed fast.

## The comparison is sound this time

Run 1 and run 3 both started against an empty `notifications` table and both ended near 450,000
notifications under the same offered load. That is the controlled pair; run 2 is included for
context but started with ~450,000 rows already present.

| | Run 1 · 0.1.3 | Run 2 · 0.1.4 | **Run 3 · 0.2.2** |
|---|---:|---:|---:|
| `notifications` at start | empty | ~450k | **empty** |
| `notifications` at end | ~450k | 916,581 | **450,024** |
| WS sessions | 100,000 | 100,000 | **100,000** |
| `http_req_failed` | 1.54% | 0.00% | **0.00%** |
| 429s on `/v1/inbox` | 6,705 | 0 | **0** |
| checks passed | 91.74% | 100.00% | **100.00%** |
| pushes received | 404,881 | 414,455 | **434,975** |
| `inbox_list_latency` p95 | 83 ms | 731 ms | **232 ms** |
| `ws_push_e2e_latency` p95 | 176 ms | 3m49s | **1m22s** |
| `send_ack_latency` p95 | 2 ms | 9 ms | **10 ms** |
| `ws_connect_latency` p95 | 31 ms | 29 ms | **34 ms** |

Cleaning the dataset took `inbox_list_latency` p95 from 731 ms back to 232 ms, which confirms the
leading hypothesis of the previous write-up. But 232 ms is still **2.8x run 1's 83 ms under
identical dataset conditions**, so table size is not a complete explanation.

The limiter fix ([ADR 0024](../adr/0024-a-full-rate-limiter-fails-open-for-credentials.md)) holds
at 100,000 connections: zero refusals in 534,879 requests.

## Delivery is fast and complete; the read path is not

Measured from the database rather than from a client, so it depends on neither k6 nor the prober:

| `created_at` → `delivered_at` | |
|---|---:|
| p50 | 53 ms |
| p95 | 218 ms |
| p99 | 403 ms |
| max | 1.60 s |
| delivered | **417,463 / 417,463 — 100%** |

Flat minute by minute across the whole run, with no degradation. The spans agree: `delivery.send`
p95 4.6 ms, `POST /v1/send` p95 9.3 ms, `worker-inbox nats.consume` p95 4.8 ms. Nothing in the
write path or the delivery path is slow.

The read path is a different story. `GET /v1/inbox` p95 is 242 ms, and **212 ms of that is the
unread-count query** — the watermark subquery from
[issue #170](https://github.com/HermesNotifications/hermes/issues/170), reproducing at scale
exactly as predicted. Its cost rises with how much newer data exists than the queried user's last
notification, so it gets worse as the table fills *during the run*.

## What actually failed: argon saturated, progressively

CPU by node through steady state (cores; argon and neon have 12, helium ~24):

| Node | Start | Peak | Runs |
|---|---:|---:|---|
| **argon** | 6.86 | **11.05 (92%)** | Postgres, Redis, inbox, send, dispatch, prober, worker-events |
| helium | 4.25 | 4.88 (20%) | all 8 k6 runners, plus SigNoz |
| neon | 1.53 | 1.53 (13%) | Centrifugo, NATS, worker-inbox, worker-email |

argon climbs monotonically rather than plateauing, which is the signature of a cost that grows
with the data rather than with the load. The driver is visible in one metric: **17,429 DB pool
acquire waits on `hermes-inbox`**, against ~0 for every other service.

`hermes-inbox` runs **1 replica with a 10-connection pool** (the `HERMES_DATABASE_MAX_CONNS`
default) against a Postgres configured for `max_connections=100`. The headroom is unused.

The consequence was measurable in the offered load itself:

| Window | notifications/min | rate |
|---|---:|---|
| 01:17–01:24 | ~30,000 | 500/s (target) |
| 01:25–01:26 | 28,139 → 25,469 | falling |
| 01:27–01:31 | ~22,000 | **~370/s** |

A 26% collapse in throughput with 5,122 dropped iterations. The synthetic prober began losing
probes at 01:28:27 and lost nine consecutively to the end of the run, having been clean for the
first ten minutes. Its socket never dropped — there is no re-subscribe in its logs — so this is
delivery degradation, not a reconnect artefact.

## Nothing crashed

Worth recording, because the failure mode is silent. All 8 k6 runners exited **99**, which is
k6's dedicated "thresholds crossed" code, with `restarts=0` — none was killed and retried
mid-run. The `BackoffLimitExceeded` warnings on their Jobs are Kubernetes reading that non-zero
exit as a failed Job, not an independent fault. All 24 Hermes containers ran continuously across
the entire run with zero restarts during it. No OOM kills, no evictions, nothing scheduled away
from argon at 92% CPU.

At 100,000 connections this system degrades by getting slower, not by falling over.

## What this run does not establish

- **Why run 3 is 2.8x run 1 on the read path.** The dataset is controlled for; the code is not.
  0.2.2 also runs full OpenTelemetry tracing, which run 1 may not have had, and that is a cheap
  A/B to settle.
- **Where `ws_push_e2e_latency`'s 1m22s p95 comes from.** It is not corroborated server-side
  (218 ms p95, 100% delivered) and Centrifugo's own counters show 435,050 publications sent
  against 434,975 received by k6. But that counter increments when a publication is written to
  the transport, not when the client reads it, so it cannot rule out socket-level backpressure at
  100,000 connections. Two client-side measurements disagree with two server-side ones. This
  needs its own investigation rather than a guess.

## What to do next

1. **Index for [#170](https://github.com/HermesNotifications/hermes/issues/170).** A non-partial
   `(user_id, id DESC)` index removes the watermark scan, which is the largest single component
   of the read path and the thing driving argon's climb. It independently fixes the FK-check scan
   on user deletion.
2. **Raise `hermes-inbox`'s pool and replica count.** 17,429 acquire waits against a 10-connection
   pool and 100 available server-side connections is the cheapest available win.
3. **Re-run with tracing off** to separate instrumentation overhead from the 2.8x.
4. **Investigate the push tail** as its own exercise, instrumenting the Centrifugo-to-client hop.

## Reproducing this run

```bash
SCENARIO=inbox-mixed RUN_ID=<id> PARALLELISM=8 \
LOADSEED_IMAGE=ghcr.io/hermesnotifications/loadseed:5345adb \
LT_ORGANIZATIONS=10 LT_USERS=10000 \
CONNECTIONS=100000 WS_SOCKETS_PER_VU=25 WS_RAMP_SECONDS=60 \
SEND_RPS=500 POLL_RPS=100 TARGET_RPS=500 DURATION=15m \
CHANNEL_WEIGHTS=inbox:100 \
loadtest/scripts/run-k8s.sh
```

Two preconditions that are not in the script and cost a run each time they are missed:

- **Hermes must not be on the generator node.** `pool=loadtest-generators` keeps k6 on helium but
  nothing keeps Hermes off it, and a Flux reconcile or a rollout will scatter Hermes back onto
  helium. Cordon helium, restart the `hermes` namespace, then **uncordon** — leaving it cordoned
  strands every runner in `Pending`.
- **`run-k8s.sh` uses the ambient kubectl context.** Pin it per invocation
  (`kubectl config view --raw --minify --context=ovh > /tmp/kc && KUBECONFIG=/tmp/kc ...`) rather
  than switching the shared context.
