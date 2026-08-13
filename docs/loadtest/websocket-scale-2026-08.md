# WebSocket connection scale — OVH cluster, 2026-08-12

How far the realtime path scales: concurrent WebSocket connections held against Centrifugo while
notifications flow through the whole `send → dispatch → worker-inbox → Centrifugo` pipeline.

**Headline: 250,000 concurrent WebSocket connections held across six Centrifugo replicas, with
connect latency at 16ms p95, push latency at a 5ms median, and zero failed requests. Sustained
6,000 sends/s against them with no errors. Hermes was not the limit at any point in this
exercise — at 250k its nodes sat at 11–24% CPU, and the throughput ceiling that did appear was
a rate-limit setting, not capacity.**

**Every degradation observed here was eventually traced to the load generator — four separate
times.** That is the single most useful thing in this document. Each one looked like a server
limit, and each was measuring the instrument: blocking VUs starving the scheduler, a client
timing its own busy event loop, a thundering herd of simultaneous connects, and a VU pool
sized by the wrong unit. The numbers below are the ones that survived a second vantage point.

## System under test

Chart `hermes-0.1.2`, helm revision 14, on a 3-node k3s cluster (helium 24 vCPU, argon 12, neon 12;
64Gi each, all three control-plane + etcd). Every Hermes service at **1 replica**, requests
`50m`/`64Mi`, limit `256Mi` memory, no CPU limit, no HPA. All Hermes pods on helium.

Centrifugo v6.6.2, **1 replica, `engine.type: memory`, no resource requests or limits**.
Bundled in-cluster Postgres 16, Redis 7, and a 3-pod NATS with `HERMES_NATS_STREAM_REPLICAS=1`.

