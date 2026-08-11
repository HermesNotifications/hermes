// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { createElement, useEffect, useLayoutEffect, useRef, type CSSProperties } from "react";
import type {
  HermesClient,
  HermesError,
  InboxUpdatedEvent,
  NewNotificationEvent,
  Notification,
  RealtimeStatus,
} from "@hermes-notifications/client";
// Type-only, and it must stay that way. `LitElement extends HTMLElement` is a class expression
// evaluated on import, and Node has no HTMLElement — so any *runtime* reference to this class
// from a module a server component can reach throws on import, before rendering anything. The
// node-environment suite in ssr.test.tsx is what holds this in place.
import type { HermesInbox as HermesInboxElement } from "@hermes-notifications/web";

/**
 * The React binding for `<hermes-inbox>`.
 *
 * There is one implementation of the inbox UI — the custom element — and this wraps it rather
 * than rendering a second markup tree. Two trees would mean two sets of accessibility wiring
 * and two chances to diverge, a bigger duplication than the one this work removed. Teams that
 * want their own markup use `useHermesInbox`, which is the headless path.
 *
 * A wrapper is necessary because React does not treat custom elements well: React 18
 * stringifies every prop, and even React 19 does not connect `on*` props to CustomEvents. In
 * raw JSX, `<hermes-inbox pageSize={20} onNotification={fn}>` would set the attribute to the
 * string `"20"` and never call the handler.
 *
 * ## Why this is hand-written rather than `@lit/react`
 *
 * `createComponent` needs the element class at module scope, and `LitElement extends
 * HTMLElement` — a class expression evaluated on import. Node has no `HTMLElement`, so any
 * module that statically imports the element throws on import, taking down every server
 * component that transitively reaches it. Making registration explicit was necessary but not
 * sufficient; the *class* has to stay out of the server's module graph too.
 *
 * Hence: a type-only import of the element, `createElement("hermes-inbox", …)` for the markup,
 * and a browser-only dynamic import for the implementation. The property and event wiring
 * below is what `createComponent` would have done.
 */

let registration: Promise<void> | undefined;

/**
 * Load and register the custom element, in the browser only.
 *
 * Idempotent, and a resolved no-op on a server. Exported because it is genuinely useful to
 * await: the element upgrades asynchronously, so a caller that needs to touch the DOM element
 * itself — or a test — has something to wait on.
 */
export function ensureHermesInboxRegistered(): Promise<void> {
  if (typeof window === "undefined") return Promise.resolve();
  registration ??= import("@hermes-notifications/web/define").then(() => undefined);
  return registration;
}

export interface HermesInboxProps {
  /** Origin serving the inbox API. Defaults to the page's own origin. */
  apiUrl?: string;
  /** Base URL for the Centrifugo websocket. */
  socketUrl?: string;
  /** A Hermes user JWT. Prefer `tokenUrl` or `getToken`, since tokens expire. */
  token?: string;
  /** Endpoint on your app that mints a token, returning `{ token, expires_at }`. */
  tokenUrl?: string;
  /** Supplies a fresh token on demand. */
  getToken?: () => Promise<string>;
  /** Defaults to the token's `sub` claim, which is what the Centrifugo channel needs. */
  userId?: string;
  pageSize?: number;
  archived?: boolean;
  heading?: string;
  emptyText?: string;
  /** A client you already own — typically from `HermesProvider`. */
  client?: HermesClient;

  className?: string;
  style?: CSSProperties;

  onNotification?: (event: NewNotificationEvent) => void;
  onUpdate?: (event: InboxUpdatedEvent) => void;
  onUnreadCountChange?: (count: number) => void;
  onOpenChange?: (open: boolean) => void;
  onConnected?: () => void;
  /**
   * Every realtime transition, for an accurate connection indicator.
   *
   * Prefer this over `onConnected` for a status display: a UI driven only by the happy path latches
   * to "connected" and then never tells the truth again.
   */
  onRealtimeChange?: (status: RealtimeStatus) => void;
  onError?: (error: HermesError) => void;
  /**
   * A notification carrying an `action_url` was activated. Call `preventDefault()` on the
   * event to stop the browser navigating and route with your own router instead — which is
   * what you want in a single-page app, where a full navigation throws away app state.
   */
  onAction?: (notification: Notification, event: CustomEvent) => void;
  onNotificationClick?: (notification: Notification) => void;
}

