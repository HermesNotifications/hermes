// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"use client";

import * as React from "react";
import { KeyRoundIcon, PlusIcon, TriangleAlertIcon } from "lucide-react";
import { toast } from "sonner";
import type { APIKeyCreated } from "@hermes-notifications/server";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { CopyButton } from "@/components/copy-button";
import { createAPIKey } from "@/lib/actions/api-keys";

const ALL_PERMISSIONS = [
  { value: "apikeys:manage", label: "apikeys:manage" },
  { value: "notifications:send", label: "notifications:send" },
  { value: "templates:manage", label: "templates:manage" },
  { value: "organizations:manage", label: "organizations:manage" },
];

const DEFAULT_PERMISSIONS = [
  "notifications:send",
  "templates:manage",
  "organizations:manage",
];

export function CreateAPIKeyDialog() {
  const [open, setOpen] = React.useState(false);
  const [name, setName] = React.useState("");
  const [permissions, setPermissions] = React.useState<string[]>(DEFAULT_PERMISSIONS);
  const [isPending, startTransition] = React.useTransition();
  const [created, setCreated] = React.useState<APIKeyCreated | null>(null);

  function resetState() {
    setName("");
    setPermissions(DEFAULT_PERMISSIONS);
    setCreated(null);
  }

  function handleOpenChange(nextOpen: boolean) {
    setOpen(nextOpen);
    if (!nextOpen) {
      resetState();
    }
  }

  function togglePermission(value: string) {
    setPermissions((prev) =>
      prev.includes(value) ? prev.filter((p) => p !== value) : [...prev, value]
    );
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    startTransition(async () => {
      try {
        const result = await createAPIKey({
          name,
          permissions: permissions.length > 0 ? permissions : undefined,
        });
        setCreated(result);
        toast.success("API key created");
      } catch (err) {
        const message = err instanceof Error ? err.message : "Failed to create API key";
        toast.error(message);
      }
    });
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger
        render={
          <Button>
            <PlusIcon />
            New API Key
          </Button>
        }
      />
      <DialogContent>
        {created ? (
          <>
            <DialogHeader>
              <DialogTitle>API Key Created</DialogTitle>
              <DialogDescription>
                Copy your API key now. You won&apos;t be able to see it again.
              </DialogDescription>
            </DialogHeader>

            <div className="space-y-3">
              <div className="flex items-center gap-2 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-200">
                <TriangleAlertIcon className="h-4 w-4 shrink-0" />
                <span>Store this key securely. It cannot be retrieved again.</span>
              </div>

              <div className="flex items-center gap-2">
                <code className="flex-1 overflow-x-auto rounded-md bg-muted px-3 py-2 font-mono text-xs break-all">
                  {created.raw_key}
                </code>
                <CopyButton value={created.raw_key} />
              </div>

              <div className="text-sm text-muted-foreground">
                <span className="font-medium text-foreground">{created.name}</span>
                {created.permissions && created.permissions.length > 0 && (
                  <span> &middot; {created.permissions.join(", ")}</span>
                )}
              </div>
            </div>

            <DialogFooter>
              <Button onClick={() => handleOpenChange(false)}>Done</Button>
            </DialogFooter>
          </>
        ) : (
          <>
            <DialogHeader>
              <DialogTitle>Create API Key</DialogTitle>
              <DialogDescription>
                Give your key a name and select the permissions it should have.
              </DialogDescription>
            </DialogHeader>

            <form onSubmit={handleSubmit} className="space-y-4">
              <div className="space-y-1.5">
                <Label htmlFor="api-key-name">Name</Label>
                <Input
                  id="api-key-name"
                  placeholder="e.g. Production Backend"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  required
                  autoFocus
                />
              </div>

              <div className="space-y-2">
                <Label>Permissions</Label>
                <div className="space-y-2">
                  {ALL_PERMISSIONS.map(({ value, label }) => (
                    <div key={value} className="flex items-center gap-2">
                      <Checkbox
                        id={`perm-${value}`}
                        checked={permissions.includes(value)}
                        onCheckedChange={() => togglePermission(value)}
                      />
                      <Label
                        htmlFor={`perm-${value}`}
                        className="cursor-pointer font-mono text-xs font-normal"
                      >
                        {label}
                      </Label>
                    </div>
                  ))}
                </div>
              </div>

              <DialogFooter>
                <Button type="submit" disabled={isPending || !name.trim()}>
                  <KeyRoundIcon />
                  {isPending ? "Creating..." : "Create Key"}
                </Button>
              </DialogFooter>
            </form>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
