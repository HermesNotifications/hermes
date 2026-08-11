// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"use client";

import { ColumnDef } from "@tanstack/react-table";
import { useTransition } from "react";
import { toast } from "sonner";
import { useRouter } from "next/navigation";
import { Trash2Icon } from "lucide-react";
import type { APIKeyInfo } from "@hermes-notifications/server";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { deleteAPIKey } from "@/lib/actions/api-keys";

function APIKeyActions({ id }: { id: string }) {
  const router = useRouter();
  const [isPending, startTransition] = useTransition();

  function handleDelete() {
    startTransition(async () => {
      try {
        await deleteAPIKey(id);
        toast.success("API key revoked");
        router.refresh();
      } catch (err) {
        const message = err instanceof Error ? err.message : "Failed to revoke API key";
        toast.error(message);
      }
    });
  }

  return (
    <ConfirmDialog
      trigger={
        <Button variant="ghost" size="icon" disabled={isPending}>
          <Trash2Icon className="h-4 w-4 text-destructive" />
          <span className="sr-only">Revoke API key</span>
        </Button>
      }
      title="Revoke API Key"
      description="Revoke this API key? This action cannot be undone. Any service using this key will lose access immediately."
      onConfirm={handleDelete}
      destructive
    />
  );
}

export const columns: ColumnDef<APIKeyInfo>[] = [
  {
    accessorKey: "name",
    header: "Name",
  },
  {
    accessorKey: "permissions",
    header: "Permissions",
    cell: ({ row }) => {
      const permissions = row.getValue<string[] | null>("permissions") ?? [];
      if (permissions.length === 0) {
        return <span className="text-muted-foreground text-sm">—</span>;
      }
      return (
        <div className="flex flex-wrap gap-1">
          {permissions.map((p) => (
            <Badge key={p} variant="secondary" className="font-mono text-xs">
              {p}
            </Badge>
          ))}
        </div>
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
  {
    id: "actions",
    cell: ({ row }) => <APIKeyActions id={row.original.id} />,
  },
];
