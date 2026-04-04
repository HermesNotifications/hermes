# Send Notification from Admin Portal

## Overview

Add a "Send Notification" page to the admin portal at `/notifications/send`. Admins can send a notification to a user using an existing template (with variable substitution) or direct content. The form calls the existing send service via the Hermes SDK (`hermes.notifications.send()`), which shares the same base URL and API key as the admin service — the ingress routes `/v1/send` to the send service.

## Page Structure

- **Route:** `app/(dashboard)/notifications/send/page.tsx`
- **Form component:** `components/send-notification-form.tsx` (client component)
- **Entry point:** "Send Notification" button on the notifications list page header

## Form Layout

### Recipient Section

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| Tenant | Searchable select | Yes | Populated from `hermes.tenants.list()`. Includes "+ Create new tenant..." option that expands an inline name field. UUID auto-generated via new `hermes.tenants.create()` SDK method. |
| User ID | Text input | Yes | External user ID |
| Email | Text input | No | Override — bypasses user profile lookup |
| Phone | Text input | No | Override — bypasses user profile lookup |

### Content Mode Toggle

Segmented control switching between "Template" and "Direct Content" modes. Exactly one must be active.

#### Template Mode

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| Template | Select dropdown | Yes | Populated from `hermes.templates.list()`. Full template objects available client-side (list endpoint returns content fields). |
| Template Data | Key-value builder | No | Dynamic rows: monospace key input + value input + remove button. "+ Add variable" to append rows. |

**Auto-populated variables:** When a template is selected, all content fields (`email_subject`, `email_body`, `sms_body`, `inbox_title`, `inbox_body`) are scanned for Go template placeholders (`{{.varName}}`). Extracted variable names are deduplicated and added as rows in the key-value builder with empty values. Users can add/remove rows freely.

#### Direct Content Mode

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| Title | Text input | Yes | |
| Body | Textarea | Yes | |
| Action URL | Text input | No | |
| Action Label | Text input | No | |

### Channels

Checkbox group: Email, SMS, Inbox.
- **Template mode:** Optional — overrides template default channels.
- **Direct content mode:** Required — at least one must be selected.

## Data Flow

1. Server component (`page.tsx`) fetches tenants and templates via server actions, passes as props.
2. Client form component manages mode toggle, template variable extraction, key-value builder state, and inline tenant creation.
3. On submit: key-value pairs serialized to `Record<string, string>`, server action called.
4. Server action calls `hermes.notifications.send()` with the assembled payload.
5. On success: toast with notification ID, redirect to `/notifications/{id}` (status detail page).

## Validation (Zod Schema)

```
sendNotificationSchema:
  tenantId: string, required (or newTenantName if creating inline)
  userId: string, required
  mode: "template" | "content"
  template: string, required if mode === "template"
  data: array of {key: string, value: string}, optional
  content.title: string, required if mode === "content"
  content.body: string, required if mode === "content"
  content.actionUrl: string, optional
  content.actionLabel: string, optional
  channels: array of "email" | "sms" | "inbox"
    - optional when mode === "template"
    - min 1 required when mode === "content"
  email: string (email format), optional
  phone: string, optional
```

## Backend Changes

### New: `POST /v1/tenants` endpoint (admin service)

The admin service already has `store.CreateTenant(ctx, id, name)` but no HTTP endpoint. Add:

- **Endpoint:** `POST /v1/tenants`
- **Request body:** `{ "name": "string" }`
- **Response:** `201 Created` with full tenant object (ID auto-generated as UUIDv4)
- **Location:** `internal/admin/handler_tenants.go` — add to existing `registerTenantRoutes()`

### New: `tenants.create()` SDK method

Add to `TenantsService` in `sdks/typescript/packages/hermes-server/src/client.ts`:

```typescript
async create(body: { name: string }): Promise<Tenant> {
  const result = await this.client.POST("/v1/tenants", { body });
  return unwrap(result);
}
```

Regenerate OpenAPI types after adding the endpoint.

## New/Modified Files

### Admin Portal (web/admin/)
- **New:** `app/(dashboard)/notifications/send/page.tsx` — server component page
- **New:** `components/send-notification-form.tsx` — client form component
- **New:** `lib/schemas/send-notification.ts` — zod validation schema
- **Modified:** `lib/actions/notifications.ts` — add `sendNotification()` server action
- **Modified:** `lib/actions/tenants.ts` — add `createTenant()` server action
- **Modified:** `app/(dashboard)/notifications/page.tsx` — add "Send Notification" button

### Backend (Go)
- **Modified:** `internal/admin/handler_tenants.go` — add `POST /v1/tenants` handler

### SDK
- **Modified:** `sdks/typescript/packages/hermes-server/src/client.ts` — add `tenants.create()`
- **Modified:** `sdks/typescript/packages/hermes-server/src/generated/admin-api.d.ts` — regenerate types

## Template Variable Extraction

Client-side utility function to extract Go template variables:

```typescript
function extractTemplateVariables(template: NotificationTemplate): string[] {
  const fields = [
    template.email_subject,
    template.email_body,
    template.sms_body,
    template.inbox_title,
    template.inbox_body,
  ];
  const pattern = /\{\{\s*\.(\w+)\s*\}\}/g;
  const vars = new Set<string>();
  for (const field of fields) {
    if (!field) continue;
    for (const match of field.matchAll(pattern)) {
      vars.add(match[1]);
    }
  }
  return Array.from(vars);
}
```

## Error Handling

- API errors surface as toast messages (sonner) — same pattern as existing forms
- Zod validation errors display inline below each field
- Template data JSON serialization errors caught before submit
- Network/auth errors handled by existing SDK error propagation
