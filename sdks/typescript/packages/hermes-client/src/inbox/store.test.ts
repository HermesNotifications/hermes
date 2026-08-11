// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { describe, expect, it } from "vitest";
import { HermesError } from "../errors.js";
import { FakeHermesClient, fakeNotification, fakePage } from "../testing/fake-client.js";
import { InboxStore } from "./store.js";

/**
 * The store is the one impure layer: it talks to the client, owns the clock, and turns
 * results into reducer actions. Everything interesting about it is a sequencing property
 * — optimistic-then-confirm, rollback on rejection, discarding a superseded response —
 * and none of that was reachable by any test before, because the widget built its own
 * client internally and there was no seam.
 */

const NOW = "2026-07-29T10:00:00.000Z";

function store(
  fake: FakeHermesClient,
  options: {
    userId?: string;
    pageSize?: number;
    archived?: boolean;
    ownsConnection?: boolean;
  } = {}
) {
  return new InboxStore({
    client: fake.asClient(),
    now: () => NOW,
    ...options,
  });
}

/** A promise whose resolution the test controls. */
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe("InboxStore: starting", () => {
  it("loads the first page with the configured page size and archived flag", async () => {
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    const inbox = store(fake, { pageSize: 25, archived: true });

    await inbox.start();

    expect(fake.listOptions[0]).toEqual({ limit: 25, archived: true });
    expect(inbox.getSnapshot().notifications.map((n) => n.id)).toEqual(["a"]);
    expect(inbox.getSnapshot().unreadCount).toBe(1);
    expect(inbox.getSnapshot().loading).toBe(false);
  });

  it("defaults to a page size of twenty", async () => {
    const fake = new FakeHermesClient(fakePage());
    await store(fake).start();
    expect(fake.listOptions[0]).toMatchObject({ limit: 20 });
  });

  it("subscribes before it lists", async () => {
    // Ordering, and it is the whole point. Listing first leaves a window between the page
    // response and the subscription going live in which a publication is dropped outright --
    // Centrifugo has no channel to deliver it to, so nothing retries and nothing recovers it
    // unless the deployment keeps history, which the bundled Helm Centrifugo does not.
    //
    // Subscribing first converts that lost update into a duplicate, which the reducer already
    // dedupes by id.
    const fake = new FakeHermesClient(fakePage());
    await store(fake, { userId: "usr_1" }).start();

    expect(fake.calls).toEqual(["connect:usr_1", "waitUntilConnected", "list"]);
  });

  it("reports loading from the moment start is called, not from the fetch", async () => {
    // start() now does socket work before it lists, and that gap is entirely visible to a
    // renderer. Without an immediate load/start the store reads "not loading, no notifications"
    // throughout the connect, so the widget shows its empty state -- "you're all caught up" --
    // over an inbox it simply has not fetched yet.
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    const inbox = store(fake, { userId: "usr_1" });

    const starting = inbox.start();
    expect(inbox.getSnapshot().loading).toBe(true);

    await starting;
    expect(inbox.getSnapshot().loading).toBe(false);
  });

  it("lists anyway when realtime never becomes ready", async () => {
    // A blocked websocket must not cost the user their inbox. The store waits a bounded time
    // and then fetches regardless, inheriting the race it was trying to avoid -- which is no
    // worse than the behaviour this ordering replaced.
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    fake.realtimeBecomesReady = false;
    const inbox = store(fake, { userId: "usr_1" });

    await inbox.start();

    expect(inbox.getSnapshot().notifications.map((n) => n.id)).toEqual(["a"]);
  });

  it("still lists when the socket refuses to connect at all", async () => {
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    fake.connect = async () => {
      throw new Error("websocket blocked");
    };
    const inbox = store(fake, { userId: "usr_1" });

    await inbox.start();

    expect(inbox.getSnapshot().notifications.map((n) => n.id)).toEqual(["a"]);
    expect(inbox.getSnapshot().realtime).toBe("disconnected");
  });

  it("registers one handler per realtime signal it consumes", async () => {
    // notification, update and status — three. A regression that dropped one would leave
    // the widget silently missing a whole class of change.
    const fake = new FakeHermesClient(fakePage());
    await store(fake).start();
    expect(fake.handlerCount()).toBe(3);
  });

  it("mirrors realtime status transitions into state", async () => {
    const fake = new FakeHermesClient(fakePage());
    const inbox = store(fake, { userId: "usr_1" });
    await inbox.start();

    fake.emitStatus("connected");

    expect(inbox.getSnapshot().realtime).toBe("connected");
  });

  it("connects for realtime when a user id is configured", async () => {
    const fake = new FakeHermesClient(fakePage());
    await store(fake, { userId: "usr_1" }).start();
    expect(fake.calls).toContain("connect:usr_1");
  });

  it("still connects with no user id, letting the client read it from the token", async () => {
    const fake = new FakeHermesClient(fakePage());
    await store(fake).start();
    expect(fake.calls.some((call) => call.startsWith("connect"))).toBe(true);
  });

  it("reports a failed load without discarding anything", async () => {
    const fake = new FakeHermesClient(fakePage());
    fake.fail("list", HermesError.fromStatus("Inbox", 500));
    const inbox = store(fake);

    await inbox.start();

    expect(inbox.getSnapshot().error).toMatchObject({ kind: "server" });
    expect(inbox.getSnapshot().loading).toBe(false);
  });

  it("does not reject when the initial load fails", async () => {
    // start() is called from a component lifecycle where there is nobody to catch.
    const fake = new FakeHermesClient(fakePage());
    fake.fail("list", HermesError.fromStatus("Inbox", 500));
    await expect(store(fake).start()).resolves.toBeUndefined();
  });

  it("keeps the list usable when only the realtime connection fails", async () => {
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    fake.connect = async () => {
      throw new Error("websocket refused");
    };
    const inbox = store(fake, { userId: "usr_1" });

    await inbox.start();

    expect(inbox.getSnapshot().notifications).toHaveLength(1);
    expect(inbox.getSnapshot().realtime).toBe("disconnected");
  });
});

