import { Badge } from "@/components/ui/badge";

const channelConfig: Record<string, { label: string; variant: "default" | "secondary" | "outline" }> = {
  email: { label: "Email", variant: "default" },
  sms: { label: "SMS", variant: "secondary" },
  inbox: { label: "Inbox", variant: "outline" },
};

export function ChannelBadge({ channel }: { channel: string }) {
  const config = channelConfig[channel] ?? { label: channel, variant: "outline" as const };
  return <Badge variant={config.variant}>{config.label}</Badge>;
}

export function ChannelBadges({ channels }: { channels: string[] }) {
  return (
    <div className="flex gap-1">
      {channels.map((ch) => (
        <ChannelBadge key={ch} channel={ch} />
      ))}
    </div>
  );
}
