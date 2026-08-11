// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { useCallback, useEffect, useMemo, useState, useSyncExternalStore } from "react";
import {
  HermesClient,
  InboxStore,
  initialInboxState,
  type HermesClientConfig,
  type InboxState,
} from "@hermes-notifications/client";

/** Everything `useHermesInbox` returns: the state, plus the actions that change it. */
export interface UseHermesInboxResult extends Readonly<InboxState> {
  markRead(id: string): Promise<void>;
  markUnread(id: string): Promise<void>;
  archive(id: string): Promise<void>;
  unarchive(id: string): Promise<void>;
  remove(id: string): Promise<void>;
  markAllRead(): Promise<void>;
  loadMore(): Promise<void>;
  refresh(): Promise<void>;
  clearError(): void;
}

export interface UseHermesInboxOptions {
  /** Defaults to the token's `sub` claim, which is what the Centrifugo channel needs. */
  userId?: string;
  pageSize?: number;
  archived?: boolean;
  /** Injected clock, so optimistic timestamps are exact in tests. */
  now?: () => string;
}

/**
 * Create and own a {@link HermesClient} for the lifetime of the component.
 *
 * Construction happens in a `useState` initializer rather than the render body. React's
 * StrictMode double-invokes initializers and discards one result, which is harmless here only
 * because constructing a client opens no socket — a property the client's own suite asserts,
 * so it stays true.
 *
 * The client is rebuilt if `apiUrl` or `socketUrl` change, and `token` changes are pushed in
 * without a rebuild.
 */
export function useHermesClient(config: HermesClientConfig): HermesClient {
  const identity = `${config.apiUrl}\u0000${config.socketUrl ?? ""}`;

  const [current, setCurrent] = useState(() => ({
    client: new HermesClient(config),
    identity,
  }));

  useEffect(() => {
    if (current.identity === identity) return;
    current.client.dispose();
    setCurrent({ client: new HermesClient(config), identity });
    // `config` is read only when the identity changes; including it here would rebuild the
    // client on every render for callers passing an inline object literal.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [identity, current]);

  useEffect(() => {
    current.client.setToken(config.token);
  }, [current.client, config.token]);

  useEffect(() => {
    const { client } = current;
    return () => {
      client.dispose();
    };
  }, [current]);

  return current.client;
}

const NOOP_SUBSCRIBE = () => () => {};

/**
 * Drive an inbox: the current page, live updates, and the actions that change it.
 *
 * A thin adapter over the client's `InboxStore` — the state logic lives there, shared with the
 * `<hermes-inbox>` custom element, rather than being reimplemented here.
 *
 * The store is created per mount and disposed on unmount, so a component that goes away stops
 * reacting to the socket immediately.
 */
export function useHermesInbox(
  client: HermesClient | null,
  options: UseHermesInboxOptions = {}
): UseHermesInboxResult {
  const { userId, pageSize, archived, now } = options;

  const store = useMemo(() => {
    if (!client) return null;
    return new InboxStore({
      client,
      ...(userId ? { userId } : {}),
      ...(pageSize !== undefined ? { pageSize } : {}),
      ...(archived !== undefined ? { archived } : {}),
      ...(now ? { now } : {}),
    });
    // `now` is deliberately excluded: it is a clock, and an inline arrow would otherwise
    // rebuild the store on every render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [client, userId, pageSize, archived]);

  useEffect(() => {
    if (!store) return;
    void store.start();
    // `stop`, not `dispose`. StrictMode runs effect -> cleanup -> effect on the same memoized store,
    // and a disposed store refuses to restart — which would leave the inbox permanently inert in
    // every StrictMode app, loading nothing and receiving no realtime updates. `stop` closes the
    // socket and unsubscribes while remaining restartable; the store is garbage once the component
    // is gone.
    return () => store.stop();
  }, [store]);

  const subscribe = useCallback(
    (listener: () => void) => (store ? store.subscribe(listener) : NOOP_SUBSCRIBE()),
    [store]
  );
  const getSnapshot = useCallback(
    () => store?.getSnapshot() ?? initialInboxState,
    [store]
  );
  const getServerSnapshot = useCallback(() => initialInboxState, []);

  const state = useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);

  const actions = useMemo(
    () => ({
      markRead: (id: string) => store?.markRead(id) ?? Promise.resolve(),
      markUnread: (id: string) => store?.markUnread(id) ?? Promise.resolve(),
      archive: (id: string) => store?.archive(id) ?? Promise.resolve(),
      unarchive: (id: string) => store?.unarchive(id) ?? Promise.resolve(),
      remove: (id: string) => store?.remove(id) ?? Promise.resolve(),
      markAllRead: () => store?.markAllRead() ?? Promise.resolve(),
      loadMore: () => store?.loadMore() ?? Promise.resolve(),
      refresh: () => store?.refresh() ?? Promise.resolve(),
      clearError: () => store?.clearError(),
    }),
    [store]
  );

  return { ...state, ...actions };
}

/**
 * Just the unread count — for a badge rendered somewhere other than the inbox itself.
 *
 * Correct from the first load, not only after the user's first mutation, because the store
 * pushes every count change back through the client.
 */
export function useUnreadCount(client: HermesClient | null): number {
  const subscribe = useCallback(
    (listener: () => void) =>
      client ? client.on("unreadCountChange", () => listener()) : NOOP_SUBSCRIBE(),
    [client]
  );
  const getSnapshot = useCallback(() => client?.unreadCount ?? 0, [client]);
  const getServerSnapshot = useCallback(() => 0, []);

  return useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}
