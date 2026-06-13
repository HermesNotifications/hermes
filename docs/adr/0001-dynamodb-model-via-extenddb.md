# ADR 0001: Adopt the DynamoDB programming model for the hot notification path

**Status:** Accepted (amended 2026-06-12: dual-path store is permanent — see Rollout posture)  
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
`notification_templates`, `api_keys`, `jwt_signing_keys`, `users`) and the Better Auth
tables remain on native Postgres.

## Scope

Migrate in three phases, lowest-risk first:

| Phase | Tables / interfaces |
|---|---|
| 1 (spike) | `notification_events` → `hermes-events` table; `user_subscriptions` → `hermes-user-subscriptions` table |
| 2 | `notifications` → `hermes-notifications` table (inbox path, status rollup) |
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

### `hermes-notifications` (Phase 2)

```
PK (pk):  USER#<user_id>            S
SK (sk):  NOTIF#<notification_id>   S   (IDs are time-sortable — keyset pagination maps to LastEvaluatedKey)

GSI "by-idempotency":
  PK:  TENANT#<tenant_id>
  SK:  IDEM#<idempotency_key>

GSI "by-tenant" (admin ListRecentNotifications):
  PK:  TENANT#<tenant_id>
  SK:  NOTIF#<notification_id>

Attributes:
  status        S
  status_rank   N   (numeric rank; ConditionExpression prevents regression)
  sent_at       S   (RFC3339, nullable)
  delivered_at  S   (RFC3339, nullable)
  read_at       S   (RFC3339, nullable)
  archived_at   S   (RFC3339, nullable)
  deleted_at    S   (RFC3339, nullable)
  title, body, channels (SS), template_id, category_id, tenant_id, ...
  ttl           N   (optional; set on soft-delete or archive for eventual cleanup)
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

`(created_at, id) < (cursor)` in Postgres → `ExclusiveStartKey` in DynamoDB. The cursor
encoding (`base64(created_at_ns|id)`) can be adapted to pass the DynamoDB
`LastEvaluatedKey` directly (the notification_id encodes time already).

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
2. **Phase 2:** Dual-write to both Postgres and DynamoDB for `notifications`; read from
   Postgres until validation is complete, then flip the read path.
3. **Phase 3 (amended 2026-06-12):** The Postgres implementations are **not** removed.
   Hermes is distributed as a self-hosted product, and requiring a DynamoDB-compatible
   endpoint (DynamoDB Local or ExtendDB) would add a fifth stateful dependency to every
   installation. Both store paths are permanent:
   - **Postgres** (native repositories in `internal/store/postgres/`) is the self-host
     default — `HERMES_DYNAMO_ENDPOINT` unset selects it.
   - **DynamoDB model** (`internal/store/dynamo/`) is the scale option — native DynamoDB
     on AWS, or ExtendDB elsewhere.
   Phase 3 work is therefore observability parity and load-test validation of both
   backends, not cutover. Both paths are covered by the integration test suite and must
   stay behaviorally equivalent (status rollup, idempotency window, pagination).

## Consequences

- `internal/store/dynamo/` package added with `EventStore` and `UserSubscriptionStore`.
- `internal/config/config.go` gains `DynamoEndpoint` and `DynamoRegion`.
- `internal/bootstrap/bootstrap.go` gains `MustConnectDynamo`.
- `deploy/k8s/overlays/local/extenddb.yaml` and `Tiltfile` updated to run ExtendDB
  locally, backed by the existing Postgres instance.
- `go.mod` — `go mod tidy` promotes `dynamodb` to a direct dependency.
