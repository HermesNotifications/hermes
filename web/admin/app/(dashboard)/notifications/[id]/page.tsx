import Link from "next/link";
import { ArrowLeft } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { NotificationDetail } from "@/components/notification-detail";
import { EventTimeline } from "@/components/event-timeline";
import { getNotificationStatus } from "@/lib/actions/notifications";

export default async function NotificationDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  const data = await getNotificationStatus(id);

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        <Button variant="ghost" size="icon" render={<Link href="/notifications" />}>
          <ArrowLeft className="h-4 w-4" />
        </Button>
        <div>
          <h1 className="text-2xl font-semibold">Notification</h1>
          <p className="text-sm text-muted-foreground font-mono">{id}</p>
        </div>
      </div>

      {data ? (
        <div className="space-y-6">
          <NotificationDetail notification={data.notification} />
          <Card>
            <CardHeader>
              <CardTitle>Event Timeline</CardTitle>
            </CardHeader>
            <CardContent>
              <EventTimeline events={data.events} />
            </CardContent>
          </Card>
        </div>
      ) : (
        <p className="text-sm text-muted-foreground">Notification not found.</p>
      )}
    </div>
  );
}
