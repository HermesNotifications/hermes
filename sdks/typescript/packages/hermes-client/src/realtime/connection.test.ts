// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { describe, expect, it } from "vitest";
import type { HermesEvent } from "../types.js";
import {
  RealtimeConnection,
  type RealtimeSubscriptionLike,
  type RealtimeTransportLike,
  type TransportFactory,
} from "./connection.js";

/**
 * Until this file existed, nothing here was covered: the connection hard-wired
 * `new Centrifuge(...)`, so there was no way to drive it without opening a socket. The
 * factory seam these tests use is the change that makes the wire-format mapping —
 * the part most likely to break silently when the server payload shifts — assertable.
 */

/** A stand-in for centrifuge's Subscription, recording what the connection did to it. */
class FakeSubscription implements RealtimeSubscriptionLike {
  subscribeCalls = 0;
  unsubscribeCalls = 0;
  readonly options: Record<string, unknown>;
  private handlers = new Map<string, Array<(ctx: never) => void>>();

  constructor(
    readonly channel: string,
    options?: Record<string, unknown>
  ) {
    this.options = options ?? {};
  }

  on(event: string, handler: (ctx: never) => void): unknown {
    const list = this.handlers.get(event) ?? [];
    list.push(handler);
    this.handlers.set(event, list);
    return this;
  }

  subscribe(): void {
    this.subscribeCalls++;
  }

  unsubscribe(): void {
    this.unsubscribeCalls++;
  }

  /** Deliver a publication the way Centrifugo would. */
  publish(data: unknown): void {
    for (const handler of this.handlers.get("publication") ?? []) {
      (handler as (ctx: unknown) => void)({ data });
    }
  }

  /** Fire the subscription's own lifecycle events. */
  fire(event: string, ctx: unknown = {}): void {
    for (const handler of this.handlers.get(event) ?? []) {
      (handler as (ctx: unknown) => void)(ctx);
    }
  }
}

class FakeTransport implements RealtimeTransportLike {
  readonly subscriptions: FakeSubscription[] = [];
  removed: Array<RealtimeSubscriptionLike | null> = [];
  connectCalls = 0;
  disconnectCalls = 0;
  private handlers = new Map<string, Array<(ctx: never) => void>>();

  constructor(
    readonly endpoint: string,
    readonly options: Record<string, unknown>
  ) {}

  on(event: string, handler: (ctx: never) => void): unknown {
    const list = this.handlers.get(event) ?? [];
    list.push(handler);
    this.handlers.set(event, list);
    return this;
  }

  newSubscription(channel: string, options?: Record<string, unknown>): RealtimeSubscriptionLike {
    const sub = new FakeSubscription(channel, options);
    this.subscriptions.push(sub);
    return sub;
  }

  removeSubscription(sub: RealtimeSubscriptionLike | null): void {
    this.removed.push(sub);
  }

  connect(): void {
    this.connectCalls++;
  }

  disconnect(): void {
    this.disconnectCalls++;
  }

  fire(event: string, ctx: unknown = {}): void {
    for (const handler of this.handlers.get(event) ?? []) {
      (handler as (ctx: unknown) => void)(ctx);
    }
  }

  get latest(): FakeSubscription {
    const sub = this.subscriptions.at(-1);
    if (!sub) throw new Error("no subscription was created");
    return sub;
  }
}

/** A connection wired to a fake transport, plus the transports it built. */
function connection(options?: { socketUrl?: string; token?: string }) {
  const transports: FakeTransport[] = [];
  const factory: TransportFactory = (endpoint, opts) => {
    const transport = new FakeTransport(endpoint, opts as Record<string, unknown>);
    transports.push(transport);
    return transport;
  };
  const events: HermesEvent[] = [];
  const conn = new RealtimeConnection(
    options?.socketUrl ?? "http://localhost:8888/realtime",
    async () => options?.token ?? "jwt-token",
    factory
  );
  const unsubscribe = conn.on((event) => events.push(event));
  return { conn, transports, events, unsubscribe, transport: () => transports[0] };
}

