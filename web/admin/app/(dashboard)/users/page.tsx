import { DataTable } from "@/components/data-table";
import { listUsers } from "@/lib/actions/users";
import { listTenants } from "@/lib/actions/tenants";
import { columns } from "./columns";
import { TenantFilter } from "./tenant-filter";

export default async function UsersPage({
  searchParams,
}: {
  searchParams: Promise<{ tenant_id?: string }>;
}) {
  const { tenant_id } = await searchParams;
  const [users, tenants] = await Promise.all([
    listUsers(tenant_id),
    listTenants(),
  ]);

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Users</h1>
          <p className="text-sm text-muted-foreground">
            View all registered users across tenants.
          </p>
        </div>
        <TenantFilter tenants={tenants ?? []} currentTenantId={tenant_id} />
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
