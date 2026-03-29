# Hermes Admin Portal — Design Spec

## Context

Hermes has a fully functional Admin API (Go, OpenAPI-documented) but no web UI. Managing templates, categories, subscriptions, API keys, and tracking notifications currently requires direct API calls. This portal gives the team a visual interface to manage the platform without needing to use curl or Postman.

**Audience:** Internal team (developers/ops managing Hermes)
**Approach:** Start simple with core CRUD, evolve as needs emerge

## Stack

| Layer | Choice | Rationale |
|-------|--------|-----------|
| Framework | Next.js 15 (App Router) | Full flexibility, great DX, large ecosystem |
| UI Components | shadcn/ui + Tailwind CSS | Polished, ownable components — no dependency lock-in |
| Auth | Better Auth | Simple email/password login for internal access control |
| API Client | Auto-generated from `api/admin/swagger.yaml` | Keeps frontend types in sync with backend |
| Forms | React Hook Form + Zod | Standard validation pattern, works well with shadcn |
| Data Tables | TanStack Table (via shadcn DataTable) | Sortable, filterable tables for list views |

## Location

`web/admin/` in the Hermes monorepo. Shares git history and makes it easy to keep the OpenAPI spec in sync.

## Architecture

```
Browser → Next.js App (Better Auth session) → Hermes Admin API (API key from env)
```

- All data fetching goes through Next.js server actions or route handlers
- The Admin API key is stored as a server-side env var (`HERMES_API_KEY`), never exposed to the browser
- No direct browser-to-Admin-API calls

## Authentication

- Better Auth runs inside the Next.js app
- Uses its own tables in the same Postgres database (under a `better_auth_` prefix)
- Login page at `/login` with email/password
- Session-based auth — Next.js middleware protects all routes except `/login`
- No role/permission system initially — anyone with a login has full access
- Admin API key is a server-side secret, shared across all portal users

## Pages

### Templates (`/templates`)

**List view:**
- Data table with columns: Name, Slug, Channels, Subscription (if linked), Created
- Search/filter by name or slug
- "New Template" button

**Create/Edit form:**
- Fields: `name`, `slug` (auto-generated from name, editable), `subscription_id` (optional dropdown), `default_channels` (multi-select: email, sms, inbox)
- Channel content sections (shown based on selected channels):
  - Email: `email_subject`, `email_body` (textarea)
  - SMS: `sms_body` (textarea with character count)
  - Inbox: `inbox_title`, `inbox_body` (textarea)
- Delete with confirmation dialog

### Categories (`/categories`)

**List view:**
- Data table with columns: Name, Slug, Default Channels, Default State, Sort Order
- "New Category" button

**Detail/Edit view:**
- Fields: `name`, `slug`, `default_channels` (multi-select), `default_state` (select: on/off/required), `sort_order` (number)
- Nested subscriptions section below the category form:
  - Inline list of subscriptions belonging to this category
  - Add/edit/delete subscriptions from within the category view
  - Subscription fields: `name`, `slug`, `sort_order`

### API Keys (`/api-keys`)

**List view:**
- Data table with columns: Name, Permissions, Created
- "Create API Key" button

**Create flow:**
- Dialog/sheet with: `name`, `permissions` (checkboxes: `apikeys:manage`, `notifications:send`, `templates:manage`, `tenants:manage`)
- On creation: display the raw key once with a copy-to-clipboard button
- Warning that the key cannot be retrieved again

**Delete:** Confirmation dialog (revoke key)

### Notifications (`/notifications`)

**Lookup:**
- Search by notification ID
- Displays: recipient user ID, template, channels, status, timestamps (created, sent, delivered, read, archived)

**Event timeline:**
- Vertical timeline of `NotificationEvent` records
- Shows: channel icon, event type, severity, metadata, timestamp
- Visual status progression (pending → sent → delivered → read)

## Layout

- **Sidebar navigation:** Fixed left sidebar with sections: Templates, Categories, API Keys, Notifications
- **Collapsible:** Sidebar collapses on smaller screens
- **Breadcrumbs:** On detail/edit pages (e.g., Categories → Marketing → Edit)
- **Theme:** Dark theme by default, light theme toggle via shadcn theming
- **Header:** Minimal — just user email + logout button

## Key Components

| Component | Purpose |
|-----------|---------|
| `DataTable` | Reusable list view with sorting, filtering, pagination |
| `ResourceForm` | Consistent create/edit form pattern with Zod validation |
| `ConfirmDialog` | Delete/revoke confirmation |
| `CopyButton` | One-click copy for API keys |
| `EventTimeline` | Vertical timeline for notification events |
| `ChannelBadge` | Visual indicator for email/sms/inbox channels |
| `Sidebar` | Collapsible navigation |

## API Client

Generate a TypeScript client from `api/admin/swagger.yaml` using a code generator (e.g., `openapi-typescript` for types + lightweight fetch wrapper). This gives us:

- Type-safe request/response types matching the backend
- Single source of truth — regenerate when the API changes
- No manual type maintenance

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `HERMES_API_KEY` | Admin API key for backend calls |
| `HERMES_API_URL` | Admin API base URL (default: `http://localhost:8080`) |
| `DATABASE_URL` | Postgres connection for Better Auth tables |
| `BETTER_AUTH_SECRET` | Session encryption secret |

## Verification Plan

1. **Dev setup:** `cd web/admin && npm install && npm run dev` starts the portal
2. **Auth flow:** Navigate to `localhost:3000` → redirected to `/login` → login → redirected to `/templates`
3. **CRUD test:** Create a template → verify it appears in list → edit it → delete it → verify removal
4. **API key test:** Create an API key → copy raw key → verify it works against the Admin API → revoke it
5. **Category test:** Create a category → add subscriptions within it → reorder → delete
6. **Notification lookup:** Enter a known notification ID → verify details and event timeline render
7. **Responsive:** Collapse sidebar on narrow viewport → verify navigation still works
