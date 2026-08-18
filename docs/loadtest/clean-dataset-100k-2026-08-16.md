# 100,000 connections: a clean dataset, and the index that fixed it

Two 100,000-connection runs on the OVH cluster, 2026-08-16. Run 3 (01:16–01:33 UTC) is step 2 of
the plan in [limiter-fix-verification-2026-08-15.md](limiter-fix-verification-2026-08-15.md):
reset the dataset and re-run, so latency can be attributed to the code rather than to table
growth. Run 4 (01:56–02:13 UTC) repeats it with one change — the index from
[issue #170](https://github.com/HermesNotifications/hermes/issues/170).

**Run 3 found the bottleneck. Run 4 removed it.** The read path was saturating the database node
on its own, while every stage of the delivery pipeline stayed fast. A single non-partial index
took `inbox_list_latency` p95 from 238 ms to **4 ms** and the database node from 92% CPU to 41%.

## The four runs

Runs 1, 3 and 4 all started against an empty `notifications` table and ended near 450,000 under
the same offered load, so they are directly comparable. Run 2 started with ~450,000 rows already
present and is included only for context.

| | Run 1 · 0.1.3 | Run 2 · 0.1.4 | Run 3 · 0.2.2 | **Run 4 · 0.2.2 + index** |
|---|---:|---:|---:|---:|
| `notifications` at start | empty | ~450k | empty | **empty** |
| `notifications` at end | ~450k | 916,581 | 450,024 | **450,037** |
| `http_req_failed` | 1.54% | 0.00% | 0.00% | **0.00%** |
| 429s on `/v1/inbox` | 6,705 | 0 | 0 | **0** |
| iterations | — | — | 534,879 | **539,962** |
| dropped iterations | — | — | 5,122 | **40** |
| pushes received | 404,881 | 414,455 | 434,975 | **435,123** |
| `inbox_list_latency` p95 | 83 ms | 731 ms | 238 ms | **4 ms** ✓ |
| `ws_push_e2e_latency` med | 8 ms | 2m36s | 806 ms | **6 ms** |
| `ws_push_e2e_latency` p95 | 176 ms | 3m49s | 1m22s | **1.06 s** ✗ |
| `send_ack_latency` p95 | 2 ms | 9 ms | 10 ms | **2 ms** |
| `ws_connect_latency` p95 | 31 ms | 29 ms | 33 ms | **33 ms** |
| probes lost | — | 31 | 9 | **0** |

Run 3 established that cleaning the dataset recovers most of run 2's regression — 731 ms to
238 ms — confirming the previous write-up's hypothesis. It also showed 238 ms was still 2.9x
run 1's 83 ms under identical conditions, which looked like a code regression. It was not. It was
a query whose cost grows with the table, and run 4 shows what happens once that cost is removed:
**4 ms, twenty times better than run 1 ever was.**

## What run 3 found

Delivery was never the problem. Measured from the database rather than from a client:

| `created_at` → `delivered_at` | Run 3 | Run 4 |
|---|---:|---:|
| p50 | 53 ms | 41 ms |
| p95 | 218 ms | 186 ms |
| p99 | 403 ms | 307 ms |
| max | 1.60 s | 1.25 s |
| delivered | 417,463 / 417,463 | **450,037 / 450,037** |

**Corrected 2026-08-17 — the completeness figure holds, the latency figures do not measure
delivery.** `delivered_at` is stamped by the event writer, which batches: `NewBatch(100,
500*time.Millisecond, …)` at `internal/eventwriter/writer.go:37`. So this column is delivery
*plus* however long the batch took to fill, and that term dominates at low event rates. The tell
is that it inverts against load — at 100 sends/s a later run measured p95 **335 ms** here while
k6 measured 17 ms end to end, because ~300 events/s needs ~333 ms to fill a 100-event batch; at
500 sends/s it falls to 115 ms. It is an upper bound contaminated by batching and is not
comparable across throughputs.

The trustworthy measurement is the trace. Walking raw span timestamps on a run-4 notification,
`POST /v1/send` starts at `.994488435` and Centrifugo's `/api/publish` completes at `.998712882`
— **4.2 ms for the entire pipeline including every NATS queue wait**. The per-hop spans agree:
`delivery.send` p95 4.6 ms, `POST /v1/send` p95 9.3 ms.

The conclusion this section draws is unchanged and if anything strengthened: delivery was never
the bottleneck. Only the number supporting it was wrong in kind.

The read path was the problem. In run 3, `GET /v1/inbox` p95 was 242 ms and **212 ms of it was
the unread-count query** — specifically its watermark subquery,
`coalesce((SELECT max(id) FROM notifications WHERE user_id = $1), '')`.

Every `user_id` index on `notifications` was partial (`WHERE archived_at IS NULL AND deleted_at
IS NULL`), and the planner cannot use a partial index for a predicate it does not imply. `max(id)`
is over *all* the user's rows, so the only option left was the primary key — and because ids are
time-sortable, that walks the table newest-first across every user:

```
Index Scan Backward using notifications_pkey
      Index Cond: (id IS NOT NULL)
      Filter: (user_id = '...')
```

The cost is proportional to how much the whole system has produced since that user's last
notification. Measured on the live 450,024-row table:

| `SELECT max(id) WHERE user_id = $1` | before index | after index |
|---|---:|---:|
| active user (prober) | 0.217 ms / 4 buffers | 0.087 ms / 5 buffers |
| **user with an empty inbox** | **133 ms / 378,799 buffers** | **0.169 ms / 3 buffers** |
| rows removed by filter | **450,053 — the whole table** | 0 (`Index Only Scan`, `Heap Fetches: 0`) |

A brand-new user with an empty inbox paid a full index walk on every inbox load, growing forever.
Uniform-random load tests cannot see this, because every user they pick is maximally recent — the
600x spread between those two rows is invisible to the scenario and entirely real in production.

## The fix, and what it moved

[Migration 000021](../../migrations/000021_notifications_user_id_index.up.sql) adds one
non-partial index, `(user_id, id DESC)`, 29 MB on a 450k-row table.

| | Run 3 | Run 4 |
|---|---:|---:|
| unread-count query p50 / p95 | 33.6 ms / 212.3 ms | **0.169 ms / 0.474 ms** |
| `GET /v1/inbox` span p50 / p95 | 35.0 ms / 241.9 ms | **0.89 ms / 2.75 ms** |
| `hermes-inbox` DB pool acquire waits | **17,429** | **33** |
| argon CPU peak | **11.05 / 12 cores (92%)** | **4.96 / 12 cores (41%)** |
| argon CPU shape | climbs monotonically | **flat** |
| offered throughput | collapsed 500/s → ~370/s | **held 500/s** |

448x at p95 on the query, 528x fewer pool waits, and the database node stopped being the
constraint. In run 3 argon climbed steadily and throughput collapsed 26% with 5,122 dropped
iterations; in run 4 argon was flat and 40 iterations dropped.

The synthetic prober is the independent confirmation: it lost nine consecutive probes in run 3
from 01:28:27 onward, and **zero** in run 4.

## Deliberately not partial

The index is also what makes user deletion viable. `notifications_user_id_fkey` is `ON DELETE NO
ACTION`, so deleting a user makes Postgres look for referencing rows — and a partial index cannot
answer that either. Without this index every user deletion sequentially scans `notifications`,
which is why clearing 200,000 orphaned seed users took ~90 minutes and wedged on lock contention.
GDPR erasure and account closure run the same path, so the index is justified independently of
the read path.

## Nothing crashed, in either run

Worth recording, because the failure mode is silent. Runners exit **99** — k6's "thresholds
crossed" code — with `restarts=0`, so the `BackoffLimitExceeded` on their Jobs is Kubernetes
reading that exit, not an independent fault. All 24 Hermes containers ran continuously across
both runs with zero restarts. No OOM kills, no evictions, nothing scheduled away from argon even
at 92% CPU.

At 100,000 connections this system degrades by getting slower, not by falling over.

## What is still open

- **`ws_push_e2e_latency` p95 is 1.06 s against a 1000 ms threshold** — the only failing one.
  Down from 1m22s, median now 6 ms. Followed up on 2026-08-17 (see [issue #172](https://github.com/HermesNotifications/hermes/issues/172));
  the summary is that no server-side measurement corroborates it. The trace puts the whole
  pipeline at 4.2 ms and the prober, holding a single socket in a Go process during run 4, saw
  p50 2.5 ms / p95 4.75 ms with 28 of 28 received.

  Four candidate mechanisms have since been **excluded by experiment**: sockets per VU (25 / 5 /
  1 — no effect), VUs per pod (40 / 200 / 1000 — no effect), sockets per k6 process (halving it
  made the tail *worse*), and CFS throttling (0.03 s across 16 pods). What remains is that the
  tail scales with offered send rate — at a constant 8,000 connections, 100/s gives p95 17 ms
  and 500/s gives 530 ms — and secondarily with connection count. The mechanism is unexplained.

  Two cautions for anyone picking this up. The prober was silently dead from 2026-08-16 05:00
  ([#173](https://github.com/HermesNotifications/hermes/issues/173)), so the 2026-08-17
  experiments had no independent control; only run 4's prober data predates the failure. And
  `delivered_at − created_at` cannot be used as the server-side comparison — see the correction
  above.
- **Why run 1 measured 83 ms where run 3 measured 238 ms** is unexplained. Run 1 shed 6,705
  requests as 429s, which would flatter its percentiles, but that is an argument rather than a
  measurement. The index makes the question academic — run 4 beats both by an order of magnitude.
- **`hermes-inbox` is still 1 replica with a 10-connection pool** (the `HERMES_DATABASE_MAX_CONNS`
  default) against `max_connections=100`. With 33 waits it is no longer urgent, but the headroom
  is still unused.

## Reproducing these runs

```bash
SCENARIO=inbox-mixed RUN_ID=<id> PARALLELISM=8 \
LOADSEED_IMAGE=ghcr.io/hermesnotifications/loadseed:5345adb \
LT_ORGANIZATIONS=10 LT_USERS=10000 \
CONNECTIONS=100000 WS_SOCKETS_PER_VU=25 WS_RAMP_SECONDS=60 \
SEND_RPS=500 POLL_RPS=100 TARGET_RPS=500 DURATION=15m \
CHANNEL_WEIGHTS=inbox:100 \
loadtest/scripts/run-k8s.sh
```

To re-run against an existing seeded population without re-seeding — which is how run 4 kept run
3's exact 100,000 users, so the index was the only variable — truncate the history and apply the
rendered TestRun directly rather than invoking the script:

```sql
TRUNCATE notification_events, notifications;
```

Two preconditions that are not in the script and cost a run each time they are missed:

- **Hermes must not be on the generator node.** `pool=loadtest-generators` keeps k6 on helium but
  nothing keeps Hermes off it, and a Flux reconcile or a rollout will scatter Hermes back onto
  helium. Cordon helium, restart the `hermes` namespace, then **uncordon** — leaving it cordoned
  strands every runner in `Pending`.
- **`run-k8s.sh` uses the ambient kubectl context.** Pin it per invocation
  (`kubectl config view --raw --minify --context=ovh > /tmp/kc && KUBECONFIG=/tmp/kc ...`) rather
  than switching the shared context.