/** Props assigned as element properties, in the order the element declares them. */
const ELEMENT_PROPERTIES = [
  "apiUrl",
  "socketUrl",
  "token",
  "tokenUrl",
  "getToken",
  "userId",
  "pageSize",
  "archived",
  "heading",
  "emptyText",
  "client",
] as const satisfies ReadonlyArray<keyof HermesInboxProps>;

export function HermesInbox({
  className,
  style,
  onNotification,
  onUpdate,
  onUnreadCountChange,
  onOpenChange,
  onConnected,
  onRealtimeChange,
  onError,
  onAction,
  onNotificationClick,
  ...properties
}: HermesInboxProps) {
  const ref = useRef<HermesInboxElement | null>(null);

  useEffect(() => {
    void ensureHermesInboxRegistered();
  }, []);

  // Assigned as properties, not attributes, so numbers stay numbers and functions and client
  // instances survive at all. Lit re-applies own properties set before upgrade, so this is
  // safe even though registration resolves asynchronously.
  // Which properties this component has actually assigned. Needed because "not set" and
  // "set back to undefined" are different events with the same value: only the second must
  // clear the element, and only if we were the one who set it.
  const assigned = useRef(new Set<string>());

  useLayoutEffect(() => {
    const element = ref.current;
    if (!element) return;
    const target = element as unknown as Record<string, unknown>;
    for (const name of ELEMENT_PROPERTIES) {
      const value = properties[name];
      if (value !== undefined) {
        target[name] = value;
        assigned.current.add(name);
      } else if (assigned.current.delete(name)) {
        // A prop going value → undefined has to be pushed through, not skipped. Skipping is
        // how a host that signs out — dropping its client, token and userId together —
        // leaves the widget holding the previous user's inbox, still live.
        target[name] = undefined;
      }
    }
  });

  // Listeners live in a ref-read closure so that changing a handler does not detach and
  // reattach every listener on each render.
  const handlers = useRef({
    onNotification,
    onUpdate,
    onUnreadCountChange,
    onOpenChange,
    onConnected,
    onRealtimeChange,
    onError,
    onAction,
    onNotificationClick,
  });
  handlers.current = {
    onNotification,
    onUpdate,
    onUnreadCountChange,
    onOpenChange,
    onConnected,
    onRealtimeChange,
    onError,
    onAction,
    onNotificationClick,
  };

  useEffect(() => {
    const element = ref.current;
    if (!element) return;

    const listeners: Array<[string, EventListener]> = [
      [
        "hermes-notification",
        (event) => handlers.current.onNotification?.((event as CustomEvent).detail),
      ],
      ["hermes-update", (event) => handlers.current.onUpdate?.((event as CustomEvent).detail)],
      [
        "hermes-unread-count-change",
        (event) => handlers.current.onUnreadCountChange?.((event as CustomEvent<number>).detail),
      ],
      [
        "hermes-open-change",
        (event) =>
          handlers.current.onOpenChange?.((event as CustomEvent<{ open: boolean }>).detail.open),
      ],
      ["hermes-connected", () => handlers.current.onConnected?.()],
      [
        "hermes-realtime-change",
        (event) =>
          handlers.current.onRealtimeChange?.(
            (event as CustomEvent<{ status: RealtimeStatus }>).detail.status
          ),
      ],
      ["hermes-error", (event) => handlers.current.onError?.((event as CustomEvent).detail)],
      [
        "hermes-action",
        (event) => {
          const custom = event as CustomEvent<{ notification: Notification }>;
          handlers.current.onAction?.(custom.detail.notification, custom);
        },
      ],
      [
        "hermes-notification-click",
        (event) =>
          handlers.current.onNotificationClick?.(
            (event as CustomEvent<{ notification: Notification }>).detail.notification
          ),
      ],
    ];

    for (const [type, listener] of listeners) element.addEventListener(type, listener);
    return () => {
      for (const [type, listener] of listeners) element.removeEventListener(type, listener);
    };
  }, []);

  return createElement("hermes-inbox", { ref, class: className, style });
}
