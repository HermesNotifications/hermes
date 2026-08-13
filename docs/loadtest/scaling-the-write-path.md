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

## Correction: concurrency, not storage, is the first lever

Everything below this heading was written before the write path was benchmarked directly on
this hardware. `cmd/dispatchbench`, run **inside the cluster** against the same
Longhorn-backed Postgres:

| dispatch workers | msgs/s |
|---:|---:|
| **8 (the default)** | 2,100 |
| 16 | 3,511 |
| 32 | 5,534 |
| 64 | **7,907** |

**3.8× throughput from a configuration change, on the storage that was supposed to be the
wall.** The raw fsync rate is 1,933/s, and dispatch is doing 7,907 notifications/s over it —
because Postgres amortises concurrent commits into shared flushes, and the amount of
amortisation scales with how many commits are in flight. Eight workers simply do not give it
enough to work with.

So the earlier conclusion — "the wall is fsync, and the fix is storage" — was half right. The
fsync rate bounds *un-amortised* commits. The pipeline was nowhere near that bound; it was
bounded by its own concurrency.

Two knobs, and they must move together:

- `HERMES_DISPATCH_CONCURRENCY` (default **8**)
- `pool_max_conns` in `HERMES_DATABASE_URL` (default **10**)

Worker count is clamped to the pool size by `ClampWorkersToPool`, so raising concurrency alone
does nothing except log a warning. Watch the server's `max_connections` (100 here) as replicas
multiply: 64 workers on one replica is already 64 of it.

**Neither knob is exposed by the Helm chart.** `grep` for `pool_max_conns` or
`DISPATCH_CONCURRENCY` in `charts/hermes/` returns nothing, so the highest-leverage tuning
available is unreachable for anyone installing the documented way. That is worth fixing before
any of the options below.

### Batching the inserts does not help — measured, both ways

Implemented and benchmarked. It is a **regression at every concurrency tested**, on both fast
and slow storage:

| | 8 workers | 32 workers |
|---|---:|---:|
| unbatched | 2,100 / 1,917 | 5,534 |
| batched | 1,606 (b=16), 1,697 (b=64) | 4,111 (b=32) — **−26%** |

The prediction was that batching would pay once flush latency dominated. It does not, and the
reason is the one the implementation flagged as its own risk: batching funnels every insert
through a single goroutine and a single connection, which destroys exactly the commit
concurrency that Postgres's group commit was using to amortise flushes. Explicit batching
replaces many concurrent commits with one serialised commit, and loses.

The mechanism ships behind `HERMES_DISPATCH_INSERT_BATCH_SIZE`, default `1` (off). Leave it
off. It is kept because the measurement is worth being able to repeat, not because there is a
configuration where it currently wins.

## Options, most leverage first

| lever | expected | cost | needs |
|---|---|---|---|
| Postgres on node-local storage | **~20× fsync headroom** | volume is node-bound; loses Longhorn's replication and reschedule survival | storage class + migration |
| Batch the notification inserts | multiplies whatever storage gives | larger failure blast radius per batch | code |
| Cache `EnsureOrganization` / `EnsureUser` | removes 1 WAL-reaching write per message, plus a Redis round trip | small — see the correction below | code |
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

Every message runs `EnsureOrganization` then `EnsureUser` before `CreateNotification`.

**An earlier draft of this document claimed that caching removes two of three write statements.
That was wrong, and implementing it is what showed why:**

- `EnsureOrganization` is *already* cached in production. `cmd/dispatch/main.go` wraps the
  store in `cached.NewOrganizationRepository`, so after the first message for an organization
  this is a Redis GET, not a Postgres upsert. Caching it in-process is still worth doing — it
  removes a network round trip from a worker that is holding a message, and one that can time
  out — but the fsync argument does not apply to it.
- `EnsureUser` is *two* round trips, not one: the upsert, and then `GetUserContacts`, because
  `routeAndDeliver` narrows channels to those the recipient has a contact point for. Contact
  points are mutable (`SetUserContact` / `UpdateUserContacts` in the user service), so caching
  them would mean mailing a stale address, or silently dropping a channel whose contact was
  added a minute ago, until an eviction happened to fix it — with no cross-replica
  invalidation. So the contacts read stays; only the upsert is cached away.

Net effect is therefore **one WAL-reaching statement removed per notification, plus one Redis
round trip** — real, but a third of what was originally projected. Which is the argument for
measuring the result rather than trusting this table.

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
