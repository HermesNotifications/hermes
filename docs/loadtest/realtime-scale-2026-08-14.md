# 100,000 connections, with the telemetry on

Re-run of the 100,000-connection case from
[websocket-scale-2026-08.md](websocket-scale-2026-08.md), on the OVH cluster, 2026-08-14,
19:19–19:37 UTC. That document ends by saying none of its measurements should be repeated as
written, because the entire exercise ran with Hermes telemetry disabled and every server-side
figure in it was read by hand. This is that re-run.

**The headline is not the throughput.** 100,000 connections held, 404,881 pushes delivered, and
every latency threshold passed. The result worth acting on is a rate limiter that starts
refusing blameless users once a deployment exceeds 50,000 of them.

## What was different this time

- **Hermes 0.1.3**, with `observability.enabled: true` exporting to SigNoz over OTLP. 0.1.2 could
  not do this: `resource.WithProcess()` called `os/user.Current()`, impossible in a CGO-disabled
  distroless image, so telemetry-on meant crash-loop. That is why the previous exercise ran dark.
- **The synthetic prober enabled**, giving an end-to-end delivery signal that is not derived from
  the load generator. (See the caveats — its series were not extracted for this write-up.)
- **The generator physically separated from the system under test.** Run C's central finding was
  that at 100k the client was measuring itself. Here helium (24 cores) carries the k6 runners and
  nothing else; every Hermes pod, including Postgres, Redis and all three NATS replicas, sits on
  argon and neon.

## Method

Three-node k3s v1.34.6 on amd64, 64 GiB per node: argon (12 cores), helium (24), neon (12).
Hermes on argon+neon; helium labelled `pool=loadtest-generators` and empty of Hermes.

| Parameter | Value |
|---|---|
| Scenario | `inbox-mixed` |
| `CONNECTIONS` | 100,000 |
| `WS_SOCKETS_PER_VU` | 25 → 4,000 VUs across 8 runner pods |
| `WS_RAMP_SECONDS` | 180 |
| `DURATION` / `WS_HOLD_SECONDS` | 15m / 1200s |
| `SEND_RPS` / `POLL_RPS` | 500 / 100 |
| `CHANNEL_WEIGHTS` | `inbox:100` |
| Seed | 10 organizations × 10,000 users = 100,000 |

The channel mix is pinned to inbox deliberately: an evaluation install has no real SMTP, so
email routing fails and retries, and the resulting DELIVERY backlog reads exactly like the
delivery tier falling behind when it is not.

## Results

| Measurement | Result |
|---|---|
| WS sessions established | **100,000** |
| Pushes received | **404,881** (~45.4/s per runner) |
| `ws_push_e2e_latency` | med **8 ms**, avg 45 ms, p95 **176 ms**, max 3.41 s |
| `ws_connect_latency` p95 | 31 ms |
| `inbox_list_latency` p95 | 83 ms (threshold `p(95)<150`) |
| `send_ack_latency` p95 | 2 ms |
| `POST /v1/send` | 449,865, **all 202** |
| `GET /v1/inbox` | 89,403 — 82,698 × 200, **6,705 × 429** |
| `http_req_failed` | 0.80%–1.55% per runner, threshold `rate<0.01` |

The percentiles are consistent across all eight runners — `ws_push_e2e_latency` p95 lands
between 170 ms and 182 ms on every pod — so these are not one pod's artefact.

`ws_push_received` is non-zero, which is not a formality. Three separate defects in this
scenario's history each produced a run where not one push arrived and every one was reported as
a pass, because a percentile over an empty trend is `0` and a green tick.

## The finding: the credential limiter overflows at 50,000 users

`http_req_failed` crossed its threshold on five of eight runners. Every failure was a **429 on
`/v1/inbox`** — 6,705 of them, confirmed identically in the inbox service's own request log,
each rejected in `duration_ms: 0`. Sends were untouched.

They occupy one window and then stop:

| Minute (UTC) | 429s |
|---|---:|
| 19:30 | 777 |
| 19:31 | 1,725 |
| 19:32 | 1,730 |
| 19:33 | 1,758 |
| 19:34 | 715 |

### Why

The inbox credential limiter buckets by user (`middleware.UserLimitKey`) at 20/s with burst 50.
Polling at 100 rps spread uniformly over 100,000 users gives each user roughly 0.001 rps, so no
user comes close to their own limit. The limit that was hit is not a per-user one.

`internal/middleware/ratelimit.go` bounds the bucket map:

