// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { DataTable } from "@/components/data-table";
import { CreateAPIKeyDialog } from "@/components/create-api-key-dialog";
import { listAPIKeys } from "@/lib/actions/api-keys";
import { columns } from "./columns";

export default async function APIKeysPage() {
  const apiKeys = await listAPIKeys();

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">API Keys</h1>
          <p className="text-sm text-muted-foreground">
            Manage API keys for server-to-server access to the Hermes API.
          </p>
        </div>
        <CreateAPIKeyDialog />
      </div>

      <DataTable
        columns={columns}
        data={apiKeys ?? []}
        searchKey="name"
        searchPlaceholder="Search API keys..."
      />
    </div>
  );
}
