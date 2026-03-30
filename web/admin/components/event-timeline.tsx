"use client";

import * as React from "react";
import type { NotificationEvent } from "@hermes-notifications/server";
import { Badge } from "@/components/ui/badge";
import { ChannelBadge } from "@/components/channel-badge";

function severityVariant(severity: string): "default" | "secondary" | "destructive" | "outline" {
  switch (severity) {
    case "error":
      return "destructive";
    case "warning":
      return "secondary";
    case "info":
    default:
      return "outline";
  }
}

function severityClass(severity: string): string {
  switch (severity) {
    case "error":
      return "";
    case "warning":
      return "border-yellow-300 bg-yellow-50 text-yellow-800 dark:border-yellow-700 dark:bg-yellow-950 dark:text-yellow-200";
    case "info":
    default:
      return "border-blue-300 bg-blue-50 text-blue-800 dark:border-blue-700 dark:bg-blue-950 dark:text-blue-200";
  }
}

function dotClass(severity: string): string {
  switch (severity) {
    case "error":
      return "bg-destructive";
    case "warning":
      return "bg-yellow-500";
    case "info":
    default:
      return "bg-blue-500";
  }
}

function formatTimestamp(ts: string): string {
  return new Date(ts).toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
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
            <div className={`mt-1 h-2.5 w-2.5 shrink-0 rounded-full ${dotClass(event.severity)}`} />
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
                variant={severityVariant(event.severity)}
                className={event.severity !== "error" ? severityClass(event.severity) : ""}
              >
                {event.severity}
              </Badge>
            </div>
            <p className="mt-0.5 text-xs text-muted-foreground">
              {formatTimestamp(event.created_at)}
            </p>
            {event.metadata && <MetadataCollapsible metadata={event.metadata} />}
          </div>
        </div>
      ))}
    </div>
  );
}
