// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"use client";

import type { NotificationTemplate, Notification } from "@hermes-notifications/server";
import { Mail, MessageSquare, Inbox } from "lucide-react";
import { cn } from "@/lib/utils";

interface ChannelContent {
  channel: string;
  label: string;
  icon: React.ElementType;
  fields: { label: string; value: string | undefined | null }[];
}

function getChannelContents(
  notification: Notification,
  template: NotificationTemplate | null,
): ChannelContent[] {
  const channels = notification.channels ?? [];
  const contents: ChannelContent[] = [];

  if (channels.includes("email")) {
    contents.push({
      channel: "email",
      label: "Email",
      icon: Mail,
      fields: [
        { label: "Subject", value: template?.content?.email?.subject ?? notification.title },
        { label: "Body", value: template?.content?.email?.body ?? notification.body },
      ],
    });
  }

  if (channels.includes("sms")) {
    contents.push({
      channel: "sms",
      label: "SMS",
      icon: MessageSquare,
      fields: [
        { label: "Body", value: template?.content?.sms?.body ?? notification.body },
      ],
    });
  }

  if (channels.includes("inbox")) {
    contents.push({
      channel: "inbox",
      label: "Inbox",
      icon: Inbox,
      fields: [
        { label: "Title", value: template?.content?.inbox?.title ?? notification.title },
        { label: "Body", value: template?.content?.inbox?.body ?? notification.body },
      ],
    });
  }

  return contents;
}

export function NotificationContent({
  notification,
  template,
}: {
  notification: Notification;
  template: NotificationTemplate | null;
}) {
  const contents = getChannelContents(notification, template);

  if (contents.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">
        No channel content available.
      </p>
    );
  }

  return (
    <div className="space-y-4">
      {template && (
        <p className="text-xs text-muted-foreground">
          Content resolved from template{" "}
          <code className="rounded bg-muted px-1 py-0.5 font-mono">
            {template.slug}
          </code>
        </p>
      )}
      {!template && notification.template_id && (
        <p className="text-xs text-muted-foreground">
          Template not found. Showing notification fallback content.
        </p>
      )}

      <div className="grid gap-4">
        {contents.map((ch) => (
          <div
            key={ch.channel}
            className="rounded-lg border bg-card"
          >
            <div className="flex items-center gap-2 border-b px-4 py-2.5">
              <ch.icon className="h-4 w-4 text-muted-foreground" />
              <span className="text-sm font-medium">{ch.label}</span>
            </div>
            <div className="space-y-3 p-4">
              {ch.fields.map((field) => (
                <div key={field.label}>
                  <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide mb-1">
                    {field.label}
                  </p>
                  {field.value ? (
                    <div
                      className={cn(
                        "text-sm whitespace-pre-wrap",
                        field.label === "Body" &&
                          "rounded-md bg-muted/50 p-3 font-mono text-xs leading-relaxed",
                      )}
                    >
                      {field.value}
                    </div>
                  ) : (
                    <span className="text-sm text-muted-foreground">&mdash;</span>
                  )}
                </div>
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
