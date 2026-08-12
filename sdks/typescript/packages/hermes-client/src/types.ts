// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import type { components as InboxComponents } from "./generated/inbox-api.js";
import type { components as UserComponents } from "./generated/user-api.js";
import type { TransportFactory } from "./realtime/connection.js";

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
  /**
   * The sender's opaque metadata, echoed back verbatim.
   *
   * Typed as the REST schema's `metadata` so a row synthesized from a live arrival is the same
   * shape as the same row after a reload. Hermes reads `level` and `toast` from it; see
   * `notificationLevel` and `toastRequested`.
   */
  metadata?: Notification["metadata"];
  /**
   * Unread count after this arrival, when the server knows it.
   *
   * Absent is normal, not exceptional: the inbox worker that publishes this event has no
   * database, so whenever the cached count has expired it cannot say what the number is, and
   * omits it rather than guessing. The reducer falls back to incrementing locally in that case.
   */
  unreadCount?: number;
}

export type HermesEvent = InboxUpdatedEvent | NewNotificationEvent;

export interface HermesClientConfig {
  /** Origin the inbox and user APIs are served from, e.g. `https://hermes.example.com`. */
  apiUrl: string;
  /**
   * Base URL for the Centrifugo websocket. Defaults to `apiUrl`, which is rarely right in
   * a real deployment — Centrifugo is typically a separate host or a distinct path.
   */
  socketUrl?: string;
  token: string;
  /**
   * Called to obtain a fresh token: on socket reconnect, and once before retrying a REST
   * call that returned 401. Tokens are minted with a multi-hour TTL plus jitter, so any
   * session that outlives one needs this.
   */
  getToken?: () => Promise<string>;
  /** Injected for tests; defaults to the global fetch. */
  fetch?: typeof fetch;
  /** Injected for tests; defaults to a real Centrifuge client. */
  transportFactory?: TransportFactory;
}
