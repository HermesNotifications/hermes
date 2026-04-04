import { DataTable } from "@/components/data-table";
import { listRecentNotifications } from "@/lib/actions/notifications";
import { columns } from "./columns";
import { NotificationLookup } from "./lookup";

export default async function NotificationsPage() {
  const notifications = await listRecentNotifications();

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-semibold">Notifications</h1>
        <p className="text-sm text-muted-foreground">
          Recent notifications and ID lookup.
        </p>
      </div>

      <NotificationLookup />

      <DataTable
        columns={columns}
        data={notifications ?? []}
        searchKey="title"
        searchPlaceholder="Search by title..."
      />
    </div>
  );
}
