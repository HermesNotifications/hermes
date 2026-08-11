// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

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
import { createOrganization } from "@/lib/actions/organizations";
import type { NotificationTemplate, Organization } from "@hermes-notifications/server";

const CHANNELS = [
  { id: "email", label: "Email" },
  { id: "sms", label: "SMS" },
  { id: "inbox", label: "Inbox" },
] as const;

function extractTemplateVariables(template: NotificationTemplate): string[] {
  const templateStrings = Object.values(template.content ?? {}).flatMap(
    (fields) => Object.values(fields)
  );
  const pattern = /\{\{\s*\.(\w+)\s*\}\}/g;
  const vars = new Set<string>();
  for (const field of templateStrings) {
    if (!field) continue;
    for (const match of field.matchAll(pattern)) {
      vars.add(match[1]);
    }
  }
  return Array.from(vars);
}

interface SendNotificationFormProps {
  organizations: Organization[];
  templates: NotificationTemplate[];
}

export function SendNotificationForm({
  organizations: initialOrganizations,
  templates,
}: SendNotificationFormProps) {
  const router = useRouter();
  const [isPending, startTransition] = useTransition();
  const [organizations, setOrganizations] = useState(initialOrganizations);
  const [creatingOrganization, setCreatingOrganization] = useState(false);
  const [newOrganizationName, setNewOrganizationName] = useState("");
  const [isCreatingOrganization, setIsCreatingOrganization] = useState(false);

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
      organizationId: "",
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
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    name: "data" as any,
  });

  const mode = watch("mode");
  const selectedChannels = watch("channels") ?? [];

  function handleTemplateChange(slug: string | null) {
    setValue("template", slug ?? "");
    const template = slug ? templates.find((t) => t.slug === slug) : undefined;
    if (template) {
      const vars = extractTemplateVariables(template);
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      replace(vars.map((key) => ({ key, value: "" })) as any);
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

  async function handleCreateOrganization() {
    if (!newOrganizationName.trim()) return;
    setIsCreatingOrganization(true);
    try {
      const organization = await createOrganization(newOrganizationName.trim());
      setOrganizations((prev) => [...prev, organization]);
      setValue("organizationId", organization.id);
      setCreatingOrganization(false);
      setNewOrganizationName("");
      toast.success(`Organization "${organization.name}" created`);
    } catch (err) {
      const message = err instanceof Error ? err.message : "Failed to create organization";
      toast.error(message);
    } finally {
      setIsCreatingOrganization(false);
    }
  }

  function handleFormSubmit(formData: SendNotificationFormData) {
    startTransition(async () => {
      try {
        const organizationId = formData.organizationId;

        if (!organizationId) {
          toast.error("Please select or create an organization");
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

        const contacts: Record<string, string> = {
          ...(formData.email ? { email: formData.email } : {}),
          ...(formData.phone ? { phone: formData.phone } : {}),
        };

        const result = await sendNotification({
          to: {
            organizationId,
            userId: formData.userId,
            contacts: Object.keys(contacts).length > 0 ? contacts : undefined,
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
      {/* Organization */}
      <div className="space-y-1.5">
        <Label>Organization</Label>
        {creatingOrganization ? (
          <div className="rounded-lg border border-primary/50 bg-primary/5 p-4 space-y-3">
            <div className="flex items-center justify-between">
              <span className="text-sm font-medium text-primary">New Organization</span>
              <button
                type="button"
                className="text-sm text-muted-foreground underline"
                onClick={() => setCreatingOrganization(false)}
              >
                Cancel
              </button>
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="newOrganizationName">Name</Label>
              <div className="flex gap-2">
                <Input
                  id="newOrganizationName"
                  placeholder="Acme Corp"
                  value={newOrganizationName}
                  onChange={(e) => setNewOrganizationName(e.target.value)}
                />
                <Button
                  type="button"
                  size="sm"
                  onClick={handleCreateOrganization}
                  disabled={isCreatingOrganization || !newOrganizationName.trim()}
                >
                  {isCreatingOrganization ? "Creating..." : "Create"}
                </Button>
              </div>
            </div>
            <p className="text-xs text-muted-foreground">
              An organization ID (UUIDv4) will be generated automatically.
            </p>
          </div>
        ) : (
          <Controller
            name="organizationId"
            control={control}
            render={({ field }) => (
              <Select value={field.value ?? ""} onValueChange={field.onChange}>
                <SelectTrigger className="w-80">
                  <SelectValue placeholder="Select organization..." />
                </SelectTrigger>
                <SelectContent>
                  {organizations.map((t) => (
                    <SelectItem key={t.id} value={t.id}>
                      {t.name}
                    </SelectItem>
                  ))}
                  <button
                    type="button"
                    className="relative flex w-full cursor-pointer items-center gap-1.5 border-t px-2 py-1.5 text-sm text-primary outline-none hover:bg-accent"
                    onClick={(e) => {
                      e.preventDefault();
                      setCreatingOrganization(true);
                    }}
                  >
                    <Plus className="size-3.5" />
                    Create new organization...
                  </button>
                </SelectContent>
              </Select>
            )}
          />
        )}
        {errors.organizationId && (
          <p className="text-sm text-destructive">{errors.organizationId.message}</p>
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
                        // eslint-disable-next-line @typescript-eslint/no-explicit-any
                        {...register(`data.${index}.key` as any)}
                      />
                      <Input
                        className="flex-3"
                        placeholder="value"
                        // eslint-disable-next-line @typescript-eslint/no-explicit-any
                        {...register(`data.${index}.value` as any)}
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
                // eslint-disable-next-line @typescript-eslint/no-explicit-any
                onClick={() => (append as any)({ key: "", value: "" })}
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
