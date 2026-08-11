# ADR 0011: Scope API keys to an organization, and derive the organization from the key

**Status:** Accepted  
**Date:** 2026-08-10  
**Author:** Daryl Robbins

---

## Context

`api_keys` was `(id, key_hash, name, created_at)` plus `permissions` — **nothing tied a key to
an organization**. `/v1/send` took the organization from the request body
(`req.To.OrganizationID`) and used it to namespace the idempotency key and to populate the
`SendMessage` published to NATS.

The only authorization on that path is `requirePermission(ctx, auth.PermNotificationsSend)`,
added by finding 3. That check gates **whether** a key may send. It has never gated **for
whom**. The comment introducing it says the previous defect let a key "forge notifications to
any user in any organization"; the fix added a permission check and left the organization
still coming from the caller.

So on any deployment hosting more than one organization, a key belonging to customer A could
deliver notifications into customer B's users' inboxes given only B's organization ID —
authenticated, permitted, and attributed to B. Organization IDs are UUIDs and not secret; they
appear in admin API responses and in every send request the customer already makes.

This is invisible on a single-organization deployment, which is why it survived: the
self-hosted evaluation path has exactly one organization, and the reference stack had no real
tenants yet.

It also blocks per-tenant rate limiting and quotas. Enforcing a quota keyed on a value the
caller chooses is not enforcement.

## Decision

We will add `api_keys.organization_id` (migration 000018) and **derive the acting organization
from the key**, not from the request.

`/v1/send` rejects with `403` when the key's organization and the request body's organization
disagree. `auth.ValidatedKey` carries `OrganizationID`, populated from the store and from the
Redis key cache.

Creating a key through the admin API now **requires** `organization_id`, validated against an
existing organization.

**The column is nullable, and a key without an organization is left unconstrained.**

## Consequences

**The cross-tenant hole is closed for every key minted from now on.** It is not closed for keys
that already exist, and this ADR does not pretend otherwise.

**Existing keys keep working.** A NOT NULL column would have required guessing which
organization each existing key belongs to, and there is no signal in the data to guess from —
the table never recorded one. A wrong guess either breaks a live integration or silently
mis-attributes its sends, and the second is worse than the hole being closed a week later.

**`hermes.send.unscoped_key_uses` is the migration's completion signal.** Every send by a key
with no organization increments it. While it is non-zero, unconstrained keys are still in use.
When it reads zero across a full billing period, `organization_id` can be backfilled from
observed usage and tightened to `NOT NULL` in a follow-up migration. **That follow-up is the
point at which this ADR's decision is actually finished**; until then the system is in a
documented intermediate state.

**A permissive default for a security control is a real cost.** A key with no organization
fails *open*. The mitigation is that it fails open to exactly today's behaviour — no deployment
becomes less safe than before this change — and the population that can do so is finite,
countable, and shrinking. A fail-closed rollout would have been a breaking change delivered
without warning to every existing integration.

**Cached keys lag by up to the 5-minute TTL.** `internal/send` caches validated keys in Redis.
Entries written before this change carry no organization and are treated as unscoped until
they expire. The read and write sides are now a single named type (`cachedAPIKey`) precisely so
they cannot drift again — two anonymous structs would have let one side gain the field and the
other silently unscope every cache hit.

**The 403 is deliberately vague.** It says the key cannot send for that organization, not
whether the organization exists. Distinguishing the two would let a key probe for valid
organization IDs.

**Admin key creation is a breaking API change.** `POST /v1/apikeys` now returns `422` without
`organization_id`. This is intentional: a key that names no organization is the defect, and
minting more of them while the metric is meant to be draining to zero would make the follow-up
migration unreachable.

## Alternatives considered

**Make `organization_id` NOT NULL immediately and backfill.** Rejected: no data exists to
backfill from. The alternatives were assigning every key to an arbitrary organization (silent
mis-attribution) or to none (breaking every integration at once).

**Ignore the body and always use the key's organization.** Tempting — it removes the mismatch
case entirely. Rejected because it changes the meaning of an existing request silently: a
caller sending organization B with a key scoped to A would get a `202` and delivery to A, with
nothing indicating their request was reinterpreted. A `403` is louder and cannot be
misread. Once `NOT NULL` lands, dropping the body field entirely becomes the clean end state.

**Keep the organization in the body and validate it against a separate key→organization
mapping table.** Rejected as the same thing with an extra join and a second place for the
mapping to go stale.

**Fix it at the dispatch layer instead.** Rejected: dispatch acts on a `SendMessage` that has
already been accepted and acknowledged with a `202`. Authorization belongs where the credential
is, at the edge, before the platform takes responsibility for the work.