describe("RealtimeConnection: endpoint construction", () => {
  it.each([
    {
      name: "rewrites http to ws and appends the websocket path",
      socketUrl: "http://localhost:8888/realtime",
      want: "ws://localhost:8888/realtime/connection/websocket",
    },
    {
      name: "rewrites https to wss",
      socketUrl: "https://hermes.example.com/realtime",
      want: "wss://hermes.example.com/realtime/connection/websocket",
    },
    {
      name: "leaves an explicit websocket path alone rather than doubling it",
      socketUrl: "wss://hermes.example.com/realtime/connection/websocket",
      want: "wss://hermes.example.com/realtime/connection/websocket",
    },
    {
      name: "accepts a ws url unchanged",
      socketUrl: "ws://localhost:8000",
      want: "ws://localhost:8000/connection/websocket",
    },
  ])("$name", async ({ socketUrl, want }) => {
    const { conn, transport } = connection({ socketUrl });
    await conn.connect("usr_1");
    expect(transport().endpoint).toBe(want);
  });
});

describe("RealtimeConnection: subscribing", () => {
  it("subscribes to the user's own channel", async () => {
    // The `#` is Centrifugo's user-boundary separator: with
    // allow_user_limited_channels the server itself refuses a subscription whose
    // suffix does not match the JWT subject.
    const { conn, transport } = connection();
    await conn.connect("usr_abc");
    expect(transport().latest.channel).toBe("user#usr_abc");
    expect(transport().latest.subscribeCalls).toBe(1);
    expect(transport().connectCalls).toBe(1);
  });

  it("asks for a recoverable subscription so a brief drop does not lose publications", async () => {
    // Centrifugo is configured with history_size 50 / history_ttl 1h. Without asking
    // for recovery the client silently misses anything published while the socket was
    // down, which for a notification inbox means a notification that never appears.
    const { conn, transport } = connection();
    await conn.connect("usr_1");
    expect(transport().latest.options.recoverable).toBe(true);
    expect(transport().latest.options.positioned).toBe(true);
  });

  it("passes a token getter so the socket can re-authenticate on reconnect", async () => {
    const { conn, transport } = connection({ token: "first-token" });
    await conn.connect("usr_1");
    const getToken = transport().options.getToken as () => Promise<string>;
    expect(getToken).toBeTypeOf("function");
    await expect(getToken()).resolves.toBe("first-token");
  });

  it("is idempotent for the same user", async () => {
    const { conn, transports } = connection();
    await conn.connect("usr_1");
    await conn.connect("usr_1");
    expect(transports).toHaveLength(1);
    expect(transports[0].subscriptions).toHaveLength(1);
  });

  it("resubscribes when asked to connect as a different user", async () => {
    // Previously `connect()` early-returned whenever a transport already existed, so
    // switching users left the socket listening to the old channel — the inbox would
    // load the new user's rows over REST and then never update.
    const { conn, transports } = connection();
    await conn.connect("usr_1");
    await conn.connect("usr_2");
    expect(transports[0].disconnectCalls).toBe(1);
    expect(transports).toHaveLength(2);
    expect(transports[1].latest.channel).toBe("user#usr_2");
  });
});

