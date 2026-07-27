// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

"use client";

import { ColumnDef } from "@tanstack/react-table";
import Link from "next/link";
import type { Organization } from "@hermes-notifications/server";
import { Badge } from "@/components/ui/badge";

export const columns: ColumnDef<Organization>[] = [
  {
    accessorKey: "name",
    header: "Name",
  },
  {
    accessorKey: "id",
    header: "ID",
    cell: ({ row }) => (
      <code className="rounded bg-muted px-1.5 py-0.5 text-xs font-mono">
        {row.getValue("id")}
      </code>
    ),
  },
  {
    accessorKey: "default_locale",
    header: "Locale",
    cell: ({ row }) => (
      <span className="text-sm text-muted-foreground">
        {row.getValue<string>("default_locale") || "en"}
      </span>
    ),
  },
  {
    accessorKey: "user_count",
    header: "Users",
    cell: ({ row }) => {
      const count = row.getValue<number>("user_count");
      return (
        <Link
          href={`/users?organization_id=${row.original.id}`}
          className="hover:underline"
        >
          <Badge variant="secondary">{count}</Badge>
        </Link>
      );
    },
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
