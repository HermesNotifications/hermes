// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import type { Notification } from "@hermes-notifications/server";
import { Badge } from "@/components/ui/badge";
import { CopyButton } from "@/components/copy-button";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { ChannelBadges } from "@/components/channel-badge";
import { relativeTime } from "@/lib/relative-time";

function statusVariant(status: string): "default" | "secondary" | "outline" | "destructive" {
  switch (status) {
    case "sent":
    case "delivered":
      return "default";
    case "read":
      return "secondary";
    case "failed":
      return "destructive";
    case "archived":
    case "pending":
    default:
      return "outline";
  }
}

function statusClass(status: string): string {
  switch (status) {
    case "pending":
      return "border-yellow-300 bg-yellow-50 text-yellow-800 dark:border-yellow-700 dark:bg-yellow-950 dark:text-yellow-200";
    case "sent":
      return "border-blue-300 bg-blue-50 text-blue-800 dark:border-blue-700 dark:bg-blue-950 dark:text-blue-200";
    case "delivered":
      return "border-green-300 bg-green-50 text-green-800 dark:border-green-700 dark:bg-green-950 dark:text-green-200";
    case "read":
      return "border-purple-300 bg-purple-50 text-purple-800 dark:border-purple-700 dark:bg-purple-950 dark:text-purple-200";
    case "failed":
      return "";
    case "archived":
      return "border-border bg-muted text-muted-foreground";
    default:
      return "";
  }
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="space-y-0.5">
      <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
        {label}
      </p>
      <div className="text-sm">{children}</div>
    </div>
  );
}

function formatTimestamp(ts: string): string {
  return new Date(ts).toISOString().replace("T", " ").slice(0, 19) + " UTC";
}

export function NotificationDetail({ notification }: { notification: Notification }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Details</CardTitle>
      </CardHeader>
      <CardContent className="grid gap-4 sm:grid-cols-2">
        <Field label="ID">
          <span className="inline-flex items-center gap-1">
            <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs break-all">
              {notification.id}
            </code>
            <CopyButton value={notification.id} />
          </span>
        </Field>

        <Field label="Status">
          <Badge
            variant={statusVariant(notification.status)}
            className={statusClass(notification.status)}
          >
            {notification.status}
          </Badge>
        </Field>

        <Field label="Channels">
          {notification.channels && notification.channels.length > 0 ? (
            <ChannelBadges channels={notification.channels} />
          ) : (
            <span className="text-muted-foreground">—</span>
          )}
        </Field>

        <Field label="User ID">
          <span className="inline-flex items-center gap-1">
            <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">
              {notification.user_id}
            </code>
            <CopyButton value={notification.user_id} />
          </span>
        </Field>

        <Field label="Organization ID">
          <span className="inline-flex items-center gap-1">
            <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">
              {notification.organization_id}
            </code>
            <CopyButton value={notification.organization_id} />
          </span>
        </Field>

        {notification.template_id && (
          <Field label="Template ID">
            <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">
              {notification.template_id}
            </code>
          </Field>
        )}

        <Field label="Title">
          <span>{notification.title}</span>
        </Field>

        <Field label="Created">
          <span className="text-xs">
            {formatTimestamp(notification.created_at)}
            <span className="text-muted-foreground ml-1.5">({relativeTime(notification.created_at)})</span>
          </span>
        </Field>
      </CardContent>
    </Card>
  );
}
