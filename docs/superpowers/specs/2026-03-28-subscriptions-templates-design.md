# Subscriptions & Templates Design Spec

**Date:** 2026-03-28
**Status:** Draft

## Overview

Replace notification groups with a two-level subscription hierarchy (subscription categories → subscriptions) and rename notification types to notification templates. This simplifies the user preference model from per-channel granularity to boolean opt-in/out toggles, and introduces category-level default states (on/off/required).

## Goals

- Replace notification groups with subscription categories and subscriptions
- Rename notification types to notification templates
- Simplify user preferences to boolean toggles per subscription
- Seed sensible defaults (Account, General, Marketing)
- Keep subscriptions and categories global across namespaces

## Non-Goals

- Per-namespace subscription scoping
- Per-channel user preference granularity
- Template versioning or approval workflows

---

## Data Model

### subscription_categories (replaces notification_groups)

| Column | Type | Notes |
|--------|------|-------|
| id | TEXT PK | Crockford Base32, prefix `sct_` |
| slug | TEXT UNIQUE | e.g. `general`, `marketing`, `account` |
| name | TEXT NOT NULL | Display name |
| default_channels | TEXT[] | Channels for subscriptions in this category |
| default_state | TEXT NOT NULL | `on`, `off`, `required` |
| sort_order | INT NOT NULL DEFAULT 0 | For preference center ordering |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT NOW() | |

### subscriptions (new)

| Column | Type | Notes |
|--------|------|-------|
| id | TEXT PK | Crockford Base32, prefix `sub_` |
| category_id | TEXT NOT NULL FK → subscription_categories(id) | Parent category |
| slug | TEXT NOT NULL | Unique within category: `UNIQUE(category_id, slug)` |
| name | TEXT NOT NULL | Display name |
| sort_order | INT NOT NULL DEFAULT 0 | Within-category ordering |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT NOW() | |

### notification_templates (replaces notification_types)

| Column | Type | Notes |
|--------|------|-------|
| id | TEXT PK | Crockford Base32, prefix `ntpl_` |
| namespace_id | TEXT NOT NULL DEFAULT 'ns_default' | From namespaces work |
| subscription_id | TEXT FK → subscriptions(id), NULLABLE | NULL = standalone template |
| slug | TEXT NOT NULL | `UNIQUE(namespace_id, slug)` |
| name | TEXT NOT NULL | |
| default_channels | TEXT[] | Used when subscription_id is NULL |
| email_subject | TEXT | |
| email_body | TEXT | |
| sms_body | TEXT | |
| inbox_title | TEXT | |
| inbox_body | TEXT | |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT NOW() | |

### user_subscriptions (replaces user_preferences)

| Column | Type | Notes |
|--------|------|-------|
| user_id | TEXT NOT NULL FK → users(id) | |
| subscription_id | TEXT NOT NULL FK → subscriptions(id) | |
| opted_in | BOOLEAN NOT NULL | true = subscribed, false = unsubscribed |
| created_at | TIMESTAMPTZ NOT NULL DEFAULT NOW() | |
| | PK | `(user_id, subscription_id)` |

### notifications table changes

- `group_id` column renamed to `category_id`
- `type_id` column renamed to `template_id`
- Foreign key constraints updated accordingly

### Dropped tables

- `notification_groups`
- `notification_types`
- `user_preferences`

---

## Seed Data

Three default categories with matching subscriptions, created by migration. All are fully mutable (can be renamed, reconfigured, or deleted after creation).

### Categories

| Slug | Name | Default State | Default Channels | Sort Order |
|------|------|--------------|-----------------|------------|
| `account` | Account | `required` | `["email", "inbox"]` | 0 |
| `general` | General | `on` | `["email", "inbox"]` | 1 |
| `marketing` | Marketing | `off` | `["email"]` | 2 |

### Subscriptions

| Category | Slug | Name | Sort Order |
|----------|------|------|------------|
| Account | `account` | Account | 0 |
| General | `general` | General | 0 |
| Marketing | `marketing` | Marketing | 0 |

