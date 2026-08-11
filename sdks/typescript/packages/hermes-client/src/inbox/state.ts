// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import type { HermesError } from "../errors.js";
import type { RealtimeStatus } from "../realtime/connection.js";
import type { InboxPage, InboxUpdatedEvent, NewNotificationEvent, Notification } from "../types.js";

/**
 * Everything an inbox UI renders.
 *
 * This is the one representation of inbox state in the SDK. The custom element and the
 * React hooks both drive the reducer below rather than keeping their own copies, which
 * is what stops them drifting apart.
 */
export interface InboxState {
  notifications: Notification[];
  unreadCount: number;
  /** Opaque forward cursor for the next page, absent on the last page. */
  cursor?: string;
  hasMore: boolean;
  /** The first page is in flight. */
  loading: boolean;
  /** A subsequent page is in flight. */
  loadingMore: boolean;
  /**
   * Whether realtime publications are actually reaching us. Exposed in state so a UI can
   * show a live indicator, and so anything waiting for the inbox to be live has something
   * to gate on other than a timer.
   */
  realtime: RealtimeStatus;
  error?: HermesError;
}

export const initialInboxState: InboxState = {
  notifications: [],
  unreadCount: 0,
  cursor: undefined,
  hasMore: false,
  loading: false,
  loadingMore: false,
  realtime: "disconnected",
  error: undefined,
};

/**
 * Every transition the inbox can make.
 *
 * Actions that stamp a timestamp carry `at` rather than letting the reducer read the
 * clock. That keeps the reducer pure — and it means a test can assert the exact stamp
 * instead of merely asserting that some string was written.
 */
export type InboxAction =
  | { type: "load/start" }
  | { type: "load/success"; page: InboxPage }
  | { type: "load/failure"; error: HermesError }
  | { type: "page/start" }
  | { type: "page/success"; page: InboxPage }
  | { type: "page/failure"; error: HermesError }
  | { type: "realtime/notification"; event: NewNotificationEvent }
  | { type: "realtime/update"; event: InboxUpdatedEvent; at: string }
  | { type: "optimistic/read"; id: string; at: string }
  | { type: "optimistic/unread"; id: string }
  | { type: "optimistic/archive"; id: string }
  | { type: "optimistic/remove"; id: string }
  | { type: "optimistic/readAll"; at: string }
  | { type: "rollback"; state: InboxState; error: HermesError }
  | { type: "unread/set"; count: number }
  | { type: "realtime/status"; status: RealtimeStatus }
  | { type: "error/clear" };

/**
 * Build a renderable row from a realtime `notification.new` payload.
 *
 * The payload is thinner than the REST schema: it carries no `organization_id`,
 * `user_id` or `category_id`, so those are empty strings here. That is a known gap
 * rather than a value worth inventing — nothing in the widget reads them, and the
 * server should grow the payload rather than the client guessing. Everything the UI
 * does render, including the action url and label, comes through.
 */
export function notificationFromEvent(event: NewNotificationEvent): Notification {
  return {
    id: event.id,
    organization_id: "",
    user_id: "",
    category_id: "",
    title: event.title,
    body: event.body,
    status: "delivered",
    channels: ["inbox"],
    created_at: event.createdAt,
    ...(event.actionUrl !== undefined ? { action_url: event.actionUrl } : {}),
    ...(event.actionLabel !== undefined ? { action_label: event.actionLabel } : {}),
  };
}

/** Append rows that are not already present, preserving order. */
function appendNew(existing: Notification[], incoming: Notification[]): Notification[] {
  const seen = new Set(existing.map((n) => n.id));
  const fresh = incoming.filter((n) => !seen.has(n.id));
  return fresh.length === 0 ? existing : [...existing, ...fresh];
}

/** Drop the `read_at` key entirely rather than setting it to undefined. */
function withoutReadAt(n: Notification): Notification {
  const { read_at: _read_at, ...rest } = n;
  return rest;
}

function isUnread(n: Notification): boolean {
  return !n.read_at;
}

function decrement(count: number): number {
  return Math.max(0, count - 1);
}