describe("InboxStore: pagination", () => {
  it("sends the cursor and appends the next page", async () => {
    const fake = new FakeHermesClient(
      fakePage({ data: [fakeNotification("a")], unreadCount: 1, cursor: "c1" })
    );
    const inbox = store(fake);
    await inbox.start();

    fake.page = fakePage({ data: [fakeNotification("b")], unreadCount: 1 });
    await inbox.loadMore();

    expect(fake.listOptions[1]).toMatchObject({ cursor: "c1" });
    expect(inbox.getSnapshot().notifications.map((n) => n.id)).toEqual(["a", "b"]);
    expect(inbox.getSnapshot().hasMore).toBe(false);
  });

  it("does nothing when there is no further page", async () => {
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")] }));
    const inbox = store(fake);
    await inbox.start();

    await inbox.loadMore();

    expect(fake.listOptions).toHaveLength(1);
  });

  it("ignores a second request while one is already in flight", async () => {
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], cursor: "c1" }));
    const inbox = store(fake);
    await inbox.start();

    const pending = deferred<ReturnType<typeof fakePage>>();
    fake.inbox.list = async () => pending.promise;

    const first = inbox.loadMore();
    const second = inbox.loadMore();
    pending.resolve(fakePage({ data: [fakeNotification("b")] }));
    await Promise.all([first, second]);

    expect(inbox.getSnapshot().notifications.map((n) => n.id)).toEqual(["a", "b"]);
  });

  it("discards a stale cursor and reloads from the start", async () => {
    // Cursors are backend-specific and are invalidated when an operator switches store
    // backends. The documented contract is that a client discards it and asks for page
    // one; nothing in the SDK did that until now.
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], cursor: "stale" }));
    const inbox = store(fake);
    await inbox.start();

    let attempt = 0;
    fake.inbox.list = async (options) => {
      attempt++;
      fake.listOptions.push(options ?? {});
      if (attempt === 1) throw HermesError.fromStatus("Inbox", 400, { detail: "invalid cursor" });
      return fakePage({ data: [fakeNotification("fresh")], unreadCount: 0 });
    };

    await inbox.loadMore();

    expect(inbox.getSnapshot().notifications.map((n) => n.id)).toEqual(["fresh"]);
    expect(inbox.getSnapshot().cursor).toBeUndefined();
    expect(inbox.getSnapshot().error).toBeUndefined();
  });
});

