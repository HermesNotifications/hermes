# Scaling the write path

How to get dispatch past ~2,000 notifications/s, and why adding replicas does not do it.

Measured on the OVH cluster, 2026-08-13. Companion to
[websocket-scale-2026-08.md](websocket-scale-2026-08.md).

## The finding

**Dispatch is not the bottleneck. WAL fsync latency on replicated storage is.**

| measurement | value |
|---|---:|
| pipeline throughput, 4 dispatch replicas | **2,006 notifications/s** |
| storage fsync rate (`pg_test_fsync`, fdatasync) | **1,933 ops/s @ 517 µs** |
| same test on node-local disk | **40,670 ops/s @ 25 µs** |
| Postgres CPU during the run | 2.5 of 24 cores |

Throughput and the storage's fsync rate agree to within 4%. That is the whole story: with
`synchronous_commit=on`, a committing transaction cannot return until its WAL is durable, and
this volume can make WAL durable 1,933 times a second.

Scaling dispatch confirms it from the other side:

| dispatch replicas | sustained drain | per replica |
|---:|---:|---:|
| 1 | ~1,242/s | 1,242/s |
| 4 | ~2,006/s | ~500/s |

**Four times the replicas bought 1.6× the throughput.** Extra replicas add workers competing
for the same fsync queue. Postgres never exceeded 2.5 of 24 cores, so nothing was
CPU-starved — the workers were waiting on disk.

## Why the storage is slow

The bundled Postgres uses a Longhorn volume with **3 replicas**. Every WAL flush is a
synchronous write to three nodes across the network before Postgres may acknowledge the
commit. 517 µs is that round trip. Node-local disk on the same hardware is 25 µs.

Also relevant, and all at defaults: `commit_delay=0` (no deliberate group-commit batching),
`shared_buffers=128MB`, `wal_sync_method=fdatasync`.

Note the chart already says the bundled datastores are for evaluation only. This number is a
property of *that* deployment, not of Hermes — but it is the deployment most self-hosters will
start from, and 2,000/s is where it lands.

## Options, most leverage first

| lever | expected | cost | needs |
|---|---|---|---|
| Postgres on node-local storage | **~20× fsync headroom** | volume is node-bound; loses Longhorn's replication and reschedule survival | storage class + migration |
| Batch the notification inserts | multiplies whatever storage gives | larger failure blast radius per batch | code |
| Cache `EnsureOrganization` / `EnsureUser` | removes 2 of 3 write statements per message | negligible — both are effectively append-only | code |
| `commit_delay` tuning | uncertain, possibly 1.5–3× | none; no durability loss | config |
| More dispatch replicas | **1.6× for 4×** (measured) | more Postgres connections against `max_connections=100` | none |
| `synchronous_commit=off` | ~10–20× | loses recently-committed transactions on crash | config + a real decision |

### 1. Storage is the single biggest lever

21× more fsync headroom, and it needs no code. The catch is exactly what
`vps-gitops/apps/hermes/release.yaml` already documents: `local-path` binds a volume to the
node that first ran the pod and does not follow a reschedule. That is a genuine availability
trade, not a free win — but for a database that is the *only* writer, node-local NVMe with
proper backups is a defensible posture, and it is what most managed Postgres actually is.

If the database moves off the bundled chart entirely (the documented production path), this
question resolves itself.

### 2. Batching is the right architectural fix

Dispatch currently persists one notification per transaction. Batching N per transaction
amortises the flush across N rows and multiplies whatever the storage provides. It composes
with every other option here.

One caveat worth measuring rather than assuming: Postgres already performs *implicit* group
commit, so concurrent workers' flushes are partly amortised today — which is why 2,006/s is
achieved against 1,933 fsync/s rather than a third of it. The gain from explicit batching is
real but should be measured, not projected.

Dispatch is already shaped for this: it has a worker pool (`HERMES_DISPATCH_CONCURRENCY`,
default 8) and a prefetch buffer (`HERMES_DISPATCH_PREFETCH`, default 64), so messages are
already arriving in groups.

### 3. Cache the org and user lookups

Every message runs `EnsureOrganization` then `EnsureUser` before `CreateNotification`. Both
are `INSERT ... ON CONFLICT DO NOTHING` round trips that write nothing after the first time
for a given organization or user — and organizations are few while users repeat constantly.
An in-process cache removes two of the three write statements per notification.

### 4. What not to bother with

**Adding dispatch replicas.** Measured at 1.6× for 4×. It also consumes `max_connections`:
the pool is 10 per replica against a server limit of 100, and worker count is clamped to the
pool size (`ClampWorkersToPool`), so replicas buy connections faster than they buy throughput.

## The operational point

At 6,000/s offered against four replicas, the dispatch consumer's backlog reached **764,173
messages** while `send_ack_latency` p95 read **60 ms** and `http_req_failed` was **0.00%**.

Send is a thin ingestion layer: it publishes to JetStream and returns, so it answers in
milliseconds whether or not anything downstream keeps up. **Ingestion latency cannot detect
this failure mode.** Alert on JetStream consumer lag.