describe("RealtimeConnection: concurrent connects", () => {
  /**
   * `connect()` must await a token before it can build a transport. The idempotence guard
   * above reads `this.transport`, which is only assigned *after* that await — so two calls
   * landing in the same tick both used to get through, and the second overwrote the first.
   * The orphan stayed subscribed, kept emitting into the same handler list, and no
   * `disconnect()` could reach it.
   *
   * These are reachable in normal use: two stores over one shared client both connect on
   * mount, and StrictMode runs effect → cleanup → effect.
   */
  it("builds a single transport for two connects in the same tick", async () => {
    const { conn, transports } = connection();
    await Promise.all([conn.connect("usr_1"), conn.connect("usr_1")]);
    expect(transports).toHaveLength(1);
    expect(transports[0].subscriptions).toHaveLength(1);
  });

  it("emits each publication once, not once per racing connect", async () => {
    const { conn, transports, events } = connection();
    await Promise.all([conn.connect("usr_1"), conn.connect("usr_1"), conn.connect("usr_1")]);

    // Publish on every transport that got built, not just the first. An orphan left behind
    // by a lost race stays subscribed and keeps emitting into the same handler list, so a
    // test that only drove transports[0] would not see the duplication at all.
    const publication = {
      type: "notification.new",
      id: "ntf_1",
      title: "Hello",
      body: "Body",
      created_at: "2026-07-30T10:00:00.000Z",
    };
    for (const transport of transports) transport.latest.publish(publication);

    expect(events).toHaveLength(1);
  });

  it("orphans no transport when racing connects name different users", async () => {
    const { conn, transports } = connection();
    await Promise.all([conn.connect("usr_1"), conn.connect("usr_2")]);

    // Every transport but the last has been closed...
    for (const superseded of transports.slice(0, -1)) {
      expect(superseded.disconnectCalls).toBeGreaterThan(0);
    }
    // ...and the survivor is the one disconnect() can still reach.
    const live = transports.at(-1);
    const before = live?.disconnectCalls ?? 0;
    conn.disconnect();
    expect(live?.disconnectCalls).toBe(before + 1);
  });

  it("serializes so a later connect wins the final subscription", async () => {
    const { conn, transports } = connection();
    await Promise.all([conn.connect("usr_1"), conn.connect("usr_2")]);
    expect(transports.at(-1)?.latest.channel).toBe("user#usr_2");
  });
});

describe("RealtimeConnection: inbox.updated publications", () => {
  it.each([
    "read",
    "unread",
    "archive",
    "unarchive",
    "delete",
    "read-all",
  ] as const)("maps the %s action from snake_case to camelCase", async (action) => {
    const { conn, events, transport } = connection();
    await conn.connect("usr_1");

    transport().latest.publish({
      type: "inbox.updated",
      notification_id: "ntf_1",
      action,
      unread_count: 4,
      timestamp: 1_769_000_000_000,
    });

    expect(events).toEqual([
      {
        type: "inbox.updated",
        notificationId: "ntf_1",
        action,
        unreadCount: 4,
        timestamp: 1_769_000_000_000,
      },
    ]);
  });

  it("carries the read-all shape through, where the id is empty and the count is zero", async () => {
    const { conn, events, transport } = connection();
    await conn.connect("usr_1");

    transport().latest.publish({
      type: "inbox.updated",
      notification_id: "",
      action: "read-all",
      unread_count: 0,
      timestamp: 1,
    });

    expect(events[0]).toMatchObject({ notificationId: "", unreadCount: 0 });
  });
});

describe("RealtimeConnection: notification.new publications", () => {
  it("maps the payload onto the event shape", async () => {
    const { conn, events, transport } = connection();
    await conn.connect("usr_1");

    transport().latest.publish({
      type: "notification.new",
      id: "ntf_9",
      title: "Invoice ready",
      body: "Invoice #1234",
      created_at: "2026-07-29T09:00:00.000Z",
      timestamp: 1_769_000_000_000,
    });

    expect(events).toEqual([
      {
        type: "notification.new",
        id: "ntf_9",
        title: "Invoice ready",
        body: "Invoice #1234",
        createdAt: "2026-07-29T09:00:00.000Z",
      },
    ]);
  });

  it("lifts the nested action object into actionUrl and actionLabel", async () => {
    const { conn, events, transport } = connection();
    await conn.connect("usr_1");

    transport().latest.publish({
      type: "notification.new",
      id: "ntf_9",
      title: "Invoice ready",
      body: "Invoice #1234",
      created_at: "2026-07-29T09:00:00.000Z",
      action: { url: "https://example.com/i/1234", label: "View invoice" },
    });

    expect(events[0]).toMatchObject({
      actionUrl: "https://example.com/i/1234",
      actionLabel: "View invoice",
    });
  });

  it("omits the action fields entirely when the payload has no action", async () => {
    const { conn, events, transport } = connection();
    await conn.connect("usr_1");

    transport().latest.publish({
      type: "notification.new",
      id: "ntf_9",
      title: "T",
      body: "B",
      created_at: "2026-07-29T09:00:00.000Z",
    });

    expect(events[0]).not.toHaveProperty("actionUrl");
    expect(events[0]).not.toHaveProperty("actionLabel");
  });
});

