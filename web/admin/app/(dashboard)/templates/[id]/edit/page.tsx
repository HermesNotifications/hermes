// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { notFound } from "next/navigation";
import Link from "next/link";
import { ChevronLeftIcon } from "lucide-react";
import { TemplateForm } from "@/components/template-form";
import { listTemplates, updateTemplate } from "@/lib/actions/templates";
import { listAllSubscriptions } from "@/lib/actions/subscriptions";
import type { TemplateFormData } from "@/lib/schemas/template";

interface EditTemplatePageProps {
  params: Promise<{ id: string }>;
}

export default async function EditTemplatePage({ params }: EditTemplatePageProps) {
  const { id } = await params;
  const [templates, subscriptionGroups] = await Promise.all([
    listTemplates(),
    listAllSubscriptions(),
  ]);
  const template = templates.find((t) => t.id === id);

  if (!template) {
    notFound();
  }

  async function handleUpdate(data: TemplateFormData) {
    "use server";
    await updateTemplate(id, {
      name: data.name,
      subscriptionId: data.subscriptionId || undefined,
      defaultChannels: data.defaultChannels,
      emailSubject: data.emailSubject || undefined,
      emailBody: data.emailBody || undefined,
      smsBody: data.smsBody || undefined,
      inboxTitle: data.inboxTitle || undefined,
      inboxBody: data.inboxBody || undefined,
    });
  }

  const defaultValues: Partial<TemplateFormData> = {
    name: template.name,
    slug: template.slug,
    subscriptionId: template.subscription_id,
    defaultChannels: ((template.default_channels ?? []) as string[])
      .filter((c: string) => ["email", "sms", "inbox"].includes(c)) as Array<"email" | "sms" | "inbox">,
    emailSubject: template.email_subject,
    emailBody: template.email_body,
    smsBody: template.sms_body,
    inboxTitle: template.inbox_title,
    inboxBody: template.inbox_body,
  };

  return (
    <div className="space-y-6">
      <div>
        <Link
          href="/templates"
          className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground mb-4"
        >
          <ChevronLeftIcon className="size-4" />
          Templates
        </Link>
        <h1 className="text-2xl font-semibold">Edit Template</h1>
        <p className="text-sm text-muted-foreground">
          Update <span className="font-mono">{template.slug}</span>
        </p>
      </div>

      <TemplateForm defaultValues={defaultValues} onSubmit={handleUpdate} subscriptionGroups={subscriptionGroups} isEdit />
    </div>
  );
}