describe("InboxStore: optimistic mutations", () => {
  it("applies the change before the server confirms it", async () => {
    // The point of optimism is that the UI does not wait a round trip. Asserting it
    // mid-flight is the only way to tell an optimistic implementation from one that
    // simply patches after awaiting.
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    const inbox = store(fake);
    await inbox.start();

    const pending = deferred<void>();
    fake.inbox.markRead = async () => pending.promise;

    const promise = inbox.markRead("a");
    expect(inbox.getSnapshot().notifications[0].read_at).toBe(NOW);
    expect(inbox.getSnapshot().unreadCount).toBe(0);

    pending.resolve();
    await promise;
  });

  it("stamps read_at with the injected clock", async () => {
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    const inbox = store(fake);
    await inbox.start();

    await inbox.markRead("a");

    expect(inbox.getSnapshot().notifications[0].read_at).toBe(NOW);
  });

  it.each([
    { name: "markRead", act: (s: InboxStore) => s.markRead("a"), call: "markRead:a" },
    { name: "markUnread", act: (s: InboxStore) => s.markUnread("a"), call: "markUnread:a" },
    { name: "archive", act: (s: InboxStore) => s.archive("a"), call: "archive:a" },
    { name: "remove", act: (s: InboxStore) => s.remove("a"), call: "delete:a" },
    { name: "markAllRead", act: (s: InboxStore) => s.markAllRead(), call: "markAllRead" },
  ])("$name reaches the API", async ({ act, call }) => {
    const fake = new FakeHermesClient(
      fakePage({ data: [fakeNotification("a", { read_at: NOW })], unreadCount: 1 })
    );
    const inbox = store(fake);
    await inbox.start();

    await act(inbox);

    expect(fake.calls).toContain(call);
  });

  it("restores the exact prior state when the server rejects the change", async () => {
    const fake = new FakeHermesClient(
      fakePage({ data: [fakeNotification("a"), fakeNotification("b")], unreadCount: 2 })
    );
    const inbox = store(fake);
    await inbox.start();
    const before = inbox.getSnapshot();

    fake.fail("markRead", HermesError.fromStatus("Inbox", 500));
    await inbox.markRead("a");

    const after = inbox.getSnapshot();
    expect(after.notifications).toEqual(before.notifications);
    expect(after.unreadCount).toBe(before.unreadCount);
    expect(after.error).toMatchObject({ kind: "server" });
  });

  it("does not reject to the caller when a mutation fails", async () => {
    // A widget click handler has nowhere to put a rejection; the error belongs in state
    // where the UI can render it.
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    const inbox = store(fake);
    await inbox.start();
    fake.fail("markRead", HermesError.fromStatus("Inbox", 500));

    await expect(inbox.markRead("a")).resolves.toBeUndefined();
  });

  it("restores the archived row when archiving fails", async () => {
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    const inbox = store(fake);
    await inbox.start();

    fake.fail("archive", HermesError.fromStatus("Inbox", 500));
    await inbox.archive("a");

    expect(inbox.getSnapshot().notifications.map((n) => n.id)).toEqual(["a"]);
  });

  it("clears a recorded error on request", async () => {
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    const inbox = store(fake);
    await inbox.start();
    fake.fail("markRead", HermesError.fromStatus("Inbox", 500));
    await inbox.markRead("a");

    inbox.clearError();

    expect(inbox.getSnapshot().error).toBeUndefined();
  });

  it("keeps a notification that arrived while a failing mutation was in flight", async () => {
    // Rollback restores a snapshot taken before the request went out, so anything realtime
    // delivered meanwhile used to be erased along with the optimistic change — and, since
    // the arrival had already been consumed, it would not come back until a manual refresh.
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    const inbox = store(fake);
    await inbox.start();

    const archive = deferred<void>();
    fake.inbox.archive = async () => archive.promise;

    const pending = inbox.archive("a");
    expect(inbox.getSnapshot().notifications.map((n) => n.id)).toEqual([]);

    // A new notification lands while the archive is still out.
    fake.emit("notification", {
      type: "notification.new",
      id: "arrived",
      title: "Arrived mid-flight",
      body: "Body",
      createdAt: "2026-07-30T10:00:00.000Z",
    });
    expect(inbox.getSnapshot().notifications.map((n) => n.id)).toContain("arrived");

    archive.reject(HermesError.fromStatus("Inbox", 500));
    await pending;

    const after = inbox.getSnapshot();
    // The archive is undone *and* the arrival survives.
    expect(after.notifications.map((n) => n.id).sort()).toEqual(["a", "arrived"]);
    expect(after.error).toBeDefined();
  });

  it("keeps a realtime update that landed while a failing mutation was in flight", async () => {
    const fake = new FakeHermesClient(
      fakePage({ data: [fakeNotification("a"), fakeNotification("b")], unreadCount: 2 })
    );
    const inbox = store(fake);
    await inbox.start();

    const archive = deferred<void>();
    fake.inbox.archive = async () => archive.promise;
    const pending = inbox.archive("a");

    // The server says b was read elsewhere.
    fake.emit("update", {
      type: "inbox.updated",
      action: "read",
      notificationId: "b",
      unreadCount: 1,
    });

    archive.reject(HermesError.fromStatus("Inbox", 500));
    await pending;

    const after = inbox.getSnapshot();
    expect(after.notifications.map((n) => n.id).sort()).toEqual(["a", "b"]);
    expect(after.notifications.find((n) => n.id === "b")?.read_at).toBeDefined();
    expect(after.unreadCount).toBe(1);
  });

  it("does not replay realtime events when the mutation succeeds", async () => {
    // The buffer exists only for the rollback path; replaying on success would double-apply.
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    const inbox = store(fake);
    await inbox.start();

    const archive = deferred<void>();
    fake.inbox.archive = async () => archive.promise;
    const pending = inbox.archive("a");

    fake.emit("notification", {
      type: "notification.new",
      id: "arrived",
      title: "Arrived",
      body: "Body",
      createdAt: "2026-07-30T10:00:00.000Z",
    });

    archive.resolve();
    await pending;

    const ids = inbox.getSnapshot().notifications.map((n) => n.id);
    expect(ids).toEqual(["arrived"]);
    expect(ids.filter((id) => id === "arrived")).toHaveLength(1);
  });
});

