// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { useEffect, useRef } from "react";
import {
  notificationFromEvent,
  notificationLevel,
  toastRequested,
  type HermesClient,
  type NewNotificationEvent,
  type Notification,
  type NotificationLevel,
} from "@hermes-notifications/client";

/**
 * Whatever the adapter's own library returns from showing a toast. Never inspected by Hermes;
 * it exists only so it can be handed back to `dismiss`.
 */
export type HermesToastHandle = unknown;

/** Everything an adapter might want in order to render one toast. */
export interface HermesToastPayload {
  /** The notification id. Also the dedupe key, and a stable id for the toast itself. */
  id: string;
  title: string;
  body: string;
  /** Undefined when `metadata.level` was absent or is a value this client does not know. */
  level: NotificationLevel | undefined;
  /** Whether the sender set `metadata.toast`. */
  toastRequested: boolean;
  /** The row exactly as the inbox reducer will insert it — the same object the list renders. */
  notification: Notification;
  /** The raw wire event, for anything the fields above do not cover. */
  event: NewNotificationEvent;
}

/**
 * The seam between Hermes and whatever renders your toasts.
 *
 * A plain object, so swapping providers is passing a different one — there is no registration
 * step and no Hermes-owned toast UI. `@hermes-notifications/react/sonner` ships one for Sonner;
 * writing your own means implementing these five methods.
 *
 * The four levels and `show` are all required rather than optional. If they were optional the
 * hook would have to own the fallback logic, and "what does a warning look like?" would then
 * depend on which methods an adapter happened to define — exactly the per-consumer divergence
 * ADR 0013 exists to prevent.
 */
export interface HermesToastAdapter {
  info(payload: HermesToastPayload): HermesToastHandle;
  success(payload: HermesToastPayload): HermesToastHandle;
  warning(payload: HermesToastPayload): HermesToastHandle;
  error(payload: HermesToastPayload): HermesToastHandle;
  /** No level, or a level this client does not recognise. The default path, not an edge case. */
  show(payload: HermesToastPayload): HermesToastHandle;
  /** Optional: not every toast library can retract one it has already shown. */
  dismiss?(handle: HermesToastHandle): void;
}

export interface UseHermesToastsOptions {
  /** Where toasts go. Required: there is no default, because Hermes renders no toast UI. */
  toast: HermesToastAdapter;
  /** Master switch, for a user preference. Defaults to true. */
  enabled?: boolean;
  /**
   * Which arrivals toast. **Replaces** the default gate rather than layering on it — the
   * default is `(payload) => payload.toastRequested`, and `toastRequested` stays on the payload
   * so a custom predicate can still consult it.
   *
   * This is also how you suppress toasts while the panel is open: track `onOpenChange` on
   * `<HermesInbox>` and read it here. The hook cannot do that for you — a headless host has no
   * panel, and the element's open state is not on the client.
   */
  shouldToast?: (payload: HermesToastPayload) => boolean;
  /**
   * Retract a toast when its notification is read, archived or deleted — including from another
   * device. Requires the adapter to implement `dismiss`. Defaults to false.
   */
  dismissOnRead?: boolean;
  /**
   * `"client"` (default) shares the seen-id set across every hook instance driving the same
   * client, so two mounted consumers toast once between them. `"hook"` gives each instance its
   * own, for a host deliberately running two independent toast surfaces.
   */
  dedupeScope?: "client" | "hook";
  /** How many ids to remember. Defaults to 200. */
  dedupeSize?: number;
}

interface SeenIds {
  ids: Set<string>;
  order: string[];
}

/**
 * Ids already toasted, per client.
 *
 * Keyed on the client and weak so that a client discarded on sign-out takes its ids with it;
 * a module-level `Map` would keep both alive for the life of the page. Per *client* rather than
 * per hook because the case worth defending against is two components calling this hook against
 * one provider's client: `client.on` registers both handlers and both fire.
 */
const SEEN_BY_CLIENT = new WeakMap<object, SeenIds>();

function remember(seen: SeenIds, id: string, limit: number): boolean {
  if (seen.ids.has(id)) return false;
  seen.ids.add(id);
  seen.order.push(id);
  while (seen.order.length > limit) {
    const evicted = seen.order.shift();
    if (evicted !== undefined) seen.ids.delete(evicted);
  }
  return true;
}

/**
 * Show a toast for arriving notifications that ask for one.
 *
 * Only *live* arrivals toast. `client.on("notification")` fires for websocket publications, not
 * for the initial list and not for the REST repair after a reconnect — so opening the page, or
 * waking a laptop that missed forty notifications, produces no burst of toasts. That is
 * deliberate and worth knowing, because it is the behaviour people assume is broken.
 */
export function useHermesToasts(
  client: HermesClient | null,
  options: UseHermesToastsOptions
): void {
  // Read through a ref so that changing an inline `shouldToast` on every render does not tear
  // down and re-register the subscription. Same pattern as the event wiring in HermesInbox.
  const latest = useRef(options);
  latest.current = options;

  const localSeen = useRef<SeenIds>({ ids: new Set(), order: [] });
  const handles = useRef(new Map<string, HermesToastHandle>());

  useEffect(() => {
    if (!client) return;

    const seenFor = (): SeenIds => {
      if (latest.current.dedupeScope === "hook") return localSeen.current;
      let seen = SEEN_BY_CLIENT.get(client);
      if (!seen) {
        seen = { ids: new Set(), order: [] };
        SEEN_BY_CLIENT.set(client, seen);
      }
      return seen;
    };

    const unsubscribeNotification = client.on("notification", (event: NewNotificationEvent) => {
      const {
        toast,
        enabled = true,
        shouldToast,
        dedupeSize = 200,
        dismissOnRead = false,
      } = latest.current;

      const payload: HermesToastPayload = {
        id: event.id,
        title: event.title,
        body: event.body,
        level: notificationLevel(event),
        toastRequested: toastRequested(event),
        // Built from the same function the reducer uses, so the toast and the row it
        // corresponds to can never disagree about the notification's shape.
        notification: notificationFromEvent(event),
        event,
      };

      if (!enabled) return;
      if (shouldToast ? !shouldToast(payload) : !payload.toastRequested) return;

      // Claimed before the adapter is called, so a re-entrant adapter cannot double-fire.
      if (!remember(seenFor(), event.id, dedupeSize)) return;

      // An unknown level lands on `show` rather than being dropped: the sender explicitly asked
      // to interrupt, and silently discarding that because of one unrecognised string would be
      // the worse failure.
      const handle = payload.level ? toast[payload.level](payload) : toast.show(payload);
      if (dismissOnRead && toast.dismiss) handles.current.set(event.id, handle);
    });

    const unsubscribeUpdate = client.on(
      "update",
      (event: { notificationId: string; action: string }) => {
        const { toast, dismissOnRead = false } = latest.current;
        if (!dismissOnRead || !toast.dismiss) return;

        if (event.action === "read-all") {
          for (const handle of handles.current.values()) toast.dismiss(handle);
          handles.current.clear();
          return;
        }
        if (event.action === "read" || event.action === "archive" || event.action === "delete") {
          const handle = handles.current.get(event.notificationId);
          if (handle !== undefined) {
            toast.dismiss(handle);
            handles.current.delete(event.notificationId);
          }
        }
      }
    );

    return () => {
      unsubscribeNotification();
      unsubscribeUpdate();
    };
  }, [client]);
}
