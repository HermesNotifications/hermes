// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { Centrifuge } from "centrifuge";
import type { InboxUpdatedEvent, NewNotificationEvent, HermesEvent } from "../types.js";

export type EventHandler = (event: HermesEvent) => void;

/**
 * Whether publications will actually reach us.
 *
 * `connected` means the channel subscription is established, not merely that the socket
 * is open — those are different moments, and only the former guarantees a publication
 * will be delivered. Anything waiting for the inbox to be live (a test, or an
 * integrator's loading state) must gate on `connected`.
 */
export type RealtimeStatus = "disconnected" | "connecting" | "connected";

export type StatusHandler = (status: RealtimeStatus) => void;

/** The slice of centrifuge's `Subscription` this connection drives. */
export interface RealtimeSubscriptionLike {
  on(event: string, handler: (ctx: never) => void): unknown;
  subscribe(): void;
  unsubscribe(): void;
}

/** The slice of centrifuge's `Centrifuge` this connection drives. */
export interface RealtimeTransportLike {
  on(event: string, handler: (ctx: never) => void): unknown;
  newSubscription(
    channel: string,
    options?: Record<string, unknown>
  ): RealtimeSubscriptionLike;
  removeSubscription(sub: RealtimeSubscriptionLike | null): void;
  connect(): void;
  disconnect(): void;
}

export type TransportFactory = (
  endpoint: string,
  options: Record<string, unknown>
) => RealtimeTransportLike;

/**
 * The production transport. The cast is because centrifuge types `on` through a typed
 * event-emitter keyed by its own event names, which no structural interface can
 * express — it is confined to this one function so the rest of the file stays testable.
 */
const centrifugeFactory: TransportFactory = (endpoint, options) =>
  new Centrifuge(endpoint, options) as unknown as RealtimeTransportLike;

/** Turn an HTTP(S) base URL into the Centrifugo websocket endpoint. */
function websocketEndpoint(socketUrl: string): string {
  const wsUrl = socketUrl.replace(/^http:/, "ws:").replace(/^https:/, "wss:");
  return wsUrl.endsWith("/connection/websocket")
    ? wsUrl
    : `${wsUrl}/connection/websocket`;
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  return typeof value === "object" && value !== null
    ? (value as Record<string, unknown>)
    : undefined;
}

/**
 * Translate a Centrifugo publication into a Hermes event, or `undefined` if it is not
 * one we understand.
 *
 * Returning `undefined` for an unknown `type` matters: the previous implementation
 * treated everything that was not `inbox.updated` as a `notification.new`, so a purely
 * additive server-side event type would have surfaced in the user's inbox as a row with
 * an undefined title and body.
 */
export function eventFromPublication(data: unknown): HermesEvent | undefined {
  const payload = asRecord(data);
  if (!payload) return undefined;

  switch (payload.type) {
    case "inbox.updated": {
      const event: InboxUpdatedEvent = {
        type: "inbox.updated",
        notificationId: payload.notification_id as string,
        action: payload.action as InboxUpdatedEvent["action"],
        unreadCount: payload.unread_count as number,
        timestamp: payload.timestamp as number,
      };
      return event;
    }
    case "notification.new": {
      const action = asRecord(payload.action);
      const event: NewNotificationEvent = {
        type: "notification.new",
        id: payload.id as string,
        title: payload.title as string,
        body: payload.body as string,
        createdAt: (payload.created_at as string) ?? new Date().toISOString(),
        ...(typeof action?.url === "string" ? { actionUrl: action.url } : {}),
        ...(typeof action?.label === "string" ? { actionLabel: action.label } : {}),
      };
      return event;
    }
    default:
      return undefined;
  }
}

/**
 * A Centrifugo connection scoped to one user's channel.
 *
 * The transport is injected so that the wire-format mapping and the connection
 * lifecycle can be driven without opening a socket.
 */
export class RealtimeConnection {
  private transport: RealtimeTransportLike | null = null;
  private subscription: RealtimeSubscriptionLike | null = null;
  private handlers: EventHandler[] = [];
  private statusHandlers: StatusHandler[] = [];
  private currentStatus: RealtimeStatus = "disconnected";
  private currentUserId: string | null = null;
  /**
   * Serializes connect attempts. `connect()` has to await a token before it can build a
   * transport, and without this queue two calls landing in the same tick would each get
   * past the "already connected" guard — the second overwriting `this.transport` and
   * orphaning the first, which no `disconnect()` could then reach.
   */
  private queue: Promise<unknown> = Promise.resolve();