describe("InboxStore: unread count propagation", () => {
  it("pushes the count from the initial load back to the client", async () => {
    // This is what makes a standalone badge correct from first paint. The client used to
    // learn the count only from an inbox.updated event, so a badge read zero until the
    // user's first mutation.
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 4 }));
    await store(fake).start();
    expect(fake.unreadCountWrites).toEqual([4]);
  });

  it("pushes the count after an optimistic change", async () => {
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    const inbox = store(fake);
    await inbox.start();

    await inbox.markRead("a");

    expect(fake.unreadCountWrites).toEqual([1, 0]);
  });

  it("does not push a count that has not changed", async () => {
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    const inbox = store(fake);
    await inbox.start();

    await inbox.refresh();

    expect(fake.unreadCountWrites).toEqual([1]);
  });
});

describe("InboxStore: realtime handling", () => {
  it("prepends an arriving notification", async () => {
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    const inbox = store(fake);
    await inbox.start();

    fake.emit("notification", {
      type: "notification.new",
      id: "b",
      title: "Fresh",
      body: "Body",
      createdAt: NOW,
    });

    expect(inbox.getSnapshot().notifications.map((n) => n.id)).toEqual(["b", "a"]);
    expect(inbox.getSnapshot().unreadCount).toBe(2);
  });

  it("takes the server count from an update event", async () => {
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    const inbox = store(fake);
    await inbox.start();

    fake.emit("update", {
      type: "inbox.updated",
      notificationId: "a",
      action: "read",
      unreadCount: 9,
      timestamp: 1,
    });

    expect(inbox.getSnapshot().unreadCount).toBe(9);
    expect(inbox.getSnapshot().notifications[0].read_at).toBe(NOW);
  });
});

