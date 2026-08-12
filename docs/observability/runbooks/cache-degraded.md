# Runbook: `CacheDegraded`

## What this alert means

A service has been failing to read Redis for 10 minutes and is falling back to Postgres.

Responses are still **correct** — that is the point of the fallback — but they are slower, and
every one of them is now load the database was not supposed to carry.

**Readiness will not tell you about this, by design.** Redis deliberately does not gate
`/readyz` ([ADR 0015](../../adr/0015-lifecycle-and-jetstream-durability.md#readiness)): every
read it serves has a database fallback, and marking pods unready for
a fault they can work around would pull every replica of every service out of its Service at
once — turning a degradation users would barely notice into a total outage. The consequence is
that Redis can be failing continuously with every probe green and every pod Ready. This alert is
the only signal.

## Immediate triage

```bash
# Which operations, and what proportion?
#   hermes.cache.result{op, result}   — result is hit | miss | stale | error
# Dashboard: Hermes service → "Cache" row.

# Is Redis reachable at all?
kubectl -n hermes exec -it deploy/hermes-inbox -- \
  sh -c 'echo > /dev/tcp/redis/6379' 2>&1 || echo "unreachable"

# ElastiCache in staging/production: check the AWS console for the replication group,
# and CloudWatch for evictions, CPU and swap.
```

Also check the database side: `DBPoolSaturated` and `HighLatency` commonly fire shortly after
this one, because the fallback traffic lands on Postgres.

## Common causes (ranked by frequency)

1. **Redis is genuinely down or failing over.** ElastiCache maintenance, a node replacement, or
   a primary failover. Usually resolves itself within minutes.
2. **Command timeouts under load, not an outage.** The client uses a 500ms read/write timeout
   (`HERMES_REDIS_TIMEOUT`), chosen deliberately: the default of 3s meant a hiccup blocked every
   inbox request for three seconds before falling back, piling up in-flight requests until the
   HTTP tier collapsed. If Redis is merely slow rather than down, this alert fires and the
   fallback works, which is the intended behaviour.
3. **Pool exhaustion in the client.** `hermes.cache.pool.timeouts` rising alongside. Raise
   `HERMES_REDIS_POOL_SIZE` (default 16).
4. **One Redis serving too much.** The same instance backs the Hermes cache, idempotency dedup,
   unread counts *and* Centrifugo's presence and history. An inbox read spike degrades websocket
   presence and vice versa. `docs/self-hosting/production.md` recommends splitting them.

## Mitigations

- **Redis down:** nothing to do in the application. Confirm the fallback is holding — error rate
  should stay flat while latency rises. Watch the database pool.
- **Timeouts under load:** scale Redis, or split the Centrifugo engine onto its own instance.
- **Pool exhaustion:** raise `HERMES_REDIS_POOL_SIZE`.

Do **not** "fix" this by making Redis gate readiness. See [ADR 0015](../../adr/0015-lifecycle-and-jetstream-durability.md#readiness)
for why that trade is
inverted.

## Escalation

- Managed Redis (ElastiCache) → platform on-call.
- One service only, while others are fine → that service's owner; it is more likely a client
  problem than a Redis one.

## Post-incident

- Did the fallback actually hold? If error rate rose rather than just latency, some path is
  treating a cache error as fatal and should not be.
- Was the unread count wrong afterwards? It should not be: `incrIfPresent` refuses to create a
  key, so a cold cache heals from an authoritative read rather than inventing a value. If a
  badge was wrong, that invariant has a hole.
