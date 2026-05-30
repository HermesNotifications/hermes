// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

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
        <Button render={<Link href="/notifications/send" />}>
          <Send className="size-4 mr-2" />
          Send Notification
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
