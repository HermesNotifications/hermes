# ADR 0016: Rate limit per credential in Redis, with the local bucket as the fallback

**Status:** Accepted  
**Date:** 2026-08-11  
**Author:** Daryl Robbins

---

## Context

Rate limiting reached `main` without an ADR. The decision lived in code comments,
`docs/configuration.md`, and finding 39 of the July architecture review. What exists is a
`golang.org/x/time/rate` token bucket per caller, held in a swept `sync.Map`, wired into all
four HTTP services *inside* the auth middleware so it can key on a validated identity
(`internal/middleware/ratelimit.go`). Alongside it, ingress-nginx annotations bound requests
per source IP (`deploy/k8s/base/ingress.yaml`).

That design is sound and this ADR keeps its shape. But it has three gaps, two of which its
own documentation already named as unfinished:

**It is per replica, not per cluster.** Each pod holds its own buckets, so the cluster-wide
ceiling is the configured rate times the replica count, and under an HPA it moves with the
autoscaler. `docs/configuration.md` said this was "tracked as future work". At the production
defaults, send's documented 2000/s is 6,000/s across three replicas and 40,000/s at twenty. A
number that varies by a factor of seven with autoscaling is not one anyone can size capacity
against, and it is not the number the API contract advertises.

**Every credential gets the same limit.** There are no per-key, per-plan or per-namespace
quotas, and no schema to express one.

**The unauthenticated path is defended only by nginx.** The limiter runs after
authentication, so an invalid-credential flood is rejected by `APIKeyMiddleware` — after an
HMAC and a Redis lookup per request — and never reaches a bucket. The only thing bounding
that work is a set of nginx-specific annotations. Hermes is a self-hosted product: an operator
may run Traefik, an ALB, or no ingress controller we influence, and the chart's own comment
concedes that "other ingress controllers ignore these". ingress-nginx has also entered
maintenance mode with its retirement announced, and this repo pins no controller version in
any of its three install paths.

The decisive constraint turned out to be one we initially mis-stated. An earlier draft of this
ADR ruled out putting the admission check in Redis on the grounds that it would add a network
round trip to the hot `/v1/send` path. **That path already makes one or two Redis round trips
per request** — the API key cache lookup in `validateAPIKey` (`internal/send/server.go`), and
the idempotency `SET NX` in `handler_send.go` when the caller supplies a key. Redis is also
already a hard startup dependency (`bootstrap.MustConnectRedis` exits on failure). Adding a
third call is an incremental cost, not a categorical change in what the request path depends
on.

What genuinely matters is the *runtime failure posture*: every existing runtime use of Redis
fails **open**. `internal/bootstrap/lifecycle.go` records why readiness deliberately does not
gate on Redis — "one blip on a shared dependency would pull *every* replica out of the Service
at once."

## Decision

We will **run the per-credential admission check in Redis, and keep the local token bucket as
the fallback used when Redis cannot answer.**

**1. The shared check is GCRA via `go-redis/redis_rate/v10`.** One atomic Lua round trip per
request, evaluated inside Redis, so there is no read-modify-write window in which two replicas
both believe they hold the last token. The configured rate is the cluster-wide rate, exactly,
with no approximation to explain to anyone. This is enabled by
`HERMES_RATELIMIT_DISTRIBUTED_ENABLED`, **off by default**.

**2. The call is bounded at 100ms, far tighter than the client's general 500ms budget.** A
Redis that has not answered in that time is treated as absent rather than allowed to add its
latency to every request — which is only a safe thing to do because of point 3.

**3. On any error the request is decided by the local bucket instead.** Not refused, and not
waved through. A backend outage degrades to per-replica enforcement — precisely the behaviour
that is in production today, a known-good state rather than an unprotected one — and increments
`hermes.http.rate_limit_backend_failures`. The local bucket is deliberately *not* consumed on
the shared path: two authorities charging for the same request would double-count, and a
fallback bucket that starts full is the right shape for a degraded mode.

**4. A per-IP limiter runs before authentication, inside the Go services.** It reuses the same
limiter type with a different key function, and honours `X-Forwarded-For` **only** when the
peer is inside `HERMES_TRUSTED_PROXY_CIDRS` — walking the chain from the right, since the
leftmost entry is whatever the client chose to send. The default trusts nothing. It carries a
hard entry cap, because per-IP keying has cardinality an attacker picks. **This scope is never
sent to Redis**: it is a flood bound whose key space an attacker chooses, and forwarding it
would convert an address scan into Redis load.

**5. API keys may carry their own limits.** `api_keys` gains nullable `rate_limit_per_second`
and `rate_limit_burst`. NULL means "use the service default" — the sentinel `ResolveLimit`
already applies to a zero override — so no backfill is needed and every existing key keeps
exactly the behaviour it has. The values ride the API key record already cached in Redis, so
they cost no additional lookup.

The nginx annotations stay as defence in depth for deployments that do run nginx.

## Consequences

**The advertised limit becomes true rather than approximately true.** With the feature on,
`RateLimit-Limit` describes the cluster. `RateLimit-Remaining` becomes a cluster-wide figure
rather than one replica's view of it.

