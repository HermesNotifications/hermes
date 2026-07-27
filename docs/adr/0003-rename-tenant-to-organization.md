# ADR 0003: Rename `tenant` to `organization`, and name the app as the isolation boundary

**Status:** Proposed  
**Date:** 2026-07-27  
**Author:** Daryl Robbins

---

## Context

Hermes uses the word **tenant** for a concept that is not the isolation boundary. In
multi-tenant SaaS the term has a settled meaning — the unit of isolation, the thing a
credential is scoped to. Hermes's `tenant` is deliberately none of those things.

The real model is:

- An **app** — a product that integrates Hermes — is the tenant in the traditional sense.
  It is the trust and isolation boundary, and it is what an API key authenticates.
- An app sends notifications on behalf of **many** organizations (its own customers).
- Those organizations **span apps**: the same organization can be served by more than one
  app.

Because organizations cross apps by design, an organization cannot be an isolation
boundary — a credential scoped to one would break the primary use case.

The schema already reflects this, which is why the naming is the only thing out of step.
`api_keys` carries no organization reference (`migrations/000002`, `000010`) — intentionally.
Only two tables carry `tenant_id` at all: `users` and `notifications`. The entire config
plane — `subscription_categories`, `subscriptions`, `notification_templates`,
`jwt_signing_keys` (`migrations/000011`, `000009`) — is app-global with no tenant column.
There is nothing tenant-scoped at the config layer to isolate.

The mismatch has a measurable cost. The docs assert the opposite of the model:
`glossary.md:6` calls the tenant "the top-level isolation boundary" and
`integration-guide.md:78` says tenants "represent isolated organizations." A July 2026
architecture review read the name and those two sentences and filed a false-positive
**critical** vulnerability — "any API key can send as any tenant" — for behaviour that is
the intended design. Every future auditor, pen-tester, and new contributor is set up to
reach the same wrong conclusion. The costlier inverse is equally live: someone assuming the
isolation exists and designing a feature on that false premise, or "fixing" the finding by
scoping API keys to an organization and breaking cross-organization sends.

Compounding it, the concept that *is* the boundary — the app — appears nowhere: no table,
no column, no glossary entry, no doc section. The vocabulary gives the boundary's name to a
non-boundary and leaves the boundary unnamed.

Two timing factors make now materially cheaper than later:

- **Nothing is in production and there are no external consumers**, so no compatibility
  window is required.
- **The only stored tenant-keyed data is short-lived.** The sole `TENANT#` key material in
  the codebase is `idem_pk` on the idempotency GSI
  (`internal/store/dynamo/notifications.go:107,144`), whose entries expire inside a 24-hour
  dedup window. [ADR 0001](0001-dynamodb-model-via-extenddb.md)'s Phase 2 `hermes-users`
  design would put `TENANT#<tenant_id>` into GSI *partition* keys, and
  [ADR 0002](0002-provider-plugin-model-bus-native-isolation.md) is about to freeze the
  `delivery.*`/`provider.*` subject family and its message schemas as "a public, versioned
  surface that providers depend on." After either lands, this rename means migrating stored
  partition keys and breaking third-party provider authors.

## Decision

**1. We will rename `tenant` to `organization` throughout** — database, Go code, REST API,
JWT claim, NATS message contracts, OpenAPI/AsyncAPI specs, and all generated SDKs. Because
nothing is in production, this is a **clean break with no compatibility aliases**: no
dual-accepted request fields, no dual-emitted claims, no deprecation window.

`organization` is chosen because it describes the entity accurately (the organization a
notification concerns) while carrying materially weaker isolation connotations than
`tenant`, `workspace`, or `project`.

**2. We will name the app as the isolation boundary, and enforce it by deployment
separation.** One Hermes installation — one database — serves exactly one app. API keys
authenticate the app; they are deliberately **not** scoped to an organization, and an app
may legitimately act on behalf of any organization within its installation. This is
documented as the security model rather than left to be inferred from the schema.

**3. We will record the deployment-separation invariant explicitly.** Because no `app`
entity exists in the schema, the boundary rests *entirely* on running separate
installations. Nothing in the code enforces it. Any future change that puts two apps in one
installation must first introduce an app entity and scope every table by it — that is not
planned, and this ADR is the record that it is a prerequisite rather than an optimization.

