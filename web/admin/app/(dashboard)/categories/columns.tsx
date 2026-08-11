// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"use client";

import { ColumnDef } from "@tanstack/react-table";
import Link from "next/link";
import { MoreHorizontalIcon } from "lucide-react";
import { useTransition } from "react";
import { toast } from "sonner";
import { useRouter } from "next/navigation";
import type { SubscriptionCategory } from "@hermes-notifications/server";
import { ChannelBadges } from "@/components/channel-badge";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { deleteCategory } from "@/lib/actions/categories";

const STATE_VARIANTS: Record<string, "default" | "secondary" | "outline"> = {
  on: "default",
  off: "secondary",
  required: "outline",
};

function CategoryActions({ id }: { id: string }) {
  const router = useRouter();
  const [isPending, startTransition] = useTransition();

  function handleDelete() {
    startTransition(async () => {
      try {
        await deleteCategory(id);
        toast.success("Category deleted");
        router.refresh();
      } catch (err) {
        const message = err instanceof Error ? err.message : "Failed to delete category";
        toast.error(message);
      }
    });
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button variant="ghost" size="icon" disabled={isPending}>
            <MoreHorizontalIcon />
            <span className="sr-only">Open menu</span>
          </Button>
        }
      />
      <DropdownMenuContent align="end">
        <DropdownMenuItem
          render={<Link href={`/categories/${id}`}>Edit</Link>}
        />
        <ConfirmDialog
          trigger={
            <DropdownMenuItem
              variant="destructive"
              onSelect={(e) => e.preventDefault()}
            >
              Delete
            </DropdownMenuItem>
          }
          title="Delete Category"
          description="Are you sure you want to delete this category? All subscriptions within it will also be deleted."
          onConfirm={handleDelete}
          destructive
        />
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export const columns: ColumnDef<SubscriptionCategory>[] = [
  {
    accessorKey: "name",
    header: "Name",
  },
  {
    accessorKey: "slug",
    header: "Slug",
    cell: ({ row }) => (
      <code className="rounded bg-muted px-1.5 py-0.5 text-xs font-mono">
        {row.getValue("slug")}
      </code>
    ),
  },
  {
    accessorKey: "default_channels",
    header: "Channels",
    cell: ({ row }) => {
      const channels = row.getValue<string[] | null>("default_channels") ?? [];
      return channels.length > 0 ? (
        <ChannelBadges channels={channels} />
      ) : (
        <span className="text-muted-foreground text-sm">—</span>
      );
    },
  },
  {
    accessorKey: "default_state",
    header: "State",
    cell: ({ row }) => {
      const state = row.getValue<string>("default_state");
      const variant = STATE_VARIANTS[state] ?? "outline";
      return (
        <Badge variant={variant} className="capitalize">
          {state}
        </Badge>
      );
    },
  },
  {
    accessorKey: "sort_order",
    header: "Sort Order",
    cell: ({ row }) => (
      <span className="text-sm text-muted-foreground">
        {row.getValue<number>("sort_order")}
      </span>
    ),
  },
  {
    id: "actions",
    cell: ({ row }) => <CategoryActions id={row.original.id} />,
  },
];