**Authenticated requests cost one more Redis round trip.** On send that is roughly a 50%
increase in Redis operations per request. Redis serves this comfortably at the volumes in
question, and the 100ms ceiling bounds the worst case, but it is a real cost and it is why the
feature is opt-in rather than default.

**There are two enforcement paths, and they can disagree.** This repo's stated preference is
that "two settings that can disagree are worse than one that cannot" (ADR 0005). We accept the
second path here because the alternative is worse in both directions: without a fallback a
Redis blip becomes either a 429 storm or an unlimited API. The disagreement is bounded — the
fallback is strictly more permissive per replica, never less — and it is observable through
`hermes.http.rate_limit_backend_failures`.

**A Redis outage silently widens the effective limit.** Requests keep flowing, which is the
point, but the cluster-wide guarantee is gone for the duration and only the counter says so.
That counter needs an alert and a runbook; neither ships here, and that is follow-up work.

**Distributed mode is opt-in, so the default is still per-replica.** Turning it on *lowers* the
throughput a multi-replica deployment gets today, which should be an operator's choice rather
than something a chart upgrade does silently. The cost is that the honest default remains the
imprecise one.

**Per-IP limiting is only as good as the trusted-proxy configuration.** Left empty behind an
ingress controller, every request appears to come from the controller's own address and all
callers collapse into one bucket. Set to `0.0.0.0/0` it is worse than useless, since the header
is caller-supplied. Both failure modes are documented; neither can be detected from inside the
process.

**Under a cardinality attack, innocent callers share a bucket.** Once the entry cap is reached
the limiter forces a sweep, and if that does not reclaim enough it diverts new keys to a single
shared bucket. That throttles the attack, but a legitimate caller arriving during one is
limited alongside it. The alternative — an unbounded map — is a memory exhaustion vector.

**New surface.** One new dependency (`go-redis/redis_rate/v10`), one new metric, four new
environment variables, and a migration.

**Namespace-level quotas remain unimplemented.** [ADR 0012](0012-api-keys-are-not-scoped-to-organizations.md)
parks `api_keys.namespace_id` as part of a deferred namespace phase. **Revisit trigger:** when
that phase is implemented.

**429 keeps its meaning.** [ADR 0010](0010-bounded-work-streams-reject-rather-than-drop.md)
reserves 429 for "your request rate is the problem" and 503 + `Retry-After` for a saturated
pipeline. Nothing here changes that split.

## Alternatives considered

**Keep enforcement local and reconcile demand through Redis out of band.** This was
implemented and working before being replaced by the decision above: replicas reported observed
consumption into per-window Redis hashes and adjusted local bucket rates to their demand-weighted
share, rationing only when aggregate demand exceeded the entitlement. It kept Redis entirely off
the request path and degraded cleanly.

Rejected on cost-for-value. It bought *approximate* convergence — overshoot bounded at roughly
1.5x, and lagging demand by one window — for ~410 lines of production code whose correctness
depended on clock skew staying under one interval, on window alignment across replicas, and on
share floors summing the way the arithmetic claimed. The library gives an exact answer with
none of that to keep true. And its central justification — keeping Redis off the request path —
did not survive checking: the path already goes to Redis one or two times per request. The
retained fallback preserves the degradation story that motivated the design in the first place,
for about 30 lines instead of 410.

**Adopt `redis_rate` with no local fallback, failing open on error.** Simpler still: one
authority, one code path. Rejected because failing open means that during a Redis blip there is
*no* rate limiting at all — including for a credential actively abusing the API, which is
exactly when the limiter is load-bearing. Failing closed is worse again: it turns a dependency
blip into a total outage, which `lifecycle.go` explicitly rejects.

**`sethvargo/go-limiter` or `throttled`.** Comparable libraries with Redis stores. `redis_rate`
was chosen because it is built on the `go-redis/v9` client already in `go.mod`, so it shares the
existing connection pool, OTel instrumentation and timeout configuration rather than introducing
a second client.

**Put quotas at the API gateway.** ingress-nginx's `global-rate-limit` is memcached-backed and
shared across controller replicas. Rejected on three counts: nginx cannot key on a Hermes
credential without validating it, so it can only limit by IP and cannot tell one customer's
egress IP from an attacker's; it would add memcached for a problem Redis already solves for us;
and it is nginx-specific, which a self-hosted product whose chart must work on any ingress
controller cannot rely on. The per-IP *flood bound* does belong at the edge, which is why the
annotations stay — as an optional second layer, not as the enforcement point.

**Store per-credential limits in Redis only, with no schema change.** Would avoid a migration.
Rejected: the limits are configuration an operator sets deliberately and must survive a cache
flush. Postgres is the system of record, and Redis already caches the key row, so reading them
costs no extra round trip anyway.

**Keep the per-IP bound at the ingress only.** The status quo. Rejected: it does not work on any
other ingress controller, does not work with no ingress controller, and cannot be tested in the
Go test suite. Keeping it *as well* costs nothing, so it stays.
