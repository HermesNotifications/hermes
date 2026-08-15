# WebSocket connection scale — OVH cluster, 2026-08-12

How far the realtime path scales: concurrent WebSocket connections held against Centrifugo while
notifications flow through the whole `send → dispatch → worker-inbox → Centrifugo` pipeline.

**Headline: 250,000 concurrent WebSocket connections held across six Centrifugo replicas, with
connect latency at 16ms p95, push latency at a 5ms median, and zero failed requests. Sustained
6,000 sends/s against them with no errors. Hermes was not the limit at any point in this
exercise — at 250k its nodes sat at 11–24% CPU, and the throughput ceiling that did appear was
a rate-limit setting, not capacity.**

**Two findings matter more than the connection count:**

1. **Sustained throughput is ~1,000/s per dispatch replica, and `send_ack_latency` cannot see
   it.** Send publishes to JetStream and returns in ~2ms whether or not anything downstream
   keeps up. A 12,000/s run left **1,010,571 messages** queued while every response looked
   healthy. Alert on consumer lag, not on ingestion latency.

2. **Every latency degradation observed here traced back to the load generator — four separate
   times.** Blocking VUs starving the scheduler; a client timing its own busy event loop; a
   thundering herd of simultaneous connects; a VU pool sized by the wrong unit. Each looked
   like a server limit. The numbers below are the ones that survived a second vantage point.

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
is addressed by the `redis.metrics.enabled` exporter sidecar in the chart.

> **Now on by default**, along with the ServiceMonitor that was missing — enabling the sidecar
> previously produced an endpoint and no time series, because nothing scraped it. The argument
> for the default is this run: Redis was measured at 0.15 cores and 3,000 ops/s while carrying
> the fan-out for 100,000 connections, and that number had to be obtained by hand.

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

The same caveat applied to `ws_push_e2e_latency` at high connection counts: its **median stayed
at 6ms** from 200 connections to 100,000, and only the tail moved. Given the p95 of a co-resident
HTTP request was inflated ~350×, the push tails were read as *not yet measured* rather than as
server latency.

**Since resolved, and they were artifacts.** Measured again at 250,000 held connections from a
runner pod carrying only 500 sockets of its own — the same socket-free-probe technique, applied
to push latency rather than to the REST API:

| measured from | push e2e median | p90 | p95 | pushes |
|---|---:|---:|---:|---:|
| pods holding 12,500 sockets each | 5–6 ms | 8 ms | ~400 ms | — |
| **a pod holding 500** | **8 ms** | **9 ms** | **11 ms** | 10,476 |

So push latency at a quarter of a million connections is **11 ms at p95**, not hundreds. The
medians were trustworthy throughout; the tails were the generator timing its own event loop,
for the fifth time in this exercise.

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

### Sustained throughput is ~1,000/s per dispatch replica, and the API does not tell you

This is the most operationally important number here, and the one that latency hides.

Measured with JetStream consumer lag rather than response time, inbox-only channel, one
dispatch replica, 2,000 sends/s offered for 180s:

| | |
|---|---:|
| `dispatch` consumer pending | 15,882 → **174,639**, growing linearly |
| `worker-inbox` consumer pending | **0**, throughout |
| drain rate once load stopped | **~1,242/s** |
| `send_ack_latency` p95 during | **11 ms** |
| `http_req_failed` | **0.00%** |

So dispatch drains ~1,000–1,250 notifications/s per replica, and the Centrifugo publish path
is never the constraint. Above that rate the queue grows without bound.

**`send_ack_latency` stayed at 1–7ms the entire time.** Send is a thin ingestion layer: it
authenticates, dedupes and publishes to JetStream, so it answers in milliseconds regardless
of whether anything downstream can keep up. Ingestion latency is not backpressure, and a
dashboard built on it shows a perfectly healthy API while the system falls irrecoverably
behind. **Alert on consumer lag, not on send latency.**

The earlier 12,000/s test made this concrete: it left **1,010,571 messages** queued on
NOTIFICATIONS and 600,500 on DELIVERY, still draining at ~940/s ten minutes after the load
stopped, writing notification rows the whole way. Every `send_ack` in that run was ~2ms.

This also reframes the ingestion numbers below: they measure what Send *accepts*, not what
the pipeline *delivers*.

### Dispatch throughput is not per-replica — Postgres commit is the wall

Adding dispatch replicas does not buy proportional throughput:

