import type { Notification } from "@hermes-notifications/server";
import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { ChannelBadges } from "@/components/channel-badge";

function statusVariant(status: string): "default" | "secondary" | "outline" | "destructive" {
  switch (status) {
    case "sent":
    case "delivered":
      return "default";
    case "read":
      return "secondary";
    case "failed":
      return "destructive";
    case "archived":
    case "pending":
    default:
      return "outline";
  }
}

function statusClass(status: string): string {
  switch (status) {
    case "pending":
      return "border-yellow-300 bg-yellow-50 text-yellow-800 dark:border-yellow-700 dark:bg-yellow-950 dark:text-yellow-200";
    case "sent":
      return "border-blue-300 bg-blue-50 text-blue-800 dark:border-blue-700 dark:bg-blue-950 dark:text-blue-200";
    case "delivered":
      return "border-green-300 bg-green-50 text-green-800 dark:border-green-700 dark:bg-green-950 dark:text-green-200";
    case "read":
      return "border-purple-300 bg-purple-50 text-purple-800 dark:border-purple-700 dark:bg-purple-950 dark:text-purple-200";
    case "failed":
      return "";
    case "archived":
      return "border-border bg-muted text-muted-foreground";
    default:
      return "";
  }
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="space-y-0.5">
      <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
        {label}
      </p>
      <div className="text-sm">{children}</div>
    </div>
  );
}

function formatTimestamp(ts: string): string {
  return new Date(ts).toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

export function NotificationDetail({ notification }: { notification: Notification }) {
  const timestamps: { label: string; value: string | undefined }[] = [
    { label: "Created", value: notification.created_at },
    { label: "Sent", value: notification.sent_at },
    { label: "Delivered", value: notification.delivered_at },
    { label: "Read", value: notification.read_at },
    { label: "Archived", value: notification.archived_at },
  ];

  return (
    <Card>
      <CardHeader>
        <CardTitle>Notification Details</CardTitle>
      </CardHeader>
      <CardContent className="grid gap-4 sm:grid-cols-2">
        <Field label="ID">
          <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs break-all">
            {notification.id}
          </code>
        </Field>

        <Field label="Status">
          <Badge
            variant={statusVariant(notification.status)}
            className={statusClass(notification.status)}
          >
            {notification.status}
          </Badge>
        </Field>

        <Field label="Channels">
          {notification.channels && notification.channels.length > 0 ? (
            <ChannelBadges channels={notification.channels} />
          ) : (
            <span className="text-muted-foreground">—</span>
          )}
        </Field>

        <Field label="User ID">
          <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">
            {notification.user_id}
          </code>
        </Field>

        <Field label="Tenant ID">
          <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">
            {notification.tenant_id}
          </code>
        </Field>

        {notification.template_id && (
          <Field label="Template ID">
            <code className="rounded bg-muted px-1.5 py-0.5 font-mono text-xs">
              {notification.template_id}
            </code>
          </Field>
        )}

        <Field label="Title">
          <span>{notification.title}</span>
        </Field>

        <Field label="Body">
          <span className="whitespace-pre-wrap">{notification.body}</span>
        </Field>

        <div className="sm:col-span-2 grid gap-3 sm:grid-cols-3">
          {timestamps
            .filter((t) => t.value)
            .map((t) => (
              <Field key={t.label} label={t.label}>
                <span className="text-xs">{formatTimestamp(t.value!)}</span>
              </Field>
            ))}
        </div>
      </CardContent>
    </Card>
  );
}
