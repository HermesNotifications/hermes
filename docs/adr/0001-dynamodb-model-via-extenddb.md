# ADR 0001: Adopt the DynamoDB programming model for the hot notification path

**Status:** Accepted (amended 2026-06-12: dual store path retained as a temporary hedge while ExtendDB matures — see Rollout posture)  
**Date:** 2026-05-29  
**Author:** Daryl Robbins

---

## Context

Hermes's three highest-volume tables — `notifications`, `notification_events`, and
`user_subscriptions` — grow unbounded with traffic, are accessed almost exclusively by
primary/sort key, and have no cross-table transactions. The config/control-plane tables
(tenants, templates, categories, api_keys, jwt_signing_keys, users) are small, rarely
written, and benefit from rich SQL.

We previously evaluated moving the hot tables to DynamoDB for scale but stalled on
multi-cloud portability: DynamoDB is AWS-only, and the standing architectural goal is
cloud portability (multi-cloud, Crossplane-managed data services).

[ExtendDB](https://extenddb.org/) (announced May 2026, maintained by AWS, Apache 2.0,
written in Rust) resolves this tension. It is **not a database** — it is an adapter that
speaks the **DynamoDB wire protocol** in front of a pluggable backend (PostgreSQL
reference, Apache Cassandra for horizontal scale, community backends). Applications use
standard AWS SDKs without modification; only the endpoint changes.

## Decision

Adopt the **DynamoDB programming model** (AWS Go SDK v2 `dynamodb`) as the persistence
abstraction for the high-volume notification path. Select the backend per environment:

| Environment | Backend |
|---|---|
| Self-hosted default | Native Postgres repositories (`internal/store/postgres/`) — no extra dependency |
| Local dev / CI | DynamoDB Local (`amazon/dynamodb-local`) — no auth, no TLS, no init step |
| Multi-cloud / on-prem / air-gapped at scale | ExtendDB + Postgres (or Cassandra for horizontal scale) |
| AWS at scale | Native DynamoDB — **zero application code change** |

Backend selection is env-driven: `HERMES_DYNAMO_ENDPOINT` unset = native Postgres
repositories (`internal/store/postgres/`, the self-host default); set = DynamoDB Local,
ExtendDB, or any DynamoDB-compatible endpoint. Native DynamoDB on AWS (endpoint set to
the regional AWS endpoint, SDK default credential chain) is pending the credential
wiring noted in `internal/store/dynamo/dynamo.go`. `HERMES_DYNAMO_REGION` defaults to
`us-east-1`.