---

## Channel Resolution

### Template with a subscription

```
1. Is the subscription's category "required"?
   → Yes: Use category's default_channels. Skip preference check.
2. Does the user have a user_subscriptions record for this subscription?
   → opted_in = false: Do not send.
   → opted_in = true: Use category's default_channels.
3. No user record — check category default_state:
   → "on": Use category's default_channels (implicit opt-in).
   → "off": Do not send (implicit opt-out).
```

### Standalone template (no subscription)

```
1. Explicit channels in send request? → Use those.
2. Template has default_channels? → Use those.
3. Neither → Reject send request (400).
```

### Explicit channel override

The send request can include an explicit `channels` array. When provided, it overrides the resolved channels — but only if the user hasn't opted out. If the subscription state says "don't send," an explicit channel override doesn't bypass that. Required subscriptions always send regardless.

### Post-resolution filter

After channels are resolved:
- Remove channels that lack template content (no `email_body` → no email)
- Remove channels where user lacks contact info (no email address → no email)

---

## API

### Admin API (API key auth)

#### Subscription Categories

- `GET /v1/subscriptions/categories` — List all categories (sorted by sort_order)
- `POST /v1/subscriptions/categories` — Create category
- `PUT /v1/subscriptions/categories/{id}` — Update category
- `DELETE /v1/subscriptions/categories/{id}` — Delete category (fails if subscriptions exist)

**Create/Update input:**
```json
{
  "slug": "marketing",
  "name": "Marketing",
  "default_channels": ["email"],
  "default_state": "off",
  "sort_order": 2
}
```

#### Subscriptions

- `GET /v1/subscriptions/categories/{category_id}/subscriptions` — List subscriptions in a category
- `POST /v1/subscriptions/categories/{category_id}/subscriptions` — Create subscription
- `PUT /v1/subscriptions/{id}` — Update subscription
- `DELETE /v1/subscriptions/{id}` — Delete subscription (fails if templates reference it)

**Create/Update input:**
```json
{
  "slug": "promotions",
  "name": "Promotions",
  "sort_order": 1
}
```

#### Notification Templates (replaces /v1/types)

- `GET /v1/templates` — List all templates (filterable by namespace, subscription)
- `POST /v1/templates` — Create template
- `PUT /v1/templates/{id}` — Update template
- `DELETE /v1/templates/{id}` — Delete template

**Create input:**
```json
{
  "slug": "order-confirmed",
  "name": "Order Confirmed",
  "subscription_id": "sub_...",
  "default_channels": null,
  "email_subject": "Order #{{.order_id}} confirmed",
  "email_body": "<p>Hello {{.name}}, your order is confirmed.</p>",
  "sms_body": "Order #{{.order_id}} confirmed",
  "inbox_title": "Order Confirmed",
  "inbox_body": "Your order #{{.order_id}} has been confirmed."
}
```

When `subscription_id` is set, `default_channels` is ignored (channels come from the category). When `subscription_id` is null, `default_channels` is used for standalone channel resolution.

#### Send (POST /v1/send)

- `type` field renamed to `template` (slug-based lookup)
- `group` field removed — category is derived from the template's subscription
- `channels` override still supported

```json
{
  "tenant_id": "...",
  "user_id": "...",
  "template": "order-confirmed",
  "data": {"order_id": "123", "name": "Alice"},
  "channels": ["email"]
}
```

### User API (JWT auth)

#### Preferences

- `GET /v1/users/me/preferences` — Full preference center view
- `PUT /v1/users/me/preferences/{subscription_id}` — Set opted_in. Returns 403 if category is `required`.
- `DELETE /v1/users/me/preferences/{subscription_id}` — Remove explicit preference, revert to category default.

