# Namespaces Design Spec

## Context

Hermes is deployed for a single company to manage notifications across their product suite. A SaaS company with multiple products (possibly acquired, on different tech stacks) needs each product to appear as part of a unified notification experience — the bell icon in any app shows notifications from all apps, emails look consistent — while still allowing per-product isolation of templates, API keys, and access control.

Today, tenants represent the company's customers and are shared across all products. But there's no organizational layer to represent the products themselves. Notification types, templates, groups, and API keys are all global. This means you can't scope templates to a specific product, restrict an API key to only one product's resources, or rate-limit on a per-product basis.

**Namespaces** introduce this product/app-level organizational boundary as an orthogonal dimension to tenants.

## Core Concepts

### Naming: "Namespace"

Generic, familiar from cloud platforms, doesn't presuppose usage. Could represent an app, a team, an environment, or any other organizational boundary.

### Two-Dimensional Model

Namespaces and tenants are **orthogonal**:
- **Namespace** = which product/app sent the notification (owns templates, API keys)
- **Tenant** = which customer the notification is for (owns users)
- A notification belongs to both a namespace and a tenant
- The unified inbox aggregates across namespaces for a given tenant+user

### What's Namespace-Scoped vs Global

| Resource | Scoping | Rationale |
|----------|---------|-----------|
| Namespaces | Managed entity | CRUD, settings, listed/queried |
| API keys | Namespace-scoped or global (NULL) | Per-product access control and rate limiting |
| Notification types/templates | Namespace-scoped | Each product has its own templates |
| Notifications | Tagged with namespace | Enables filtering, analytics, logging |
| Groups | **Global** (shared) | Cross-cutting categories (Marketing, Security) shared across products |
| Tenants | **Global** (shared) | Customers span all products |
| Users | **Global** (per-tenant) | Same user across all products |
| User preferences | **Global** (per-user, per-group) | Preferences apply across namespaces |
| JWT signing keys | **Global** | Auth infrastructure, not product-specific |

### Environments

Deferred. Environments (staging, production) can be layered on later, either as separate namespaces by convention or as a property. The existing API key environment prefix (`hms_dev_`, `hms_stg_`) remains a visual hint in the key format, not a boundary.

## Data Model

### Namespace Entity

