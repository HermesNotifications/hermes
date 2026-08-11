// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { describe, expect, it } from "vitest";
import { HermesError } from "../errors.js";
import type { InboxPage, NewNotificationEvent, Notification } from "../types.js";
import {
  initialInboxState,
  inboxReducer,
  notificationFromEvent,
  type InboxAction,
  type InboxState,
} from "./state.js";

/**
 * This reducer is the single implementation of inbox state. Before it existed, the
 * custom element and the React hooks each carried their own copy of realtime-event
 * synthesis and optimistic patching, and the two had already drifted (one floored the
 * unread count at zero, the other did not). That duplication is what shipped a build
 * break when `group_id` was renamed to `category_id`.
 *
 * Two properties make this file worth more than the code it covers:
 *
 * 1. It is pure — no clock, no I/O. Every action that stamps a timestamp carries `at`,
 *    so assertions are exact values rather than `expect(read_at).toBeTypeOf("string")`.
 *    A test that only checks the type cannot tell a correct stamp from any other.
 * 2. Fixtures are typed against the generated `Notification`, so a schema rename breaks
 *    compilation here instead of leaving a green suite pinned to a field nobody has.
 */

const AT = "2026-07-29T10:00:00.000Z";
const EARLIER = "2026-07-01T00:00:00.000Z";

function notification(id: string, overrides: Partial<Notification> = {}): Notification {
  return {
    id,
    organization_id: "org_1",
    user_id: "usr_1",
    category_id: "sct_default_general",
    title: `Title ${id}`,
    body: `Body ${id}`,
    status: "delivered",
    channels: ["inbox"],
    created_at: "2026-07-29T09:00:00.000Z",
    ...overrides,
  };
}

function page(overrides: Partial<InboxPage> = {}): InboxPage {
  return { data: [], unreadCount: 0, ...overrides };
}

function event(overrides: Partial<NewNotificationEvent> = {}): NewNotificationEvent {
  return {
    type: "notification.new",
    id: "ntf_new",
    title: "You have a new invoice",
    body: "Invoice #1234 is ready",
    createdAt: "2026-07-29T09:59:00.000Z",
    ...overrides,
  };
}

/** A state with two unread rows loaded, which most action tests start from. */
function loaded(overrides: Partial<InboxState> = {}): InboxState {
  return {
    ...initialInboxState,
    notifications: [notification("ntf_2"), notification("ntf_1")],
    unreadCount: 2,
    ...overrides,
  };
}

describe("inboxReducer: loading the first page", () => {
  it("marks loading on load/start without discarding what is already shown", () => {
    const next = inboxReducer(loaded(), { type: "load/start" });
    expect(next.loading).toBe(true);
    expect(next.notifications).toHaveLength(2);
  });

  it("clears loadingMore on load/start, since a reload supersedes any page in flight", () => {
    const next = inboxReducer({ ...loaded(), loadingMore: true }, { type: "load/start" });
    expect(next.loadingMore).toBe(false);
    expect(next.loading).toBe(true);
  });

  it("still short-circuits load/start when already loading and no page is in flight", () => {
    const state = { ...loaded(), loading: true, loadingMore: false };
    expect(inboxReducer(state, { type: "load/start" })).toBe(state);
  });

  it("publishes the page, the unread count and the cursor", () => {
    const next = inboxReducer(
      { ...initialInboxState, loading: true },
      {
        type: "load/success",
        page: page({ data: [notification("a")], unreadCount: 7, cursor: "c1" }),
      }
    );
    expect(next.notifications.map((n) => n.id)).toEqual(["a"]);
    expect(next.unreadCount).toBe(7);
    expect(next.cursor).toBe("c1");
    expect(next.loading).toBe(false);
  });

  it("replaces the previous list rather than appending to it", () => {
    const next = inboxReducer(loaded(), {
      type: "load/success",
      page: page({ data: [notification("fresh")], unreadCount: 1 }),
    });
    expect(next.notifications.map((n) => n.id)).toEqual(["fresh"]);
  });

  it("clears a previous error on a successful load", () => {
    const start = loaded({ error: HermesError.fromStatus("Inbox", 500) });
    expect(inboxReducer(start, { type: "load/success", page: page() }).error).toBeUndefined();
  });

  it("records the error and stops loading on load/failure, keeping the list visible", () => {
    const error = HermesError.fromStatus("Inbox", 500);
    const next = inboxReducer({ ...loaded(), loading: true }, { type: "load/failure", error });
    expect(next.error).toBe(error);
    expect(next.loading).toBe(false);
    expect(next.notifications).toHaveLength(2);
  });
});

