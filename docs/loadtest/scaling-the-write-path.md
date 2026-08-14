# Scaling the write path

How to get dispatch past ~2,000 notifications/s, and why adding replicas does not do it.

Measured on the OVH cluster, 2026-08-13. Companion to
[websocket-scale-2026-08.md](websocket-scale-2026-08.md).

> **Amended 2026-08-13**, after `cmd/dispatchbench` benchmarked the write path directly on
> this hardware. The original finding — "dispatch is not the bottleneck, WAL fsync latency on
> replicated storage is" — was wrong, and the section it was based on is preserved below under
> [What this looked like first](#what-this-looked-like-first) because the way it was wrong is
> more useful than the conclusion was.

## The finding

**Dispatch was bounded by its own worker pool, not by storage.** `cmd/dispatchbench`, run
inside the cluster against the same Longhorn-backed Postgres:

| dispatch workers | msgs/s |
|---:|---:|
| **8 (the default)** | 2,100 |
| 16 | 3,511 |
| 32 | 5,534 |
| 64 | **7,907** |

**3.8× throughput from one configuration value**, on the storage that was supposed to be the
wall. Postgres amortises concurrent commits into shared flushes, and the amount of
amortisation scales with how many commits are in flight; eight workers never gave it enough to
work with.

Two knobs, and they must move together:

- `HERMES_DISPATCH_CONCURRENCY` (default **8**) — `dispatch.concurrency` in the chart
- `pool_max_conns` in `HERMES_DATABASE_URL` (default **10**) — `dispatch.database.maxConns`

Worker count is clamped to the pool size by `ClampWorkersToPool`, so raising concurrency alone
does nothing but log a warning. Watch `max_connections` (100 by default) as replicas multiply:
64 workers on one replica is already 64 of it. `postgresql.maxConnections` raises it, and
`templates/_validate.tpl` does the arithmetic at render time.

## What this looked like first

The original measurement, and why it read as a storage ceiling:

| measurement | value |
|---|---:|
| pipeline throughput, 4 dispatch replicas | **2,006 notifications/s** |
| storage fsync rate (`pg_test_fsync`, fdatasync) | **1,933 ops/s @ 517 µs** |
| same test on node-local disk | **40,670 ops/s @ 25 µs** |
| Postgres CPU during the run | 2.5 of 24 cores |

Throughput and the fsync rate agreed to within 4%, which looked like confirmation and was
coincidence. **`pg_test_fsync` is single-threaded**: it issues one fsync, waits, issues the
next. `1 / 517 µs = 1,934/s`, so the ops/s figure is the reciprocal of the latency and the same
number twice. It says nothing about how many *concurrent* fsyncs the volume can absorb, which
is the quantity that actually bounds a worker pool — and which nobody measured.

That is why 7,907/s over "1,933 fsync/s storage" is not a contradiction. The volume was never
limited to 1,933 writes per second; it was limited to 1,933 *serialised* ones, and dispatch at
8 workers was not asking for more than that.

Scaling replicas looked like confirmation from the other side:

| dispatch replicas | sustained drain | per replica |
|---:|---:|---:|
| 1 | ~1,242/s | 1,242/s |
| 4 | ~2,006/s | ~500/s |

**Four times the replicas bought 1.6× the throughput.** That part still holds, and the reason
is unchanged: extra replicas add workers competing for the same fsync queue while consuming
`max_connections`. Raising the pool on one replica is the cheaper move.

## Why the storage is slow

Slow in *latency*, which is the distinction the section above turns on. Nothing here has been
shown to saturate.

The bundled Postgres uses a Longhorn volume with **3 replicas**. The engine and replicas are
userspace processes, and replication is synchronous to *all* replicas rather than a quorum, so
every WAL flush leaves the kernel, crosses a process boundary, goes over TCP to three nodes —
two of them remote — and commit latency is the slowest of the three. 517 µs is that round trip.
Node-local disk on the same hardware is 25 µs.

WAL is the worst-case IO shape for it: small writes with a forced fdatasync per commit, so the
fixed per-IO overhead amortises worst.

Also relevant, and all at defaults: `commit_delay=0` (no deliberate group-commit batching),
`shared_buffers=128MB`, `wal_sync_method=fdatasync`.

Note the chart already says the bundled datastores are for evaluation only. These numbers are a
property of *that* deployment, not of Hermes.

### What would actually establish contention

Nothing here has. To find out whether the volume has a saturation point at all:

- `fio` against the PVC with `--fsync=1` across an `--iodepth` sweep, idle and under load. Flat
  latency across depths means structural cost and there is nothing to fix; latency that climbs
  means something is contending.
- `pg_stat_wal` (`wal_sync_time / wal_sync`) and `pg_stat_activity` wait events, which
  `deploy/observability/base/exporters/postgres-exporter.yaml` now collects — the same
  measurement continuously and under real concurrency, rather than from a synthetic tool.
- `etcd_disk_wal_fsync_duration_seconds`. All three nodes are control-plane + etcd, and etcd is
  fsync-heavy; if it shares a device with the Longhorn data path, that is the one genuine
  contention candidate on the table.

## The lesson, separately from the number

The earlier conclusion — "the wall is fsync, and the fix is storage" — was half right in a way
worth naming, because the same mistake is easy to repeat with any storage benchmark.

The fsync rate bounds *un-amortised* commits. It is a real limit and it was measured correctly;
it simply was not the limit the pipeline was against. A single-threaded latency probe answers
"how long does one durable write take", and a worker pool is bounded by "how many can be in
flight at once" — two different questions whose answers happened to sit 4% apart, which read as
corroboration.

The general form: **a benchmark that does not reproduce the concurrency of the workload cannot
tell you the workload's ceiling**, however precisely it measures what it does measure.

> Resolved since: the chart now exposes both knobs. `dispatch.concurrency` and
> `postgresql.maxConnections` landed in `charts/hermes/values.yaml` (`eb694c7`), with
> `templates/_validate.tpl` refusing a pool that would be clamped or would exhaust the bundled
> Postgres at render time. This document previously said neither was reachable from the
> documented install path, which was true when it was written and is no longer.

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

The implementation is **not merged**. It is preserved on the branch
`experiment/dispatch-insert-batching` — complete, tested against real Postgres and DynamoDB,
correct on at-least-once and idempotency, and defaulted off. It is not on `main` because
merging 1,400 lines and a new store-interface method to carry a feature that measurably makes
things worse is a maintenance cost with no upside.

Recover it from that branch if the storage picture changes enough to invert the arithmetic —
but re-measure before trusting it, because the arithmetic was what predicted it would help
here, and it did not.

## Options, most leverage first

Reordered after the benchmark. Concurrency was previously absent from this table and storage
led it, which is the ranking the measurement inverted.

| lever | expected | cost | needs |
|---|---|---|---|
| **Raise `dispatch.concurrency` and the pool with it** | **3.8× (measured, 8→64)** | Postgres connections; raise `postgresql.maxConnections` | config |
| `commit_delay` tuning | uncertain, possibly 1.5–3× | none; no durability loss | config |
| Postgres on node-local storage | ~20× *fsync* headroom, not 20× throughput | volume is node-bound; loses Longhorn's replication and reschedule survival | storage class + migration |
| Cache `EnsureOrganization` / `EnsureUser` | removes 1 WAL-reaching write per message, plus a Redis round trip | small — see the correction below | code |
| More dispatch replicas | **1.6× for 4×** (measured) | more Postgres connections against `max_connections=100` | none |
| Batch the notification inserts | **−26% (measured)** — see below | — | — |
| `synchronous_commit=off` | ~10–20× | **notification loss**, not just durability — see below | config + a real decision |

### 1. Concurrency, and it is free

Covered under [The finding](#the-finding). One value, 3.8×, no code, no durability trade, and
now reachable from the chart. Do this before evaluating anything else in this table, because it
changes the baseline every other row is measured against.

### 2. `commit_delay`

The textbook lever for this exact shape — slow fsync, many concurrent committers. It makes a
committing backend pause briefly so more commits join the same flush, which delays the flush
without ever skipping it, so durability is unchanged. Rule of thumb is half the measured fsync
latency: `commit_delay = 250` (µs) against the 517 µs measured here, with `commit_siblings = 5`.

Unmeasured. `postgresql.extraSettings` in the chart makes it reachable, and `cmd/dispatchbench`
will settle it in an afternoon.

### 3. Storage is a smaller lever than it looks

20× more fsync headroom is real and it is not 20× more throughput — group commit already
recovers much of what serialised fsync latency costs, which is why 7,907/s runs over a volume
that does 1,933 serialised fsyncs a second. Treat this as raising a ceiling the pipeline has
not yet reached.

The catch is what `vps-gitops/apps/hermes/release.yaml` already documents: `local-path` binds a
volume to the node that first ran the pod and does not follow a reschedule. A genuine
availability trade — though for a database that is the *only* writer, node-local NVMe with
proper backups is defensible, and it is what most managed Postgres actually is.

If the database moves off the bundled chart entirely (the documented production path), this
question resolves itself. Note that it does not resolve in the direction people expect: EBS gp3
is network-attached replicated storage too, typically 1–2 ms per write against Longhorn's
517 µs. `io2` Block Express roughly ties it. The 25 µs figure lives on instance-store NVMe.

### 4. `synchronous_commit=off` is not safe here as written

Listed at ~10–20× and it is the largest number in the table, so the hazard is worth stating
where the number is.

`internal/messaging/nats.go` acks a message only after the handler returns, and the handler
returns when `CreateNotification` commits. With `synchronous_commit=off` that commit returns
*before* the WAL is durable — dispatch then acks, and NOTIFICATIONS is WorkQueue retention, so
the ack **deletes the message**. A crash inside the window (up to 3× `wal_writer_delay`,
~600 ms at defaults) loses the notification row *and* the only copy that could rebuild it.

Not corruption; Postgres stays consistent. Silent, unrecoverable notification loss, with
nothing to alert on, because the at-least-once design assumes the ack means "durably
persisted" and this setting quietly redefines it.

The safe version of the same trade is to scope it to the Event Writer. Events are an audit
trail and notification status is a derived rollup that only advances, so losing the tail of
those on a crash is recoverable in a way that losing the notification is not.

### 5. Batching looked like the right architectural fix, and was not

**Superseded by measurement — see [Batching the inserts does not
help](#batching-the-inserts-does-not-help--measured-both-ways) above.** Kept because the
argument was reasonable and is the one anyone will reach for next.

The reasoning was: dispatch persists one notification per transaction, so batching N per
transaction amortises the flush across N rows and multiplies whatever the storage provides. It
composes with every other option here. Dispatch is already shaped for it — a worker pool and a
prefetch buffer of 64, so messages already arrive in groups.

The caveat in the original text turned out to be the whole answer: Postgres already performs
*implicit* group commit, so concurrent workers' flushes are partly amortised today. That is why
2,006/s was achieved against 1,933 fsync/s rather than a third of it — and why explicit
batching, which funnels every insert through one goroutine and one connection, **destroys the
commit concurrency it was meant to exploit**. Measured at −26% at 32 workers.

The general shape: batching helps when the per-operation cost is fixed and serialisation is
free. Here serialisation was the expensive part, and the amortisation was already happening.

### 6. Cache the org and user lookups

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

### 7. What not to bother with

**Adding dispatch replicas.** Measured at 1.6× for 4×. It also consumes `max_connections`:
the pool is 10 per replica against a server limit of 100, and worker count is clamped to the
pool size (`ClampWorkersToPool`), so replicas buy connections faster than they buy throughput.
Raising the pool on one replica is the same lever without the connection cost.

## The operational point

At 6,000/s offered against four replicas, the dispatch consumer's backlog reached **764,173
messages** while `send_ack_latency` p95 read **60 ms** and `http_req_failed` was **0.00%**.

Send is a thin ingestion layer: it publishes to JetStream and returns, so it answers in
milliseconds whether or not anything downstream keeps up. **Ingestion latency cannot detect
this failure mode.** Alert on JetStream consumer lag.