describe("InboxStore: subscription contract", () => {
  it("notifies subscribers when state changes", async () => {
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    const inbox = store(fake);
    let notifications = 0;
    inbox.subscribe(() => notifications++);

    await inbox.start();

    expect(notifications).toBeGreaterThan(0);
  });

  it("returns a stable snapshot reference between changes", async () => {
    // useSyncExternalStore re-renders whenever this reference changes, so an unchanging
    // store must return an unchanging object.
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    const inbox = store(fake);
    await inbox.start();

    expect(inbox.getSnapshot()).toBe(inbox.getSnapshot());
  });

  it("stops notifying a subscriber that unsubscribed", async () => {
    const fake = new FakeHermesClient(fakePage());
    const inbox = store(fake);
    let notifications = 0;
    const unsubscribe = inbox.subscribe(() => notifications++);
    unsubscribe();

    await inbox.start();

    expect(notifications).toBe(0);
  });

  it("serves an inert snapshot for server rendering", () => {
    const fake = new FakeHermesClient(fakePage());
    const inbox = store(fake);
    expect(inbox.getServerSnapshot()).toMatchObject({
      notifications: [],
      unreadCount: 0,
      loading: false,
    });
  });
});

describe("InboxStore: stop and restart", () => {
  it("resumes after a stop, as React StrictMode requires", async () => {
    // This is not hypothetical. StrictMode deliberately runs an effect, its cleanup, and then the
    // effect again on the same instance. Before `stop()` existed the cleanup called `dispose()`, so
    // the second `start()` hit the disposed guard and returned — leaving the inbox permanently inert
    // in every StrictMode app: no first page, no socket, no realtime. It reached a live browser
    // before anything caught it, because Testing Library's renderHook does not use StrictMode.
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    const inbox = store(fake, { userId: "usr_1" });

    await inbox.start();
    inbox.stop();
    await inbox.start();

    expect(fake.handlerCount()).toBe(3);
    expect(fake.calls.filter((call) => call === "connect:usr_1")).toHaveLength(2);
    expect(inbox.getSnapshot().notifications.map((n) => n.id)).toEqual(["a"]);
  });

  it("delivers realtime events again after a restart", async () => {
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    const inbox = store(fake);
    await inbox.start();
    inbox.stop();
    await inbox.start();

    fake.emit("notification", {
      type: "notification.new",
      id: "b",
      title: "After restart",
      body: "body",
      createdAt: NOW,
    });

    expect(inbox.getSnapshot().notifications.map((n) => n.id)).toEqual(["b", "a"]);
  });

  it("registers each handler exactly once across a stop/start cycle", async () => {
    // A stop that forgot to unsubscribe would double every handler, so one arrival would increment
    // the badge twice.
    const fake = new FakeHermesClient(fakePage());
    const inbox = store(fake);
    await inbox.start();
    inbox.stop();
    await inbox.start();

    fake.emit("notification", {
      type: "notification.new",
      id: "b",
      title: "T",
      body: "B",
      createdAt: NOW,
    });

    expect(inbox.getSnapshot().unreadCount).toBe(1);
  });

  it("closes the socket on stop", async () => {
    const fake = new FakeHermesClient(fakePage());
    const inbox = store(fake, { userId: "usr_1" });
    await inbox.start();

    inbox.stop();

    expect(fake.calls).toContain("disconnect");
    expect(fake.handlerCount()).toBe(0);
  });

  it("ignores a repeated start, so one mount cannot load twice", async () => {
    const fake = new FakeHermesClient(fakePage());
    const inbox = store(fake);

    await inbox.start();
    await inbox.start();

    expect(fake.calls.filter((call) => call === "list")).toHaveLength(1);
  });

  it("does not fetch when stopped while the socket was coming up", async () => {
    // The mirror of the old ordering guard. start() now subscribes first, so the window in
    // which stop() can race it is the connect, not the list -- and fetching afterwards would
    // push a page into a store nobody is listening to.
    const fake = new FakeHermesClient(fakePage());
    const pending = deferred<void>();
    fake.connect = async () => {
      fake.calls.push("connect");
      await pending.promise;
    };
    const inbox = store(fake, { userId: "usr_1" });

    const starting = inbox.start();
    inbox.stop();
    pending.resolve();
    await starting;

    expect(fake.calls.filter((call) => call === "list")).toHaveLength(0);
  });
});