describe("inboxReducer: hasMore", () => {
  // InboxAPI.list normalises the API's `cursor: ""` last-page sentinel to `undefined`,
  // so `hasMore` keys off undefined. A fixture that hand-writes `cursor: undefined`
  // would pass even if the normalisation were removed, so these go through the shape
  // the API actually returns.
  it("is false when the server reports no further pages", () => {
    const next = inboxReducer(initialInboxState, {
      type: "load/success",
      page: page({ data: [notification("a")], cursor: undefined }),
    });
    expect(next.hasMore).toBe(false);
  });

  it("is true when the server returns a cursor", () => {
    const next = inboxReducer(initialInboxState, {
      type: "load/success",
      page: page({ data: [notification("a")], cursor: "c1" }),
    });
    expect(next.hasMore).toBe(true);
  });

  it("becomes false once a later page comes back without a cursor", () => {
    const withMore = inboxReducer(initialInboxState, {
      type: "load/success",
      page: page({ data: [notification("a")], cursor: "c1" }),
    });
    const next = inboxReducer(withMore, {
      type: "page/success",
      page: page({ data: [notification("b")] }),
    });
    expect(next.hasMore).toBe(false);
    expect(next.cursor).toBeUndefined();
  });
});

describe("inboxReducer: paginating", () => {
  it("appends the next page instead of replacing the list", () => {
    const start = loaded({ cursor: "c1", hasMore: true });
    const next = inboxReducer(start, {
      type: "page/success",
      page: page({ data: [notification("ntf_0")], cursor: "c2" }),
    });
    expect(next.notifications.map((n) => n.id)).toEqual(["ntf_2", "ntf_1", "ntf_0"]);
    expect(next.cursor).toBe("c2");
    expect(next.loadingMore).toBe(false);
  });

  it("does not duplicate a row already present when a page overlaps", () => {
    // A realtime arrival can shift the server-side window, so a cursor page can return
    // something already on screen. Rendering it twice is the classic cursor regression.
    const start = loaded({ cursor: "c1", hasMore: true });
    const next = inboxReducer(start, {
      type: "page/success",
      page: page({ data: [notification("ntf_1"), notification("ntf_0")] }),
    });
    expect(next.notifications.map((n) => n.id)).toEqual(["ntf_2", "ntf_1", "ntf_0"]);
  });

  it("sets loadingMore on page/start without disturbing the first-page loading flag", () => {
    const next = inboxReducer(loaded(), { type: "page/start" });
    expect(next.loadingMore).toBe(true);
    expect(next.loading).toBe(false);
  });

  it("records the error and stops loadingMore on page/failure", () => {
    const error = HermesError.fromStatus("Inbox", 400, { detail: "invalid cursor" });
    const next = inboxReducer({ ...loaded(), loadingMore: true }, { type: "page/failure", error });
    expect(next.error).toBe(error);
    expect(next.loadingMore).toBe(false);
    expect(next.notifications).toHaveLength(2);
  });
});

