// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

"use client";

import * as React from "react";
import type { Notification } from "@hermes-notifications/server";
import { cn } from "@/lib/utils";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Clock,
  Send,
  CheckCircle2,
  Eye,
  Archive,
  AlertTriangle,
  Copy,
  XCircle,
} from "lucide-react";
import { toast } from "sonner";

interface Step {
  key: string;
  label: string;
  icon: React.ElementType;
  timestamp: string | null | undefined;
}

import { relativeTime } from "@/lib/relative-time";

function absoluteTime(ts: string): string {
  const d = new Date(ts);
  const utc = d.toISOString().replace("T", " ").slice(0, 19) + " UTC";
  const local = d.toLocaleString("en-US", {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: true,
  });
  return `${local}\n${utc}`;
}

function isStale(notification: Notification): boolean {
  if (notification.status !== "pending") return false;
  const age = Date.now() - new Date(notification.created_at).getTime();
  return age > 60_000; // older than 1 minute
}

function getSteps(notification: Notification): Step[] {
  return [
    { key: "pending", label: "Created", icon: Clock, timestamp: notification.created_at },
    { key: "sent", label: "Sent", icon: Send, timestamp: notification.sent_at },
    { key: "delivered", label: "Delivered", icon: CheckCircle2, timestamp: notification.delivered_at },
    { key: "read", label: "Read", icon: Eye, timestamp: notification.read_at },
    { key: "archived", label: "Archived", icon: Archive, timestamp: notification.archived_at },
  ];
}

function getActiveIndex(status: string): number {
  const order = ["pending", "sent", "delivered", "read", "archived"];
  const idx = order.indexOf(status);
  return idx >= 0 ? idx : 0;
}

function StepDot({
  step,
  isCompleted,
  isCurrent,
  isFailed,
  isStuck,
}: {
  step: Step;
  isCompleted: boolean;
  isCurrent: boolean;
  isFailed: boolean;
  isStuck: boolean;
}) {
  const Icon = step.icon;

  function handleCopy() {
    if (!step.timestamp) return;
    const utc = new Date(step.timestamp).toISOString().replace("T", " ").slice(0, 19) + " UTC";
    navigator.clipboard.writeText(utc);
    toast.success("Timestamp copied");
  }

  const dot = (
    <div
      className={cn(
        "flex h-8 w-8 items-center justify-center rounded-full border-2 transition-all",
        isFailed && isCurrent
          ? "border-red-500 bg-red-50 text-red-600 dark:bg-red-950 dark:text-red-400"
          : isStuck && isCurrent
            ? "border-yellow-500 bg-yellow-50 text-yellow-600 dark:bg-yellow-950 dark:text-yellow-400 animate-pulse"
            : isCompleted
              ? "border-emerald-500 bg-emerald-50 text-emerald-600 dark:bg-emerald-950 dark:text-emerald-400"
              : "border-muted-foreground/25 bg-background text-muted-foreground/40",
      )}
    >
      {isFailed && isCurrent ? (
        <XCircle className="h-4 w-4" />
      ) : isStuck && isCurrent ? (
        <AlertTriangle className="h-4 w-4" />
      ) : (
        <Icon className="h-4 w-4" />
      )}
    </div>
  );

  if (!step.timestamp) {
    return (
      <div className="flex flex-col items-center gap-1">
        {dot}
        <span className="text-[10px] text-muted-foreground/40">{step.label}</span>
      </div>
    );
  }

  return (
    <DropdownMenu>
      <Tooltip>
        <TooltipTrigger
          render={<DropdownMenuTrigger className="cursor-default" />}
        >
          <div className="flex flex-col items-center gap-1">
            {dot}
            <span
              className={cn(
                "text-[10px]",
                isCompleted ? "text-muted-foreground" : "text-muted-foreground/40",
              )}
            >
              {step.label}
            </span>
            <span
              className={cn(
                "text-[10px]",
                isCompleted ? "text-muted-foreground" : "text-muted-foreground/40",
              )}
            >
              {relativeTime(step.timestamp)}
            </span>
          </div>
        </TooltipTrigger>
        <TooltipContent className="whitespace-pre text-center">
          {absoluteTime(step.timestamp)}
        </TooltipContent>
      </Tooltip>
      <DropdownMenuContent align="center" sideOffset={4}>
        <DropdownMenuGroup>
          <DropdownMenuItem onClick={handleCopy}>
            <Copy className="h-3.5 w-3.5" />
            Copy timestamp
          </DropdownMenuItem>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function NotificationStatusStepper({
  notification,
}: {
  notification: Notification;
}) {
  const steps = getSteps(notification);
  const isFailed = notification.status === "failed";
  const stuck = isStale(notification);
  const activeIndex = isFailed ? 0 : getActiveIndex(notification.status);

  // Connector between step i and i+1
  function connectorClass(i: number): string {
    if (isFailed) return "bg-red-500/20";
    if (i < activeIndex) return "bg-emerald-500";
    return "bg-muted-foreground/15";
  }

  return (
    <div className="space-y-3">
      <div className="flex items-start justify-between">
        {steps.map((step, i) => (
          <React.Fragment key={step.key}>
            <StepDot
              step={step}
              isCompleted={!isFailed && i <= activeIndex}
              isCurrent={i === activeIndex}
              isFailed={isFailed}
              isStuck={stuck}
            />
            {i < steps.length - 1 && (
              <div className="mt-4 flex-1 mx-1">
                <div className={cn("h-0.5 rounded-full", connectorClass(i))} />
              </div>
            )}
          </React.Fragment>
        ))}
      </div>

      {isFailed && (
        <div className="flex items-center gap-2 rounded-md border border-red-200 bg-red-50 px-3 py-2 dark:border-red-800 dark:bg-red-950">
          <XCircle className="h-4 w-4 text-red-500 shrink-0" />
          <p className="text-xs text-red-700 dark:text-red-300">
            This notification failed to deliver.
          </p>
        </div>
      )}

      {stuck && (
        <div className="flex items-center gap-2 rounded-md border border-yellow-200 bg-yellow-50 px-3 py-2 dark:border-yellow-800 dark:bg-yellow-950">
          <AlertTriangle className="h-4 w-4 text-yellow-600 dark:text-yellow-400 shrink-0" />
          <p className="text-xs text-yellow-700 dark:text-yellow-300">
            This notification has been pending for{" "}
            {relativeTime(notification.created_at).replace(" ago", "")}
            . It may be stuck in processing.
          </p>
        </div>
      )}
    </div>
  );
}
