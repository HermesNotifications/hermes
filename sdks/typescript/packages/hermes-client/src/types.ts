// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import type { components as InboxComponents } from "./generated/inbox-api.js";
import type { components as UserComponents } from "./generated/user-api.js";

export type Notification = InboxComponents["schemas"]["Notification"];
export type User = UserComponents["schemas"]["User"];
export type UserPreference = UserComponents["schemas"]["UserPreference"];

export interface InboxPage {
  data: Notification[];
  unreadCount: number;
  cursor?: string;
}

export interface InboxUpdatedEvent {
  type: "inbox.updated";
  notificationId: string;
  action: "read" | "unread" | "archive" | "unarchive" | "delete" | "read-all";
  unreadCount: number;
  timestamp: number;
}

export interface NewNotificationEvent {
  type: "notification.new";
  id: string;
  title: string;
  body: string;
  createdAt: string;
  actionUrl?: string;
  actionLabel?: string;
}

export type HermesEvent = InboxUpdatedEvent | NewNotificationEvent;

export interface HermesClientConfig {
  apiUrl: string;
  socketUrl?: string;
  token: string;
  getToken?: () => Promise<string>;
}
