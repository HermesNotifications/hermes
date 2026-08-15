# ADR 0024: Fail open for credentials when the rate limiter's entry map is full

**Status:** Accepted  
**Date:** 2026-08-14  
**Author:** Platform

---

## Context

Each service holds one token bucket per caller in a `sync.Map`, bounded by `maxEntries`
(50,000). Once full, every further key was handed a single shared `overflowKey` bucket.

That mitigation was designed for the **pre-authentication per-IP limiter**, where the key space
belongs to the caller: a `/16` scan mints 65,000 keys on demand, and without a bound the map
becomes the amplifier for the attack it exists to stop. Collapsing the overflow into one bucket
makes a cardinality flood throttle itself. For that scope it is right.

It was applied to the **credential scopes** too, because `NewRateLimiter` defaults to
`ScopeCredential` and the cap is scope-blind. The comment above the constant recorded the
assumption: credential scopes are "naturally bounded by how many keys exist, so the cap is close
to free there." That holds for API keys, which number in the hundreds. It does not hold for
**users**, who are the population the Inbox and User services exist to serve.

The 100,000-connection run on 2026-08-14
([docs/loadtest/realtime-scale-2026-08-14.md](../loadtest/realtime-scale-2026-08-14.md))
demonstrated the consequence. Polling `/v1/inbox` at 100 rps across 100,000 users puts each user
at roughly 0.001 rps against a 20/s limit — three orders of magnitude below their own ceiling.
Distinct users accumulate as `N(1 − e^(−n/N))`, so 50,000 of 100,000 was reached after `N·ln2`
requests, 693 seconds in. From that moment roughly half of all requests fell into one 20/s
bucket: **6,705 requests refused 429 in four minutes**, every one of them to a user who had done
nothing.

Two properties made it worse than a simple misconfiguration. It is a *fleet* property — driven
by distinct callers active within the 30-minute `entryTTL`, not by request rate, so a quiet
deployment with 60,000 daily users reaches it too. And it was silent: nothing logged an
overflow, and the 429 carried `RateLimit-*` headers describing a per-user limit the user never
approached.

## Decision

**When the entry map is full, the behaviour depends on who chose the key.**

- `ScopeIP` keeps the shared overflow bucket. The key space is attacker-chosen; a key past the
  cap is evidence of a flood, and joint throttling is the mitigation.
- `ScopeCredential` **fails open**: the request is admitted with no bucket. The key space is
  ours — an API key id or a user id exists because we issued it — so a key past the cap is
  evidence that the cap is smaller than the user base, not evidence of abuse.

Failing open is counted as `hermes.http.rate_limit_decisions{decision="overflow_admitted"}`, so
a cap below the active population is visible rather than silent.

`HERMES_RATELIMIT_MAX_ENTRIES` makes the bound configurable per service; zero keeps the built-in
50,000.

## Consequences

- Users past the cap are no longer refused for other users' traffic. This is the point.
- **The per-credential limit stops being enforced for callers who do not fit.** That is a real
  loss of protection, and it is the cost we are accepting: the limiter's purpose is to stop one
  caller affecting others, and a shared bucket achieves the opposite of that purpose while
  claiming to serve it. Refusing outright would convert a capacity shortfall into an outage for
  everyone beyond the cap, which is strictly worse than not limiting them.
- Callers already resident in the map are limited exactly as before. Failing open applies only
  to keys that could not be admitted, not to anyone over their own limit.
- Operators gain an obligation: watch `decision="overflow_admitted"`. Non-zero means the
  configured limit is not being applied to part of the traffic. The remedies are to raise
  `HERMES_RATELIMIT_MAX_ENTRIES` or to enable distributed rate limiting
  ([ADR 0016](0016-distributed-rate-limiting-with-local-fallback.md)), which keeps no local map.
- The per-IP limiter is untouched, so the cardinality-attack mitigation is unchanged.
- A deliberate asymmetry now exists between the two scopes. It is the kind of thing that looks
  like an inconsistency to a future reader, which is why it is written down here.

## Alternatives considered

**Raise `maxEntries` and leave the behaviour alone.** The smallest change, and it would have
prevented this run's failure. Rejected as the whole answer: it moves the cliff rather than
removing it, and the failure mode past the new number is identical and equally silent. Adopted
as *part* of the decision — the bound is now configurable — but not as a substitute for it.

**Refuse requests past the cap instead of sharing a bucket.** Honest about the limiter's
inability to make a decision, and safe against abuse. Rejected because it turns a sizing mistake
into a total outage for every caller beyond the cap, and the population past the cap is
arbitrary — whoever happened to arrive after the map filled.

**Make the credential check distributed by default** ([ADR 0016](0016-distributed-rate-limiting-with-local-fallback.md)).
Removes the in-process map, so there is no cap to overflow. Rejected as a default because it
puts a Redis round trip on the Inbox read path — measured at 83 ms p95 during the same run — and
makes a Redis outage a factor in every authenticated request. It remains the right answer for
deployments that want a cluster-wide limit, and is now the documented remedy when
`overflow_admitted` is non-zero.

**Evict least-recently-used instead of failing open.** Keeps every caller in a bucket by pushing
someone else out. Rejected because under a population larger than the cap it degenerates into
thrashing — each request evicts the entry another user is about to need — and a freshly created
bucket starts full, so eviction quietly hands out extra allowance to whoever churns most.
