// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import type { components as InboxComponents } from "./generated/inbox-api.js";
import type { components as UserComponents } from "./generated/user-api.js";

export type Notification = InboxComponents["schemas"]["Notification"];
export type User = UserComponents["schemas"]["User"];
export type PreferenceCategory = UserComponents["schemas"]["PreferenceCategory"];
export type PreferenceSubscription = UserComponents["schemas"]["PreferenceSubscription"];

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