describe("InboxStore: disposal", () => {
  it("unsubscribes from the client and closes the socket", async () => {
    const fake = new FakeHermesClient(fakePage());
    const inbox = store(fake, { userId: "usr_1" });
    await inbox.start();

    inbox.dispose();

    expect(fake.handlerCount()).toBe(0);
    expect(fake.calls).toContain("disconnect");
  });

  it("ignores mutations issued after disposal", async () => {
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    const inbox = store(fake);
    await inbox.start();
    inbox.dispose();

    await inbox.markRead("a");

    expect(fake.calls).not.toContain("markRead:a");
  });

  it("discards a load that resolves after disposal", async () => {
    // Otherwise an unmounted component's in-flight request writes into a dead store, and
    // in React that is the classic setState-after-unmount warning.
    const fake = new FakeHermesClient(fakePage());
    const pending = deferred<ReturnType<typeof fakePage>>();
    fake.inbox.list = async () => pending.promise;
    const inbox = store(fake);

    const starting = inbox.start();
    inbox.dispose();
    pending.resolve(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    await starting;

    expect(inbox.getSnapshot().notifications).toEqual([]);
  });

  it("discards a superseded load when two are in flight", async () => {
    // Two rapid config changes race. Without a generation counter the loser can resolve
    // last and clobber the winner's list.
    const fake = new FakeHermesClient(fakePage());
    const first = deferred<ReturnType<typeof fakePage>>();
    const second = deferred<ReturnType<typeof fakePage>>();
    let call = 0;
    fake.inbox.list = async () => {
      call++;
      return call === 1 ? first.promise : second.promise;
    };
    const inbox = store(fake);

    const a = inbox.refresh();
    const b = inbox.refresh();
    // The second request resolves first; the first must not overwrite it.
    second.resolve(fakePage({ data: [fakeNotification("winner")], unreadCount: 1 }));
    first.resolve(fakePage({ data: [fakeNotification("loser")], unreadCount: 9 }));
    await Promise.all([a, b]);

    expect(inbox.getSnapshot().notifications.map((n) => n.id)).toEqual(["winner"]);
    expect(inbox.getSnapshot().unreadCount).toBe(1);
  });

  it("recovers pagination when a refresh supersedes an in-flight page", async () => {
    // The regression: loadMore's response is dropped on the generation check *before* it
    // can dispatch page/success or page/failure, so nothing cleared loadingMore and every
    // later loadMore() returned at its guard — pagination dead until remount.
    const fake = new FakeHermesClient(
      fakePage({ data: [fakeNotification("a")], unreadCount: 0, cursor: "cur_1" })
    );
    const inbox = store(fake);
    await inbox.start();
    expect(inbox.getSnapshot().hasMore).toBe(true);

    const page = deferred<ReturnType<typeof fakePage>>();
    fake.inbox.list = async () => page.promise;

    const pending = inbox.loadMore();
    expect(inbox.getSnapshot().loadingMore).toBe(true);

    // A refresh lands while that page is still out.
    const reloaded = fakePage({ data: [fakeNotification("b")], unreadCount: 0, cursor: "cur_2" });
    fake.inbox.list = async () => reloaded;
    await inbox.refresh();

    page.resolve(fakePage({ data: [fakeNotification("stale")], unreadCount: 0 }));
    await pending;

    expect(inbox.getSnapshot().loadingMore).toBe(false);
    expect(inbox.getSnapshot().notifications.map((n) => n.id)).toEqual(["b"]);

    // And the next page actually loads, rather than returning at the guard.
    fake.inbox.list = async () =>
      fakePage({ data: [fakeNotification("c")], unreadCount: 0 });
    await inbox.loadMore();
    expect(inbox.getSnapshot().notifications.map((n) => n.id)).toEqual(["b", "c"]);
  });
});

describe("InboxStore: reconciling on reconnect", () => {
  /**
   * A dropped socket loses publications permanently — the NATS broker keeps no history for
   * Centrifugo to replay (issue #102). So the store repairs itself against the API whenever
   * realtime comes back, and only then: the first connect follows start()'s own load.
   */
  it("does not reconcile on the first connect", async () => {
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    const inbox = store(fake, { userId: "usr_1" });
    await inbox.start();

    fake.emitStatus("connected");
    await new Promise((resolve) => setTimeout(resolve, 0));

    // Exactly one list(): the one start() performed.
    expect(fake.calls.filter((c) => c === "list")).toHaveLength(1);
  });

  it("refetches when realtime comes back after a drop", async () => {
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    const inbox = store(fake, { userId: "usr_1" });
    await inbox.start();
    fake.emitStatus("connected");

    fake.emitStatus("disconnected");
    fake.emitStatus("connected");
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(fake.calls.filter((c) => c === "list")).toHaveLength(2);
  });

  it("merges what it finds instead of replacing the list", async () => {
    const fake = new FakeHermesClient(
      fakePage({ data: [fakeNotification("older")], unreadCount: 1, cursor: "cur_1" })
    );
    const inbox = store(fake, { userId: "usr_1" });
    await inbox.start();
    fake.emitStatus("connected");

    // While the socket was down, something arrived that the client never saw.
    fake.page = fakePage({
      data: [fakeNotification("missed"), fakeNotification("older")],
      unreadCount: 2,
    });
    fake.emitStatus("disconnected");
    fake.emitStatus("connected");
    await new Promise((resolve) => setTimeout(resolve, 0));

    const after = inbox.getSnapshot();
    expect(after.notifications.map((n) => n.id)).toEqual(["missed", "older"]);
    expect(after.unreadCount).toBe(2);
    // Pagination untouched, so a user who had paged deeper keeps what they loaded.
    expect(after.cursor).toBe("cur_1");
  });

  it("does not surface an error when the repair itself fails", async () => {
    // Background repair. An error banner over a working inbox would be the store reporting a
    // problem the user does not have.
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    const inbox = store(fake, { userId: "usr_1" });
    await inbox.start();
    fake.emitStatus("connected");

    fake.fail("list", HermesError.fromStatus("Inbox", 500));
    fake.emitStatus("disconnected");
    fake.emitStatus("connected");
    await new Promise((resolve) => setTimeout(resolve, 0));

    expect(inbox.getSnapshot().error).toBeUndefined();
    expect(inbox.getSnapshot().notifications.map((n) => n.id)).toEqual(["a"]);
  });

  it("treats a restart as a first connect, not a gap", async () => {
    // StrictMode's effect/cleanup/effect, or a genuine remount: start() loads the page again,
    // so the connect that follows is not a gap to repair.
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    const inbox = store(fake, { userId: "usr_1" });
    await inbox.start();
    fake.emitStatus("connected");
    inbox.stop();

    await inbox.start();
    fake.emitStatus("connected");
    await new Promise((resolve) => setTimeout(resolve, 0));

    // Two list() calls, one per start(), and no reconcile on top.
    expect(fake.calls.filter((c) => c === "list")).toHaveLength(2);
  });
});

describe("InboxStore: connection ownership", () => {
  it("closes the socket on stop when the connection is its own", async () => {
    const fake = new FakeHermesClient(fakePage());
    const inbox = store(fake, { userId: "usr_1" });
    await inbox.start();
    inbox.stop();
    expect(fake.calls).toContain("disconnect");
  });

  it("leaves a shared socket open on stop", async () => {
    // A client handed in from outside may be driving a second widget or a standalone
    // badge. Closing its socket here would stop their updates with nothing to restart it.
    const fake = new FakeHermesClient(fakePage());
    const inbox = store(fake, { userId: "usr_1", ownsConnection: false });
    await inbox.start();
    inbox.stop();
    expect(fake.calls).not.toContain("disconnect");
  });

  it("still unsubscribes its own handlers from a shared client", async () => {
    const fake = new FakeHermesClient(fakePage());
    const inbox = store(fake, { userId: "usr_1", ownsConnection: false });
    await inbox.start();
    const wired = fake.handlerCount();
    expect(wired).toBeGreaterThan(0);
    inbox.stop();
    expect(fake.handlerCount()).toBe(0);
  });
});