export function inboxReducer(state: InboxState, action: InboxAction): InboxState {
  switch (action.type) {
    case "load/start":
      // Clearing loadingMore is what keeps pagination alive across a refresh. A page
      // request that is in flight when the list reloads has its response dropped on the
      // generation check, before it can dispatch page/success or page/failure — so if the
      // flag were left set, nothing would ever clear it and every later loadMore() would
      // return at its `loadingMore` guard with the footer stuck on "Loading…".
      if (state.loading && !state.loadingMore) return state;
      return { ...state, loading: true, loadingMore: false, error: undefined };

    case "load/success":
      return {
        ...state,
        notifications: action.page.data,
        unreadCount: action.page.unreadCount,
        cursor: action.page.cursor,
        hasMore: action.page.cursor !== undefined,
        loading: false,
        error: undefined,
      };

    case "load/failure":
      return { ...state, loading: false, error: action.error };

    case "page/start":
      return state.loadingMore ? state : { ...state, loadingMore: true, error: undefined };

    case "page/success":
      return {
        ...state,
        notifications: appendNew(state.notifications, action.page.data),
        cursor: action.page.cursor,
        hasMore: action.page.cursor !== undefined,
        loadingMore: false,
        error: undefined,
      };

    case "page/failure":
      return { ...state, loadingMore: false, error: action.error };

    case "realtime/notification": {
      const incoming = notificationFromEvent(action.event);
      const index = state.notifications.findIndex((n) => n.id === incoming.id);
      if (index >= 0) {
        // Already on screen — a redelivery, or an arrival that raced the initial
        // list(). Replace in place and leave the count alone; incrementing here is
        // how a badge ends up ahead of the server.
        const notifications = [...state.notifications];
        notifications[index] = incoming;
        return { ...state, notifications };
      }
      return {
        ...state,
        notifications: [incoming, ...state.notifications],
        unreadCount: state.unreadCount + 1,
      };
    }

    case "realtime/update": {
      const { event, at } = action;
      // The server counts unread from the database. It always wins.
      const base = { ...state, unreadCount: event.unreadCount };

      switch (event.action) {
        case "read":
          return {
            ...base,
            notifications: state.notifications.map((n) =>
              n.id === event.notificationId && isUnread(n) ? { ...n, read_at: at } : n
            ),
          };
        case "unread":
          return {
            ...base,
            notifications: state.notifications.map((n) =>
              n.id === event.notificationId ? withoutReadAt(n) : n
            ),
          };
        case "read-all":
          return {
            ...base,
            notifications: state.notifications.map((n) =>
              isUnread(n) ? { ...n, read_at: at } : n
            ),
          };
        case "archive":
        case "delete":
          return {
            ...base,
            notifications: state.notifications.filter((n) => n.id !== event.notificationId),
          };
        case "unarchive":
          // The row belongs in the archived view, which this state does not model.
          return base;
      }
      return base;
    }

    case "optimistic/read": {
      const target = state.notifications.find((n) => n.id === action.id);
      if (!target || !isUnread(target)) return state;
      return {
        ...state,
        notifications: state.notifications.map((n) =>
          n.id === action.id ? { ...n, read_at: action.at } : n
        ),
        unreadCount: decrement(state.unreadCount),
      };
    }

    case "optimistic/unread": {
      const target = state.notifications.find((n) => n.id === action.id);
      if (!target || isUnread(target)) return state;
      return {
        ...state,
        notifications: state.notifications.map((n) =>
          n.id === action.id ? withoutReadAt(n) : n
        ),
        unreadCount: state.unreadCount + 1,
      };
    }

    case "optimistic/archive":
    case "optimistic/remove": {
      const target = state.notifications.find((n) => n.id === action.id);
      if (!target) return state;
      return {
        ...state,
        notifications: state.notifications.filter((n) => n.id !== action.id),
        unreadCount: isUnread(target) ? decrement(state.unreadCount) : state.unreadCount,
      };
    }

    case "optimistic/readAll": {
      if (state.notifications.every((n) => !isUnread(n)) && state.unreadCount === 0) {
        return state;
      }
      return {
        ...state,
        notifications: state.notifications.map((n) =>
          isUnread(n) ? { ...n, read_at: action.at } : n
        ),
        unreadCount: 0,
      };
    }

    case "rollback":
      return { ...action.state, error: action.error };

    case "unread/set":
      return state.unreadCount === action.count
        ? state
        : { ...state, unreadCount: action.count };

    case "realtime/status":
      return state.realtime === action.status
        ? state
        : { ...state, realtime: action.status };

    case "error/clear":
      return state.error === undefined ? state : { ...state, error: undefined };

    default:
      return state;
  }
}
