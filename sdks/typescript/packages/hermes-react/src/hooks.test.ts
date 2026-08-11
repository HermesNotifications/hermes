// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import type {
  HermesClient,
  InboxPage,
  InboxUpdatedEvent,
  NewNotificationEvent,
  Notification,
} from "@hermes-notifications/client";
import { useHermesInbox } from "./hooks.js";

/**
 * The four inbox methods useHermesInbox calls. Typing the fake's `inbox` as this
 * Pick is what makes the fake self-checking: change an argument or return type on
 * the real InboxAPI and this file stops compiling.
 */
type InboxSurface = Pick<
  HermesClient["inbox"],
  "list" | "markRead" | "archive" | "markAllRead"
>;

/**
 * A hand-written stand-in for HermesClient. Only the surface useHermesInbox
 * actually touches is implemented.
 *
 * `asClient()` casts through `unknown` because HermesClient carries private
 * fields, so no structural object is ever assignable to it — that cast is a
 * limitation, not a guarantee, and it means the top-level shape is unchecked.
 * What *is* checked is everything underneath it: `inbox` against InboxSurface
 * above, and every fixture below against the generated `Notification` and
 * `InboxPage` types. That last part is the one that matters here — a stale field
 * name in a fixture is exactly the drift that shipped `group_id` after the schema
 * moved to `category_id`.
 */
class FakeHermesClient {
  page: InboxPage;
  readonly calls: string[] = [];
  private handlers = new Map<string, Set<(event: never) => void>>();

  constructor(page: InboxPage) {
    this.page = page;
  }

  inbox: InboxSurface = {
    list: async (): Promise<InboxPage> => {
      this.calls.push("list");
      return this.page;
    },
    markRead: async (id: string): Promise<void> => {
      this.calls.push(`markRead:${id}`);
    },
    archive: async (id: string): Promise<void> => {
      this.calls.push(`archive:${id}`);
    },
    markAllRead: async (): Promise<void> => {
      this.calls.push("markAllRead");
    },
  };

  on(event: string, handler: (e: never) => void): () => void {
    const set = this.handlers.get(event) ?? new Set();
    set.add(handler);
    this.handlers.set(event, set);
    return () => set.delete(handler);
  }

  async connect(userId: string): Promise<void> {
    this.calls.push(`connect:${userId}`);
  }

  disconnect(): void {
    this.calls.push("disconnect");
  }

  /** Drive the hook the way the realtime connection would. */
  emit(event: "notification" | "update", payload: unknown) {
    for (const handler of this.handlers.get(event) ?? []) {
      (handler as (e: unknown) => void)(payload);
    }
  }

  /** How many handlers are still registered — proves cleanup on unmount. */
  handlerCount(): number {
    let total = 0;
    for (const set of this.handlers.values()) total += set.size;
    return total;
  }

  asClient(): HermesClient {
    return this as unknown as HermesClient;
  }
}

function notification(id: string, overrides: Partial<Notification> = {}): Notification {
  return {
    id,
    title: `Title ${id}`,
    body: `Body ${id}`,
    status: "delivered",
    channels: ["inbox"],
    created_at: "2026-07-28T12:00:00.000Z",
    organization_id: "org_1",
    user_id: "usr_1",
    category_id: "cat_1",
    ...overrides,
  };
}

function page(overrides: Partial<InboxPage> = {}): InboxPage {
  return {
    data: [notification("n1"), notification("n2")],
    unreadCount: 2,
    ...overrides,
  };
}

/** Render the hook and wait for the initial load to settle. */
async function renderLoaded(fake: FakeHermesClient) {
  const view = renderHook(() => useHermesInbox(fake.asClient()));
  await waitFor(() => expect(view.result.current.loading).toBe(false));
  return view;
}

// Testing Library only self-registers its cleanup when the runner exposes globals,
// which this config deliberately does not. Unmount explicitly instead, or a hook
// left mounted keeps reacting to the next test's events.
afterEach(cleanup);

