// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import type { HermesClient } from "../client.js";
import { HermesError } from "../errors.js";
import type { InboxUpdatedEvent, NewNotificationEvent } from "../types.js";
import {
  inboxReducer,
  initialInboxState,
  type InboxAction,
  type InboxState,
} from "./state.js";

export interface InboxStoreOptions {
  client: HermesClient;
  /**
   * Whose inbox to subscribe to for realtime. Omit to let the client read the internal id
   * from the token's `sub` claim, which is what the Centrifugo channel needs.
   */
  userId?: string;
  /** Rows per page. Defaults to 20; the API caps it at 100. */
  pageSize?: number;
  /** Show archived rows instead of active ones. Defaults to false. */
  archived?: boolean;
  /** Injected clock, so timestamps in tests are exact values. */
  now?: () => string;
}

/**
 * Drives an inbox: fetches pages, applies realtime events, and performs mutations
 * optimistically with rollback.
 *
 * `subscribe` / `getSnapshot` deliberately match React's `useSyncExternalStore` contract,
 * and the reducer's immutable snapshots satisfy its identity requirement for free — so the
 * React hooks are a thin adapter rather than a second implementation.
 */
export class InboxStore {
  private state: InboxState = initialInboxState;
  private listeners = new Set<() => void>();
  private cleanups: Array<() => void> = [];
  private disposed = false;

  /**
   * Incremented on every load. A response tagged with a stale generation is dropped, so
   * two racing loads cannot have the loser resolve last and clobber the winner.
   */
  private generation = 0;

  private readonly client: HermesClient;
  private readonly userId?: string;
  private readonly pageSize: number;
  private readonly archived: boolean;
  private readonly now: () => string;

  constructor(options: InboxStoreOptions) {
    this.client = options.client;
    this.userId = options.userId;
    this.pageSize = options.pageSize ?? 20;
    this.archived = options.archived ?? false;
    this.now = options.now ?? (() => new Date().toISOString());
  }

  getSnapshot(): InboxState {
    return this.state;
  }

  /**
   * The snapshot to render on a server, where there is no client and no socket. Always the
   * initial state, and stable, because `useSyncExternalStore` requires a server snapshot
   * that never changes between calls.
   */
  getServerSnapshot(): InboxState {
    return initialInboxState;
  }

  subscribe(listener: () => void): () => void {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  }

  private dispatch(action: InboxAction): void {
    const next = inboxReducer(this.state, action);
    if (next === this.state) return;

    const countChanged = next.unreadCount !== this.state.unreadCount;
    this.state = next;

    // One writer for the unread count. This is what keeps a standalone badge and the
    // widget from diverging during optimistic updates.
    if (countChanged) this.client.setUnreadCount(next.unreadCount);

    for (const listener of this.listeners) listener();
  }

  /** Load the first page, subscribe to realtime, and connect. */
  async start(): Promise<void> {
    if (this.disposed) return;

    this.cleanups.push(
      this.client.on("notification", (event: NewNotificationEvent) => {
        this.dispatch({ type: "realtime/notification", event });
      })
    );
    this.cleanups.push(
      this.client.on("update", (event: InboxUpdatedEvent) => {
        this.dispatch({ type: "realtime/update", event, at: this.now() });
      })
    );
    this.cleanups.push(
      this.client.onStatusChange((status) => {
        this.dispatch({ type: "realtime/status", status });
      })
    );

    await this.refresh();

    try {
      await this.client.connect(this.userId);
    } catch (cause) {
      // A dead socket must not take the inbox down with it: the list still works, it
      // just will not update on its own. Surfacing the status is how a UI can say so.
      console.error("Hermes: realtime connection failed:", cause);
      this.dispatch({ type: "realtime/status", status: "disconnected" });
    }
  }

  /** Reload the first page, discarding any cursor. */
  async refresh(): Promise<void> {
    if (this.disposed) return;
    const generation = ++this.generation;

    this.dispatch({ type: "load/start" });
    try {
      const page = await this.client.inbox.list({
        limit: this.pageSize,
        archived: this.archived,
      });
      if (this.disposed || generation !== this.generation) return;
      this.dispatch({ type: "load/success", page });
    } catch (cause) {
      if (this.disposed || generation !== this.generation) return;
      this.dispatch({ type: "load/failure", error: this.asHermesError(cause) });
    }
  }

  /** Fetch and append the next page. */
  async loadMore(): Promise<void> {
    if (this.disposed) return;
    const { cursor, hasMore, loadingMore } = this.state;
    if (!hasMore || !cursor || loadingMore) return;

    const generation = this.generation;
    this.dispatch({ type: "page/start" });
    try {
      const page = await this.client.inbox.list({ limit: this.pageSize, archived: this.archived, cursor });
      if (this.disposed || generation !== this.generation) return;
      this.dispatch({ type: "page/success", page });
    } catch (cause) {
      if (this.disposed || generation !== this.generation) return;
      const error = this.asHermesError(cause);
      this.dispatch({ type: "page/failure", error });
      // A cursor the server no longer recognises is recoverable: drop it and start over,
      // which is the documented client contract for a switched store backend.
      if (error.kind === "invalid-cursor") await this.refresh();
    }
  }

  /**
   * Apply `action` immediately, then confirm it with the server, restoring the previous
   * state if the server refuses.
   *
   * Deliberately never rejects: a click handler has nowhere to put a rejection, so the
   * error goes into state where the UI can render it.
   */
  private async mutate(action: InboxAction, call: () => Promise<void>): Promise<void> {
    if (this.disposed) return;
    const snapshot = this.state;
    this.dispatch(action);
    try {
      await call();
    } catch (cause) {
      if (this.disposed) return;
      this.dispatch({ type: "rollback", state: snapshot, error: this.asHermesError(cause) });
    }
  }

  markRead(id: string): Promise<void> {
    return this.mutate({ type: "optimistic/read", id, at: this.now() }, () =>
      this.client.inbox.markRead(id)
    );
  }

  markUnread(id: string): Promise<void> {
    return this.mutate({ type: "optimistic/unread", id }, () =>
      this.client.inbox.markUnread(id)
    );
  }

  archive(id: string): Promise<void> {
    return this.mutate({ type: "optimistic/archive", id }, () =>
      this.client.inbox.archive(id)
    );
  }

  unarchive(id: string): Promise<void> {
    return this.mutate({ type: "optimistic/remove", id }, () =>
      this.client.inbox.unarchive(id)
    );
  }

  remove(id: string): Promise<void> {
    return this.mutate({ type: "optimistic/remove", id }, () =>
      this.client.inbox.delete(id)
    );
  }

  markAllRead(): Promise<void> {
    return this.mutate({ type: "optimistic/readAll", at: this.now() }, () =>
      this.client.inbox.markAllRead()
    );
  }

  clearError(): void {
    this.dispatch({ type: "error/clear" });
  }

  /** Unsubscribe from the client, close the socket, and stop accepting work. */
  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    for (const cleanup of this.cleanups) cleanup();
    this.cleanups = [];
    this.listeners.clear();
    this.client.disconnect();
  }

  private asHermesError(cause: unknown): HermesError {
    return cause instanceof HermesError
      ? cause
      : new HermesError("Inbox operation failed", "network", { cause });
  }
}
