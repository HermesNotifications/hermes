// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import Link from "next/link";
import { ArrowLeft } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { NotificationStatusStepper } from "@/components/notification-status-stepper";
import { NotificationDetail } from "@/components/notification-detail";
import { NotificationTabs } from "@/components/notification-tabs";
import { getNotificationStatus } from "@/lib/actions/notifications";
import { listTemplates } from "@/lib/actions/templates";

export default async function NotificationDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  const data = await getNotificationStatus(id);

  // Resolve template if the notification references one
  let template = null;
  if (data?.notification.template_id) {
    const templates = await listTemplates();
    template =
      templates?.find((t) => t.id === data.notification.template_id) ?? null;
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        <Button variant="ghost" size="icon" render={<Link href="/notifications" />}>
          <ArrowLeft className="h-4 w-4" />
        </Button>
        <div>
          <h1 className="text-2xl font-semibold">Notification</h1>
          <p className="text-sm text-muted-foreground font-mono">{id}</p>
        </div>
      </div>

      {data ? (
        <div className="space-y-6">
          <NotificationDetail notification={data.notification} />

          <Card>
            <CardHeader>
              <CardTitle>Status</CardTitle>
            </CardHeader>
            <CardContent>
              <NotificationStatusStepper notification={data.notification} />
            </CardContent>
          </Card>

          <Card>
            <CardContent className="pt-6">
              <NotificationTabs
                notification={data.notification}
                events={data.events}
                template={template}
              />
            </CardContent>
          </Card>
        </div>
      ) : (
        <p className="text-sm text-muted-foreground">Notification not found.</p>
      )}
    </div>
  );
}