Config/control-plane tables (`tenants`, `subscription_categories`, `subscriptions`,
`notification_templates`, `api_keys`, `jwt_signing_keys`) and the Better Auth tables
remain on native Postgres — see [Entities considered and not migrated](#entities-considered-and-not-migrated)
for the per-table rationale.

`users` is **not** in that list: it grows unbounded with the customer's end-user base —
the same scaling axis as `notifications` and `user_subscriptions` — and is accessed by
key on the hot path. It is a Phase 2 candidate; see [`hermes-users`](#hermes-users-phase-2-candidate)
below.

## Scope

Migrate in three phases, lowest-risk first:

| Phase | Tables / interfaces |
|---|---|
| 1 (spike) | `notification_events` → `hermes-events` table; `user_subscriptions` → `hermes-user-subscriptions` table |
| 2 | `notifications` → `hermes-notifications` table (inbox path, status rollup); `users` → `hermes-users` (co-located with the `USER#` partition) |
| 3 | Observability, load-test validation of both store backends |

## Table design

### `hermes-events`

Replaces `notification_events`.

```
PK (pk):  NOTIF#<notification_id>   S
SK (sk):  EVT#<id>                  S

Attributes:
  channel     S
  event       S
  severity    S
  metadata    S   (JSON)
  created_at  S   (RFC3339)
  ttl         N   (Unix epoch seconds — native TTL replaces DeleteEventsOlderThan)
```

Access patterns:
- `InsertEvent` / `InsertEvents` → `PutItem` / `BatchWriteItem`
- `GetNotificationEvents` → `Query` PK, SK begins_with `EVT#`, ascending order
- TTL deletion replaces `DeleteEventsOlderThan`; the cleanup binary becomes a no-op for
  events managed by DynamoDB.

### `hermes-user-subscriptions`

Replaces `user_subscriptions`.

```
PK (pk):  USER#<user_id>            S
SK (sk):  SUB#<subscription_id>     S

Attributes:
  user_id          S
  subscription_id  S
  opted_in         BOOL
  created_at       S   (RFC3339)
```

Access patterns:
- `GetUserSubscription` → `GetItem`
- `GetUserSubscriptions` → `Query` PK, SK begins_with `SUB#`
- `SetUserSubscription` → `PutItem` (upsert — overwrites existing)
- `DeleteUserSubscription` → `DeleteItem`

### `hermes-users` (Phase 2 candidate)

Replaces `users` (the Hermes end-user profile/contact table — **not** the Better Auth
identity tables, which stay on Postgres). End users grow unbounded with the customer's
user base, are read by key on the hot path (`EnsureUser` on every send, `GetUserByID`
for contact info), and have no rich-SQL access needs — so they fit the DynamoDB model
by the same criteria as the events/subscriptions tables.

The profile co-locates in the **same `USER#<user_id>` partition** as
`hermes-user-subscriptions`, so "load a user with their subscriptions" is a single
`Query`:

```
PK (pk):  USER#<user_id>            S
SK (sk):  PROFILE#                  S   (singleton item per user)

GSI "by-external-id" (EnsureUser upsert lookup):
  PK:  TENANT#<tenant_id>
  SK:  EXT#<external_id>

GSI "by-tenant" (admin ListUsers):
  PK:  TENANT#<tenant_id>
  SK:  USER#<user_id>

Attributes:
  user_id      S
  tenant_id    S
  external_id  S
  email        S   (nullable)
  phone        S   (nullable)
  locale       S   (nullable)
  created_at   S   (RFC3339)
```

Access patterns:
- `GetUserByID` → `GetItem` (PK `USER#<id>`, SK `PROFILE#`)
- `EnsureUser(tenant_id, external_id)` → `Query` the `by-external-id` GSI, then
  `PutItem` if absent (the upsert is the only composite-key path that needs a GSI)
- `ListUsers(tenant_id)` → `Query` the `by-tenant` GSI (replaces the Postgres scan)

Open question for Phase 2: `EnsureUser` is a read-then-write that must stay idempotent
under concurrent first-sends for the same external user; a conditional `PutItem`
(`attribute_not_exists`) on a deterministic `user_id` derived from `(tenant_id,
external_id)` avoids the GSI race. Validate during the spike.

### `hermes-notifications` (Phase 2)

> **Implementation note (2026-06-13):** The ADR originally proposed `PK=USER#<user_id> / SK=NOTIF#<id>`
> so the inbox query could be a primary-key `Query`. The implementation uses a different layout that
> keeps the notification itself under its own partition for O(1) `GetItem` by notification ID (the
> primary admin access pattern), and uses a dedicated GSI for inbox listing:

```
PK (pk):  NOTIF#<notification_id>   S
SK (sk):  NOTIF#<notification_id>   S   (same as PK — single-item table pattern)

GSI "gsi-by-user" (inbox listing):
  PK:  user_id          S   (denormalized attribute on each item)
  SK:  notif_id         S   (time-sortable ID — ScanIndexForward=false gives newest-first)
  Projection: ALL

GSI "gsi-by-idempotency" (sparse — only items with idem_pk attribute are indexed):
  PK:  idem_pk      S   (value: "TENANT#<tenant_id>#IDEM#<key>")
  SK:  created_at   S   (RFC3339 — range query for 24-hour dedup window)
  Projection: ALL

Attributes:
  notif_id      S
  user_id       S
  tenant_id     S
  status        S
  status_rank   N   (numeric rank; ConditionExpression prevents regression)
  inbox_state   S   ("active" | "archived" | "deleted" — FilterExpression on GSI queries)
  sent_at       S   (RFC3339, nullable)
  delivered_at  S   (RFC3339, nullable)
  read_at       S   (RFC3339, nullable)
  archived_at   S   (RFC3339, nullable)
  deleted_at    S   (RFC3339, nullable)
  title, body, channels (SS), template_id, category_id, idem_pk (sparse), ...
```

## Logic ported from SQL to application code

### Status-rollup state machine

The Postgres `CASE`-based rank comparison (`events.go:95–102`) that prevents status
regression becomes a **DynamoDB conditional write**:

```
ConditionExpression: "attribute_not_exists(pk) OR status_rank < :new_rank"
```

This is semantically equivalent and handles out-of-order events identically.

### Partial-index filter semantics (inbox query)

`WHERE archived_at IS NULL AND deleted_at IS NULL` (currently a Postgres partial index)
maps to a DynamoDB `FilterExpression` on the `Query`. For high-volume users, a GSI with
`archived_status` (values: `ACTIVE`, `ARCHIVED`, `DELETED`) as part of the SK could push
the filter into the index — deferred to Phase 2 benchmarking.

### Unread count

Already Redis-backed (10-min TTL). No change needed.

### Cursor-based inbox pagination

Postgres keyset pagination uses a compound cursor `(created_at, id) < (cursor_ts, cursor_id)`
(`internal/store/postgres/inbox.go`). The DynamoDB implementation uses a simpler cursor:
`base64(notif_id)` — the notification ID is a time-sortable base62 string (from
`internal/id/v2`), so descending SK order on `gsi-by-user` is already newest-first, and the
cursor is just the last returned item's ID reconstructed into the `ExclusiveStartKey` map.

**Cutover note.** Cursors from the Postgres inbox path are opaque to clients but are **not
forward-compatible** with the DynamoDB path: a client holding a Postgres-format cursor
(`created_at_ns|id` base64) will receive an `"invalid cursor"` error on the first page
request after the store is switched to DynamoDB. This is handled gracefully in
`NotificationStore.ListInbox` (`internal/store/dynamo/notifications.go`) — `base64.DecodeString`
returns an error on a malformed cursor, which surfaces as `"invalid cursor: ..."` to the
caller. Clients should treat any cursor error as an indication to re-request page 1 with an
empty cursor. No data migration is required: cursors are short-lived session state, not
persisted data.

## Entities considered and not migrated

The deciding criterion for moving a table to the DynamoDB model is **unbounded growth
with traffic/user base**, combined with key-only access and no rich-SQL need. Being a
point-lookup is *not* on its own a reason to migrate — Postgres serves keyed reads fine,
and a small, cached table gains nothing from DynamoDB. Each control-plane table was
checked against that bar and stays on Postgres:

| Table | Grows with traffic? | Hot path? | Cached today | Verdict |
|---|---|---|---|---|
| `api_keys` | No — bounded (keys per namespace) | Yes (Send auth, every request) | Redis 5 min + in-process fallback (`validateAPIKey`) | **Stay.** The hot read is already absorbed by cache; the table doesn't grow; admin list/rotate/scope wants SQL. |
| `jwt_signing_keys` | No — 1–5 rows | Loaded once at startup | In-process (1 min) + Redis (10 min) | **Stay.** Bulk-loaded, not keyed; tiny; already optimal. |
| `tenants` | No — 10s–100s | Yes (dispatch `EnsureTenant`) | Redis 24 h | **Stay.** Tiny, long-TTL cached; admin queries want SQL. |
| `notification_templates` | No — config | Yes (dispatch `Resolve` by slug) | Redis 5 min | **Stay.** Bounded config; cache is effective. |
| `subscription_categories`, `subscriptions` | No — config | Yes (channel resolution) | Redis 5 min | **Stay.** Bounded config; cached. |
| Better Auth tables | No | Auth flows | — | **Stay.** Relational identity model; coupled to SQL. |

`api_keys` is the instructive case: it *feels* like a DynamoDB candidate because it is
read on every Send request, but the thing that makes it feel hot (the read volume) is
already handled by caching, and the thing that would justify DynamoDB (unbounded growth)
is absent. `users` is the opposite — it is the one control-plane-adjacent table that
*does* grow unbounded, which is why it moves (see `hermes-users` above).

## Tradeoffs

**Gains:**
- Cloud portability: same application code runs on ExtendDB+Postgres anywhere and on
  native DynamoDB on AWS with only an endpoint change.
- Proven AWS scale path for notification ingestion without rewriting later.
- Native DynamoDB TTL replaces the cleanup cron for `notification_events`.
- Single-item reads (status updates, inbox items) are O(1) regardless of table size.

**Costs:**
- Single-table design discipline: no ad-hoc SQL queries; access patterns must be planned.
- Status-rollup logic moves from SQL into Go (conditional writes) — more code, but also
  more testable and explicit.
- ExtendDB is a self-hosted component: needs a k8s manifest, resource allocation,
  operational runbook for non-AWS environments.
- `aws-sdk-go-v2/service/dynamodb` promoted from indirect to direct dependency (already
  present in go.mod as indirect).
- Phase 2 (notifications table migration) is non-trivial: dual-write period required to
  maintain inbox consistency during cutover.

## Rollout posture

1. **Phase 1 (spike):** `EventRepository` and `UserSubscriptionRepository` implementations
   in `internal/store/dynamo/`. Backend selected per env via `HERMES_DYNAMO_ENDPOINT`.
   Postgres impls remain as fallback. The spike validates the DynamoDB conditional-write
   pattern and table creation in-cluster via ExtendDB.
2. **Phase 2:** Dual-write to both Postgres and DynamoDB for `notifications` (and `users`,
   if the `hermes-users` design validates in the spike); read from Postgres until
   validation is complete, then flip the read path.
3. **Phase 3 (amended 2026-06-12):** Cutover is **deferred, not cancelled.** The original
   plan — collapse to a single DynamoDB-model code path with ExtendDB+Postgres as the
   self-host backend — remains the target architecture. The native Postgres
   implementations in `internal/store/postgres/` are retained for now as a **temporary
   hedge**, for one reason: ExtendDB was announced May 2026 and is, at the time of this
   amendment, roughly one month old. Making a one-month-old adapter a hard runtime
   dependency for *every* self-host install — with no native fallback to the database
   operators already trust — is a maturity risk we are not yet willing to take.

   Note that ExtendDB is **stateless**: it is a protocol adapter in front of Postgres, not
   a fifth stateful dependency. The cost of the single-path approach is therefore an extra
   *process* to deploy, monitor, and upgrade (plus dependence on its correctness and
   performance), not extra stored state.

   Until that risk clears, both store paths are maintained:
   - **Postgres** (native repositories in `internal/store/postgres/`) is the self-host
     default — `HERMES_DYNAMO_ENDPOINT` unset selects it.
   - **DynamoDB model** (`internal/store/dynamo/`) is the scale option — native DynamoDB
     on AWS, or ExtendDB elsewhere.

   Both paths are covered by the integration test suite and must stay behaviorally
   equivalent (status rollup, idempotency window, pagination). Near-term Phase 3 work is
   therefore observability parity and load-test validation of both backends.

   **Revisit trigger.** (Tracked in [#18](https://github.com/darylrobbins/hermes/issues/18).)
   Re-evaluate collapsing to the single ExtendDB path once ExtendDB has a track record we
   trust — concretely, when **all** of the following hold:
   - ExtendDB has run the Hermes hot path under production-representative load for a
     sustained period without correctness or availability regressions vs. the Postgres
     path;
   - it has cut at least one stable (non-RC) release line with a documented upgrade and
     security-patch cadence;
   - operating it (deploy, monitor, upgrade, recover) is well understood and runbooked.

   When the trigger is met, removing the native Postgres path becomes a deliberate
   follow-up decision (its own ADR or amendment), not an automatic step — so the
   "permanent vs. temporary" question is revisited with evidence rather than presumed
   either way.

## Consequences

- `internal/store/dynamo/` package added with `EventStore` and `UserSubscriptionStore`
  (and, in Phase 2, a `UserStore` for the `hermes-users` table).
- `internal/config/config.go` gains `DynamoEndpoint` and `DynamoRegion`.
- `internal/bootstrap/bootstrap.go` gains `MustConnectDynamo`.
- `deploy/k8s/overlays/local/extenddb.yaml` and `Tiltfile` updated to run ExtendDB
  locally, backed by the existing Postgres instance.
- `go.mod` — `go mod tidy` promotes `dynamodb` to a direct dependency.