describe("inboxReducer: realtime arrivals", () => {
  it("prepends the new notification so the newest is first", () => {
    const next = inboxReducer(loaded(), {
      type: "realtime/notification",
      event: event({ id: "ntf_3" }),
    });
    expect(next.notifications.map((n) => n.id)).toEqual(["ntf_3", "ntf_2", "ntf_1"]);
  });

  it("increments the unread count", () => {
    expect(
      inboxReducer(loaded(), { type: "realtime/notification", event: event() }).unreadCount
    ).toBe(3);
  });

  it("carries the action url and label through onto the row", () => {
    // These live on the wire payload and on the Notification schema, but the two old
    // hand-written synthesizers both dropped them, so the widget could never render a
    // call to action for a notification that arrived live.
    const next = inboxReducer(initialInboxState, {
      type: "realtime/notification",
      event: event({ actionUrl: "https://example.com/invoices/1234", actionLabel: "View invoice" }),
    });
    expect(next.notifications[0].action_url).toBe("https://example.com/invoices/1234");
    expect(next.notifications[0].action_label).toBe("View invoice");
  });

  it("replaces a row in place and does not double-count when the id is already present", () => {
    // Centrifugo can redeliver, and an arrival can race the initial list() request.
    // Either way the same id must not appear twice or inflate the badge.
    const start = loaded();
    const next = inboxReducer(start, {
      type: "realtime/notification",
      event: event({ id: "ntf_2", title: "Updated title" }),
    });
    expect(next.notifications).toHaveLength(2);
    expect(next.notifications.filter((n) => n.id === "ntf_2")).toHaveLength(1);
    expect(next.notifications.find((n) => n.id === "ntf_2")?.title).toBe("Updated title");
    expect(next.unreadCount).toBe(2);
  });
});

describe("inboxReducer: realtime inbox.updated", () => {
  it("takes the server's unread count as authoritative", () => {
    // The server counts from the database; the client must never argue with it.
    const next = inboxReducer(loaded(), {
      type: "realtime/update",
      event: {
        type: "inbox.updated",
        notificationId: "ntf_1",
        action: "read",
        unreadCount: 7,
        timestamp: 1,
      },
      at: AT,
    });
    expect(next.unreadCount).toBe(7);
  });

  it.each([
    { action: "archive" as const, remaining: ["ntf_1"] },
    { action: "delete" as const, remaining: ["ntf_1"] },
  ])("removes the row from the active list on $action", ({ action, remaining }) => {
    const next = inboxReducer(loaded(), {
      type: "realtime/update",
      event: {
        type: "inbox.updated",
        notificationId: "ntf_2",
        action,
        unreadCount: 1,
        timestamp: 1,
      },
      at: AT,
    });
    expect(next.notifications.map((n) => n.id)).toEqual(remaining);
  });

  it("stamps only the named row as read", () => {
    const next = inboxReducer(loaded(), {
      type: "realtime/update",
      event: {
        type: "inbox.updated",
        notificationId: "ntf_2",
        action: "read",
        unreadCount: 1,
        timestamp: 1,
      },
      at: AT,
    });
    expect(next.notifications.find((n) => n.id === "ntf_2")?.read_at).toBe(AT);
    expect(next.notifications.find((n) => n.id === "ntf_1")?.read_at).toBeUndefined();
  });

  it("clears the stamp on unread", () => {
    const start = loaded({
      notifications: [notification("ntf_2", { read_at: EARLIER }), notification("ntf_1")],
    });
    const next = inboxReducer(start, {
      type: "realtime/update",
      event: {
        type: "inbox.updated",
        notificationId: "ntf_2",
        action: "unread",
        unreadCount: 2,
        timestamp: 1,
      },
      at: AT,
    });
    expect(next.notifications.find((n) => n.id === "ntf_2")?.read_at).toBeUndefined();
  });

  it("stamps every unread row on read-all and preserves an existing stamp", () => {
    // read-all arrives with notification_id "" and unread_count 0. Clobbering an
    // existing read_at would be shorter and would read fine, so this asserts the
    // preserved value explicitly.
    const start = loaded({
      notifications: [notification("ntf_2", { read_at: EARLIER }), notification("ntf_1")],
    });
    const next = inboxReducer(start, {
      type: "realtime/update",
      event: {
        type: "inbox.updated",
        notificationId: "",
        action: "read-all",
        unreadCount: 0,
        timestamp: 1,
      },
      at: AT,
    });
    expect(next.notifications.find((n) => n.id === "ntf_2")?.read_at).toBe(EARLIER);
    expect(next.notifications.find((n) => n.id === "ntf_1")?.read_at).toBe(AT);
    expect(next.unreadCount).toBe(0);
  });

  it("leaves the active list alone on unarchive", () => {
    const next = inboxReducer(loaded(), {
      type: "realtime/update",
      event: {
        type: "inbox.updated",
        notificationId: "ntf_9",
        action: "unarchive",
        unreadCount: 3,
        timestamp: 1,
      },
      at: AT,
    });
    expect(next.notifications.map((n) => n.id)).toEqual(["ntf_2", "ntf_1"]);
    expect(next.unreadCount).toBe(3);
  });
});

