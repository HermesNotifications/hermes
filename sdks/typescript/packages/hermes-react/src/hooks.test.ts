// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import type { InboxPage, Notification } from "@hermes-notifications/client";
import {
  FakeHermesClient,
  fakeNotification,
  fakePage,
} from "@hermes-notifications/client/testing";
import { useHermesInbox, useUnreadCount } from "./hooks.js";

/**
 * These hooks are now a thin adapter over the client's InboxStore rather than a second
 * implementation of inbox state — the duplicated realtime synthesis and optimistic patching
 * that used to live here (and had already drifted from the widget's copy) is gone.
 *
 * The fake comes from `@hermes-notifications/client/testing` for the same reason: one shared
 * fake, typed against the real interface, instead of a copy per package.
 *
 * Two conventions this file depends on:
 * - The runner exposes no globals, so `cleanup` is registered by hand. A hook left mounted
 *   keeps reacting to the next test's events.
 * - The store's clock is injectable, so `read_at` assertions are exact values. The previous
 *   version of this suite could only manage `toBeTypeOf("string")`, which passes for any
 *   stamp at all.
 */

const NOW = "2026-07-29T10:00:00.000Z";
const EARLIER = "2026-07-01T00:00:00.000Z";

function page(overrides: Partial<InboxPage> = {}): InboxPage {
  return {
    data: [fakeNotification("n1"), fakeNotification("n2")],
    unreadCount: 2,
    ...overrides,
  };
}

/** Render the hook against `fake` and wait for the initial load to settle. */
async function renderLoaded(fake: FakeHermesClient, options: { userId?: string } = {}) {
  const view = renderHook(() =>
    useHermesInbox(fake.asClient(), { ...options, now: () => NOW })
  );
  await waitFor(() => expect(view.result.current.loading).toBe(false));
  return view;
}

afterEach(cleanup);

describe("useHermesInbox: loading", () => {
  it("publishes the first page, its unread count and its cursor", async () => {
    const fake = new FakeHermesClient(page({ cursor: "cur_1" }));
    const { result } = await renderLoaded(fake);

    expect(result.current.notifications.map((n) => n.id)).toEqual(["n1", "n2"]);
    expect(result.current.unreadCount).toBe(2);
    expect(result.current.cursor).toBe("cur_1");
    expect(result.current.hasMore).toBe(true);
  });

  it("reports loading before the first page arrives", () => {
    const fake = new FakeHermesClient(page());
    const { result } = renderHook(() => useHermesInbox(fake.asClient()));

    expect(result.current.loading).toBe(true);
    expect(result.current.notifications).toEqual([]);
  });

  it("returns an inert state when there is no client yet", () => {
    // A host app fetching its token asynchronously renders with null on the first pass.
    const { result } = renderHook(() => useHermesInbox(null));

    expect(result.current.notifications).toEqual([]);
    expect(result.current.loading).toBe(false);
    expect(result.current.unreadCount).toBe(0);
  });

  it("reports hasMore as false on the last page", async () => {
    const fake = new FakeHermesClient(page());
    const { result } = await renderLoaded(fake);
    expect(result.current.hasMore).toBe(false);
  });
});

