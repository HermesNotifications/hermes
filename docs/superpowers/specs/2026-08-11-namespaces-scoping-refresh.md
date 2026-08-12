# Namespaces: scoping refresh

**Status:** Scoping — not a design. Supersedes the staleness of
[2026-03-28-namespaces-design.md](2026-03-28-namespaces-design.md); does not replace it as a
design until the open decisions below are answered.

**Date:** 2026-08-11
**Author:** Daryl Robbins

---

## Why this exists

[ADR 0012](../../adr/0012-api-keys-are-not-scoped-to-organizations.md) parks namespace scoping
and sets a revisit trigger with an explicit order of work:

> when namespaces are picked up, refresh that spec first, and treat `api_keys.namespace_id` as
> part of that work rather than as a standalone security fix.

This is that refresh pass. It is deliberately **not** a design document. It does three things:

1. audits the March spec against the schema and services as they exist today;
2. sizes the work in phases;
3. names the decisions that need answering before a design can be written.

It answers none of those decisions, because the last time someone reasoned from the code alone
about this axis they produced [ADR 0011](../../adr/0011-api-keys-are-scoped-to-an-organization.md)
— a convincing argument for the wrong answer, superseded within a day.

## What the March spec got right, and should be kept

The two-axis model is sound and survives unchanged:

- **Organization = customer.** Global, shared across applications, owns users. Stays a
  per-request parameter.
- **Namespace = application/product.** Owns templates and API keys.

`api_keys.namespace_id`, nullable, NULL meaning a global key, is still the right shape — ADR 0012
says so explicitly. So is "no foreign keys on namespace_id, validate at write time", which keeps
the schema portable across the Postgres and DynamoDB paths.

## What is stale

### 1. It scopes two tables that no longer exist

The spec's schema section targets `notification_types` and `notification_groups`. Migration
`000011_subscriptions_templates` **dropped both**, replacing them with `subscription_categories`,
`subscriptions`, `notification_templates` and `user_subscriptions`.

The mapping is not mechanical, and getting it backwards is the main risk in this whole phase:

| March spec | Scoping it assigned | Today's table | Why |
|---|---|---|---|
| `notification_types` | namespace-scoped | `notification_templates` | Each product has its own copy |
| `notification_groups` | **global** | `subscription_categories` | Cross-cutting (Marketing, Security); user preferences hang off them |

**`subscription_categories` inherits the *groups* row, not the *types* row.** It is what
`user_subscriptions` references and what a user's opt-in gate is expressed against. Scoping it per
namespace would fragment a user's preferences per product, which is the exact opposite of the
unified inbox the spec exists to enable. Anyone reading the spec's "notification types →
namespace-scoped" line and pattern-matching it onto "categories" would make that mistake.

### 2. It predates the Send/Dispatch split

The spec has one service resolving templates at send time. Today Send is a thin ingestion layer
that does no template or channel work, and Dispatch resolves both after consuming from NATS. So:

- Namespace has to be **resolved at Send** (it comes from the credential) and **carried on the
  message**, because Dispatch is where template lookup happens and it never sees the API key.
- `hermenats.SendMessage` (`internal/nats/messages.go`) carries `OrganizationID` and no namespace
  field. Adding one is a **cross-service contract change** — the kind ADR-worthy under CLAUDE.md,
  and one that has to be rolled out compatibly with messages already in the stream.

### 3. It predates the tenant → organization rename

The spec says "tenant" throughout. [ADR 0003](../../adr/0003-rename-tenant-to-organization.md) and
migration `000017` renamed it. Mechanical, but the spec is unreadable next to the code until it is
fixed.

### 4. Its rate limiting motivation is now half-built

The spec cites "rate-limit on a per-product basis" as a reason for namespaces.
[ADR 0016](../../adr/0016-distributed-rate-limiting-with-local-fallback.md) has since built the
per-credential half. The limiter is deliberately an **ordered chain of scopes**
(IP → credential → namespace → plan), so a namespace bucket is additive: a key function, a limit
lookup, and one more entry in the chain. The `api_keys` table already carries
`rate_limit_per_second` and `rate_limit_burst`, so a namespace would want the same two columns and
the same "NULL means default" sentinel.

This is the cheapest part of the phase and should be sequenced **last**, not first — it is a
consequence of namespaces existing, not a reason to rush them.

## Suggested phasing

Each phase is independently shippable and leaves the system correct.

**Phase 1 — the entity.** `namespaces` table, `ns_` prefixed IDs, CRUD under `/v1/namespaces`,
a `default` namespace created by the migration. Nothing reads it yet. Small.

**Phase 2 — credential scoping.** `api_keys.namespace_id` nullable, populated on create, exposed
on the admin API, resolved into `auth.ValidatedKey`. NULL keys stay global, so every existing key
is unaffected. Small, and this is the piece ADR 0012 named.

**Phase 3 — the send path.** Namespace on `SendMessage`, resolved at Send from the credential,
consumed by Dispatch for template lookup. Needs an ADR for the contract change and a compatible
rollout. **This is the phase with real risk** — it touches the hot path and a stream that may hold
in-flight messages.

**Phase 4 — template scoping.** `notification_templates.namespace_id`, uniqueness moves to
`(namespace_id, slug)`, existing rows to `default`. Medium; the resolution logic in
`internal/dispatch` is the substance.

**Phase 5 — namespace rate limits.** Two columns, one scope in the existing chain. Small,
given ADR 0016.

**Explicitly out:** environments. The March spec already defers them and nothing since changes
that.

## Decisions this needs before a design can be written

1. **Is a namespace-scoped key restricted to its namespace's templates only, or does it merely
   default to them?** Restriction is a real authorization boundary and the more useful answer, but
   ADR 0012's warning applies directly: check the calling pattern before concluding that
   constraining a credential closes a hole.

2. **What is the failure mode when a namespace-scoped key sends against a template in another
   namespace?** 403, 404, or silently resolve in its own namespace and miss. This determines
   whether namespace is an authorization boundary or an organizational one.

3. **Does the unified inbox filter by namespace?** The spec says notifications are *tagged* with
   it for analytics. If the widget also filters, that is a public contract change under
   [ADR 0013](../../adr/0013-embeddable-inbox-widget-contract.md).

4. **Does the DynamoDB path need namespace in its key schema now or later?**
   [ADR 0001](../../adr/0001-dynamodb-model-via-extenddb.md) makes access patterns expensive to
   change after the fact, so "later" is not free here the way it is in Postgres.

5. **Is the single-operator trust model still the premise?** ADR 0012 rests its whole argument on
   it. If Hermes is ever offered as a shared multi-operator service, the boundary belongs at the
   namespace level and that is a larger decision than any of the above.

## What not to do

**Do not add `api_keys.namespace_id` on its own.** It is nullable and nothing would write it, so
it would sit there inviting someone to start enforcing it without reading ADR 0012 — which is
precisely the alternative that ADR rejected, in those words.

**Do not treat this as a security fix.** Under the single-operator model, cross-namespace
visibility is the operator's own data plane. ADR 0011 read it as a hole, and was wrong.
