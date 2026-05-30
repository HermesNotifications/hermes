// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { InboxAPI } from "./api/inbox.js";
import { UserAPI } from "./api/user.js";
import { RealtimeConnection, type EventHandler } from "./realtime/connection.js";
import type {
  HermesClientConfig,
  HermesEvent,
  Notification,
  InboxUpdatedEvent,
  NewNotificationEvent,
} from "./types.js";

type NotificationHandler = (event: NewNotificationEvent) => void;
type UpdateHandler = (event: InboxUpdatedEvent) => void;
type UnreadCountHandler = (count: number) => void;

export class HermesClient {
  readonly inbox: InboxAPI;
  readonly user: UserAPI;

  private realtime: RealtimeConnection;
  private token: string;
  private getTokenFn?: () => Promise<string>;

  private notificationHandlers: NotificationHandler[] = [];
  private updateHandlers: UpdateHandler[] = [];
  private unreadCountHandlers: UnreadCountHandler[] = [];

  constructor(config: HermesClientConfig) {
    this.token = config.token;
    this.getTokenFn = config.getToken;

    const tokenGetter = () => this.token;
    this.inbox = new InboxAPI(config.apiUrl, tokenGetter);
    this.user = new UserAPI(config.apiUrl, tokenGetter);

    const socketUrl = config.socketUrl ?? config.apiUrl;
    this.realtime = new RealtimeConnection(socketUrl, async () => {
      if (this.getTokenFn) {
        this.token = await this.getTokenFn();
      }
      return this.token;
    });

    this.realtime.on((event: HermesEvent) => this.handleEvent(event));
  }

  onNotification(handler: NotificationHandler): () => void {
    this.notificationHandlers.push(handler);
    return () => {
      this.notificationHandlers = this.notificationHandlers.filter(
        (h) => h !== handler
      );
    };
  }

  onUpdate(handler: UpdateHandler): () => void {
    this.updateHandlers.push(handler);
    return () => {
      this.updateHandlers = this.updateHandlers.filter((h) => h !== handler);
    };
  }

  onUnreadCountChange(handler: UnreadCountHandler): () => void {
    this.unreadCountHandlers.push(handler);
    return () => {
      this.unreadCountHandlers = this.unreadCountHandlers.filter(
        (h) => h !== handler
      );
    };
  }

  on(event: "notification", handler: NotificationHandler): () => void;
  on(event: "update", handler: UpdateHandler): () => void;
  on(event: "unreadCountChange", handler: UnreadCountHandler): () => void;
  on(
    event: "notification" | "update" | "unreadCountChange",
    handler: NotificationHandler | UpdateHandler | UnreadCountHandler
  ): () => void {
    switch (event) {
      case "notification":
        return this.onNotification(handler as NotificationHandler);
      case "update":
        return this.onUpdate(handler as UpdateHandler);
      case "unreadCountChange":
        return this.onUnreadCountChange(handler as UnreadCountHandler);
    }
  }

  async connect(userId: string): Promise<void> {
    await this.realtime.connect(userId);
  }

  disconnect(): void {
    this.realtime.disconnect();
  }

  setToken(token: string): void {
    this.token = token;
  }

  private handleEvent(event: HermesEvent) {
    if (event.type === "notification.new") {
      for (const h of this.notificationHandlers) h(event);
    } else if (event.type === "inbox.updated") {
      for (const h of this.updateHandlers) h(event);
      for (const h of this.unreadCountHandlers) h(event.unreadCount);
    }
  }
}