**Revisit trigger.** Re-evaluate decision 3 if a deployment ever needs to serve more than
one app (for example a shared control plane, or consolidating installations for cost). At
that point the app must become a real entity before, not after, the consolidation.

## Consequences

**Database.** A migration renames `tenants` → `organizations`, `users.tenant_id` →
`organization_id`, `notifications.tenant_id` → `organization_id`, and the two indexes
(`idx_users_tenant_external`, `idx_notifications_idempotency`). This is in-place column
renaming, for which `migrations/000011` is direct precedent — it renamed `type_id` →
`template_id` and `group_id` → `category_id` the same way.

**JWT.** `jwt_signing_keys.tenant_id_claim` becomes `organization_id_claim`, and its default
value changes from `tenant_id` to `organization_id`. Note this column already parameterizes
the claim name per signing key, so externally-issued tokens that use a different claim name
keep working — the flexibility is preserved, only the default moves.

**Wire contracts.** `SendMessage.TenantID` → `OrganizationID` and the equivalent on
`DeliveryMessage` (`internal/nats/messages.go`), with `api/async/asyncapi.yaml` regenerated.
Landing this *before* ADR 0002's subject-contract freeze means third-party provider authors
never see the old name.

**DynamoDB.** The `TENANT#<id>#IDEM#<key>` prefix becomes `ORG#<id>#IDEM#<key>`. Safe
because those entries expire within the 24-hour dedup window; the worst case at cutover is a
duplicate send for a request replayed during the switch, since old- and new-format keys will
not match each other.

**Public API and SDKs.** `tenant_id` → `organization_id` in the send request body,
`/v1/tenants` → `/v1/organizations` (`internal/admin/handler_tenants.go:41,73`), and
`TenantsApi`/`TenantItem`/`CreateTenantInputBody` → their `Organization*` equivalents across
the Java, Python, .NET, and TypeScript SDKs. All four are regenerated (`make openapi` plus
SDK generation) and take a major version bump.

**Documentation this discharges.** The false isolation claims at `glossary.md:6` and
`integration-guide.md:78`; the missing multi-tenancy-isolation section flagged in the July
2026 review; and a new glossary entry for *app* alongside the corrected *organization*
entry.

**Costs, honestly.** Roughly 130 files touched. Every SDK is a breaking major release. Any
in-flight branch rebases through a wide mechanical rename. Beyond the services: the
load-test fixtures and manifests (`cmd/loadseed`), the admin portal (`web/admin`), and four
observability overlay files reference tenant by name — the observability ones carry the real
risk, since renaming a metric label silently breaks saved queries, dashboards, and any alert
rule that groups by it. Those need a coordinated dashboard update rather than a find-replace.

**Not discharged by this ADR.** ADR 0002 committed to a follow-up ADR for the normalized
content/contact model; PR #43 shipped that phase without one. That ADR is still owed and is
out of scope here.

## Alternatives considered

**Keep `tenant`, fix only the documentation.** Much cheaper, and it was the initial
instinct. Rejected: the name is what a reader encounters first and trusts most, and it
asserts the precise opposite of the model. Documentation whose job is to explain that a
tenant is not a tenant is a smell, and it has already failed once — the review that produced
the false positive had those docs available.

**Rename internally, freeze `tenant_id` on the wire as a legacy name.** Rejected: it leaves
the misleading term in exactly the place integrators and auditors look first, which is where
the damage occurs. This trade only makes sense to protect published consumers, and there are
none.

**Rename to `account`.** Rejected outright: Better Auth already owns an `account` table
(`migrations/000012`), so the collision would be worse than the problem being solved.

**Rename to `customer`.** Genuinely viable and arguably the most precise description of the
relationship. Rejected as ambiguous about *whose* customer — these are the app's customers,
not Hermes's, and `customer` in a Hermes-facing API invites the wrong reading.

**Introduce an `apps` table now and scope API keys to it.** Rejected as premature: one
installation serves one app, so the entity would be a constant single row threaded through
every query path, buying no isolation that deployment separation does not already provide.
Recorded in decision 3 as the required first step should that assumption ever change.

**Defer until after ADR 0001 Phase 2 and provider GA.** Rejected: the cost strictly
increases with time. Waiting converts a spec regeneration into a stored-partition-key
migration plus a breaking change to a public provider contract.