describe("useHermesInbox: realtime", () => {
  it("prepends an arriving notification and raises the unread count", async () => {
    const fake = new FakeHermesClient(page());
    const { result } = await renderLoaded(fake);

    act(() =>
      fake.emit("notification", {
        type: "notification.new",
        id: "n3",
        title: "Fresh",
        body: "Just arrived",
        createdAt: "2026-07-28T13:00:00.000Z",
      })
    );

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

  it("carries an arriving notification's action url through to the row", async () => {
    const fake = new FakeHermesClient(page());
    const { result } = await renderLoaded(fake);

    act(() =>
      fake.emit("notification", {
        type: "notification.new",
        id: "n3",
        title: "Invoice",
        body: "Ready",
        createdAt: NOW,
        actionUrl: "https://example.com/i/1",
        actionLabel: "View",
      })
    );

    expect(result.current.notifications[0]).toMatchObject({
      action_url: "https://example.com/i/1",
      action_label: "View",
    });
  });

  it.each([
    { name: "drops the notification named by a delete event", action: "delete" as const, wantIds: ["n2"] },
    { name: "keeps the notification named by a read event", action: "read" as const, wantIds: ["n1", "n2"] },
    { name: "drops the notification named by an archive event", action: "archive" as const, wantIds: ["n2"] },
  ])("$name", async ({ action, wantIds }) => {
    const fake = new FakeHermesClient(page());
    const { result } = await renderLoaded(fake);

    act(() =>
      fake.emit("update", {
        type: "inbox.updated",
        notificationId: "n1",
        action,
        unreadCount: 7,
        timestamp: 1_769_000_000,
      })
    );

    expect(result.current.notifications.map((n) => n.id)).toEqual(wantIds);
    // The server's count is authoritative on an update event, never a local guess.
    expect(result.current.unreadCount).toBe(7);
  });

  it("exposes the realtime connection status", async () => {
    const fake = new FakeHermesClient(page());
    const { result } = await renderLoaded(fake);

    act(() => fake.emitStatus("connected"));

    expect(result.current.realtime).toBe("connected");
  });
});

describe("useHermesInbox: mutations", () => {
  it("marks a notification read, stamping the exact time and lowering the count", async () => {
    const fake = new FakeHermesClient(page());
    const { result } = await renderLoaded(fake);

    await act(async () => {
      await result.current.markRead("n1");
    });

    expect(fake.calls).toContain("markRead:n1");
    expect(result.current.notifications[0].read_at).toBe(NOW);
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
    expect(result.current.notifications.every((n) => n.read_at === NOW)).toBe(true);
    expect(result.current.unreadCount).toBe(0);
  });

  it("preserves an original read_at when marking all read", async () => {
    const alreadyRead: Notification = fakeNotification("n1", { read_at: EARLIER });
    const fake = new FakeHermesClient(page({ data: [alreadyRead, fakeNotification("n2")] }));
    const { result } = await renderLoaded(fake);

    await act(async () => {
      await result.current.markAllRead();
    });

    expect(result.current.notifications[0].read_at).toBe(EARLIER);
    expect(result.current.notifications[1].read_at).toBe(NOW);
  });

  it("marks a notification unread again", async () => {
    const fake = new FakeHermesClient(page({ data: [fakeNotification("n1", { read_at: EARLIER })], unreadCount: 0 }));
    const { result } = await renderLoaded(fake);

    await act(async () => {
      await result.current.markUnread("n1");
    });

    expect(fake.calls).toContain("markUnread:n1");
    expect(result.current.notifications[0].read_at).toBeUndefined();
    expect(result.current.unreadCount).toBe(1);
  });

  it("deletes a notification", async () => {
    const fake = new FakeHermesClient(page());
    const { result } = await renderLoaded(fake);

    await act(async () => {
      await result.current.remove("n1");
    });

    expect(fake.calls).toContain("delete:n1");
    expect(result.current.notifications.map((n) => n.id)).toEqual(["n2"]);
  });

  it("rolls back and reports the error when the server rejects a mutation", async () => {
    const fake = new FakeHermesClient(page());
    fake.fail("archive", new Error("boom"));
    const { result } = await renderLoaded(fake);

    await act(async () => {
      await result.current.archive("n1");
    });

    expect(result.current.notifications.map((n) => n.id)).toEqual(["n1", "n2"]);
    expect(result.current.error).toBeDefined();
  });

  it("does not throw at the caller when a mutation fails", async () => {
    // A click handler has nowhere to put a rejection; the error belongs in state.
    const fake = new FakeHermesClient(page());
    fake.fail("markRead", new Error("boom"));
    const { result } = await renderLoaded(fake);

    await act(async () => {
      await expect(result.current.markRead("n1")).resolves.toBeUndefined();
    });
  });

  it("clears a recorded error on request", async () => {
    const fake = new FakeHermesClient(page());
    fake.fail("archive", new Error("boom"));
    const { result } = await renderLoaded(fake);
    await act(async () => {
      await result.current.archive("n1");
    });

    act(() => result.current.clearError());

    expect(result.current.error).toBeUndefined();
  });
});

describe("useHermesInbox: pagination", () => {
  it("appends the next page, sending the cursor", async () => {
    const fake = new FakeHermesClient(page({ cursor: "cur_1" }));
    const { result } = await renderLoaded(fake);

    fake.page = fakePage({ data: [fakeNotification("n3")], unreadCount: 2 });
    await act(async () => {
      await result.current.loadMore();
    });

    expect(fake.listOptions[1]).toMatchObject({ cursor: "cur_1" });
    expect(result.current.notifications.map((n) => n.id)).toEqual(["n1", "n2", "n3"]);
    expect(result.current.hasMore).toBe(false);
  });

  it("does nothing when there is no further page", async () => {
    const fake = new FakeHermesClient(page());
    const { result } = await renderLoaded(fake);

    await act(async () => {
      await result.current.loadMore();
    });

    expect(fake.listOptions).toHaveLength(1);
  });
});

describe("useHermesInbox: lifecycle", () => {
  it("connects for realtime, letting the client resolve the channel from the token", async () => {
    // The hook always connects now. The user id is optional because the client reads the
    // internal id from the token's `sub` claim — passing the *external* id produced a
    // subscription Centrifugo rejected while REST kept working, so the inbox loaded and then
    // silently never updated.
    const fake = new FakeHermesClient(page());
    await renderLoaded(fake);
    expect(fake.calls.some((call) => call.startsWith("connect"))).toBe(true);
  });

  it("passes an explicit user id through when given one", async () => {
    const fake = new FakeHermesClient(page());
    await renderLoaded(fake, { userId: "usr_1" });
    expect(fake.calls).toContain("connect:usr_1");
  });

  it("unsubscribes its event handlers on unmount", async () => {
    // The store is owned by the mount, so its lifetime is the component's. This assertion is
    // what keeps that true.
    const fake = new FakeHermesClient(page());
    const { unmount } = await renderLoaded(fake);
    expect(fake.handlerCount()).toBeGreaterThan(0);

    unmount();

    expect(fake.handlerCount()).toBe(0);
  });

  it("loads exactly once per mount", async () => {
    const fake = new FakeHermesClient(page());
    await renderLoaded(fake);
    expect(fake.calls.filter((call) => call === "list")).toHaveLength(1);
  });

  it("does not reload when the component re-renders with unchanged options", async () => {
    const fake = new FakeHermesClient(page());
    const view = renderHook(() => useHermesInbox(fake.asClient(), { now: () => NOW }));
    await waitFor(() => expect(view.result.current.loading).toBe(false));

    view.rerender();
    view.rerender();

    expect(fake.calls.filter((call) => call === "list")).toHaveLength(1);
  });
});

describe("useUnreadCount", () => {
  it("starts at zero", () => {
    const fake = new FakeHermesClient(page());
    const { result } = renderHook(() => useUnreadCount(fake.asClient()));
    expect(result.current).toBe(0);
  });

  it("reports the count pushed by a load, before any mutation", async () => {
    // The gap this closes: the client only ever learned the count from an inbox.updated
    // event, so a standalone badge read zero until the user's first mutation — even with
    // unread rows on screen.
    const fake = new FakeHermesClient(page({ unreadCount: 4 }));
    const { result } = renderHook(() => {
      const count = useUnreadCount(fake.asClient());
      useHermesInbox(fake.asClient(), { now: () => NOW });
      return count;
    });

    await waitFor(() => expect(result.current).toBe(4));
  });

  it("follows a server-driven update", async () => {
    const fake = new FakeHermesClient(page());
    const { result } = renderHook(() => useUnreadCount(fake.asClient()));

    act(() => fake.setUnreadCount(9));

    expect(result.current).toBe(9);
  });

  it("returns zero when there is no client", () => {
    const { result } = renderHook(() => useUnreadCount(null));
    expect(result.current).toBe(0);
  });

  it("unsubscribes on unmount", () => {
    const fake = new FakeHermesClient(page());
    const { unmount } = renderHook(() => useUnreadCount(fake.asClient()));
    expect(fake.handlerCount()).toBe(1);

    unmount();

    expect(fake.handlerCount()).toBe(0);
  });
});
