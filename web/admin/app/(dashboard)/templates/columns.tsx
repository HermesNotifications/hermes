"use client";

import { ColumnDef } from "@tanstack/react-table";
import Link from "next/link";
import { MoreHorizontalIcon } from "lucide-react";
import { useTransition } from "react";
import { toast } from "sonner";
import { useRouter } from "next/navigation";
import type { NotificationTemplate } from "@hermes-notifications/server";
import { ChannelBadges } from "@/components/channel-badge";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { deleteTemplate } from "@/lib/actions/templates";

function TemplateActions({ id }: { id: string }) {
  const router = useRouter();
  const [isPending, startTransition] = useTransition();

  function handleDelete() {
    startTransition(async () => {
      try {
        await deleteTemplate(id);
        toast.success("Template deleted");
        router.refresh();
      } catch (err) {
        const message = err instanceof Error ? err.message : "Failed to delete template";
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
          render={<Link href={`/templates/${id}/edit`}>Edit</Link>}
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
          title="Delete Template"
          description="Are you sure you want to delete this template? This action cannot be undone."
          onConfirm={handleDelete}
          destructive
        />
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export const columns: ColumnDef<NotificationTemplate>[] = [
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
      return channels.length > 0 ? <ChannelBadges channels={channels} /> : <span className="text-muted-foreground text-sm">—</span>;
    },
  },
  {
    accessorKey: "subscription_id",
    header: "Subscription",
    cell: ({ row }) => {
      const subId = row.original.subscription_id;
      return subId ? (
        <code className="rounded bg-muted px-1.5 py-0.5 text-xs font-mono">{subId}</code>
      ) : (
        <span className="text-muted-foreground text-sm">—</span>
      );
    },
  },
  {
    accessorKey: "created_at",
    header: "Created",
    cell: ({ row }) => {
      const date = new Date(row.getValue<string>("created_at"));
      return (
        <span className="text-sm text-muted-foreground">
          {date.toLocaleDateString()}
        </span>
      );
    },
  },
  {
    id: "actions",
    cell: ({ row }) => <TemplateActions id={row.original.id} />,
  },
];
