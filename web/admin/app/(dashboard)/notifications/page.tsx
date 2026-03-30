"use client";

import * as React from "react";
import { SearchIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { NotificationDetail } from "@/components/notification-detail";
import { EventTimeline } from "@/components/event-timeline";
import { getNotificationStatus } from "@/lib/actions/notifications";
import type { Notification, NotificationEvent } from "@hermes-notifications/server";

type Result =
  | { found: true; notification: Notification; events: NotificationEvent[] }
  | { found: false };

export default function NotificationsPage() {
  const [id, setId] = React.useState("");
  const [isPending, startTransition] = React.useTransition();
  const [result, setResult] = React.useState<Result | null>(null);

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    const trimmed = id.trim();
    if (!trimmed) return;

    startTransition(async () => {
      const data = await getNotificationStatus(trimmed);
      if (data) {
        setResult({ found: true, notification: data.notification, events: data.events });
      } else {
        setResult({ found: false });
      }
    });
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">Notifications</h1>
        <p className="text-sm text-muted-foreground">
          Look up a notification by ID to view its status and event timeline.
        </p>
      </div>

      <form onSubmit={handleSubmit} className="flex gap-2">
        <Input
          placeholder="Notification ID"
          value={id}
          onChange={(e) => setId(e.target.value)}
          className="max-w-sm"
        />
        <Button type="submit" disabled={isPending || !id.trim()}>
          <SearchIcon />
          {isPending ? "Looking up..." : "Look up"}
        </Button>
      </form>

      {isPending && (
        <p className="text-sm text-muted-foreground">Loading...</p>
      )}

      {!isPending && result !== null && (
        <>
          {result.found ? (
            <div className="space-y-6">
              <NotificationDetail notification={result.notification} />

              <Card>
                <CardHeader>
                  <CardTitle>Event Timeline</CardTitle>
                </CardHeader>
                <CardContent>
                  <EventTimeline events={result.events} />
                </CardContent>
              </Card>
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">
              Notification not found.
            </p>
          )}
        </>
      )}
    </div>
  );
}
