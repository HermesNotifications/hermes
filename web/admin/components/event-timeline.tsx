// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

"use client";

import * as React from "react";
import type { NotificationEvent } from "@hermes-notifications/server";
import { Badge } from "@/components/ui/badge";
import { ChannelBadge } from "@/components/channel-badge";

const severityStyles: Record<string, { badge: string; dot: string }> = {
  error: {
    badge: "border-red-300 bg-red-50 text-red-800 dark:border-red-700 dark:bg-red-950 dark:text-red-200",
    dot: "bg-red-500",
  },
  warn: {
    badge: "border-yellow-300 bg-yellow-50 text-yellow-800 dark:border-yellow-700 dark:bg-yellow-950 dark:text-yellow-200",
    dot: "bg-yellow-500",
  },
  info: {
    badge: "border-blue-300 bg-blue-50 text-blue-800 dark:border-blue-700 dark:bg-blue-950 dark:text-blue-200",
    dot: "bg-blue-500",
  },
  success: {
    badge: "border-green-300 bg-green-50 text-green-800 dark:border-green-700 dark:bg-green-950 dark:text-green-200",
    dot: "bg-green-500",
  },
};

const defaultSeverityStyle = severityStyles.info;

function formatTimestamp(ts: string): string {
  return new Date(ts).toISOString().replace("T", " ").slice(0, 19) + " UTC";
}

function MetadataCollapsible({ metadata }: { metadata: string }) {
  const [open, setOpen] = React.useState(false);

  let parsed: unknown = metadata;
  try {
    parsed = JSON.parse(metadata);
  } catch {
    // keep as string
  }

  return (
    <div className="mt-2">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="text-xs text-muted-foreground underline underline-offset-2 hover:text-foreground"
      >
        {open ? "Hide metadata" : "Show metadata"}
      </button>
      {open && (
        <pre className="mt-1.5 overflow-x-auto rounded-md bg-muted px-3 py-2 text-xs">
          {typeof parsed === "string" ? parsed : JSON.stringify(parsed, null, 2)}
        </pre>
      )}
    </div>
  );
}

function EventMetadata({ metadata, severity }: { metadata: string; severity: string }) {
  let parsed: Record<string, unknown> | null = null;
  try {
    parsed = JSON.parse(metadata);
  } catch {
    // not JSON
  }

  // Show error/reason messages inline for error-severity events
  const errorMsg = parsed?.error ?? parsed?.reason;
  if (severity === "error" && typeof errorMsg === "string") {
    return (
      <p className="mt-1 text-xs text-destructive">
        {errorMsg}
      </p>
    );
  }

  return <MetadataCollapsible metadata={metadata} />;
}

export function EventTimeline({ events }: { events: NotificationEvent[] }) {
  const sorted = [...events].sort(
    (a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime()
  );

  if (sorted.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">No events recorded yet.</p>
    );
  }

  return (
    <div className="space-y-0">
      {sorted.map((event, index) => (
        <div key={event.id} className="flex gap-4">
          {/* Timeline spine */}
          <div className="flex flex-col items-center">
            <div className={`mt-1 h-2.5 w-2.5 shrink-0 rounded-full ${(severityStyles[event.severity] ?? defaultSeverityStyle).dot}`} />
            {index < sorted.length - 1 && (
              <div className="w-px flex-1 bg-border mt-1 mb-1" style={{ minHeight: "1.5rem" }} />
            )}
          </div>

          {/* Event content */}
          <div className={`pb-5 ${index === sorted.length - 1 ? "pb-0" : ""}`}>
            <div className="flex flex-wrap items-center gap-1.5">
              <ChannelBadge channel={event.channel} />
              <span className="text-sm font-medium">{event.event}</span>
              <Badge
                variant="outline"
                className={(severityStyles[event.severity] ?? defaultSeverityStyle).badge}
              >
                {event.severity}
              </Badge>
            </div>
            <p className="mt-0.5 text-xs text-muted-foreground">
              {formatTimestamp(event.created_at)}
            </p>
            {event.metadata && <EventMetadata metadata={event.metadata} severity={event.severity} />}
          </div>
        </div>
      ))}
    </div>
  );
}
