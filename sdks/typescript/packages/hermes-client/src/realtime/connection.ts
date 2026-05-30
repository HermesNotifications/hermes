// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { Centrifuge, type PublicationContext } from "centrifuge";
import type { InboxUpdatedEvent, NewNotificationEvent, HermesEvent } from "../types.js";

export type EventHandler = (event: HermesEvent) => void;

export class RealtimeConnection {
  private centrifuge: Centrifuge | null = null;
  private handlers: EventHandler[] = [];
  private socketUrl: string;
  private getToken: () => string | Promise<string>;

  constructor(socketUrl: string, getToken: () => string | Promise<string>) {
    this.socketUrl = socketUrl;
    this.getToken = getToken;
  }

  on(handler: EventHandler): () => void {
    this.handlers.push(handler);
    return () => {
      this.handlers = this.handlers.filter((h) => h !== handler);
    };
  }

  private emit(event: HermesEvent) {
    for (const handler of this.handlers) {
      handler(event);
    }
  }

  async connect(userId: string): Promise<void> {
    if (this.centrifuge) return;

    const wsUrl = this.socketUrl
      .replace(/^http:/, "ws:")
      .replace(/^https:/, "wss:");
    const endpoint = wsUrl.endsWith("/connection/websocket")
      ? wsUrl
      : `${wsUrl}/connection/websocket`;

    const token = await this.getToken();
    this.centrifuge = new Centrifuge(endpoint, { token });

    this.centrifuge.on("error", (ctx) => {
      console.error("Hermes realtime error:", ctx);
    });

    const sub = this.centrifuge.newSubscription(`user#${userId}`);
    sub.on("publication", (ctx: PublicationContext) => {
      this.handlePublication(ctx);
    });

    sub.subscribe();
    this.centrifuge.connect();
  }

  disconnect(): void {
    if (this.centrifuge) {
      this.centrifuge.disconnect();
      this.centrifuge = null;
    }
  }

  private handlePublication(ctx: PublicationContext) {
    const data = ctx.data as Record<string, unknown>;
    const type = data.type as string;

    if (type === "inbox.updated") {
      const event: InboxUpdatedEvent = {
        type: "inbox.updated",
        notificationId: data.notification_id as string,
        action: data.action as InboxUpdatedEvent["action"],
        unreadCount: data.unread_count as number,
        timestamp: data.timestamp as number,
      };
      this.emit(event);
    } else {
      // "notification.new" or legacy events
      const event: NewNotificationEvent = {
        type: "notification.new",
        id: data.id as string,
        title: data.title as string,
        body: data.body as string,
        createdAt: (data.created_at as string) ?? new Date().toISOString(),
        actionUrl: (data.action as Record<string, string>)?.url,
        actionLabel: (data.action as Record<string, string>)?.label,
      };
      this.emit(event);
    }
  }
}