describe("inboxReducer: optimistic actions", () => {
  it("stamps read_at with exactly the supplied timestamp", () => {
    // The exact-value assertion is the whole reason the clock is a parameter. With
    // `new Date()` inside the reducer the strongest available assertion would be
    // toBeTypeOf("string"), which passes for any stamp at all.
    const next = inboxReducer(loaded(), { type: "optimistic/read", id: "ntf_2", at: AT });
    expect(next.notifications.find((n) => n.id === "ntf_2")?.read_at).toBe(AT);
  });

  it("decrements the unread count and leaves sibling rows untouched", () => {
    const next = inboxReducer(loaded(), { type: "optimistic/read", id: "ntf_2", at: AT });
    expect(next.unreadCount).toBe(1);
    expect(next.notifications.find((n) => n.id === "ntf_1")?.read_at).toBeUndefined();
  });

  it("floors the unread count at zero", () => {
    const start = loaded({ unreadCount: 0 });
    expect(inboxReducer(start, { type: "optimistic/read", id: "ntf_2", at: AT }).unreadCount).toBe(0);
  });

  it("does not decrement twice when a row is already read", () => {
    const start = loaded({
      notifications: [notification("ntf_2", { read_at: EARLIER }), notification("ntf_1")],
      unreadCount: 1,
    });
    const next = inboxReducer(start, { type: "optimistic/read", id: "ntf_2", at: AT });
    expect(next.unreadCount).toBe(1);
    expect(next.notifications.find((n) => n.id === "ntf_2")?.read_at).toBe(EARLIER);
  });

  it("clears the stamp and increments the count on unread", () => {
    const start = loaded({
      notifications: [notification("ntf_2", { read_at: EARLIER }), notification("ntf_1")],
      unreadCount: 1,
    });
    const next = inboxReducer(start, { type: "optimistic/unread", id: "ntf_2" });
    expect(next.notifications.find((n) => n.id === "ntf_2")?.read_at).toBeUndefined();
    expect(next.unreadCount).toBe(2);
  });

  it("removes the row and drops the unread count on archive", () => {
    const next = inboxReducer(loaded(), { type: "optimistic/archive", id: "ntf_2" });
    expect(next.notifications.map((n) => n.id)).toEqual(["ntf_1"]);
    expect(next.unreadCount).toBe(1);
  });

  it("does not drop the unread count when archiving an already-read row", () => {
    const start = loaded({
      notifications: [notification("ntf_2", { read_at: EARLIER }), notification("ntf_1")],
      unreadCount: 1,
    });
    expect(inboxReducer(start, { type: "optimistic/archive", id: "ntf_2" }).unreadCount).toBe(1);
  });

  it("stamps every unread row and zeroes the count on readAll", () => {
    const start = loaded({
      notifications: [notification("ntf_2", { read_at: EARLIER }), notification("ntf_1")],
    });
    const next = inboxReducer(start, { type: "optimistic/readAll", at: AT });
    expect(next.notifications.find((n) => n.id === "ntf_2")?.read_at).toBe(EARLIER);
    expect(next.notifications.find((n) => n.id === "ntf_1")?.read_at).toBe(AT);
    expect(next.unreadCount).toBe(0);
  });

  it("removes the row on remove", () => {
    const next = inboxReducer(loaded(), { type: "optimistic/remove", id: "ntf_2" });
    expect(next.notifications.map((n) => n.id)).toEqual(["ntf_1"]);
  });
});

describe("inboxReducer: snapshot discipline", () => {
  it("restores a prior snapshot verbatim on rollback", () => {
    // The store uses this to undo an optimistic patch when the server rejects it.
    const snapshot = loaded();
    const dirty = inboxReducer(snapshot, { type: "optimistic/read", id: "ntf_2", at: AT });
    const error = HermesError.fromStatus("Inbox", 500);
    const rolled = inboxReducer(dirty, { type: "rollback", state: snapshot, error });
    expect(rolled.notifications).toEqual(snapshot.notifications);
    expect(rolled.unreadCount).toBe(snapshot.unreadCount);
    expect(rolled.error).toBe(error);
  });

  it("clears the error on error/clear", () => {
    const start = loaded({ error: HermesError.fromStatus("Inbox", 500) });
    expect(inboxReducer(start, { type: "error/clear" }).error).toBeUndefined();
  });

  it("takes the server count on setUnreadCount", () => {
    expect(inboxReducer(loaded(), { type: "unread/set", count: 42 }).unreadCount).toBe(42);
  });
});

