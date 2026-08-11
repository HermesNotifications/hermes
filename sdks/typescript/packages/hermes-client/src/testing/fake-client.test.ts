// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { describe, expect, it } from "vitest";
import { HermesError } from "../errors.js";
import { FakeHermesClient, fakePage, fakeNotification } from "./fake-client.js";

/**
 * The fake is shipped as part of the package (via the `./testing` subpath), so it is
 * itself production code for anyone writing tests against the SDK — the store suite,
 * the widget's controller suite and the React hooks suite all drive it. A fake whose
 * `emit` silently reached nobody, or whose `calls` log missed a method, would make
 * every one of those suites vacuously green. Hence this file.
 */
describe("FakeHermesClient: recording calls", () => {
  it("logs every inbox method it was asked to perform", async () => {
    const fake = new FakeHermesClient(fakePage());
    await fake.inbox.list();
    await fake.inbox.markRead("a");
    await fake.inbox.markUnread("a");
    await fake.inbox.archive("b");
    await fake.inbox.unarchive("b");
    await fake.inbox.delete("c");
    await fake.inbox.markAllRead();

    expect(fake.calls).toEqual([
      "list",
      "markRead:a",
      "markUnread:a",
      "archive:b",
      "unarchive:b",
      "delete:c",
      "markAllRead",
    ]);
  });

  it("returns the configured page from list", async () => {
    const fake = new FakeHermesClient(fakePage({ unreadCount: 9, cursor: "c1" }));
    const result = await fake.inbox.list();
    expect(result.unreadCount).toBe(9);
    expect(result.cursor).toBe("c1");
  });

  it("records the options list was called with, so pagination can be asserted", async () => {
    const fake = new FakeHermesClient(fakePage());
    await fake.inbox.list({ limit: 20, archived: false });
    await fake.inbox.list({ limit: 20, cursor: "c1" });
    expect(fake.listOptions).toEqual([
      { limit: 20, archived: false },
      { limit: 20, cursor: "c1" },
    ]);
  });

  it("logs connect and disconnect", async () => {
    const fake = new FakeHermesClient(fakePage());
    await fake.connect("usr_1");
    fake.disconnect();
    expect(fake.calls).toEqual(["connect:usr_1", "disconnect"]);
  });

  it("drops handlers and disconnects on dispose, as the real client does", () => {
    // The fake must carry dispose because consumers call it on teardown; a fake missing it
    // turns a correct teardown path into a TypeError only at runtime.
    const fake = new FakeHermesClient(fakePage());
    fake.on("notification", () => {});

    fake.dispose();

    expect(fake.handlerCount()).toBe(0);
    expect(fake.calls).toContain("disconnect");
  });
});

describe("FakeHermesClient: event delivery", () => {
  it("delivers an emitted event to every registered handler", () => {
    const fake = new FakeHermesClient(fakePage());
    const seen: string[] = [];
    fake.on("notification", () => seen.push("first"));
    fake.on("notification", () => seen.push("second"));

    fake.emit("notification", { type: "notification.new", id: "x" });

    expect(seen).toEqual(["first", "second"]);
  });

  it("passes the payload through untouched", () => {
    const fake = new FakeHermesClient(fakePage());
    let received: unknown;
    fake.on("update", (event) => {
      received = event;
    });
    const payload = { type: "inbox.updated", notificationId: "a", unreadCount: 3 };

    fake.emit("update", payload);

    expect(received).toEqual(payload);
  });

  it("removes exactly one handler when its unsubscribe is called", () => {
    const fake = new FakeHermesClient(fakePage());
    const unsubscribe = fake.on("notification", () => {});
    fake.on("notification", () => {});
    expect(fake.handlerCount()).toBe(2);

    unsubscribe();

    expect(fake.handlerCount()).toBe(1);
  });

  it("counts handlers across all event names", () => {
    const fake = new FakeHermesClient(fakePage());
    fake.on("notification", () => {});
    fake.on("update", () => {});
    fake.on("unreadCountChange", () => {});
    expect(fake.handlerCount()).toBe(3);
  });

  it("emits nothing to a handler registered for a different event", () => {
    const fake = new FakeHermesClient(fakePage());
    let called = false;
    fake.on("update", () => {
      called = true;
    });

    fake.emit("notification", { type: "notification.new", id: "x" });

    expect(called).toBe(false);
  });
});

describe("FakeHermesClient: forced failures", () => {
  it("makes the named method reject with the supplied error", async () => {
    // This is what lets the store suite assert optimistic rollback: without a way to
    // fail a single call, the rejection path is unreachable.
    const fake = new FakeHermesClient(fakePage());
    const error = HermesError.fromStatus("Inbox", 500);
    fake.fail("markRead", error);

    await expect(fake.inbox.markRead("a")).rejects.toBe(error);
  });

  it("leaves other methods working", async () => {
    const fake = new FakeHermesClient(fakePage());
    fake.fail("markRead", HermesError.fromStatus("Inbox", 500));

    await expect(fake.inbox.archive("a")).resolves.toBeUndefined();
  });

  it("still records the attempted call", async () => {
    const fake = new FakeHermesClient(fakePage());
    fake.fail("markRead", HermesError.fromStatus("Inbox", 500));

    await expect(fake.inbox.markRead("a")).rejects.toThrow();

    expect(fake.calls).toContain("markRead:a");
  });

  it("stops failing once the failure is cleared", async () => {
    const fake = new FakeHermesClient(fakePage());
    fake.fail("markRead", HermesError.fromStatus("Inbox", 500));
    fake.clearFailures();

    await expect(fake.inbox.markRead("a")).resolves.toBeUndefined();
  });
});

describe("FakeHermesClient: unread count writes", () => {
  it("records what the store pushed back through setUnreadCount", () => {
    const fake = new FakeHermesClient(fakePage());
    fake.setUnreadCount(4);
    fake.setUnreadCount(3);
    expect(fake.unreadCountWrites).toEqual([4, 3]);
  });

  it("notifies unreadCountChange handlers, as the real client does", () => {
    const fake = new FakeHermesClient(fakePage());
    const counts: number[] = [];
    fake.on("unreadCountChange", (count) => counts.push(count as unknown as number));

    fake.setUnreadCount(4);

    expect(counts).toEqual([4]);
  });

  it("drops a repeated value so subscribers do not re-render for nothing", () => {
    const fake = new FakeHermesClient(fakePage());
    const counts: number[] = [];
    fake.on("unreadCountChange", (count) => counts.push(count as unknown as number));

    fake.setUnreadCount(4);
    fake.setUnreadCount(4);

    expect(counts).toEqual([4]);
    expect(fake.unreadCountWrites).toEqual([4]);
  });
});

describe("fixtures", () => {
  it("builds a notification that is unread by default", () => {
    expect(fakeNotification("a").read_at).toBeUndefined();
  });

  it("lets overrides through", () => {
    expect(fakeNotification("a", { title: "Custom" }).title).toBe("Custom");
  });

  it("builds an empty page by default", () => {
    expect(fakePage()).toMatchObject({ data: [], unreadCount: 0 });
  });
});