**GET response:**
```json
{
  "categories": [
    {
      "id": "sct_...",
      "slug": "account",
      "name": "Account",
      "default_channels": ["email", "inbox"],
      "default_state": "required",
      "subscriptions": [
        {
          "id": "sub_...",
          "slug": "account",
          "name": "Account",
          "opted_in": true,
          "toggleable": false
        }
      ]
    },
    {
      "id": "sct_...",
      "slug": "general",
      "name": "General",
      "default_channels": ["email", "inbox"],
      "default_state": "on",
      "subscriptions": [
        {
          "id": "sub_...",
          "slug": "general",
          "name": "General",
          "opted_in": true,
          "toggleable": true
        }
      ]
    },
    {
      "id": "sct_...",
      "slug": "marketing",
      "name": "Marketing",
      "default_channels": ["email"],
      "default_state": "off",
      "subscriptions": [
        {
          "id": "sub_...",
          "slug": "marketing",
          "name": "Marketing",
          "opted_in": false,
          "toggleable": true
        }
      ]
    }
  ]
}
```

The `opted_in` field reflects the effective state: explicit user preference if set, otherwise the category default. The `toggleable` field is false when the category is `required`.

**PUT input:**
```json
{
  "opted_in": true
}
```

### Removed Endpoints

- `GET/POST/PUT /v1/groups`
- `GET/POST/PUT/DELETE /v1/types`
- `GET/PUT/DELETE /v1/users/me/preferences/{group_id}`

### Permission Changes

The existing `templates:manage` permission covers subscription categories, subscriptions, and templates (same scope, expanded resources).

---

## NATS Message Changes

### SendMessage (admin → dispatch)

- `group_id` → `category_id` (derived from template's subscription, or empty for standalone)
- Add `subscription_id` (nullable)
- `metadata.type` → `metadata.template`
- `metadata.group` removed

### DeliveryMessage (dispatch → workers)

- `type_id` → `template_id` in metadata
- No structural changes

### EventMessage (workers → event writer)

- Metadata key `type` → `template`
- No structural changes

---

## Service Changes

### Admin Service

- New handlers for subscription category and subscription CRUD
- Template handlers replace type handlers (same pattern, new field names)
- Send handler: `type` → `template`, remove `group` field, resolve `category_id` and `subscription_id` from template's subscription chain

### Dispatch Service

- `TemplateResolver` cache key becomes `template_config:{namespace_id}:{slug}`
- `ChannelResolver` rewritten with new resolution chain (required check → user subscription → category default → template default_channels)
- `GroupRepository` dependency replaced with `SubscriptionCategoryRepository` + `SubscriptionRepository`

### User Service

- Preference handlers rewritten for boolean model
- `GET /v1/users/me/preferences` assembles full preference center (categories → subscriptions → user state)

### Event Writer

- Column name changes on insert (`group_id` → `category_id`, `type_id` → `template_id`)

### Inbox Service

- No functional changes — reads from `notifications` table which just has renamed columns

### Store Interface Changes

- `GroupRepository` → `SubscriptionCategoryRepository`
- New `SubscriptionRepository`
- `TypeRepository` → `TemplateRepository`
- `PreferenceRepository` → `UserSubscriptionRepository`

---

## Migration Strategy

Single migration file that:

1. Creates `subscription_categories` table
2. Creates `subscriptions` table
3. Inserts three default categories (Account, General, Marketing) with matching subscriptions
4. Creates `notification_templates` table with data migrated from `notification_types`:
   - `subscription_id` set to the General subscription (existing types get assigned to General)
   - `default_channels` copied from the type's former group
5. Creates `user_subscriptions` table (empty — no clean mapping from old channel-per-group model)
6. Alters `notifications` table: renames `group_id` → `category_id`, `type_id` → `template_id`
7. Drops `user_preferences`, `notification_types`, `notification_groups` tables
8. Updates foreign key constraints

### What happens to existing data

- **Groups** → Deleted. Channel defaults copied onto migrated templates.
- **Types** → Migrated to templates under the General subscription.
- **User preferences** → Dropped. Users start fresh with category defaults.
- **Existing notifications** → `category_id` and `template_id` columns preserve historical references.