```sql
CREATE TABLE namespaces (
    id         TEXT PRIMARY KEY,                    -- ns_<base64url>, random bits only
    slug       TEXT NOT NULL UNIQUE,                -- url-friendly, immutable after creation
    name       TEXT NOT NULL,                       -- human-readable display name
    settings   JSONB NOT NULL DEFAULT '{}',         -- future: rate limits, branding, etc.
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

**ID generation**: v2 ID generator with prefix `ns`, random bits only (no time bits). Expected cardinality: fewer than a few hundred.

```go
namespaceIDGen = id.NewGenerator(id.Config{Prefix: "ns", RandBits: 80})
```

**Go model**:
```go
type Namespace struct {
    ID        string    `json:"id"`
    Slug      string    `json:"slug"`
    Name      string    `json:"name"`
    Settings  []byte    `json:"settings,omitempty"`
    CreatedAt time.Time `json:"created_at"`
}
```

**Default namespace**: A namespace with slug `default` and name `Default` is created during the migration that adds this table. All existing data is associated with this namespace.

### Schema Changes to Existing Tables

**No foreign key constraints** on namespace_id columns. Application-level validation ensures the namespace exists at write time. This keeps the schema portable across datastores.

#### api_keys

```sql
ALTER TABLE api_keys ADD COLUMN namespace_id TEXT;  -- NULL = global key
```

- NULL means global (can operate across all namespaces)
- Non-NULL means scoped to that namespace

#### notification_types

```sql
ALTER TABLE notification_types ADD COLUMN namespace_id TEXT NOT NULL DEFAULT 'default';
DROP INDEX idx_notification_types_slug;
CREATE UNIQUE INDEX idx_notification_types_ns_slug ON notification_types (namespace_id, slug);
```

- Uniqueness becomes `(namespace_id, slug)` — same slug can exist in different namespaces
- Existing types move to `default` namespace via the DEFAULT clause

#### notifications

```sql
ALTER TABLE notifications ADD COLUMN namespace_id TEXT NOT NULL DEFAULT 'default';
```

- Tags each notification with the namespace it was sent from
- Enables namespace-level filtering and analytics

## API Changes

### New: Namespace CRUD

Requires new permission: `namespaces:manage`

- `POST /v1/namespaces` — Create a namespace (slug, name)
- `GET /v1/namespaces` — List all namespaces
- `GET /v1/namespaces/{id}` — Get a namespace by ID
- `PATCH /v1/namespaces/{id}` — Update name/settings (slug is immutable)
- `DELETE /v1/namespaces/{id}` — Delete namespace (only if no resources reference it)

### Modified: API Key Creation

`POST /v1/api-keys` gains an optional `namespace_id` field. If provided, the key is scoped to that namespace. If omitted, the key is global.

An api key that is tied to a given namespace can only be used to create a key for the same namespace. Only global 
api keys can be used to create namespace-scoped keys for any namespace or other global keys.

### Modified: Send Notification

`POST /v1/send`:
- **Namespace-scoped key**: Namespace is implicit. No `namespace_id` field needed in the request body. The notification is tagged with the key's namespace.
- **Global key**: Should provide `namespace_id` in the request body to specify which namespace context to use for type resolution and tagging. If omitted, defaults to the `default` namespace for backward compatibility.

### Modified: Type CRUD

`POST/GET/LIST /v1/types`:
- **Namespace-scoped key**: Operations are automatically scoped to the key's namespace.
- **Global key**: Can provide `namespace_id` as a query parameter (list) or in the request body (create) to scope operations. If omitted, defaults to `default` namespace. Can list across all namespaces with a query parameter.

### Modified: Inbox

`GET /v1/inbox`:
- Returns notifications across all namespaces by default (unified inbox)
- Optional `namespace` query parameter to filter to a specific namespace

## Auth & Middleware

### API Key Middleware Changes

After validating the API key, also load `namespace_id` from the key record and inject into request context:

```go
ctx = context.WithValue(ctx, ContextKeyNamespaceID, key.NamespaceID)  // *string, nil for global
```

### Namespace Enforcement

For namespace-scoped keys, the middleware or handler layer enforces:
- Send: notification tagged with key's namespace; type resolution within key's namespace
- Type CRUD: can only read/write types within key's namespace
- API key CRUD: can only see/create keys within key's namespace (unless has elevated permissions)

For global keys, namespace must be explicitly provided where required.

### Permission Model

Existing permissions continue to work. New permission:
- `namespaces:manage` — Create/update/delete namespaces
- only global keys can create/update/delete namespaces

Namespace scoping is orthogonal to permissions. A key with `notifications:send` scoped to namespace "billing" can send notifications, but only within "billing."

## NATS Message Changes

`SendMessage` and `DeliveryMessage` gain a `NamespaceID string` field. This flows through the entire pipeline:

```
Admin API (tag) → NATS SendMessage → Dispatch (preserved) → NATS DeliveryMessage → Workers (preserved) → NATS EventMessage (in metadata)
```

The `EventMessage` already has a `Metadata map[string]any` field — namespace_id is included there rather than as a top-level field, keeping EventMessage generic.

## Structured Logging

All services add `namespace_id` to their structured log context wherever available:
- **Admin API**: From API key middleware context
- **Dispatch**: From SendMessage.NamespaceID
- **Workers**: From DeliveryMessage.NamespaceID
- **Event Writer**: From EventMessage.Metadata

## Migration Plan

Single migration that:
1. Creates `namespaces` table
2. Inserts the `default` namespace record
3. Adds `namespace_id TEXT` column to `api_keys` (nullable, no default — existing keys become global)
4. Adds `namespace_id TEXT NOT NULL DEFAULT 'default'` to `notification_types`
5. Adds `namespace_id TEXT NOT NULL DEFAULT 'default'` to `notifications`
6. Drops old unique index on `notification_types(slug)`
7. Creates new unique index on `notification_types(namespace_id, slug)`

No data backfill script needed — DEFAULT clauses handle existing rows. Existing API keys become global keys (NULL namespace_id), which is the correct behavior for backward compatibility.

## Verification

1. **Migration**: Run `make migrate` — verify tables are altered, default namespace exists, existing data is intact
2. **Unit tests**: Mock-based tests for new namespace CRUD handlers, updated send handler, updated type resolution
3. **Integration tests**: Create namespaces, create namespace-scoped API keys, send notifications, verify isolation
4. **E2E tests**: Full pipeline — send via namespace-scoped key, verify notification tagged correctly, verify inbox returns cross-namespace results and filters work
5. **Backward compatibility**: Existing global API keys continue to work with `namespace_id` in request body (or default namespace)
