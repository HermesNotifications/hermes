// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { Mail, MessageSquare, Inbox } from "lucide-react";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";

const channelConfig: Record<string, { label: string; icon: typeof Mail }> = {
  email: { label: "Email", icon: Mail },
  sms: { label: "SMS", icon: MessageSquare },
  inbox: { label: "Inbox", icon: Inbox },
};

export function ChannelBadge({ channel }: { channel: string }) {
  const config = channelConfig[channel];
  if (!config) {
    return <span className="text-xs text-muted-foreground">{channel}</span>;
  }
  const Icon = config.icon;
  return (
    <Tooltip>
      <TooltipTrigger>
        <Icon className="size-4 text-muted-foreground" />
      </TooltipTrigger>
      <TooltipContent>{config.label}</TooltipContent>
    </Tooltip>
  );
}

export function ChannelBadges({ channels }: { channels: string[] }) {
  return (
    <div className="flex gap-1.5">
      {channels.map((ch) => (
        <ChannelBadge key={ch} channel={ch} />
      ))}
    </div>
  );
}
