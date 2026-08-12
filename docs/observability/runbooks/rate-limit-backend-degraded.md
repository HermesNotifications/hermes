# Runbook: `RateLimitBackendDegraded`

## What this alert means

A service has been unable to reach the shared rate limiter in Redis for 5 minutes, and is
deciding admissions from its own in-process token bucket instead.

**Nothing is failing.** Requests are still served, still limited, and still get correct
`Retry-After` headers. What has changed is *what the limit means*: with the shared backend the
configured rate is the cluster-wide ceiling; on the local fallback each replica enforces that
rate independently, so the effective ceiling is **the configured rate times the replica count**
— and under an HPA it moves with the autoscaler.

At the production defaults that is the difference between send's advertised 2000/s and 6,000/s
across three replicas, or 40,000/s if it scales to twenty.

**Nothing else will tell you.** There is no error rate, no latency spike, and no failing probe:
the fallback is a deliberate design choice ([ADR 0016](../../adr/0016-distributed-rate-limiting-with-local-fallback.md))
precisely so that a Redis blip cannot become a 429 storm or an outage. The trade is that the
degradation is invisible except through this counter. That is what this alert is for.

Requests are also never *blocked* by this: the shared check is bounded at 100ms, so a slow Redis
costs at most that before the local bucket answers.

## Immediate triage

```bash
# Which services, and how often?
#   hermes.http.rate_limit_backend_failures   — one increment per fallback
#   hermes.http.rate_limit_decisions{decision, scope}   — allowed | limited, credential | ip

# Is Redis reachable from the affected service at all?
kubectl -n hermes exec -it deploy/hermes-send -- \
  sh -c 'echo > /dev/tcp/redis/6379' 2>&1 || echo "unreachable"

# The application logs name the scope and the underlying error:
kubectl -n hermes logs deploy/hermes-send | grep "shared rate limiter unavailable"
```

`CacheDegraded` almost always fires alongside this one — the same Redis backs the API key
cache, idempotency and unread counts. **If it does, treat this as a symptom and work
`cache-degraded.md` instead**; this alert is only the interesting one when it fires *alone*,
which points at the rate limiter's own path rather than at Redis generally.

## Common causes (ranked by frequency)

1. **Redis is genuinely down or failing over.** ElastiCache maintenance, node replacement, or a
   primary failover. Usually resolves itself within minutes and needs no action.
2. **The 100ms check timeout is being exceeded under load, not an outage.** This bound is much
   tighter than the client's general 500ms budget, deliberately: the caller has a local answer
   available, so waiting is never better than falling back. If Redis is merely slow, this alert
   fires, the fallback works, and that is the intended behaviour.
3. **Pool exhaustion in the client.** The rate limit check adds one Redis operation per
   authenticated request on top of the API key lookup and idempotency write. Look for
   `hermes.cache.pool.timeouts` rising alongside; raise `HERMES_REDIS_POOL_SIZE` (default 16).
4. **One Redis serving too much.** The same instance backs the Hermes cache, idempotency dedup,
   unread counts, the rate limiter *and* Centrifugo's presence and history. See
   `docs/self-hosting/production.md` on splitting them.

## Mitigations

- **Redis down or failing over:** nothing to do in the application. Confirm the fallback is
  holding — `rate_limit_decisions` should keep flowing with a normal allowed/limited mix. Accept
  the widened ceiling until Redis returns.
- **Timeouts under load:** scale Redis, or split the Centrifugo engine onto its own instance.
- **Pool exhaustion:** raise `HERMES_REDIS_POOL_SIZE`.
- **If the widened ceiling is itself the problem** — an abusive caller is exploiting the outage —
  lower `HERMES_RATELIMIT_PER_SECOND` on the affected service and restart it, remembering the
  limit is now per replica.

  > **`PUT /v1/apikeys/{id}/rate-limit` will not help you here.** A caller's limit is pinned when
  > its bucket is created, and a continuously active caller never lets that bucket go idle — so
  > the new limit applies only after it stops, which is not the situation you are in. To stop an
  > abusive key now, revoke it (`DELETE /v1/apikeys/{id}`) or block it at the ingress.

Do **not** "fix" this by making the limiter fail closed when Redis is unreachable. That converts
a dependency blip into a total API outage, which is the trade [ADR 0016](../../adr/0016-distributed-rate-limiting-with-local-fallback.md)
explicitly rejects.

## Escalation

- Managed Redis (ElastiCache) → platform on-call.
- One service degraded while others are fine → that service's owner; it is more likely a client
  or pool problem than a Redis one.

## Post-incident

- **Did the ceiling actually matter?** Compare `rate_limit_decisions{decision="limited"}` before
  and during. If nothing was near its limit, the widened ceiling was academic and the alert can
  stay at `warn`.
- **Did anything 5xx?** It should not have. A failed shared check must fall back, never fail the
  request — if error rate rose, some path is treating the backend error as fatal.
- **Was the fallback bucket cold?** It starts full by design, so the first moments after a
  failover are the most permissive. If an abusive caller exploited exactly that window, the
  bucket needs warming on the shared path — which is a real design change, not a tuning knob.
