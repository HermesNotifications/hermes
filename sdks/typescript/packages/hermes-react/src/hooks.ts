// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { useState, useEffect, useRef, useCallback } from "react";
import {
  HermesClient,
  type HermesClientConfig,
  type Notification,
  type InboxPage,
  type NewNotificationEvent,
  type InboxUpdatedEvent,
} from "@hermes-notifications/client";

export function useHermesClient(config: HermesClientConfig): HermesClient | null {
  const clientRef = useRef<HermesClient | null>(null);

  if (!clientRef.current) {
    clientRef.current = new HermesClient(config);
  }

  useEffect(() => {
    const client = clientRef.current;
    return () => {
      client?.disconnect();
    };
  }, []);

  useEffect(() => {
    clientRef.current?.setToken(config.token);
  }, [config.token]);

  return clientRef.current;
}

export function useHermesInbox(
  client: HermesClient | null,
  options?: { userId?: string }
) {
  const [notifications, setNotifications] = useState<Notification[]>([]);
  const [unreadCount, setUnreadCount] = useState(0);
  const [loading, setLoading] = useState(true);
  const [cursor, setCursor] = useState<string | undefined>();

  useEffect(() => {
    if (!client) return;

    let cancelled = false;

    async function load() {
      setLoading(true);
      try {
        const page = await client!.inbox.list({ limit: 20 });
        if (!cancelled) {
          setNotifications(page.data);
          setUnreadCount(page.unreadCount);
          setCursor(page.cursor);
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    }

    load();

    const unsub1 = client.on("notification", (e: NewNotificationEvent) => {
      const notif: Notification = {
        id: e.id,
        title: e.title,
        body: e.body,
        status: "delivered",
        channels: ["inbox"],
        created_at: e.createdAt,
        organization_id: "",
        user_id: "",
        category_id: "",
      };
      setNotifications((prev) => [notif, ...prev]);
      setUnreadCount((c) => c + 1);
    });

    const unsub2 = client.on("update", (e: InboxUpdatedEvent) => {
      setUnreadCount(e.unreadCount);
      if (e.action === "delete") {
        setNotifications((prev) => prev.filter((n) => n.id !== e.notificationId));
      } else if (e.action === "read") {
        setNotifications((prev) =>
          prev.map((n) =>
            n.id === e.notificationId
              ? { ...n, read_at: new Date().toISOString() }
              : n
          )
        );
      } else if (e.action === "read-all") {
        setNotifications((prev) =>
          prev.map((n) => ({ ...n, read_at: n.read_at ?? new Date().toISOString() }))
        );
      }
    });

    if (options?.userId) {
      client.connect(options.userId).catch(console.error);
    }

    return () => {
      cancelled = true;
      unsub1();
      unsub2();
    };
  }, [client, options?.userId]);

  const markRead = useCallback(
    async (id: string) => {
      await client?.inbox.markRead(id);
      setNotifications((prev) =>
        prev.map((n) =>
          n.id === id ? { ...n, read_at: new Date().toISOString() } : n
        )
      );
      setUnreadCount((c) => Math.max(0, c - 1));
    },
    [client]
  );

  const archive = useCallback(
    async (id: string) => {
      await client?.inbox.archive(id);
      setNotifications((prev) => prev.filter((n) => n.id !== id));
    },
    [client]
  );

  const markAllRead = useCallback(async () => {
    await client?.inbox.markAllRead();
    setNotifications((prev) =>
      prev.map((n) => ({ ...n, read_at: n.read_at ?? new Date().toISOString() }))
    );
    setUnreadCount(0);
  }, [client]);

  return { notifications, unreadCount, loading, cursor, markRead, archive, markAllRead };
}

export function useUnreadCount(client: HermesClient | null): number {
  const [count, setCount] = useState(0);

  useEffect(() => {
    if (!client) return;
    return client.on("unreadCountChange", setCount);
  }, [client]);

  return count;
}
