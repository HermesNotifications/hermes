// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

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

/** One rung of the transport ladder. */
export interface TransportEndpoint {
  transport: "websocket" | "http_stream" | "sse";
  endpoint: string;
}

export type TransportFactory = (
  endpoints: TransportEndpoint[],
  options: Record<string, unknown>
) => RealtimeTransportLike;

/**
 * The production transport. The cast is because centrifuge types `on` through a typed
 * event-emitter keyed by its own event names, which no structural interface can
 * express — it is confined to this one function so the rest of the file stays testable.
 */
const centrifugeFactory: TransportFactory = (endpoints, options) =>
  new Centrifuge(endpoints, options) as unknown as RealtimeTransportLike;

/**
 * Derive the transport ladder from one base URL.
 *
 * The client tries these in order and stops at the first that connects, so a healthy
 * network behaves exactly as it did when this was websocket-only. The rungs below exist
 * for networks that break websockets specifically — TLS-intercepting corporate proxies,
 * and middleboxes that strip the `Upgrade` header. Without them such a client loaded its
 * inbox once over REST and then went permanently stale, with no error anywhere: the
 * socket never opens, so no reconnect ever fires, so nothing ever reconciles.
 *
 * `http_stream` sits above `sse` deliberately. Both carry the same emulation layer, but
 * SSE goes through `EventSource`, which under HTTP/1.1 competes for the browser's six
 * connections per origin — a real constraint when the page shares an origin with the API.
 *
 * There is no long-polling rung. Centrifugo's only long-polling transport was SockJS,
 * removed in v6; the emulation layer used here replaces it and, unlike SockJS, needs no
 * sticky sessions, which is what makes it safe behind our round-robin ingress.
 *
 * All three are derived from a single `socketUrl` rather than configured separately, so
 * the public contract stays one URL. Both scheme directions are normalized because the
 * caller may hand us either — `ws://…` is accepted, and the HTTP rungs cannot use it.
 */
export function transportEndpoints(socketUrl: string): TransportEndpoint[] {
  const base = socketUrl.replace(/\/connection\/websocket$/, "");
  const ws = base.replace(/^http:/, "ws:").replace(/^https:/, "wss:");
  const http = httpBase(socketUrl);

  return [
    { transport: "websocket", endpoint: `${ws}/connection/websocket` },
    { transport: "http_stream", endpoint: `${http}/connection/http_stream` },
    { transport: "sse", endpoint: `${http}/connection/sse` },
  ];
}

/**
 * Where the fallback transports send their client→server commands.
 *
 * Both HTTP rungs are unidirectional on their own — the stream only flows server→client — so every
 * command the client issues, including the `subscribe` without which no publication is ever
 * delivered, goes out as a separate POST to this endpoint.
 *
 * It must be absolute, and that is the whole reason this function exists. centrifuge-js defaults
 * `emulationEndpoint` to the *path* `/emulation`, which the browser resolves against the page's own
 * origin. Hermes is cross-origin by construction — the widget is embedded in the customer's app
 * while Centrifugo lives on the Hermes domain — so the default aims every command at the customer's
 * own server, which answers 404. The transport then connects, streams nothing, and retries forever.
 *
 * That failure is invisible on a healthy network, because the websocket is bidirectional and never
 * uses this endpoint at all. It surfaces only once a client has fallen down the ladder, which is
 * exactly the population the ladder exists to serve. Caught by `tests/browser/transport-ladder`.
 */
export function emulationEndpoint(socketUrl: string): string {
  return `${httpBase(socketUrl)}/emulation`;
}

/** The realtime base URL, normalized to http(s) and stripped of any websocket path. */
function httpBase(socketUrl: string): string {
  return socketUrl
    .replace(/\/connection\/websocket$/, "")
    .replace(/^ws:/, "http:")
    .replace(/^wss:/, "https:");
}

/**
 * Narrow a value to a plain JSON object.
 *
 * The array check is load-bearing, not defensive: `typeof [] === "object"` and an array is not
 * null, so without it a JSON array would be handed back as a "record" and every property read
 * on it would silently yield undefined.
 */