```go
DefaultMaxEntries = 50_000
entryTTL          = 30 * time.Minute
// overflowKey is the single bucket every caller shares once the cap is hit.
overflowKey = "\x00overflow"
```

Distinct users accumulate slower than requests, because picks repeat. After `n` uniform picks
from `N` users the expected distinct count is `N(1 − e^(−n/N))`, so reaching 50,000 of 100,000
takes `N·ln2 ≈ 69,315` requests — at 100 rps, **693 seconds**. Polling began at 19:19; 693
seconds later is 19:30.6, and the first 429 is at 19:30. `entryTTL` is 30 minutes, so during a
15-minute run nothing is ever reclaimed.

From that moment the map is pinned at 50,000 and every request for one of the other ~50,000
users falls into the shared bucket: roughly 50/s arriving against a 20/s allowance, leaving
~30/s refused. The observed rate is ~29/s.

They stop at 19:34 because that is when the poll scenario's 15 minutes elapse. The websocket
scenario ran until 19:37 owing to its 180-second ramp, which is why the run continues past the
last 429.

The comment above the constant says credential scopes are "naturally bounded by how many keys
exist, so the cap is close to free there." That is the assumption this run falsifies. It is true
of API keys, which number in the hundreds. It is false of end users, who are exactly the
unbounded population the inbox serves.

### What it means for an operator

**Any deployment with more than 50,000 active inbox users will return 429s to users who have
done nothing wrong**, at a rate set by how far past the cap the active population sits. The
response carries `RateLimit-*` headers describing a per-user limit the user never approached.
Nothing logs an overflow, and the per-replica map means the threshold moves with replica count in
a way no configuration mentions.

Note this is a *fleet* property, not a load property: it is triggered by how many distinct users
are active within `entryTTL`, not by requests per second. A quiet deployment with 60,000 daily
users reaches it too.

### Options

1. **Move the credential check to the distributed limiter.** `RateLimitDistributedEnabled`
   already exists and keeps buckets in Redis, so there is no in-process map to overflow. It costs
   a Redis round trip on the read path.
2. **Do not share one bucket on overflow for the credential scope.** The shared bucket is the
   right answer for the pre-auth IP limiter, where the key space is chosen by the caller and a
   `/16` scan would otherwise allocate 65k buckets. For an authenticated user it inverts the
   intent: the limiter exists to bound one caller and instead bounds everyone jointly.
3. **Make `maxEntries` configurable** and raise the default. This moves the cliff rather than
   removing it, but it is the smallest change and would have prevented this run's failure.

(1) and (2) are complementary — (2) is the correct local behaviour, (1) removes the cap entirely
for deployments that want it. (3) alone leaves a silent cliff at whatever the new number is.

## What this run does *not* establish

- **Node CPU at full load was not captured.** The only samples were taken during the ramp at
  ~31,000 connections (helium 21% of 24 cores, argon 17%, neon 9%). They are not steady-state
  figures at 100,000 and should not be quoted as such.
- **The prober's own series were not extracted** from SigNoz. It was running and healthy
  throughout; its measurements are not part of this write-up.
- One 15-minute run, one load shape, `inbox:100`, `streamReplicas: 1`. Nothing here speaks to
  sustained soak behaviour, to the email or SMS paths, or to NATS with replicated streams.
- The generator is separated from Hermes *by node*, not by machine — they share a network and a
  control plane.

## Tooling defects found on the way

Three faults in the runner had to be fixed before this run could be trusted, all in
[PR #154](https://github.com/HermesNotifications/hermes/pull/154):

- `run-k8s.sh` exported 16 variables while `testrun.yaml` referenced 25. `envsubst` has no strict
  mode, so `CONNECTIONS=""` meant a run commissioned for 100,000 connections would hold 100 and
  pass.
- `--out experimental-prometheus-rw=<url>` is accepted and ignored — that output reads
  `K6_PROMETHEUS_RW_SERVER_URL`. Every runner logged connection-refused against its own
  localhost while the TestRun reached `finished` and Grafana stayed empty.
- `kubectl cp` cannot extract the seed manifest from the distroless `loadseed` image; it needs
  `tar` inside the container.

And one in the chart, [PR #155](https://github.com/HermesNotifications/hermes/pull/155):
`prober.organizationID` defaulted to a non-UUID, which no `organizations` row can hold, so
`prober.enabled: true` crash-looped on a 500 from `/v1/auth/token`.
