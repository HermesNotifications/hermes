# Send Notification from Admin Portal — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a send notification page to the admin portal that supports template-based and direct content sends, with inline tenant creation and auto-populated template variables.

**Architecture:** The admin portal gets a new page at `/notifications/send` with a client-side form component. It calls the existing send service via `hermes.notifications.send()` (same base URL, routed by ingress). A new `POST /v1/tenants` backend endpoint and `tenants.create()` SDK method support inline tenant creation.

**Tech Stack:** Go (huma), TypeScript, Next.js 15 (App Router), React 19, react-hook-form, zod, shadcn/ui (Tabs, Select, Input, Button, Checkbox, Label, Textarea), @hermes-notifications/server SDK

---

### Task 1: Add `POST /v1/tenants` endpoint to admin service

**Files:**
- Modify: `internal/admin/server.go` (add `CreateTenant` to `AdminStore` interface)
- Modify: `internal/admin/handler_tenants.go` (add create handler)
- Test: `internal/admin/handler_tenants_test.go`

- [ ] **Step 1: Add `CreateTenant` to the `AdminStore` interface**

In `internal/admin/server.go`, add `CreateTenant` to the `AdminStore` interface in the Tenants section (after the existing `ListTenants` and `CountUsersByTenant` lines):

```go
	// Tenants
	CreateTenant(ctx context.Context, id, name string) (*models.Tenant, error)
	ListTenants(ctx context.Context) ([]models.Tenant, error)
	CountUsersByTenant(ctx context.Context) (map[string]int, error)
```

- [ ] **Step 2: Write the failing test for create tenant**

Add to `internal/admin/handler_tenants_test.go`:

```go
func TestCreateTenant(t *testing.T) {
	srv := newTestServerWithStore(t, &mockStore{})

	body := `{"name":"New Tenant"}`
	req := httptest.NewRequest("POST", "/v1/tenants", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Name != "New Tenant" {
		t.Errorf("expected name 'New Tenant', got %s", resp.Name)
	}
	if resp.ID == "" {
		t.Error("expected non-empty ID")
	}
}

func TestCreateTenant_MissingName(t *testing.T) {
	srv := newTestServerWithStore(t, &mockStore{})

	body := `{}`
	req := httptest.NewRequest("POST", "/v1/tenants", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code < 400 {
		t.Fatalf("expected 4xx error, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateTenant_StoreError(t *testing.T) {
	store := &mockStore{
		errors: map[string]error{"CreateTenant": fmt.Errorf("db error")},
	}
	srv := newTestServerWithStore(t, store)

	body := `{"name":"Fail Tenant"}`
	req := httptest.NewRequest("POST", "/v1/tenants", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}
```

Add `"strings"` and `"fmt"` to the import block (add only if not already present).

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/admin/... -run TestCreateTenant -v`
Expected: FAIL — route not registered yet.

- [ ] **Step 4: Add error support to mock CreateTenant**

In `internal/admin/testutil_test.go`, update the `CreateTenant` mock method to support error injection:

```go
func (m *mockStore) CreateTenant(ctx context.Context, id, name string) (*models.Tenant, error) {
	if err := m.shouldError("CreateTenant"); err != nil {
		return nil, err
	}
	t := models.Tenant{ID: id, Name: name, CreatedAt: time.Now()}
	m.tenants = append(m.tenants, t)
	return &t, nil
}
```

- [ ] **Step 5: Implement the create tenant handler**

In `internal/admin/handler_tenants.go`, add the create handler. Add `"github.com/google/uuid"` to the imports:

```go
type createTenantInput struct {
	Body struct {
		Name string `json:"name" required:"true" minLength:"1" doc:"Tenant name"`
	}
}

