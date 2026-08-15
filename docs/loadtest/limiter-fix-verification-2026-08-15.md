# Verifying the limiter fix at 100,000 connections

Second 100,000-connection run on the OVH cluster, 2026-08-15, 17:02–17:22 UTC. Companion to
[realtime-scale-2026-08-14.md](realtime-scale-2026-08-14.md), which found the defect this run
exists to check.

**One result is clean and one is not, and the difference matters.** The rate limiter fix
([ADR 0024](../adr/0024-a-full-rate-limiter-fails-open-for-credentials.md)) is confirmed: the
429 storm is gone. The latency figures got dramatically worse, and **they are not comparable to
the previous run** — the database had roughly doubled in between, and nothing was reset.

## What changed since the last run

- Hermes **0.1.4**, deployed by Flux from the cluster's GitOps repository rather than by hand.
  The previous run's configuration had been applied with `helm upgrade` directly and was
  reverted at the next reconcile; that is the correct behaviour and the reason this one went
  through git.
- Telemetry on and the **synthetic prober enabled**, both from the released chart.
- Generators alone on helium, Hermes on argon and neon. Re-established before the run: a
  `rollout restart` does not move StatefulSet pods, so `hermes-redis-0` — the Centrifugo engine,
  and therefore on the realtime critical path — had to be deleted explicitly to get it off the
  generator node.

## The limiter fix works

Same load, same scenario, same 100,000-user population.

| | Run 1 — 0.1.3 | Run 2 — 0.1.4 |
|---|---:|---:|
| `http_req_failed` | 1.54% | **0.00%** |
| 429s on `/v1/inbox` | **6,705** | **0** |
| checks passed | 91.74% | **100.00%** |
| WS sessions | 100,000 | 100,000 |
| Sends (`202`) | 449,865 | 449,791 |
| Pushes received | 404,881 | 414,455 |

Zero refusals in 66,970 requests per runner. The same crossing of 50,000 distinct users that
previously produced a four-minute 429 storm now passes through without a single rejection.

This comparison is sound: identical offered load, and the outcome is binary — a request was
either refused or it was not.

## Two thresholds failed, and the comparison behind them is not sound

| | Run 1 | Run 2 | Threshold |
|---|---:|---:|---|
| `inbox_list_latency` p95 | 83 ms | **731 ms** | `p(95)<150` |
| `ws_push_e2e_latency` med | 8 ms | **2m 36s** | — |
| `ws_push_e2e_latency` p95 | 176 ms | **3m 49s** | `p(95)<1000` |
| `send_ack_latency` p95 | 2 ms | 9 ms | `p(99)<200` |
| `ws_connect_latency` p95 | 31 ms | 29 ms | `p(95)<500` |

The write path and the connection path are essentially unchanged. The read path and end-to-end
delivery are not.

**The database roughly doubled between the two runs, and nothing cleaned it up.** After run 2:

| Table | Rows |
|---|---:|
| `notifications` | 916,581 |
| `notification_events` | 2,749,738 |
| `users` | 200,002 |

Run 1 started against a freshly seeded database with no notifications. Run 2 started where run 1
finished — about 450,000 notifications and 1.3M events — and ended at the figures above. The
200,002 users include an orphaned 100,000 from a first seed pass whose manifest could not be
recovered.

So two things changed at once: the code, and the dataset. Attributing the latency to either one
from this run alone would be guessing.

## What the evidence does point at

Three observations, none of which depends on the run-to-run comparison:

**The inbox service did less work and was still far slower.** Completed polls fell from 89,403
to 55,594 — `constant-arrival-rate` drops iterations when the VU pool cannot keep up, and at
731 ms per iteration it could not. Fewer queries, each an order of magnitude slower, is a
datastore symptom rather than a load one.

**argon was CPU-saturated and the other two nodes were idle.** Nineteen samples through steady
state:

| Node | Median | Peak | What it runs |
|---|---:|---:|---|
| argon | **79%** | 86% | Postgres, a NATS replica, half of Hermes |
| helium | 15% | 19% | all 8 k6 runners |
| neon | 13% | 18% | Redis, two NATS replicas, half of Hermes |

This inverts the finding that dominated the earlier exercise. At 100,000 connections the load
generator is comfortably idle; the node running Postgres is the constraint. That is only legible
because the split was enforced, and it is the first time these runs have had a steady-state CPU
figure at full load at all — the previous write-up could only offer a ramp-phase sample.

**The prober lost every probe, independently of k6.** Its first real outing, and it did the job
it was added for: 31 lost probes at exactly 2 per minute — the full probe rate — from 17:06,
when steady state began, to 17:21, when the run ended.

```
{"level":"WARN","msg":"probes lost","count":1,"timeout":30000000000}
```

The prober is a different user, a different code path, and a different process from the load
generator. It reports send-to-socket delivery exceeding 30 seconds throughout, which corroborates
k6's 2m36s median from a source that shares nothing with it. Under the previous exercise's
hand-read methodology this signal did not exist.

The JetStream streams drained to zero within about 25 seconds of the load stopping, which is
what a backlog that clears looks like rather than a stall.

## What this run does not establish

- **Whether 0.1.4 is slower than 0.1.3.** Nothing here separates the code from the dataset. The
  limiter change itself touches one branch on the admission path and is not a plausible cause of
  minutes of queueing, but that is an argument, not a measurement.
- **Whether the read path degrades with table size.** It is the leading hypothesis — `notifications`
  and `notification_events` grew by roughly 2x and 2x respectively while `inbox_list_latency` p95
  grew 8.8x — but a hypothesis fitted to two points.
- The prober's own latency series were still not extracted from SigNoz; the loss count above
  comes from its logs.

## What would settle it

Reset the dataset and re-run, changing one thing at a time:

1. Clean the seeded data and the accumulated notifications, then re-seed to the same shape.
2. Re-run 100k on 0.1.4 against the fresh dataset. If latency returns to run 1's numbers, the
   dataset was the cause and the read path's scaling becomes the question worth its own run.
3. If it does not, the regression is in the code and bisecting 0.1.3 → 0.1.4 is cheap.

A deliberate read-path scaling run — the same load against 1x, 2x and 4x the notification volume
— would answer the more useful question directly, and is worth more than another repetition at a
single size.