| dispatch replicas | sustained drain | per replica |
|---:|---:|---:|
| 1 | ~1,242/s | 1,242/s |
| 4 | ~2,006/s | ~500/s |

**Four times the replicas bought 1.6× the throughput.** At 6,000/s offered against four
replicas the dispatch consumer's pending count climbed to 764,173 while `send_ack_latency`
p95 stayed at 60ms and `http_req_failed` was 0.00%.

Postgres sat at **2.5 of helium's 24 cores** throughout — it is not CPU-bound. It is
commit-bound: `synchronous_commit=on` and `fsync=on` against Longhorn-replicated storage
means every notification insert waits on a replicated disk write, and `shared_buffers` is
still the 128MB container default.

So the lever past ~2,000/s is storage or batching, not more dispatch pods:

- put the database on local NVMe rather than replicated network storage;
- batch the notification inserts in dispatch rather than one row per message;
- or make a deliberate durability tradeoff on `synchronous_commit`.

Adding replicas mostly adds connections to the same fsync queue. Worth checking
`max_connections` too — it is 100, and four dispatch replicas at a pool of 10 each already
take 40 of it alongside every other service.

### An evaluation install's email worker will look like a delivery bottleneck

At 800/s with the default 70/30 inbox/email mix, the DELIVERY stream grew to 43,389 and did
not drain. That is not the delivery tier failing to keep up — the inbox consumer was at zero
pending throughout. It is `worker-email` retrying against an SMTP server that does not exist:

```
delivery failed ... attempt 9 ... smtp dial: dial tcp [::1]:1025: connect: connection refused
```

The chart defaults `hermes.email.smtp.host` to empty, so a bundled install routes 30% of
notifications into an infinite retry loop. Pin `CHANNEL_WEIGHTS=inbox:100` for realtime
throughput testing, or configure a sink.