function asRecord(value: unknown): Record<string, unknown> | undefined {
  return typeof value === "object" && value !== null && !Array.isArray(value)
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
      const metadata = asRecord(payload.metadata);
      const event: NewNotificationEvent = {
        type: "notification.new",
        id: payload.id as string,
        title: payload.title as string,
        body: payload.body as string,
        createdAt: (payload.created_at as string) ?? new Date().toISOString(),
        ...(typeof action?.url === "string" ? { actionUrl: action.url } : {}),
        ...(typeof action?.label === "string" ? { actionLabel: action.label } : {}),
        // Only when the server actually sent a number. Coercing an absent field to 0 here would
        // be worse than having no count at all: the reducer trusts a present count absolutely,
        // so a fabricated zero would blank the badge on every arrival.
        ...(typeof payload.unread_count === "number" ? { unreadCount: payload.unread_count } : {}),
        // Absent stays absent, like every field above. A `metadata: {}` invented here would be
        // indistinguishable to a consumer from a sender who supplied an empty object.
        ...(metadata ? { metadata: metadata as NewNotificationEvent["metadata"] } : {}),
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
  /** Set by {@link dispose}. Terminal: a disposed connection never reopens. */
  private disposed = false;
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

  /**
   * Resolve once the channel is subscribed, or when `timeoutMs` elapses — reporting which.
   *
   * `connect()` cannot serve this purpose: it resolves as soon as the transport has been told
   * to connect, which is several round trips before Centrifugo confirms the subscription. Until
   * that confirmation the channel does not exist server-side and anything published to it is
   * discarded, so "connect resolved" is not the same question as "is it safe to stop worrying
   * about missing a publication".
   *
   * Resolves `false` rather than rejecting on timeout: the callers are ones that want to get on
   * with their work regardless, and a rejection would force each of them to write the same
   * try/catch to ignore it.
   */
  waitUntilConnected(timeoutMs: number): Promise<boolean> {
    if (this.currentStatus === "connected") return Promise.resolve(true);

    return new Promise<boolean>((resolve) => {
      let settle = (result: boolean) => {
        settle = () => {};
        clearTimeout(timer);
        unsubscribe();
        resolve(result);
      };
      const timer = setTimeout(() => settle(false), timeoutMs);
      // Node keeps the process alive for a pending timer, so this wait could be the reason an
      // SSR render or a script refuses to exit. Browsers type setTimeout as returning a number
      // and have no unref, hence the guarded cast rather than a Node-typed timer.
      (timer as unknown as { unref?: () => void }).unref?.();
      const unsubscribe = this.onStatusChange((status) => {
        if (status === "connected") settle(true);
      });
    });
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
    if (this.disposed) throw new Error("Hermes: this realtime connection has been disposed");
    if (this.transport && this.currentUserId === userId) return;
    if (this.transport) this.teardown();

    this.currentUserId = userId;
    this.setStatus("connecting");

    const token = await this.getToken();

    // Re-checked after the await, and this is the whole point of the disposed flag rather than
    // a doc comment. dispose() is synchronous, so it can land while the token is in flight —
    // between a caller asking to connect and the transport existing. Without this check the
    // connection then opens, subscribes and starts receiving publications *after* dispose()
    // emptied the handler lists: a live socket delivering to nobody, which is precisely the
    // zombie dispose() is supposed to rule out. Silent, and it looks healthy from every angle.
    if (this.disposed) return;

    const transport = this.factory(transportEndpoints(this.socketUrl), {
      token,
      // Centrifuge calls this on reconnect, so a socket that outlives its token
      // re-authenticates instead of dropping permanently.
      getToken: async () => await this.getToken(),
      // Absolute, not the library's page-relative default. See emulationEndpoint().
      emulationEndpoint: emulationEndpoint(this.socketUrl),
    });
    this.transport = transport;

    transport.on("error", ((ctx: unknown) => {
      console.error("Hermes realtime error:", ctx);
    }) as (ctx: never) => void);

    // Deliberately NOT recoverable/positioned.
    //
    // Those options ask Centrifugo to replay publications missed while the socket was down.
    // This deployment runs `broker: nats` (deploy/k8s/base/infra/centrifugo.yaml), and the
    // NATS broker is at-most-once with no history — so there is nothing to replay from and
    // the options are inert. The `history_size` and `history_ttl` settings alongside it are
    // inert for the same reason. Requesting a guarantee the server cannot honour is worse
    // than not requesting it: it reads like a safety net in code review.
    //
    // Recovery is done one layer up instead, and from the durable source rather than a
    // 50-message buffer: InboxStore reconciles against the Hermes API on every reconnect.
    // See issue #102 for the decision and the alternatives weighed.
    const subscription = transport.newSubscription(`user#${userId}`);
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

  /**
   * Close the socket, drop every handler, and refuse to reconnect. Permanent.
   *
   * The refusal is the part that matters. A disposed connection that could still be reopened
   * is a connection that delivers to nobody, because the handlers are gone — so `connect()`
   * after this rejects rather than quietly succeeding.
   */
  dispose(): void {
    this.disposed = true;
    this.disconnect();
    this.handlers = [];
    this.statusHandlers = [];
  }
}