Load generated in-cluster by k6-operator on argon and neon, talking to ClusterIP services — no
Traefik and no Tailscale in the measurement. Hermes app telemetry was **off** for the whole run
(see [the OTel note](#observability-during-this-run)), so server-side figures come from
kubeletstats and Centrifugo's own `:9000` Prometheus endpoint.

## Results

`inbox-mixed`, 300 sends/s and 50 inbox polls/s held constant at every step so the only variable
is connection count. 3–4 minutes per step.

| Connections | push e2e p95 | send ack p95 | ws connect p95 | Centrifugo CPU | Centrifugo mem | helium CPU | failed reqs |
|---:|---:|---:|---:|---:|---:|---:|---:|
| 200 | 9 ms | 2 ms | 0.11 s | ~1 m | 59 Mi | 3 % | 0 |
| 1,000 | 10 ms | 2 ms | 0.18 s | 60 m | 130 Mi | 11 % | 0 |
| 2,500 | 9 ms | 2 ms | 0.23 s | 75 m | 236 Mi | 11 % | 0 |
| 5,000 | 9 ms | 2 ms | 0.30 s | 87 m | 425 Mi | 11 % | 0 |
| 10,000 | 8 ms | 2 ms | 0.70 s | 136 m | 624 Mi | 11 % | 0 |
| 25,000 | 10–15 ms | 2 ms | 2.5 s | 229 m | 1.47 Gi | 14 % | 0 |
| 50,000 | 149 ms | 2 ms (max 7.3 s) | 5.7 s | 330 m | 2.6 Gi | 25 % | 0 |

Centrifugo's own `centrifugo_node_num_clients` confirmed the connection count exactly at every
step. **`http_req_failed` was 0.00% at every step, including 50,000.**

Derived costs, roughly linear across the range: **~52 KiB of Centrifugo memory per connection**
and **~6.6 m CPU per 1,000 connections** at this send rate.

## What this says

**Push latency is flat and fast until very late.** 8–10ms at p95 from `POST /v1/send` to the frame
arriving on the socket, unchanged from 200 to 25,000 connections. It degrades at 50,000
(149ms p95) — a 15× jump for a 2× connection increase, so that is where the knee is.

**Nothing in Hermes was close to saturated.** At 25,000 connections Centrifugo used 0.23 of one
core. The service that was expected to fall over first — `hermes-inbox` on a 256Mi limit — was
never OOM-killed, and no pod restarted once.

**Connection *establishment* is the first thing to bend, and it bends early.** `ws_connect_latency`
crosses its 500ms threshold at 10,000 and reaches 5.7s at 50,000. This is a connection *storm*
— every VU dialling at once — not a steady-state property, so it models a mass reconnect (a
Centrifugo restart, a network blip) rather than normal operation. It is the realistic failure
mode to care about, precisely because a single-replica Centrifugo guarantees one on every deploy.

**The 50,000 figure is a floor, not a ceiling, and it is not certain the SUT was the limit.** At
that step the generator was 20 pods each holding 2,500 blocking VUs at ~1.8Gi, and k6's `k6/ws`
module is one OS-level socket per VU. The connect-latency and send-ack tails may well be
generator-side scheduling rather than Hermes.

*Resolved in Run C: they were.* Node metrics showed the generator hosts at a load average of 10.1
against 12 cores while using 3.4, and moving to the async websockets module halved connect latency
at 50k without touching Hermes.

## Run B — Redis engine and three replicas

The baseline made the case for changing Centrifugo, but not the case the plan expected. One pod
was never the throughput constraint. The constraint was that `engine.type: memory` makes more than
one replica *silently wrong*, so Hermes could not run a second Centrifugo at all — and a single
replica means every rollout drops every connection at once.

Switched to `engine.type: redis` against the existing `hermes-redis`, `replicaCount: 3`, plus
resource requests and limits (the chart ships Centrifugo with none, so it scheduled BestEffort and
would be first evicted under node pressure).

**Cross-replica fan-out works.** At 10,000 connections spread over three pods (~3,325 each, one per
node), push throughput was **208/s against 210/s for the single-pod baseline** — statistically
unchanged. Had the broadcast not crossed pods, only publications landing on the same replica as
the recipient would arrive, or roughly a third.

**A rolling restart is now survivable.** `churn`, 3,000 connections, with a genuine
`kubectl rollout restart deploy/hermes-centrifugo` completing mid-run (37s, one pod at a time):

| | result | threshold |
|---|---:|---|
| `http_req_failed` | **0.00%** | `rate<0.001`, abortOnFail |
| `ws_push_e2e_latency` p95 | **8–9 ms** | `p(95)<3000` |
| `send_ack_latency` p95 | 2 ms | `p(99)<1000` |
| `ws_connection_drops` | ~4,000 total | bounded, not zero |

Every threshold passed. Sockets on a restarting pod drop and reconnect — unavoidable, the pod is
going away — but HTTP traffic saw no failures at all, and **push latency across the restart was
indistinguishable from steady state**. Under the previous single-replica memory-engine config the
same restart would have dropped 100% of connections simultaneously with no peer to absorb them.

The cost of the change is that the single bundled Redis is now on the realtime critical path as
well as being the template cache and idempotency store. It had no metrics endpoint at all, which
is addressed by the new `redis.metrics.enabled` exporter sidecar in the chart — off by default,
and worth turning on wherever the Redis engine is in use.

## Run C — 100,000 connections, and what the load generator was hiding

Two changes were needed to get here, and the second one invalidates part of what Runs A and B
appeared to show.

### The generator had to stop being the bottleneck

`k6/ws` blocks a VU for the life of its socket, so 50,000 connections meant 50,000 VUs. Node
metrics from Run A show what that cost: the generator hosts ran at a **load average of 10.1
against 12 cores while consuming 3.4 cores** — runnable threads waiting for a scheduler slot, not
work — and the peak coincided exactly with the latency tails being attributed to Hermes, whose own
node was at 38%.

Rewriting `lib/centrifugo.js` onto the async `k6/websockets` module decoupled connections from VUs
(`WS_SOCKETS_PER_VU`). At 10,000 connections the same test went from 1,100Mi to **385Mi** per
runner pod and from 10,000 VUs to 400. At 50,000, `ws_connect_latency` p95 fell from **5.7s to
2.6s** with no change to Hermes at all.

### At 100k, the client was still measuring itself

| Connections | Centrifugo CPU / replica | Centrifugo mem / replica | Hermes node CPU | failed reqs |
|---:|---:|---:|---:|---:|
| 10,000 | ~50 m | 250 Mi | 12–19 % | 0 |
| 50,000 | ~100 m | 1.2 Gi | 24–30 % | 0 |
| 100,000 | ~150–185 m | 1.8–2.0 Gi | 3–13 % | 0 |

Measured *from the socket-holding pods*, latency looked bad at 100k: `inbox_list_latency` p95
693ms, `ws_push_e2e_latency` p95 ~400ms. But every server-side number contradicted it — Redis at
**0.15 cores and 3,000 ops/s**, the inbox pod at **50m CPU and 50Mi against a 256Mi limit**, and
the actual inbox query at **0.25ms** under `EXPLAIN ANALYZE` on an index built for it
(`idx_notifications_inbox`), over a table that had grown to 631k rows.

So the API was measured again from a **separate runner pod holding no sockets**, while 99,522
connections were held by the others:

| Measured from | `inbox_list_latency` p95 | `send_ack_latency` p95 | requests | failures |
|---|---:|---:|---:|---:|
| Pods holding 10k sockets each | 693 ms | 2 ms (max 9 s) | — | 0 |
| **A pod holding none** | **2 ms** | **2 ms** | 42,002 | **0** |

The degradation was entirely in the measuring process. Hermes served the REST API at 2ms p95
while carrying ~100k concurrent WebSocket connections.

The same caveat now applies to `ws_push_e2e_latency` at high connection counts: its **median stayed
at 6ms** from 200 connections to 100,000, and only the tail moved. Given the p95 of a co-resident
HTTP request was inflated ~350×, the push tails at 50k and 100k should be read as *not yet
measured* rather than as server latency. The medians are trustworthy; the tails are not.

### What would actually be needed to find the ceiling

Nothing in Hermes was near saturation at 100k. To find a real limit you would need to remove the
remaining generator artifacts first: run socket-holding and traffic-generating pods separately (as
the probe above does), and add generator nodes rather than density. Centrifugo's own consumption
— roughly **60 KiB and 5m CPU per 1,000 connections per replica** — suggests a single 4Gi replica
carries ~65k connections, so the three-replica tier as configured has headroom well past 200k
before memory becomes the binding constraint.

## Run D — 250,000 connections, and the publication-rate ceiling

Six Centrifugo replicas (temporarily; ~2.5Gi each against the 4Gi limit, where three would
have needed ~5Gi and been OOM-killed).

**250,000 concurrent connections, every threshold green:**

| | |
|---|---:|
| connections established | **250,000** of 250,000 |
| `ws_connect_latency` p95 | **16 ms** |
| `send_ack_latency` p95 | **1–2 ms** |
| `ws_push_e2e_latency` | median **5–6 ms**, p90 8 ms |
| `http_req_failed` | **0.00%** across all 20 runner pods |
| Centrifugo | ~150m CPU, 1.5–2.1 Gi per replica |
| node CPU | 11–24% |

### The first attempt failed, and the reason is worth more than the number

Opening all 250k at once plateaued at **162,477** connections with `ws_connect_latency` at
**30 seconds** and up to 18% HTTP failures. It reads exactly like a server limit. It was a
thundering herd: `constant-vus` starts every VU simultaneously, the generator hosts hit 80%
CPU in the first seconds, and a socket that fails to open never retries inside its iteration
— so the shortfall is permanent. The server side sat at 3–10% throughout.

Adding a 150-second connection ramp (`WS_RAMP_SECONDS`) changed nothing else and took
`ws_connect_latency` p95 from **30s to 16ms**, a factor of ~2000, with all 250k established.

Real systems do get their whole population at once — after an outage. That case is the churn
scenario's job, and it should be measured deliberately rather than by accident.

### Publication rate, with 250k connections held

Measured from pods holding no sockets, against the held connections:

| offered | accepted | `send_ack` p95 | `inbox_list` p95 | note |
|---:|---:|---:|---:|---|
| 3,000/s | 100% | 130 ms | 41 ms | 4 send replicas |
| 6,000/s | **100%** | 138 ms | 58 ms | ~160k requests/pod, zero errors |
| 12,000/s | **~56%** | 297 ms | 160 ms | ~500–610 errors/s per pod |

**The ceiling is configuration, not capacity.** `rateLimit.distributed` is unset, so the
per-credential limit applies *per replica*: one send pod caps at 2000 rps and rejected 44.7%
of a 3000/s load; four replicas moved the same wall to ~8000/s. Underneath it, Postgres
peaked at **2.5 cores** and Redis at **0.7** — neither close to saturated on a 24-core node.

To go past it: raise `rateLimit.perSecond`, enable distributed rate limiting so the budget is
shared rather than multiplied, or add replicas.

### A third generator artifact

An 8,000/s step OOM-killed every runner pod in 14 seconds. `preAllocatedVUs` was sized as one
VU per requested rps, which assumes each VU completes one iteration per second — but a send
returns in ~2ms, so the pool was over-allocated by two orders of magnitude. Invisible below
~1000/s and fatal above it, and it presents as the system refusing load. Now sized from
throughput per VU.

## Limits worth knowing before trusting these numbers

- The table above is the ceiling of **one Centrifugo pod on the memory engine**, which is how the
  cluster was configured when it was measured. Run B changed that; the per-connection costs still
  apply per replica.
- The send rate was held at 300/s throughout. This measures **connection scale**, not ingestion
  throughput; `send_ack` p95 never moved off 2ms, so the write path was idle by comparison.
- Users were reused above 20,000 connections in the 25k step (20k seeded), so some users held two
  sockets. The 50k step was re-seeded to 60k users and had 49,705 distinct users.

## Observability during this run

Hermes app telemetry was off, so there are no Hermes spans or metrics in SigNoz for this window.
`observability.enabled: false` is a deliberate workaround: with it on, every service crash-loops
on

```
observability init failed: build resource: error detecting resource:
user: Current requires cgo or $USER set in environment
```

because `resource.WithProcess()` includes a process-owner detector that calls `os/user.Current()`,
which cannot work in a CGO-disabled distroless image. A fix exists on an unmerged branch
(`fix-observability-process-owner`); tag `v0.1.2` does not contain it.

The `loadtest` namespace was excluded from SigNoz log collection for the duration, to keep k6's
output from becoming the dominant log source on a shared cluster.

## Harness correctness — read this before comparing to older numbers

`ws_push_e2e_latency` in this document is **the first real measurement this metric has produced.**
Four defects were fixed to get here, three of which caused it to be evaluated over zero samples
while reporting a green threshold:

1. The seeder wrote the **internal** user id into the manifest and the scenarios sent it as
   `to.user_id`, which dispatch resolves as an **external** id. Every send minted a duplicate user
   and pushed to a channel nobody was subscribed to.
2. The send and socket scenarios selected users by `__VU`. Under k6 execution segments `__VU` is
   per-instance and the two scenarios' VUs interleave, so they addressed **disjoint** users.
3. The send→receive timestamp was handed over in a module-scope `Map`, but a socket VU is parked
   inside `ws.connect()` and never runs the send function — written by one VU, read by another.
   The timestamp now travels on the notification's `metadata`, which dispatch echoes to the client.
4. The manifest enumerated every user: 1.9MB at 20k users and ~10MB at the seeder's own 100k
   default, which cannot be mounted as a Secret. Users are now derived from counts. Materialising
   that list at module scope also gave every VU its own copy of the population and OOM-killed the
   runners at 500 VUs.

`ws_push_received: ['count>0']` is now a threshold, so a run that measures nothing fails instead of
passing. Any WebSocket latency figure in this directory predating this run should be treated as
unmeasured.
