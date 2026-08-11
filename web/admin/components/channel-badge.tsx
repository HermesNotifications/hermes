// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

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
