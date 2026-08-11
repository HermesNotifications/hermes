// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import Link from "next/link";
import { ChevronLeftIcon } from "lucide-react";
import { SendNotificationForm } from "@/components/send-notification-form";
import { listOrganizations } from "@/lib/actions/organizations";
import { listTemplates } from "@/lib/actions/templates";

export default async function SendNotificationPage() {
  const [organizations, templates] = await Promise.all([
    listOrganizations(),
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

      <SendNotificationForm organizations={organizations ?? []} templates={templates ?? []} />
    </div>
  );
}
