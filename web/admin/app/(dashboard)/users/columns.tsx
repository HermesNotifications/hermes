// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"use client";

import { ColumnDef } from "@tanstack/react-table";
import type { User } from "@hermes-notifications/server";

export const columns: ColumnDef<User>[] = [
  {
    accessorKey: "external_id",
    header: "External ID",
    cell: ({ row }) => (
      <code className="rounded bg-muted px-1.5 py-0.5 text-xs font-mono">
        {row.getValue("external_id")}
      </code>
    ),
  },
  {
    accessorKey: "organization_name",
    header: "Organization",
  },
  {
    accessorKey: "email",
    header: "Email",
    cell: ({ row }) => {
      const email = row.getValue<string | null>("email");
      return email ? (
        <span className="text-sm">{email}</span>
      ) : (
        <span className="text-sm text-muted-foreground">&mdash;</span>
      );
    },
  },
  {
    accessorKey: "phone",
    header: "Phone",
    cell: ({ row }) => {
      const phone = row.getValue<string | null>("phone");
      return phone ? (
        <span className="text-sm">{phone}</span>
      ) : (
        <span className="text-sm text-muted-foreground">&mdash;</span>
      );
    },
  },
  {
    accessorKey: "locale",
    header: "Locale",
    cell: ({ row }) => {
      const locale = row.getValue<string | null>("locale");
      return (
        <span className="text-sm text-muted-foreground">
          {locale || "\u2014"}
        </span>
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