With a sink attached (Mailpit, in this cluster's GitOps repo), the picture separates cleanly:

- **Mailpit** handled a measured **~2,200 msg/s** — flat at 50 and 100 concurrent senders, so
  that is its ceiling rather than the benchmark client's. At the default 30% email share it
  only becomes the constraint above ~7,300/s of total traffic.
- **`worker-email` at one replica** fell behind at roughly **600/s** of email, its consumer lag
  growing to 15,826 while Mailpit itself idled at ~1 core.

So the email tier's limit is the worker, not the sink. Do not assume a dev mail sink is the
bottleneck without measuring it — and do not assume it is fine either.

### Ingestion rate (what Send accepts), with 250k connections held

Measured from pods holding no sockets, against the held connections:

| offered | accepted | `send_ack` p95 | `inbox_list` p95 | note |
|---:|---:|---:|---:|---|
| 3,000/s | 100% | 130 ms | 41 ms | 4 send replicas |
| 6,000/s | **100%** | 138 ms | 58 ms | ~160k requests/pod, zero errors |
| 12,000/s | **~56%** | 297 ms | 160 ms | ~500–610 errors/s per pod |

**The ingestion ceiling is configuration, not capacity.** `rateLimit.distributed` is unset, so
the per-credential limit applies *per replica*: one send pod caps at 2000 rps and rejected
44.7% of a 3000/s load; four replicas moved the same wall to ~8000/s. Underneath it, Postgres
peaked at **2.5 cores** and Redis at **0.7** — neither close to saturated on a 24-core node.

To raise it: raise `rateLimit.perSecond`, enable distributed rate limiting so the budget is
shared rather than multiplied, or add replicas.

But note what these rows do *not* say. Every one of them was accepted by Send and queued; the
sustained figure is ~1,000/s per dispatch replica, above. **6,000/s accepted with zero errors
is a statement about ingestion, and the queue absorbing the difference is the design working
as intended — right up until nobody is watching the queue.**

### A third generator artifact

An 8,000/s step OOM-killed every runner pod in 14 seconds. `preAllocatedVUs` was sized as one
VU per requested rps, which assumes each VU completes one iteration per second — but a send
returns in ~2ms, so the pool was over-allocated by two orders of magnitude. Invisible below
~1000/s and fatal above it, and it presents as the system refusing load. Now sized from
throughput per VU.

## Unexplained: dispatch stopped consuming and reported healthy for three hours

During the repeat 250k run, dispatch was found **idle at 1m CPU with 133,472 messages pending**
on its JetStream consumer. Its last log line was 2h58m earlier. Throughout that window the pod
was `Ready=true` with **zero restarts**, so nothing in Kubernetes noticed and nothing would
have alerted. A rollout restart resolved it immediately — CPU went 1m → 1,159m and the backlog
drained.

The write path was completely stopped for three hours and every health signal was green.

**The trigger is not identified.** The obvious hypothesis was that the `nats stream purge` used
in load-test cleanup had wedged the attached consumer. That was tested directly — 20 sends
consumed cleanly, purge, 20 more sends — and the consumer advanced normally through it. So
purging is *not* the cause, at least not on an idle stream.

The wedged pod's logs were lost by restarting it before capturing them, which is the mistake to
avoid if this recurs: capture `kubectl logs` and `consumer info` **before** restarting. The
window may still be in SigNoz, which collects the `hermes` namespace.

What is actionable regardless of trigger:

- **`/readyz` does not reflect whether the NATS consumer is consuming.** A service whose entire
  job is draining a work queue can stop draining it and still pass its readiness probe. Making
  readiness (or a liveness check) depend on consumer progress would turn a silent three-hour
  outage into a pod restart.
- **Alert on JetStream consumer lag**, which is the same conclusion the throughput measurements
  reached from the other direction. It is the one signal that was screaming while every other
  one was green.

> **Both done.** Liveness now follows consumer progress (ADR 0022): `/healthz` fails when a
> consumer holds work and settles none of it for `HERMES_NATS_CONSUMER_STALL_TIMEOUT`, and
> `HermesConsumerStalled` fires at half that window so an operator reaches the wedged pod
> *before* the kubelet destroys the evidence — which is the specific mistake made here.
>
> Lag alerting exists but not as a depth threshold. `nats_jetstream_consumer_num_pending > 1000`
> was tried and is wrong for exactly the reason this document demonstrates: at 6,000/s offered,
> pending reached 764,173, so a static line carries no information about severity, and a healthy
> burst crosses it too. The rules now alert on **time-to-drain** (backlog over drain rate) and
> on **sustained growth**, which distinguish a queue absorbing a burst from a pipeline losing a
> race. See `docs/observability/runbooks/nats-consumer-lag.md`.

## Limits worth knowing before trusting these numbers

- The table above is the ceiling of **one Centrifugo pod on the memory engine**, which is how the
  cluster was configured when it was measured. Run B changed that; the per-connection costs still
  apply per replica.
- The send rate was held at 300/s throughout. This measures **connection scale**, not ingestion
  throughput; `send_ack` p95 never moved off 2ms, so the write path was idle by comparison.
- Users were reused above 20,000 connections in the 25k step (20k seeded), so some users held two
  sockets. The 50k step was re-seeded to 60k users and had 49,705 distinct users.

## Observability during this run

**Hermes app telemetry was off for the entire exercise.** Every server-side figure above came
from kubeletstats and Centrifugo's own `:9000` endpoint, read by hand — there are no Hermes
spans or metrics in SigNoz for this window, and `centrifugo_node_num_clients` was confirmed
with `curl` rather than from a time series.

`observability.enabled: false` was a deliberate workaround: with it on, every service
crash-looped on

```
observability init failed: build resource: error detecting resource:
user: Current requires cgo or $USER set in environment
```

because `resource.WithProcess()` includes a process-owner detector that calls
`os/user.Current()`, which cannot work in a CGO-disabled distroless image.

> **Fixed since.** `internal/observability/otel.go:buildResource` now spells out
> `WithProcess()` minus `WithProcessOwner()`, and a detector error degrades rather than kills
> the process. `observability.enabled` defaults to **true**, the Redis exporter and a
> Centrifugo scrape are on, and a synthetic prober measures send-to-socket continuously.
>
> **So none of the measurements in this document should be repeated as-is.** Re-running with
> telemetry on is not a formality: it replaces hand-read node metrics with per-service spans,
> and it makes the two findings at the top of this document — consumer lag as the real signal,
> and the load generator as the recurring source of phantom latency — checkable from a
> dashboard rather than by inference.
>
> **Done, for the 100,000 case:**
> [realtime-scale-2026-08-14.md](realtime-scale-2026-08-14.md). Telemetry on, the generator on
> its own node this time, 100,000 connections held and 404,881 pushes delivered with
> `ws_push_e2e_latency` at a median of 8 ms. It also turned up the thing neither run was looking
> for: the per-credential rate limiter shares one bucket across every caller past 50,000 entries,
> so an inbox with more than 50,000 active users starts returning 429s to users who did nothing.

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
