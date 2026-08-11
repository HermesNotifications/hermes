// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { DataTable } from "@/components/data-table";
import { listOrganizations } from "@/lib/actions/organizations";
import { columns } from "./columns";

export default async function OrganizationsPage() {
  const organizations = await listOrganizations();

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">Organizations</h1>
        <p className="text-sm text-muted-foreground">
          View all registered organizations and their user counts.
        </p>
      </div>

      <DataTable
        columns={columns}
        data={organizations ?? []}
        searchKey="name"
        searchPlaceholder="Search organizations..."
      />
    </div>
  );
}
