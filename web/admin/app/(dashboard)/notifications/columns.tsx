// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"use client";

import { ColumnDef } from "@tanstack/react-table";
import Link from "next/link";
import type { NotificationItem } from "@hermes-notifications/server";
import { Badge } from "@/components/ui/badge";
import { ChannelBadges } from "@/components/channel-badge";

const statusStyles: Record<string, string> = {
  pending: "border-yellow-300 bg-yellow-50 text-yellow-800 dark:border-yellow-700 dark:bg-yellow-950 dark:text-yellow-200",
  sent: "border-blue-300 bg-blue-50 text-blue-800 dark:border-blue-700 dark:bg-blue-950 dark:text-blue-200",
  delivered: "border-green-300 bg-green-50 text-green-800 dark:border-green-700 dark:bg-green-950 dark:text-green-200",
  read: "border-purple-300 bg-purple-50 text-purple-800 dark:border-purple-700 dark:bg-purple-950 dark:text-purple-200",
  failed: "border-red-300 bg-red-50 text-red-800 dark:border-red-700 dark:bg-red-950 dark:text-red-200",
  archived: "border-border bg-muted text-muted-foreground",
};

export const columns: ColumnDef<NotificationItem>[] = [
  {
    accessorKey: "id",
    header: "ID",
    cell: ({ row }) => (
      <Link
        href={`/notifications/${row.original.id}`}
        className="hover:underline"
      >
        <code className="rounded bg-muted px-1.5 py-0.5 text-xs font-mono">
          {row.getValue<string>("id")}
        </code>
      </Link>
    ),
  },
  {
    accessorKey: "status",
    header: "Status",
    cell: ({ row }) => {
      const status = row.getValue<string>("status");
      return (
        <Badge variant="outline" className={statusStyles[status] ?? ""}>
          {status}
        </Badge>
      );
    },
  },
  {
    accessorKey: "template_slug",
    header: "Template",
    cell: ({ row }) => {
      const slug = row.getValue<string | undefined>("template_slug");
      return slug ? (
        <code className="rounded bg-muted px-1.5 py-0.5 text-xs font-mono">
          {slug}
        </code>
      ) : (
        <span className="text-sm text-muted-foreground">&mdash;</span>
      );
    },
  },
  {
    accessorKey: "title",
    header: "Title",
    cell: ({ row }) => (
      <span className="text-sm max-w-[200px] truncate block">
        {row.getValue<string>("title")}
      </span>
    ),
  },
  {
    accessorKey: "channels",
    header: "Channels",
    cell: ({ row }) => {
      const channels = row.getValue<string[] | null>("channels") ?? [];
      return channels.length > 0 ? (
        <ChannelBadges channels={channels} />
      ) : (
        <span className="text-sm text-muted-foreground">&mdash;</span>
      );
    },
  },
  {
    accessorKey: "organization_id",
    header: "Organization",
    cell: ({ row }) => (
      <code className="rounded bg-muted px-1.5 py-0.5 text-xs font-mono">
        {(row.getValue<string>("organization_id") ?? "").slice(0, 8)}...
      </code>
    ),
  },
  {
    accessorKey: "created_at",
    header: "Created",
    cell: ({ row }) => (
      <span className="text-sm text-muted-foreground">
        {new Date(row.getValue<string>("created_at")).toISOString().split("T")[0]}
      </span>
    ),
  },
];
