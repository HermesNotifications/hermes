import { DataTable } from "@/components/data-table";
import { listTenants } from "@/lib/actions/tenants";
import { columns } from "./columns";

export default async function TenantsPage() {
  const tenants = await listTenants();

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">Tenants</h1>
        <p className="text-sm text-muted-foreground">
          View all registered tenants and their user counts.
        </p>
      </div>

      <DataTable
        columns={columns}
        data={tenants ?? []}
        searchKey="name"
        searchPlaceholder="Search tenants..."
      />
    </div>
  );
}
