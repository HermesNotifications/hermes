import Link from "next/link";
import { ChevronLeftIcon } from "lucide-react";
import { TemplateForm } from "@/components/template-form";
import { createTemplate } from "@/lib/actions/templates";
import type { TemplateFormData } from "@/lib/schemas/template";

export default function NewTemplatePage() {
  async function handleCreate(data: TemplateFormData) {
    "use server";
    await createTemplate({
      slug: data.slug,
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
        <h1 className="text-2xl font-semibold">New Template</h1>
        <p className="text-sm text-muted-foreground">
          Create a new notification template.
        </p>
      </div>

      <TemplateForm onSubmit={handleCreate} />
    </div>
  );
}
