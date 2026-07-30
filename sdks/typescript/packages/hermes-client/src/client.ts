// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { InboxAPI } from "./api/inbox.js";
import { UserAPI } from "./api/user.js";
import { subjectFromToken } from "./jwt.js";
import {
  RealtimeConnection,
  type RealtimeStatus,
  type StatusHandler,
  type TransportFactory,
} from "./realtime/connection.js";
import type {
  HermesClientConfig,
  HermesEvent,
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
  private lastUnreadCount = 0;

  private notificationHandlers: NotificationHandler[] = [];
  private updateHandlers: UpdateHandler[] = [];
  private unreadCountHandlers: UnreadCountHandler[] = [];

  constructor(config: HermesClientConfig) {
    this.token = config.token;
    this.getTokenFn = config.getToken;

    const tokenGetter = () => this.token;
    // A 401 on any REST call refreshes the token and retries once. Without this hook the
    // socket would silently re-auth on reconnect while every REST call kept failing.
    const apiOptions = {
      ...(config.fetch ? { fetch: config.fetch } : {}),
      onUnauthorized: async () => {
        await this.refreshToken();
      },
    };
    this.inbox = new InboxAPI(config.apiUrl, tokenGetter, apiOptions);
    this.user = new UserAPI(config.apiUrl, tokenGetter, apiOptions);

    const socketUrl = config.socketUrl ?? config.apiUrl;
    this.realtime = new RealtimeConnection(
      socketUrl,
      async () => {
        await this.refreshToken();
        return this.token;
      },
      config.transportFactory
    );

    this.realtime.on((event: HermesEvent) => this.handleEvent(event));
  }

  /** Whether realtime publications will actually reach us. */
  get realtimeStatus(): RealtimeStatus {
    return this.realtime.status;
  }

  /** The last unread count seen, from either a load or a realtime update. */
  get unreadCount(): number {
    return this.lastUnreadCount;
  }

  private async refreshToken(): Promise<void> {
    if (this.getTokenFn) this.token = await this.getTokenFn();
  }

  onNotification(handler: NotificationHandler): () => void {
    this.notificationHandlers.push(handler);
    return () => {
      this.notificationHandlers = this.notificationHandlers.filter((h) => h !== handler);
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
      this.unreadCountHandlers = this.unreadCountHandlers.filter((h) => h !== handler);
    };
  }

  /** Subscribe to realtime connection-status changes. */
  onStatusChange(handler: StatusHandler): () => void {
    return this.realtime.onStatusChange(handler);
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

  /** How many handlers are registered. Exposed so consumers can assert teardown. */
  handlerCount(): number {
    return (
      this.notificationHandlers.length +
      this.updateHandlers.length +
      this.unreadCountHandlers.length
    );
  }

  /**
   * Connect the realtime socket.
   *
   * `userId` defaults to the `sub` claim of the current token, which is the internal
   * Hermes id that Centrifugo's `user#<sub>` channel requires. Passing the *external* id
   * instead produces a subscription the server rejects while REST keeps working, so the
   * inbox loads and then never updates — defaulting from the token removes that trap.
   */
  async connect(userId?: string): Promise<void> {
    const resolved = userId ?? subjectFromToken(this.token);
    if (!resolved) {
      throw new Error(
        "Hermes: cannot resolve a user id for realtime — pass one to connect(), or supply a token carrying a sub claim"
      );
    }
    await this.realtime.connect(resolved);
  }

  /** Close the socket, keeping handlers so a later connect() still delivers. */
  disconnect(): void {
    this.realtime.disconnect();
  }

  /** Close the socket and drop every handler. */
  dispose(): void {
    this.realtime.dispose();
    this.notificationHandlers = [];
    this.updateHandlers = [];
    this.unreadCountHandlers = [];
  }

  setToken(token: string): void {
    this.token = token;
  }

  /**
   * Publish an unread count that came from somewhere other than a realtime event —
   * chiefly the initial page load and optimistic updates.
   *
   * This is what keeps a standalone badge in step with the widget: one writer, one
   * notification chain. Repeats of the same value are dropped so subscribers do not
   * re-render for nothing.
   */
  setUnreadCount(count: number): void {
    if (this.lastUnreadCount === count) return;
    this.lastUnreadCount = count;
    for (const handler of this.unreadCountHandlers) handler(count);
  }

  private handleEvent(event: HermesEvent) {
    if (event.type === "notification.new") {
      for (const handler of this.notificationHandlers) handler(event);
    } else if (event.type === "inbox.updated") {
      for (const handler of this.updateHandlers) handler(event);
      this.setUnreadCount(event.unreadCount);
    }
  }
}