describe("inboxReducer: realtime status", () => {
  it("starts disconnected", () => {
    expect(initialInboxState.realtime).toBe("disconnected");
  });

  it("records a status transition", () => {
    const next = inboxReducer(loaded(), { type: "realtime/status", status: "connected" });
    expect(next.realtime).toBe("connected");
  });

  it("returns the identical state for an unchanged status", () => {
    const start = loaded();
    expect(inboxReducer(start, { type: "realtime/status", status: "disconnected" })).toBe(start);
  });

  it("leaves the notification list untouched", () => {
    const start = loaded();
    const next = inboxReducer(start, { type: "realtime/status", status: "connected" });
    expect(next.notifications).toBe(start.notifications);
  });
});

describe("inboxReducer: purity", () => {
  it("never mutates the state it is given", () => {
    const start = loaded();
    const before = JSON.stringify(start);
    const actions: InboxAction[] = [
      { type: "load/start" },
      { type: "load/success", page: page({ data: [notification("x")], unreadCount: 1 }) },
      { type: "page/success", page: page({ data: [notification("y")] }) },
      { type: "realtime/notification", event: event() },
      { type: "optimistic/read", id: "ntf_2", at: AT },
      { type: "optimistic/readAll", at: AT },
      { type: "optimistic/archive", id: "ntf_2" },
      { type: "error/clear" },
    ];
    for (const action of actions) inboxReducer(start, action);
    expect(JSON.stringify(start)).toBe(before);
  });

  it("does not reuse the notifications array identity when rows change", () => {
    const start = loaded();
    const next = inboxReducer(start, { type: "optimistic/read", id: "ntf_2", at: AT });
    expect(next.notifications).not.toBe(start.notifications);
  });

  // useSyncExternalStore re-renders whenever getSnapshot() returns a new reference, so
  // an action that changes nothing must return the same object or every no-op event
  // from the socket causes a render.
  it("returns the identical state object when an action changes nothing", () => {
    const start = loaded();
    expect(inboxReducer(start, { type: "optimistic/read", id: "missing", at: AT })).toBe(start);
    expect(inboxReducer(start, { type: "optimistic/archive", id: "missing" })).toBe(start);
    expect(inboxReducer(start, { type: "error/clear" })).toBe(start);
    expect(inboxReducer(start, { type: "unread/set", count: start.unreadCount })).toBe(start);
  });

  it("returns the identical state object for an unrecognised action", () => {
    const start = loaded();
    expect(inboxReducer(start, { type: "nope" } as unknown as InboxAction)).toBe(start);
  });
});

describe("notificationFromEvent", () => {
  it("builds a row the list can render from a realtime payload", () => {
    const built = notificationFromEvent(event({ id: "ntf_7" }));
    expect(built).toMatchObject({
      id: "ntf_7",
      title: "You have a new invoice",
      body: "Invoice #1234 is ready",
      status: "delivered",
      channels: ["inbox"],
      created_at: "2026-07-29T09:59:00.000Z",
    });
    expect(built.read_at).toBeUndefined();
  });

  it("leaves the ids the payload does not carry as empty strings", () => {
    // notification.new carries no organization_id, user_id or category_id. The schema
    // requires them, so they are empty here — a known lie, documented rather than
    // papered over with an invented value. Nothing in the widget reads them.
    const built = notificationFromEvent(event());
    expect(built.organization_id).toBe("");
    expect(built.user_id).toBe("");
    expect(built.category_id).toBe("");
  });

  it("omits the action fields when the payload has none", () => {
    const built = notificationFromEvent(event());
    expect(built.action_url).toBeUndefined();
    expect(built.action_label).toBeUndefined();
  });
});
