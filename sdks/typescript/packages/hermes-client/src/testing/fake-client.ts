// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import type { HermesClient } from "../client.js";
import type { RealtimeStatus } from "../realtime/connection.js";
import type { InboxPage, Notification } from "../types.js";

/**
 * The inbox surface a consumer can drive. Typing the fake's `inbox` as this Pick of the
 * real `InboxAPI` is what makes the fake self-checking: change an argument or a return
 * type on the real API and every file using this fake stops compiling, instead of
 * leaving a mock that passes against a method nobody has any more.
 */
export type FakeInboxSurface = Pick<
  HermesClient["inbox"],
  "list" | "markRead" | "markUnread" | "archive" | "unarchive" | "delete" | "markAllRead"
>;

/** Methods `fail()` can be pointed at. */
export type FakeInboxMethod = keyof FakeInboxSurface;

/** Options `InboxAPI.list` accepts, recorded so pagination can be asserted. */
export type FakeListOptions = { archived?: boolean; cursor?: string; limit?: number };

/** Build a `Notification` typed against the generated schema. */
export function fakeNotification(
  id: string,
  overrides: Partial<Notification> = {}
): Notification {
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

/** Build an `InboxPage`; empty unless told otherwise. */
export function fakePage(overrides: Partial<InboxPage> = {}): InboxPage {
  return { data: [], unreadCount: 0, ...overrides };
}

/**
 * A hand-written stand-in for `HermesClient`, shipped so that consumers of the SDK —
 * and the SDK's own widget and hooks packages — test against one fake rather than three
 * divergent copies.
 *
 * `asClient()` casts through `unknown` because `HermesClient` carries private fields, so
 * no structural object is ever assignable to it. That cast is a limitation, not a
 * guarantee: the top-level shape is unchecked. What *is* checked is everything
 * underneath — `inbox` against {@link FakeInboxSurface}, and every fixture against the
 * generated `Notification` and `InboxPage`. That last part is the one that matters, since
 * a stale field name in a fixture is exactly the drift that shipped `group_id` after the
 * schema moved to `category_id`.
 */
export class FakeHermesClient {
  /** The page `list()` resolves with. Reassign between calls to simulate pagination. */
  page: InboxPage;

  /** Ordered log of every call, e.g. `["list", "markRead:a"]`. */
  readonly calls: string[] = [];

  /** Every options object `list()` was called with, in order. */
  readonly listOptions: FakeListOptions[] = [];

  /** Every value pushed through `setUnreadCount`, in order. */
  readonly unreadCountWrites: number[] = [];

  private handlers = new Map<string, Set<(event: never) => void>>();
  private failures = new Map<FakeInboxMethod, unknown>();
  private lastUnreadCount = 0;

  constructor(page: InboxPage = fakePage()) {
    this.page = page;
  }

  /** The last count seen, mirroring the real client — `useUnreadCount` reads this. */
  get unreadCount(): number {
    return this.lastUnreadCount;
  }

  private async perform(method: FakeInboxMethod, label: string): Promise<void> {
    this.calls.push(label);
    const failure = this.failures.get(method);
    if (failure) throw failure;
  }

  inbox: FakeInboxSurface = {
    list: async (options?: FakeListOptions): Promise<InboxPage> => {
      this.listOptions.push(options ?? {});
      await this.perform("list", "list");
      return this.page;
    },
    markRead: (id: string) => this.perform("markRead", `markRead:${id}`),
    markUnread: (id: string) => this.perform("markUnread", `markUnread:${id}`),
    archive: (id: string) => this.perform("archive", `archive:${id}`),
    unarchive: (id: string) => this.perform("unarchive", `unarchive:${id}`),
    delete: (id: string) => this.perform("delete", `delete:${id}`),
    markAllRead: () => this.perform("markAllRead", "markAllRead"),
  };

  on(event: string, handler: (event: never) => void): () => void {
    const set = this.handlers.get(event) ?? new Set();
    set.add(handler);
    this.handlers.set(event, set);
    return () => set.delete(handler);
  }

  onStatusChange(handler: (status: RealtimeStatus) => void): () => void {
    return this.on("statusChange", handler as (event: never) => void);
  }

  /** Drive a realtime status transition. */
  emitStatus(status: RealtimeStatus): void {
    this.emit("statusChange", status);
  }

  async connect(userId: string): Promise<void> {
    this.calls.push(`connect:${userId}`);
  }

  disconnect(): void {
    this.calls.push("disconnect");
  }

  /** Mirrors the real client: close the socket and drop every handler. */
  dispose(): void {
    this.disconnect();
    this.handlers.clear();
  }

  setToken(token: string): void {
    this.calls.push(`setToken:${token}`);
  }

  /**
   * Record the count and notify `unreadCountChange` handlers, exactly as the real client
   * does. The fan-out is not incidental: it is how a standalone badge learns the count, so a
   * fake that only recorded the write would let a broken fan-out pass.
   */
  setUnreadCount(count: number): void {
    if (this.lastUnreadCount === count) return;
    this.lastUnreadCount = count;
    this.unreadCountWrites.push(count);
    this.emit("unreadCountChange", count);
  }

  /** Drive a consumer the way the realtime connection would. */
  emit(
    event: "notification" | "update" | "unreadCountChange" | "statusChange",
    payload: unknown
  ): void {
    for (const handler of this.handlers.get(event) ?? []) {
      (handler as (event: unknown) => void)(payload);
    }
  }

  /** Make `method` reject with `error` until {@link clearFailures} is called. */
  fail(method: FakeInboxMethod, error: unknown): void {
    this.failures.set(method, error);
  }

  clearFailures(): void {
    this.failures.clear();
  }

  /** How many handlers are still registered — proves cleanup on teardown. */
  handlerCount(): number {
    let total = 0;
    for (const set of this.handlers.values()) total += set.size;
    return total;
  }

  asClient(): HermesClient {
    return this as unknown as HermesClient;
  }
}
