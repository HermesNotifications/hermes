// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { DataTable } from "@/components/data-table";
import { listUsers } from "@/lib/actions/users";
import { listOrganizations } from "@/lib/actions/organizations";
import { columns } from "./columns";
import { OrganizationFilter } from "./organization-filter";

export default async function UsersPage({
  searchParams,
}: {
  searchParams: Promise<{ organization_id?: string }>;
}) {
  const { organization_id } = await searchParams;
  const [users, organizations] = await Promise.all([
    listUsers(organization_id),
    listOrganizations(),
  ]);

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Users</h1>
          <p className="text-sm text-muted-foreground">
            View all registered users across organizations.
          </p>
        </div>
        <OrganizationFilter organizations={organizations ?? []} currentOrganizationId={organization_id} />
      </div>

      <DataTable
        columns={columns}
        data={users ?? []}
        searchKey="external_id"
        searchPlaceholder="Search by external ID..."
      />
    </div>
  );
}