  constructor(
    private readonly socketUrl: string,
    private readonly getToken: () => string | Promise<string>,
    private readonly factory: TransportFactory = centrifugeFactory
  ) {}

  get status(): RealtimeStatus {
    return this.currentStatus;
  }

  on(handler: EventHandler): () => void {
    this.handlers.push(handler);
    return () => {
      this.handlers = this.handlers.filter((h) => h !== handler);
    };
  }

  onStatusChange(handler: StatusHandler): () => void {
    this.statusHandlers.push(handler);
    return () => {
      this.statusHandlers = this.statusHandlers.filter((h) => h !== handler);
    };
  }

  /** How many event handlers are registered. Exposed for teardown assertions. */
  handlerCount(): number {
    return this.handlers.length + this.statusHandlers.length;
  }

  private emit(event: HermesEvent): void {
    for (const handler of this.handlers) handler(event);
  }

  private setStatus(status: RealtimeStatus): void {
    if (this.currentStatus === status) return;
    this.currentStatus = status;
    for (const handler of this.statusHandlers) handler(status);
  }

  /**
   * Subscribe to `userId`'s channel, replacing any existing subscription.
   *
   * Reconnecting as the same user is a no-op. Connecting as a *different* user tears the
   * old socket down first — without that, switching users leaves the client listening on
   * the previous channel while REST returns the new user's rows, so the inbox loads once
   * and then never updates.
   *
   * Calls are serialized, so concurrent callers — two stores over one shared client, or
   * StrictMode's effect/cleanup/effect — resolve to a single socket. The no-op check runs
   * inside the queue, once any earlier attempt has settled and `transport` reflects it.
   */
  connect(userId: string): Promise<void> {
    const attempt = this.queue.then(
      () => this.openConnection(userId),
      () => this.openConnection(userId)
    );
    // Swallow on the queue only; `attempt` still rejects for the caller that asked.
    this.queue = attempt.catch(() => undefined);
    return attempt;
  }

  private async openConnection(userId: string): Promise<void> {
    if (this.transport && this.currentUserId === userId) return;
    if (this.transport) this.teardown();

    this.currentUserId = userId;
    this.setStatus("connecting");

    const transport = this.factory(websocketEndpoint(this.socketUrl), {
      token: await this.getToken(),
      // Centrifuge calls this on reconnect, so a socket that outlives its token
      // re-authenticates instead of dropping permanently.
      getToken: async () => await this.getToken(),
    });
    this.transport = transport;

    transport.on("error", ((ctx: unknown) => {
      console.error("Hermes realtime error:", ctx);
    }) as (ctx: never) => void);

    // recoverable + positioned ask Centrifugo to replay anything published while the
    // socket was down, using the history it is already configured to keep.
    const subscription = transport.newSubscription(`user#${userId}`, {
      recoverable: true,
      positioned: true,
    });
    this.subscription = subscription;

    subscription.on("publication", ((ctx: { data: unknown }) => {
      const event = eventFromPublication(ctx.data);
      if (event) this.emit(event);
    }) as (ctx: never) => void);

    subscription.on("subscribed", (() => this.setStatus("connected")) as (ctx: never) => void);
    subscription.on("unsubscribed", (() => {
      if (this.transport) this.setStatus("connecting");
    }) as (ctx: never) => void);
    subscription.on("error", ((ctx: unknown) => {
      console.error("Hermes realtime subscription error:", ctx);
    }) as (ctx: never) => void);

    subscription.subscribe();
    transport.connect();
  }

  private teardown(): void {
    if (this.subscription) {
      this.subscription.unsubscribe();
      this.transport?.removeSubscription(this.subscription);
      this.subscription = null;
    }
    this.transport?.disconnect();
    this.transport = null;
    this.currentUserId = null;
  }

  /**
   * Close the socket, keeping event handlers registered.
   *
   * Handlers belong to the owning `HermesClient`, not to one socket, so a
   * `disconnect()` / `connect()` cycle must keep delivering. Use {@link dispose} to drop
   * them.
   */
  disconnect(): void {
    this.teardown();
    this.setStatus("disconnected");
  }

  /** Close the socket and drop every handler. */
  dispose(): void {
    this.disconnect();
    this.handlers = [];
    this.statusHandlers = [];
  }
}