describe("RealtimeConnection: unrecognised publications", () => {
  it("emits nothing for an unknown event type", async () => {
    // The old code's `else` branch treated everything that was not inbox.updated as a
    // notification.new, so any future event type became a row with an undefined title
    // and body — a visible corruption of the user's inbox from a purely additive
    // server change.
    const { conn, events, transport } = connection();
    await conn.connect("usr_1");

    transport().latest.publish({ type: "notification.snoozed", id: "ntf_1" });

    expect(events).toEqual([]);
  });

  it("emits nothing for a payload with no type at all", async () => {
    const { conn, events, transport } = connection();
    await conn.connect("usr_1");

    transport().latest.publish({ id: "ntf_1", title: "T" });

    expect(events).toEqual([]);
  });

  it("emits nothing for a non-object payload", async () => {
    const { conn, events, transport } = connection();
    await conn.connect("usr_1");

    transport().latest.publish("surprise");

    expect(events).toEqual([]);
  });
});

describe("RealtimeConnection: status reporting", () => {
  it("starts disconnected", () => {
    const { conn } = connection();
    expect(conn.status).toBe("disconnected");
  });

  it("reports connected once the channel subscription is established", async () => {
    // "The socket is open" is not the same as "we will receive publications". Only the
    // subscribed event means a send is guaranteed to reach us, which is exactly what a
    // live test — and an integrator — needs to gate on.
    const { conn, transport } = connection();
    await conn.connect("usr_1");
    expect(conn.status).toBe("connecting");

    transport().latest.fire("subscribed");

    expect(conn.status).toBe("connected");
  });

  it("notifies status listeners on the transition", async () => {
    const { conn, transport } = connection();
    const seen: string[] = [];
    conn.onStatusChange((status) => seen.push(status));
    await conn.connect("usr_1");
    transport().latest.fire("subscribed");
    transport().latest.fire("unsubscribed");

    expect(seen).toEqual(["connecting", "connected", "connecting"]);
  });

  it("returns to disconnected on disconnect", async () => {
    const { conn, transport } = connection();
    await conn.connect("usr_1");
    transport().latest.fire("subscribed");

    conn.disconnect();

    expect(conn.status).toBe("disconnected");
  });
});

describe("RealtimeConnection: teardown", () => {
  it("unsubscribes and disconnects the transport", async () => {
    const { conn, transport } = connection();
    await conn.connect("usr_1");
    const sub = transport().latest;

    conn.disconnect();

    expect(sub.unsubscribeCalls).toBe(1);
    expect(transport().removed).toContain(sub);
    expect(transport().disconnectCalls).toBe(1);
  });

  it("keeps event handlers across a disconnect so reconnecting still delivers", async () => {
    // Handlers belong to the owning HermesClient, not to one socket. Clearing them here
    // would make client.disconnect() followed by client.connect() silently deliver
    // nothing.
    const { conn, events, transport, transports } = connection();
    await conn.connect("usr_1");
    conn.disconnect();
    await conn.connect("usr_1");

    transports[1].latest.publish({
      type: "inbox.updated",
      notification_id: "ntf_1",
      action: "read",
      unread_count: 0,
      timestamp: 1,
    });

    expect(events).toHaveLength(1);
    expect(transport().disconnectCalls).toBe(1);
  });

  it("drops every handler on dispose", async () => {
    const { conn, events, transport } = connection();
    await conn.connect("usr_1");
    const sub = transport().latest;

    conn.dispose();
    sub.publish({
      type: "inbox.updated",
      notification_id: "ntf_1",
      action: "read",
      unread_count: 0,
      timestamp: 1,
    });

    expect(events).toEqual([]);
    expect(conn.handlerCount()).toBe(0);
  });

  it("stops delivering to a handler that unsubscribed", async () => {
    const { conn, events, unsubscribe, transport } = connection();
    await conn.connect("usr_1");
    unsubscribe();

    transport().latest.publish({
      type: "inbox.updated",
      notification_id: "ntf_1",
      action: "read",
      unread_count: 0,
      timestamp: 1,
    });

    expect(events).toEqual([]);
  });

  it("tolerates disconnecting when never connected", () => {
    const { conn } = connection();
    expect(() => conn.disconnect()).not.toThrow();
  });
});
