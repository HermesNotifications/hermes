// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"use client";

import type {
  Notification,
  NotificationEvent,
  NotificationTemplate,
} from "@hermes-notifications/server";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@/components/ui/tabs";
import { EventTimeline } from "@/components/event-timeline";
import { NotificationContent } from "@/components/notification-content";

export function NotificationTabs({
  notification,
  events,
  template,
}: {
  notification: Notification;
  events: NotificationEvent[];
  template: NotificationTemplate | null;
}) {
  return (
    <Tabs defaultValue="timeline">
      <TabsList>
        <TabsTrigger value="timeline">Timeline</TabsTrigger>
        <TabsTrigger value="content">Content</TabsTrigger>
      </TabsList>
      <TabsContent value="timeline" className="pt-4">
        <EventTimeline events={events} />
      </TabsContent>
      <TabsContent value="content" className="pt-4">
        <NotificationContent notification={notification} template={template} />
      </TabsContent>
    </Tabs>
  );
}