describe("useHermesInbox", () => {
  it("publishes the first page and its unread count once loading settles", async () => {
    const fake = new FakeHermesClient(page({ cursor: "cur_1" }));
    const { result } = await renderLoaded(fake);

    expect(result.current.notifications.map((n) => n.id)).toEqual(["n1", "n2"]);
    expect(result.current.unreadCount).toBe(2);
    expect(result.current.cursor).toBe("cur_1");
  });

  it("reports loading before the first page arrives", () => {
    const fake = new FakeHermesClient(page());
    const { result } = renderHook(() => useHermesInbox(fake.asClient()));

    expect(result.current.loading).toBe(true);
    expect(result.current.notifications).toEqual([]);
  });

  it("prepends a realtime notification and raises the unread count", async () => {
    const fake = new FakeHermesClient(page());
    const { result } = await renderLoaded(fake);

    const event: NewNotificationEvent = {
      type: "notification.new",
      id: "n3",
      title: "Fresh",
      body: "Just arrived",
      createdAt: "2026-07-28T13:00:00.000Z",
    };
    act(() => fake.emit("notification", event));

    // Newest first — a realtime arrival goes to the head, not the tail.
    expect(result.current.notifications.map((n) => n.id)).toEqual(["n3", "n1", "n2"]);
    expect(result.current.notifications[0]).toMatchObject({
      title: "Fresh",
      body: "Just arrived",
      created_at: "2026-07-28T13:00:00.000Z",
      status: "delivered",
      channels: ["inbox"],
    });
    expect(result.current.unreadCount).toBe(3);
  });

  it("marks a notification read, stamps read_at, and lowers the unread count", async () => {
    const fake = new FakeHermesClient(page());
    const { result } = await renderLoaded(fake);

    await act(async () => {
      await result.current.markRead("n1");
    });

    expect(fake.calls).toContain("markRead:n1");
    expect(result.current.notifications[0].read_at).toBeTypeOf("string");
    // The untouched one must stay unread, or the map is rewriting every row.
    expect(result.current.notifications[1].read_at).toBeUndefined();
    expect(result.current.unreadCount).toBe(1);
  });

  it("floors the unread count at zero when more are read than were unread", async () => {
    const fake = new FakeHermesClient(page({ unreadCount: 1 }));
    const { result } = await renderLoaded(fake);

    await act(async () => {
      await result.current.markRead("n1");
    });
    await act(async () => {
      await result.current.markRead("n2");
    });

    expect(result.current.unreadCount).toBe(0);
  });

  it("removes an archived notification from the list", async () => {
    const fake = new FakeHermesClient(page());
    const { result } = await renderLoaded(fake);

    await act(async () => {
      await result.current.archive("n1");
    });

    expect(fake.calls).toContain("archive:n1");
    expect(result.current.notifications.map((n) => n.id)).toEqual(["n2"]);
  });

  it("stamps every unread notification when marking all read", async () => {
    const fake = new FakeHermesClient(page());
    const { result } = await renderLoaded(fake);

    await act(async () => {
      await result.current.markAllRead();
    });

    expect(fake.calls).toContain("markAllRead");
    expect(result.current.notifications.every((n) => typeof n.read_at === "string")).toBe(true);
    expect(result.current.unreadCount).toBe(0);
  });

  it("preserves the original read_at when marking all read", async () => {
    const alreadyRead = notification("n1", { read_at: "2026-07-01T00:00:00.000Z" });
    const fake = new FakeHermesClient(page({ data: [alreadyRead, notification("n2")] }));
    const { result } = await renderLoaded(fake);

    await act(async () => {
      await result.current.markAllRead();
    });

    expect(result.current.notifications[0].read_at).toBe("2026-07-01T00:00:00.000Z");
  });

  it.each([
    {
      name: "drops the notification named by a delete event",
      action: "delete" as const,
      wantIds: ["n2"],
    },
    {
      name: "keeps the notification named by a read event",
      action: "read" as const,
      wantIds: ["n1", "n2"],
    },
  ])("$name", async ({ action, wantIds }) => {
    const fake = new FakeHermesClient(page());
    const { result } = await renderLoaded(fake);

    const event: InboxUpdatedEvent = {
      type: "inbox.updated",
      notificationId: "n1",
      action,
      unreadCount: 7,
      timestamp: 1_769_000_000,
    };
    act(() => fake.emit("update", event));

    expect(result.current.notifications.map((n) => n.id)).toEqual(wantIds);
    // The server's count is authoritative on an update event, not a local guess.
    expect(result.current.unreadCount).toBe(7);
  });

  it("connects for realtime updates only when a userId is supplied", async () => {
    const withUser = new FakeHermesClient(page());
    const withoutUser = new FakeHermesClient(page());

    const view = renderHook(() => useHermesInbox(withUser.asClient(), { userId: "usr_1" }));
    await waitFor(() => expect(view.result.current.loading).toBe(false));
    await renderLoaded(withoutUser);

    expect(withUser.calls).toContain("connect:usr_1");
    expect(withoutUser.calls.some((c) => c.startsWith("connect"))).toBe(false);
  });

  it("unsubscribes its event handlers on unmount", async () => {
    const fake = new FakeHermesClient(page());
    const { unmount } = await renderLoaded(fake);

    expect(fake.handlerCount()).toBeGreaterThan(0);
    unmount();
    expect(fake.handlerCount()).toBe(0);
  });
});