type createTenantOutput struct {
	Body tenantItem
}
```

Then in `registerTenantRoutes()`, add after the existing `huma.Register` call for list:

```go
	huma.Register(s.api, huma.Operation{
		OperationID:   "create-tenant",
		Method:        http.MethodPost,
		Path:          "/v1/tenants",
		Summary:       "Create a tenant",
		Tags:          []string{"Tenants"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, input *createTenantInput) (*createTenantOutput, error) {
		id := uuid.New().String()
		tenant, err := s.store.CreateTenant(ctx, id, input.Body.Name)
		if err != nil {
			s.logger.Error("failed to create tenant", "error", err)
			return nil, huma.Error500InternalServerError("internal server error")
		}
		return &createTenantOutput{Body: tenantItem{
			ID:            tenant.ID,
			Name:          tenant.Name,
			DefaultLocale: tenant.DefaultLocale,
			UserCount:     0,
			CreatedAt:     tenant.CreatedAt,
		}}, nil
	})
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/admin/... -run TestCreateTenant -v`
Expected: All 3 tests PASS.

- [ ] **Step 7: Run full admin test suite**

Run: `go test ./internal/admin/... -v`
Expected: All tests PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/admin/server.go internal/admin/handler_tenants.go internal/admin/handler_tenants_test.go internal/admin/testutil_test.go
git commit -m "feat(admin): add POST /v1/tenants endpoint for tenant creation"
```

---

### Task 2: Regenerate OpenAPI spec and SDK types

**Files:**
- Modified by codegen: `api/admin/openapi.yaml`, `api/admin/openapi.json`
- Modified by codegen: `sdks/typescript/packages/hermes-server/src/generated/admin-api.d.ts`

- [ ] **Step 1: Regenerate the OpenAPI spec**

Run: `make openapi`
Expected: Updates `api/admin/openapi.yaml` and `api/admin/openapi.json` with the new `create-tenant` operation.

- [ ] **Step 2: Verify the new endpoint appears in the spec**

Run: `grep "create-tenant" api/admin/openapi.yaml`
Expected: Match showing the new operation ID.

- [ ] **Step 3: Regenerate SDK types**

Run: `make sdk-ts-generate`
Expected: Updates `admin-api.d.ts` with the new `CreateTenantInputBody` schema and `create-tenant` operation types.

- [ ] **Step 4: Commit**

```bash
git add api/admin/ sdks/typescript/packages/hermes-server/src/generated/admin-api.d.ts
git commit -m "chore: regenerate OpenAPI spec and SDK types for create-tenant endpoint"
```

---

### Task 3: Add `tenants.create()` to the TypeScript SDK

**Files:**
- Modify: `sdks/typescript/packages/hermes-server/src/client.ts`

- [ ] **Step 1: Add the create method to TenantsService**

In `sdks/typescript/packages/hermes-server/src/client.ts`, add to the `TenantsService` class after the existing `list()` method:

```typescript
  async create(body: { name: string }): Promise<Tenant> {
    const result = await this.client.POST("/v1/tenants", {
      body: { name: body.name },
    });
    return unwrap(result);
  }
```

- [ ] **Step 2: Build the SDK to verify it compiles**

Run: `make sdk-ts-build`
Expected: Compiles without errors.

- [ ] **Step 3: Commit**

```bash
git add sdks/typescript/packages/hermes-server/src/client.ts
git commit -m "feat(sdk): add tenants.create() method"
```

---

### Task 4: Add zod schema for send notification form

**Files:**
- Create: `web/admin/lib/schemas/send-notification.ts`

- [ ] **Step 1: Create the schema file**

Create `web/admin/lib/schemas/send-notification.ts`:

```typescript
import { z } from "zod";

const channels = z.array(z.enum(["email", "sms", "inbox"]));

const templateModeSchema = z.object({
  mode: z.literal("template"),
  template: z.string().min(1, "Template is required"),
  data: z
    .array(z.object({ key: z.string().min(1, "Key is required"), value: z.string() }))
    .optional(),
  channels: channels.optional(),
});

const contentModeSchema = z.object({
  mode: z.literal("content"),
  title: z.string().min(1, "Title is required"),
  body: z.string().min(1, "Body is required"),
  actionUrl: z.string().optional(),
  actionLabel: z.string().optional(),
  channels: channels.min(1, "At least one channel is required for direct content"),
});

export const sendNotificationSchema = z
  .object({
    tenantId: z.string().optional(),
    newTenantName: z.string().optional(),
    userId: z.string().min(1, "User ID is required"),
    email: z.string().email("Invalid email").optional().or(z.literal("")),
    phone: z.string().optional(),
  })
  .and(z.discriminatedUnion("mode", [templateModeSchema, contentModeSchema]))
  .refine((data) => data.tenantId || data.newTenantName, {
    message: "Select a tenant or create a new one",
    path: ["tenantId"],
  });

export type SendNotificationFormData = z.infer<typeof sendNotificationSchema>;
```

- [ ] **Step 2: Verify the admin portal still compiles**

Run: `cd web/admin && pnpm build`
Expected: Build succeeds (new file is not imported yet, but syntax is valid).

- [ ] **Step 3: Commit**

```bash
git add web/admin/lib/schemas/send-notification.ts
git commit -m "feat(admin-portal): add zod schema for send notification form"
```

---

### Task 5: Add server actions for send notification and create tenant

**Files:**
- Modify: `web/admin/lib/actions/notifications.ts`
- Modify: `web/admin/lib/actions/tenants.ts`

- [ ] **Step 1: Add `sendNotification` server action**

In `web/admin/lib/actions/notifications.ts`, add the following after the existing exports:

```typescript
export async function sendNotification(options: {
  to: {
    tenantId: string;
    userId: string;
    email?: string;
    phone?: string;
  };
  template?: string;
  content?: {
    title: string;
    body: string;
    actionUrl?: string;
    actionLabel?: string;
  };
  data?: Record<string, unknown>;
  channels?: string[];
}) {
  const hermes = getHermes();
  const result = await hermes.notifications.send(options);
  revalidatePath("/notifications");
  return result;
}
```

Add `revalidatePath` to the imports from `"next/cache"`:

```typescript
import { revalidatePath } from "next/cache";
```

- [ ] **Step 2: Add `createTenant` server action**

In `web/admin/lib/actions/tenants.ts`, add after the existing export:

```typescript
export async function createTenant(name: string) {
  const hermes = getHermes();
  const result = await hermes.tenants.create({ name });
  revalidatePath("/tenants");
  return result;
}
```

Add `revalidatePath` to the imports:

```typescript
import { revalidatePath } from "next/cache";
```

- [ ] **Step 3: Verify the admin portal compiles**

Run: `cd web/admin && pnpm build`
Expected: Build succeeds.

- [ ] **Step 4: Commit**

```bash
git add web/admin/lib/actions/notifications.ts web/admin/lib/actions/tenants.ts
git commit -m "feat(admin-portal): add sendNotification and createTenant server actions"
```

---

### Task 6: Build the send notification form component

**Files:**
- Create: `web/admin/components/send-notification-form.tsx`

- [ ] **Step 1: Create the form component**

Create `web/admin/components/send-notification-form.tsx`:

```typescript
"use client";

import { useForm, Controller, useFieldArray } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useRouter } from "next/navigation";
import { useState, useTransition } from "react";
import { toast } from "sonner";
import { Plus, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Checkbox } from "@/components/ui/checkbox";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  sendNotificationSchema,
  type SendNotificationFormData,
} from "@/lib/schemas/send-notification";
import { sendNotification } from "@/lib/actions/notifications";
import { createTenant } from "@/lib/actions/tenants";
import type { NotificationTemplate, Tenant } from "@hermes-notifications/server";

const CHANNELS = [
  { id: "email", label: "Email" },
  { id: "sms", label: "SMS" },
  { id: "inbox", label: "Inbox" },
] as const;

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

interface SendNotificationFormProps {
  tenants: Tenant[];
  templates: NotificationTemplate[];
}

export function SendNotificationForm({
  tenants: initialTenants,
  templates,
}: SendNotificationFormProps) {
  const router = useRouter();
  const [isPending, startTransition] = useTransition();
  const [tenants, setTenants] = useState(initialTenants);
  const [creatingTenant, setCreatingTenant] = useState(false);
  const [newTenantName, setNewTenantName] = useState("");
  const [isCreatingTenant, setIsCreatingTenant] = useState(false);

  const {
    register,
    handleSubmit,
    watch,
    setValue,
    control,
    formState: { errors },
  } = useForm<SendNotificationFormData>({
    resolver: zodResolver(sendNotificationSchema),
    defaultValues: {
      mode: "template",
      tenantId: "",
      userId: "",
      email: "",
      phone: "",
      template: "",
      data: [],
      channels: [],
    },
  });

  const { fields, append, remove, replace } = useFieldArray({
    control,
    name: "data",
  });

  const mode = watch("mode");
  const selectedChannels = watch("channels") ?? [];

  function handleTemplateChange(slug: string) {
    setValue("template", slug);
    const template = templates.find((t) => t.slug === slug);
    if (template) {
      const vars = extractTemplateVariables(template);
      replace(vars.map((key) => ({ key, value: "" })));
    }
  }

  function toggleChannel(channel: "email" | "sms" | "inbox", checked: boolean) {
    const current = selectedChannels;
    if (checked) {
      setValue("channels", [...current, channel]);
    } else {
      setValue("channels", current.filter((c) => c !== channel));
    }
  }

  async function handleCreateTenant() {
    if (!newTenantName.trim()) return;
    setIsCreatingTenant(true);
    try {
      const tenant = await createTenant(newTenantName.trim());
      setTenants((prev) => [...prev, tenant]);
      setValue("tenantId", tenant.id);
      setCreatingTenant(false);
      setNewTenantName("");
      toast.success(`Tenant "${tenant.name}" created`);
    } catch (err) {
      const message = err instanceof Error ? err.message : "Failed to create tenant";
      toast.error(message);
    } finally {
      setIsCreatingTenant(false);
    }
  }

  function handleFormSubmit(formData: SendNotificationFormData) {
    startTransition(async () => {
      try {
        let tenantId = formData.tenantId;

        if (!tenantId && formData.newTenantName) {
          const tenant = await createTenant(formData.newTenantName);
          tenantId = tenant.id;
        }

        if (!tenantId) {
          toast.error("Please select or create a tenant");
          return;
        }

        const dataMap: Record<string, string> | undefined =
          formData.mode === "template" && formData.data && formData.data.length > 0
            ? Object.fromEntries(
                formData.data
                  .filter((d) => d.key.trim() !== "")
                  .map((d) => [d.key, d.value])
              )
            : undefined;

        const result = await sendNotification({
          to: {
            tenantId,
            userId: formData.userId,
            email: formData.email || undefined,
            phone: formData.phone || undefined,
          },
          template: formData.mode === "template" ? formData.template : undefined,
          content:
            formData.mode === "content"
              ? {
                  title: formData.title,
                  body: formData.body,
                  actionUrl: formData.actionUrl || undefined,
                  actionLabel: formData.actionLabel || undefined,
                }
              : undefined,
          data: dataMap,
          channels:
            formData.channels && formData.channels.length > 0
              ? formData.channels
              : undefined,
        });

        toast.success(`Notification sent: ${result.notificationId}`);
        router.push(`/notifications/${result.notificationId}`);
      } catch (err) {
        const message = err instanceof Error ? err.message : "Something went wrong";
        toast.error(message);
      }
    });
  }

  const selectedTemplate = templates.find(
    (t) => t.slug === watch("template")
  );

  return (
    <form onSubmit={handleSubmit(handleFormSubmit)} className="space-y-6 max-w-2xl">
      {/* Tenant */}
      <div className="space-y-1.5">
        <Label>Tenant</Label>
        {creatingTenant ? (
          <div className="rounded-lg border border-primary/50 bg-primary/5 p-4 space-y-3">
            <div className="flex items-center justify-between">
              <span className="text-sm font-medium text-primary">New Tenant</span>
              <button
                type="button"
                className="text-sm text-muted-foreground underline"
                onClick={() => setCreatingTenant(false)}
              >
                Cancel
              </button>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="newTenantName">Name</Label>
              <div className="flex gap-2">
                <Input
                  id="newTenantName"
                  placeholder="Acme Corp"
                  value={newTenantName}
                  onChange={(e) => setNewTenantName(e.target.value)}
                />
                <Button
                  type="button"
                  size="sm"
                  onClick={handleCreateTenant}
                  disabled={isCreatingTenant || !newTenantName.trim()}
                >
                  {isCreatingTenant ? "Creating..." : "Create"}
                </Button>
              </div>
            </div>
            <p className="text-xs text-muted-foreground">
              A tenant ID (UUIDv4) will be generated automatically.
            </p>
          </div>
        ) : (
          <Controller
            name="tenantId"
            control={control}
            render={({ field }) => (
              <Select value={field.value ?? ""} onValueChange={field.onChange}>
                <SelectTrigger className="w-80">
                  <SelectValue placeholder="Select tenant..." />
                </SelectTrigger>
                <SelectContent>
                  {tenants.map((t) => (
                    <SelectItem key={t.id} value={t.id}>
                      {t.name}
                    </SelectItem>
                  ))}
                  <button
                    type="button"
                    className="relative flex w-full cursor-pointer items-center gap-1.5 border-t px-2 py-1.5 text-sm text-primary outline-none hover:bg-accent"
                    onClick={(e) => {
                      e.preventDefault();
                      setCreatingTenant(true);
                    }}
                  >
                    <Plus className="size-3.5" />
                    Create new tenant...
                  </button>
                </SelectContent>
              </Select>
            )}
          />
        )}
        {errors.tenantId && (
          <p className="text-sm text-destructive">{errors.tenantId.message}</p>
        )}
      </div>

      {/* User ID */}
      <div className="space-y-1.5">
        <Label htmlFor="userId">User ID</Label>
        <Input
          id="userId"
          placeholder="External user ID"
          {...register("userId")}
        />
        {errors.userId && (
          <p className="text-sm text-destructive">{errors.userId.message}</p>
        )}
      </div>

      {/* Email & Phone overrides */}
      <div className="flex gap-4">
        <div className="flex-1 space-y-1.5">
          <Label htmlFor="email">
            Email <span className="font-normal text-muted-foreground">(optional override)</span>
          </Label>
          <Input id="email" placeholder="user@example.com" {...register("email")} />
          {errors.email && (
            <p className="text-sm text-destructive">{errors.email.message}</p>
          )}
        </div>
        <div className="flex-1 space-y-1.5">
          <Label htmlFor="phone">
            Phone <span className="font-normal text-muted-foreground">(optional override)</span>
          </Label>
          <Input id="phone" placeholder="+1234567890" {...register("phone")} />
        </div>
      </div>

      <hr className="border-border" />

      {/* Content Mode Toggle */}
      <Tabs
        value={mode}
        onValueChange={(v) => setValue("mode", v as "template" | "content")}
      >
        <div className="space-y-1.5">
          <Label>Content Mode</Label>
          <TabsList>
            <TabsTrigger value="template">Template</TabsTrigger>
            <TabsTrigger value="content">Direct Content</TabsTrigger>
          </TabsList>
        </div>

        {/* Template Mode */}
        <TabsContent value="template">
          <div className="space-y-4 rounded-lg border p-4">
            <div className="space-y-1.5">
              <Label>Template</Label>
              <Controller
                name="template"
                control={control}
                render={({ field }) => (
                  <Select
                    value={field.value ?? ""}
                    onValueChange={handleTemplateChange}
                  >
                    <SelectTrigger className="w-80">
                      <SelectValue placeholder="Select template..." />
                    </SelectTrigger>
                    <SelectContent>
                      {templates.map((t) => (
                        <SelectItem key={t.id} value={t.slug}>
                          {t.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
              />
              {selectedTemplate && (
                <p className="text-xs text-muted-foreground">
                  Channels: {selectedTemplate.default_channels?.join(", ") || "none configured"}
                </p>
              )}
              {"template" in errors && errors.template && (
                <p className="text-sm text-destructive">
                  {(errors.template as { message?: string }).message}
                </p>
              )}
            </div>

            {/* Key-Value Data Builder */}
            <div className="space-y-2">
              <Label>
                Template Data{" "}
                <span className="font-normal text-muted-foreground">(optional)</span>
              </Label>
              {fields.length > 0 && (
                <div className="space-y-2">
                  <div className="flex gap-2 pr-9">
                    <span className="flex-2 text-xs uppercase text-muted-foreground tracking-wide">
                      Key
                    </span>
                    <span className="flex-3 text-xs uppercase text-muted-foreground tracking-wide">
                      Value
                    </span>
                  </div>
                  {fields.map((field, index) => (
                    <div key={field.id} className="flex gap-2 items-center">
                      <Input
                        className="flex-2 font-mono text-sm"
                        placeholder="key"
                        {...register(`data.${index}.key`)}
                      />
                      <Input
                        className="flex-3"
                        placeholder="value"
                        {...register(`data.${index}.value`)}
                      />
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon"
                        className="shrink-0 size-8 text-muted-foreground hover:text-destructive"
                        onClick={() => remove(index)}
                      >
                        <X className="size-4" />
                      </Button>
                    </div>
                  ))}
                </div>
              )}
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="text-primary"
                onClick={() => append({ key: "", value: "" })}
              >
                <Plus className="size-3.5 mr-1" />
                Add variable
              </Button>
            </div>
          </div>
        </TabsContent>

        {/* Direct Content Mode */}
        <TabsContent value="content">
          <div className="space-y-4 rounded-lg border p-4">
            <div className="space-y-1.5">
              <Label htmlFor="title">Title</Label>
              <Input
                id="title"
                placeholder="Your order has shipped"
                {...register("title")}
              />
              {"title" in errors && errors.title && (
                <p className="text-sm text-destructive">
                  {(errors.title as { message?: string }).message}
                </p>
              )}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="body">Body</Label>
              <Textarea
                id="body"
                placeholder="Your order #12345 has been shipped and is on its way."
                rows={4}
                {...register("body")}
              />
              {"body" in errors && errors.body && (
                <p className="text-sm text-destructive">
                  {(errors.body as { message?: string }).message}
                </p>
              )}
            </div>
            <div className="flex gap-4">
              <div className="flex-1 space-y-1.5">
                <Label htmlFor="actionUrl">
                  Action URL{" "}
                  <span className="font-normal text-muted-foreground">(optional)</span>
                </Label>
                <Input
                  id="actionUrl"
                  placeholder="https://example.com/orders/12345"
                  {...register("actionUrl")}
                />
              </div>
              <div className="flex-1 space-y-1.5">
                <Label htmlFor="actionLabel">
                  Action Label{" "}
                  <span className="font-normal text-muted-foreground">(optional)</span>
                </Label>
                <Input
                  id="actionLabel"
                  placeholder="Track Order"
                  {...register("actionLabel")}
                />
              </div>
            </div>
          </div>
        </TabsContent>
      </Tabs>

      {/* Channels */}
      <div className="space-y-2">
        <Label>
          Channels{" "}
          <span className="font-normal text-muted-foreground">
            {mode === "template"
              ? "(optional — overrides template defaults)"
              : "(required)"}
          </span>
        </Label>
        <div className="flex gap-4">
          {CHANNELS.map(({ id, label }) => (
            <Controller
              key={id}
              name="channels"
              control={control}
              render={() => (
                <label className="flex items-center gap-2 cursor-pointer">
                  <Checkbox
                    checked={selectedChannels.includes(id)}
                    onCheckedChange={(checked) => toggleChannel(id, !!checked)}
                  />
                  <span className="text-sm">{label}</span>
                </label>
              )}
            />
          ))}
        </div>
        {errors.channels && (
          <p className="text-sm text-destructive">
            {(errors.channels as { message?: string }).message}
          </p>
        )}
      </div>

      {/* Actions */}
      <div className="flex gap-2">
        <Button type="submit" disabled={isPending}>
          {isPending ? "Sending..." : "Send Notification"}
        </Button>
        <Button
          type="button"
          variant="outline"
          onClick={() => router.push("/notifications")}
          disabled={isPending}
        >
          Cancel
        </Button>
      </div>
    </form>
  );
}
```

- [ ] **Step 2: Verify the admin portal compiles**

Run: `cd web/admin && pnpm build`
Expected: Build succeeds.

- [ ] **Step 3: Commit**

```bash
git add web/admin/components/send-notification-form.tsx
git commit -m "feat(admin-portal): add send notification form component"
```

---

### Task 7: Add the send notification page and update notifications list

**Files:**
- Create: `web/admin/app/(dashboard)/notifications/send/page.tsx`
- Modify: `web/admin/app/(dashboard)/notifications/page.tsx`

- [ ] **Step 1: Create the send notification page**

Create `web/admin/app/(dashboard)/notifications/send/page.tsx`:

```typescript
import Link from "next/link";
import { ChevronLeftIcon } from "lucide-react";
import { SendNotificationForm } from "@/components/send-notification-form";
import { listTenants } from "@/lib/actions/tenants";
import { listTemplates } from "@/lib/actions/templates";

export default async function SendNotificationPage() {
  const [tenants, templates] = await Promise.all([
    listTenants(),
    listTemplates(),
  ]);

  return (
    <div className="space-y-6">
      <div>
        <Link
          href="/notifications"
          className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground mb-4"
        >
          <ChevronLeftIcon className="size-4" />
          Notifications
        </Link>
        <h1 className="text-2xl font-semibold">Send Notification</h1>
        <p className="text-sm text-muted-foreground">
          Send a notification to a user via template or custom content.
        </p>
      </div>

      <SendNotificationForm tenants={tenants ?? []} templates={templates ?? []} />
    </div>
  );
}
```

- [ ] **Step 2: Add "Send Notification" button to notifications list page**

In `web/admin/app/(dashboard)/notifications/page.tsx`, update the page header to include a send button. Replace the entire file:

```typescript
import Link from "next/link";
import { Send } from "lucide-react";
import { DataTable } from "@/components/data-table";
import { Button } from "@/components/ui/button";
import { listRecentNotifications } from "@/lib/actions/notifications";
import { columns } from "./columns";
import { NotificationLookup } from "./lookup";

export default async function NotificationsPage() {
  const notifications = await listRecentNotifications();

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Notifications</h1>
          <p className="text-sm text-muted-foreground">
            Recent notifications and ID lookup.
          </p>
        </div>
        <Button asChild>
          <Link href="/notifications/send">
            <Send className="size-4 mr-2" />
            Send Notification
          </Link>
        </Button>
      </div>

      <NotificationLookup />

      <DataTable
        columns={columns}
        data={notifications ?? []}
        searchKey="title"
        searchPlaceholder="Search by title..."
      />
    </div>
  );
}
```

- [ ] **Step 3: Verify the admin portal compiles**

Run: `cd web/admin && pnpm build`
Expected: Build succeeds.

- [ ] **Step 4: Commit**

```bash
git add web/admin/app/\(dashboard\)/notifications/send/page.tsx web/admin/app/\(dashboard\)/notifications/page.tsx
git commit -m "feat(admin-portal): add send notification page and link from notifications list"
```

---

### Task 8: Manual smoke test

- [ ] **Step 1: Start infrastructure**

Run: `make infra-up`

- [ ] **Step 2: Start the admin portal dev server**

Run: `cd web/admin && pnpm dev`

- [ ] **Step 3: Verify the notifications page shows the Send button**

Open `http://localhost:3000/notifications` in a browser. Confirm the "Send Notification" button appears in the top-right.

- [ ] **Step 4: Verify the send form loads**

Click "Send Notification". Confirm:
- Tenant dropdown populates with existing tenants
- "Create new tenant..." option appears at the bottom
- Template dropdown populates with existing templates
- Tabs toggle between Template and Direct Content modes

- [ ] **Step 5: Test template variable auto-population**

Select a template that uses variables (e.g. `{{.name}}`). Confirm the key-value builder auto-populates with the extracted variable names.

- [ ] **Step 6: Test inline tenant creation**

Click "Create new tenant...", enter a name, click Create. Confirm the tenant is created and selected in the dropdown.

- [ ] **Step 7: Test sending a notification**

Fill in user ID, select a template, fill template data values, and click Send. Confirm:
- Toast shows the notification ID
- Page redirects to `/notifications/{id}` showing the status
