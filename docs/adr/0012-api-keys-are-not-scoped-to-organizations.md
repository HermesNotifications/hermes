# ADR 0012: API keys are not scoped to organizations; the organization is a per-request parameter

**Status:** Accepted  
**Date:** 2026-08-11  
**Author:** Daryl Robbins

Supersedes [ADR 0011](0011-api-keys-are-scoped-to-an-organization.md).

---

## Context

ADR 0011 added `api_keys.organization_id` and made `/v1/send` reject a request whose
`organization_id` differed from the key's. It described the previous behaviour — an API key that
could address any organization — as a cross-tenant authorization hole.

**That diagnosis was wrong, and the fix broke the intended calling pattern.**

Hermes has two orthogonal axes, and they are easy to conflate:

- An **organization** is a *customer*. It is global and **shared across applications** — two
  different applications can both serve Customer A. It owns users.
- An **application** (namespace) is an internal product using the Hermes instance. It is the
  thing that owns templates and API keys.

A single application legitimately sends to **every customer it serves**. That is why
`organization_id` is a parameter on each send request rather than a property of the credential:
one key, many organizations, by design. Constraining a key to one organization does not close a
hole — it makes the normal case impossible, and would have produced a `403` on the second
customer any application tried to notify.

The trust model reinforces this. Hermes is deployed self-hosted by a single operator, so
visibility across organizations within one deployment is the operator's own data plane, not a
tenant boundary being crossed.

## Decision

We will **not** scope API keys to organizations. `organization_id` stays a per-request
parameter on `/v1/send`, and `api_keys` carries no organization column.

Migration 000018, `ValidatedKey.OrganizationID`, the `403` mismatch check, the required
`organization_id` on `POST /v1/apikeys`, and the `hermes.send.unscoped_key_uses` metric are all
removed.

When namespaces are implemented, the correct field is **`api_keys.namespace_id`, nullable**
(NULL meaning a global key). A namespace-scoped key would derive its namespace from the key
while still taking `organization_id` per request. The two are not alternatives; they are
different axes.

## Consequences

**The calling convention is unchanged from before ADR 0011.** Nothing that worked stops
working, and integrators keep sending `organization_id` per request.

**`POST /v1/apikeys` no longer requires `organization_id`.** This reverts the breaking change
ADR 0011 introduced. Specs, generated SDKs, and the admin portal move back with it.

**Keys remain scoped only by permission.** Any key holding `notifications:send` can send for any
organization on the deployment. Under the single-operator model this is the intended posture,
not an outstanding risk — but it is worth stating plainly so nobody re-derives ADR 0011's
conclusion from the code alone.

**Namespace scoping is still owed, and is not near-term.** It is parked as its own feature phase
and its design spec is stale: it targets `notification_types`/`notification_groups`, dropped in
migration 000011, and predates the Send/Dispatch split. **Revisit trigger:** when namespaces are
picked up, refresh that spec first, and treat `api_keys.namespace_id` as part of that work rather
than as a standalone security fix.

**ADR 0011 is kept, superseded rather than deleted.** Its reasoning reads convincingly and
arrives at the wrong answer, which is exactly the sort of thing that gets re-proposed. The record
of why it is wrong is more useful than a clean tree.

## Alternatives considered

**Repoint the column at `namespace_id` now instead of reverting.** Rejected: namespaces do not
exist, the design spec is stale, and the work is deliberately deferred. Inventing a partial
namespace implementation as a side effect of unwinding a mistake is how the stale spec got that
way.

**Keep the column but stop enforcing it.** Rejected: an unenforced nullable column that nothing
writes is an invitation to enforce it later without revisiting why it was wrong. Removing it
makes the next person read this ADR.

**Keep enforcement but allow a key to list several organizations.** Rejected: it encodes the
customer list into the credential, so onboarding a customer means reissuing keys. The set of
customers an application serves is exactly the thing that changes most often.

**Treat it as a real hole and add a different tenant boundary.** Rejected because the premise
does not hold on a single-operator self-hosted deployment. If Hermes is ever offered as a shared
multi-operator service, the boundary belongs at the namespace/deployment level, and that is a
larger decision than a column on `api_keys`.
