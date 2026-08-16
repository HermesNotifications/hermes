# ADR 0014: Serve the unread count from cache, and keep it off the websocket's critical path

**Status:** Accepted (amended 2026-08-12, 2026-08-16)
**Date:** 2026-08-10
**Author:** Daryl Robbins

Amends [ADR 0001](0001-dynamodb-model-via-extenddb.md) ("Unread count: Already Redis-backed
(10-min TTL). No change needed."). Closes a gap recorded in
[ADR 0013](0013-embeddable-inbox-widget-contract.md).

---

## Context

The unread count is the single most-read number in Hermes: it is on every inbox response, it
drives the badge in the embedded widget, and it is the only piece of inbox state a host
application may render without mounting an inbox at all. It was also, on inspection, wrong in
three independent ways, none of which was visible from the outside.

**The cache existed and nothing read it.** `GET /v1/inbox` computed the count with
`SELECT COUNT(*) … WHERE read_at IS NULL AND archived_at IS NULL AND deleted_at IS NULL` and
then *wrote* the result to Redis. No read path consulted that key. So every page of every
scroll paid for a full count, and on the DynamoDB path that count was an unbounded paginated
`Query` with a `FilterExpression` — billed for every item scanned, not every item counted.

**No index covered it.** The only relevant index, `idx_notifications_inbox`, omits `read_at`
from its predicate, so answering the count meant walking every active row a user had ever
received. The cost grew with account age rather than with unread volume.

**The increment could mint a permanently wrong value.** The inbox worker did a plain Redis
`INCR` on `unread:<user>`. On a missing key Redis creates it at 1 — with no expiry, because
`INCR` sets none. A user whose true count was 47 could end up pinned at a never-expiring 1. The
existing code accepted this on the grounds that "the cache will self-correct on next
ListInbox", which was true precisely because the list path ignored the cache and overwrote it.
That property is the one a cache-first read removes.

Separately, `notification.new` carried no count even though the worker had just computed one,
so clients incremented locally — ADR 0013 named this ("the honest fix is a richer server
payload, tracked separately") after count arithmetic diverging between two client
implementations was the defect that motivated the shared reducer.

Finally, the store's `start()` fetched the first page and *then* opened the websocket, so
anything published in between was lost outright: Centrifugo has no channel to deliver it to,
and nothing retries. `recoverable: true` covers that only where the engine keeps history, which
the bundled Helm Centrifugo does not.

## Decision

**We will serve the unread count from Redis, refreshed ahead of expiry, bounded by a cap, and
delivered over HTTP — not over a Centrifugo proxy.**

Concretely:

1. **Cache-first reads with refresh-ahead.** `GET /v1/inbox` and the new
   `GET /v1/inbox/unread-count` read `unread:<user>`. A miss fills it under `SET NX`; an entry
   with less than `unreadCountTTL - unreadCountRefreshAfter` remaining is recomputed under a
   short lease, with losers serving the slightly stale value; a Redis error falls back to the
   store *without* writing back.

2. **Refresh-ahead replaces the accidental self-heal.** The old behaviour bounded drift by
   *traffic*, which is backwards: the users who most need a recount are the ones who never open
   the panel, and they were the ones who never got one. Time-bounding it costs one
   authoritative count per user per refresh window regardless of request volume.

3. **The count saturates at `models.UnreadCountCap` (1000).** Exact below it; a returned 1000
   means "at least 1000". Postgres counts through a `LIMIT` subquery so the planner stops at the
   cap; DynamoDB additionally bounds its page loop.

4. **`ListInbox` no longer returns the count.** It is a property of the user, not of a page.

5. **The increment refuses to create a key.** `incrIfPresent` (Lua, mirroring the existing
   `decrIfPositive`) increments only an existing entry, never re-arms the TTL, clamps at the
   cap, and deletes a non-numeric value. A miss returns `UnreadCountMiss`, leaving the fill to
   an authoritative read — the only party that knows the answer.

6. **The worker's increment is idempotent** on notification ID, because delivery is
   at-least-once and a Centrifugo publish failure used to increment a second time on redelivery.

7. **`notification.new` carries an optional `unread_count`.** Optional, because
   `cmd/worker-inbox` wires NATS, Redis and Centrifugo and *no database*: on a cache miss it
   genuinely cannot know, and a guess there is how a badge becomes confidently wrong. Clients
   fall back to a local increment when it is absent.

8. **The client subscribes before it lists.** That converts the lost-publication window into a
   harmless duplicate, which the reducer already dedupes by id.

**We will not put the count on a Centrifugo connect or subscribe proxy**, and more generally
reads and mutations stay on HTTP. The websocket channel remains strictly server→client.

## Consequences

- A warm badge read is one Redis `GET` and touches no database. The pathological user — a very
  large unread backlog — now costs the same as an ordinary one on both stores.
- **`unread_count` is capped.** Both the OpenAPI and AsyncAPI schemas gain `maximum: 1000` and
  say what it means. The widget already collapses anything above 99 to `99+`, so no real user
  sees a difference.
- **A new public endpoint**, `GET /v1/inbox/unread-count`, and a new optional field on a public
  event. Both are additive; the list response is unchanged on the wire.
- **`ListInbox`'s signature changed** across the store interface, both implementations, the
  service and its mock. A cross-service contract change, hence this ADR.
- **A residual race is accepted and documented.** A fill reads the store at T0, an arrival at T1
  finds no key and so does not increment, and the T0 value lands at T2 — one short. The window
  is a single indexed count and the error is bounded by refresh-ahead. Closing it properly needs
  a separately-expiring delta key the filler reconciles against, which is a great deal of
  machinery for a badge.

> **Amendment, 2026-08-12 — the same race in the other direction, and the watermark that closes
> both.**
>
> The paragraph above reasons about a fill landing *after* an arrival, which loses an increment.
> The mirror case was not considered and is worse, because it does not look like a loss: a fill
> that lands *between* the row being persisted and its delivery seeds a count that already
> includes that notification, and the delivery then counts it again. The badge reads high, and
> unlike the undercount it is not obviously wrong to anyone looking at it.
>
> It shipped. It surfaced as a browser test failing about half the time — `GET /v1/inbox`
> answering `unread_count=2` for a single row, with the notification list itself correct — and it
> took a while to attribute, because the number is only wrong in the cache and every other layer
> agrees.
>
> **The cached entry now carries a watermark** alongside the count: the newest notification id
> that count includes. Ids are time-sortable, so an arrival at or below the watermark is already
> in the count and its increment is skipped. `unread:<user>` therefore becomes a two-field hash,
> under a versioned key name (`unread:v2:`) so v1's plain-integer values are ignored rather than
> failing `WRONGTYPE` until they expire.
>
> Three things about it are load-bearing, each learned by getting them wrong first:
>
> - **Only a fill or a refresh may move the watermark, never an increment.** Advancing it per
>   increment looks equivalent and is not: nothing orders `delivery.inbox`, so a message for an
>   older notification can be handled after a newer one, and a watermark moved to the newer id
>   makes the older arrival look already-counted and drops it for good. Observed as a count stuck
>   at 24 of 25.
> - **The count and the watermark must come from one store read.** Two reads straddle an insert,
>   and then one of them is ahead: either an arrival is lost or it is counted twice. Postgres does
>   both in a single statement. DynamoDB gets one descending pass over the by-user index — the
>   watermark is simply the first item — because a GSI is eventually consistent and a second query
>   can observe *less* than the first, which reintroduced the overcount about one run in eight.
> - **Marking a notification unread is not an arrival** and keeps the unguarded increment. It
>   flips a read flag on something the count already knows about, so the set of arrivals reflected
>   in the count has not changed and the watermark must not move.
>
> Mark-all-read now invalidates the entry rather than writing a zero, because a zero needs a
> watermark to go with it and that handler has no snapshot to take one from.
- **A crash between claiming the dedup guard and incrementing loses an increment.** Deliberate:
  the alternative ordering repeats one, and an undercount heals at the next refresh while an
  overcount compounds with every retry.
- New migration `000018` adds `idx_notifications_unread`, built `CONCURRENTLY`. It is a sparse
  partial index containing only unread rows, so it *shrinks as users read*.
- **Amended 2026-08-16 — the watermark needed an index of its own.** `000018` covers the capped
  count but not the `max(id)` watermark this ADR introduces alongside it, and *every* `user_id`
  index on `notifications` was partial. A partial index cannot serve a predicate it does not
  imply, and the watermark is over all of a user's rows, so the planner fell back to a backward
  scan of the primary key — which, because ids are time-sortable, walks the table newest-first
  across every user. Cost is therefore proportional to how long since that user's last
  notification: 0.2 ms for an active user, **133 ms and the entire 450k-row table** for one with
  an empty inbox. Migration `000021` adds a non-partial `(user_id, id DESC)` index, taking the
  whole query from 212 ms to 0.47 ms at p95 under 100,000 connections. See
  [issue #170](https://github.com/HermesNotifications/hermes/issues/170) and
  [the load-test write-up](../loadtest/clean-dataset-100k-2026-08-16.md). The decision in this
  ADR is unchanged; this records that it was incompletely indexed.
- **Follow-up, not done here:** DynamoDB still answers the count with a filtered `Query`, now
  page-bounded. The real fix is a sparse GSI (`unread_uid`, KEYS_ONLY) maintained by the
  mutation methods, which makes scanned equal counted. It needs a backfill job.

## Alternatives considered

**A Centrifugo subscribe proxy returning the count as the subscription's initial data.** The
most attractive option on its face: zero extra round trips, and real server-side channel
authorization as a bonus. Rejected on three grounds.

*There is no fail-open.* Centrifugo passes a proxy error or timeout straight to the client and
the subscription is denied. An inbox blip therefore stops users *receiving notifications*, not
merely seeing a count — inverting the cost against the benefit.

*The reconnect profile has no controller.* Centrifugo runs two replicas, so a rollout reconnects
every socket in two roughly one-second waves. The inbox HPA has a 30s stabilization window and
scales +2 pods/60s; the spike is over before it evaluates. The proxy would have to survive its
worst case statically, at `minReplicas`.

*So it would have to be cache-only* — never falling through to Postgres — which means it would
sometimes return no count, which means the client needs the HTTP fallback regardless. It earns
nothing it does not already have, while adding a failure mode.

Two further costs made it worse than it looked: `middleware.RateLimit` has no path exemption, so
proxy calls would share one bucket keyed on the empty string and need a separate listener plus a
NetworkPolicy (production is default-deny with no `centrifugo → hermes-inbox` rule); and the v5
Kustomize and v6 Helm deployments use entirely different proxy configuration keys.

**A Centrifugo connect proxy.** Everything above, plus it gates *opening a connection at all* on
the inbox tier. It does buy something real — Hermes would validate the JWT itself, retiring the
single-`token_hmac_secret_key` coupling that currently makes signing-key rotation a manual
coordinated operation. That is worth doing on its own merits and should get its own ADR. The
unread count is not the reason to do it.

**A `subs` claim in the connection JWT.** Elegant: Hermes already mints the token, Centrifugo
supports server-side subscriptions carrying initial `data`, and it needs no new infrastructure
and creates no new availability coupling. Rejected because tokens live for hours and are reused
on reconnect, so the embedded count would be staler than the ten-minute cache it bypassed; it
also moves the subscription server-side, changing where publications arrive on the client, and
puts the admin service on the unread-count path. Recorded because someone will propose it again.

**Moving reads and mutations onto the websocket generally.** Centrifugo RPC does not remove a
network hop, it adds one — browser → Centrifugo → Hermes — and costs the HTTP middleware that
already carries authentication and rate limiting, the `otelhttp` metrics the existing alerts are
built on, and the property that Centrifugo being down does not stop a user reading their inbox.
The one genuine advantage, that websockets are exempt from CORS while the HTTP API is not, is
better bought by adding CORS, which ADR 0013 already earmarks for its own ADR.

**Leaving the cache write-only and simply adding the index.** Cheaper, and it would have fixed
the query cost. It leaves the TTL-less-key bug in place and leaves DynamoDB paying for a
filtered scan per badge read, so it addresses the symptom that was easiest to see rather than
the one that was hardest.
