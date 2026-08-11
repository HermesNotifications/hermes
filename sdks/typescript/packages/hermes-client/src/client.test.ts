// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { describe, expect, it } from "vitest";
import { HermesClient } from "./client.js";
import type {
  RealtimeSubscriptionLike,
  RealtimeTransportLike,
  TransportFactory,
} from "./realtime/connection.js";

/** Build an unsigned token carrying `sub`. */
function tokenFor(sub: string): string {
  const encode = (value: unknown) =>
    btoa(JSON.stringify(value)).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  return `${encode({ alg: "HS256" })}.${encode({ sub, organization_id: "org_1" })}.sig`;
}

class FakeSubscription implements RealtimeSubscriptionLike {
  private handlers = new Map<string, Array<(ctx: never) => void>>();
  constructor(readonly channel: string) {}
  on(event: string, handler: (ctx: never) => void): unknown {
    const list = this.handlers.get(event) ?? [];
    list.push(handler);
    this.handlers.set(event, list);
    return this;
  }
  subscribe(): void {}
  unsubscribe(): void {}
  publish(data: unknown): void {
    for (const handler of this.handlers.get("publication") ?? []) {
      (handler as (ctx: unknown) => void)({ data });
    }
  }
}

class FakeTransport implements RealtimeTransportLike {
  readonly subscriptions: FakeSubscription[] = [];
  constructor(readonly options: Record<string, unknown>) {}
  on(): unknown {
    return this;
  }
  newSubscription(channel: string): RealtimeSubscriptionLike {
    const sub = new FakeSubscription(channel);
    this.subscriptions.push(sub);
    return sub;
  }
  removeSubscription(): void {}
  connect(): void {}
  disconnect(): void {}
  get latest(): FakeSubscription {
    const sub = this.subscriptions.at(-1);
    if (!sub) throw new Error("no subscription");
    return sub;
  }
}

function client(config?: {
  token?: string;
  getToken?: () => Promise<string>;
  fetch?: typeof fetch;
}) {
  const transports: FakeTransport[] = [];
  const transportFactory: TransportFactory = (_endpoint, options) => {
    const transport = new FakeTransport(options);
    transports.push(transport);
    return transport;
  };
  const hermes = new HermesClient({
    apiUrl: "http://localhost:8888",
    socketUrl: "http://localhost:8888/realtime",
    token: config?.token ?? tokenFor("usr_internal"),
    ...(config?.getToken ? { getToken: config.getToken } : {}),
    ...(config?.fetch ? { fetch: config.fetch } : {}),
    transportFactory,
  });
  return { hermes, transports };
}

describe("HermesClient: construction", () => {
  it("opens no socket until connect is called", () => {
    // useHermesClient builds the client in a useState initializer, which React's
    // StrictMode deliberately double-invokes and discards one result from. That is only
    // harmless because construction is inert — so this property is load-bearing.
    const { transports } = client();
    expect(transports).toHaveLength(0);
  });

  it("exposes the inbox and user surfaces", () => {
    const { hermes } = client();
    expect(hermes.inbox).toBeDefined();
    expect(hermes.user).toBeDefined();
  });

  it("starts with a disconnected realtime status", () => {
    expect(client().hermes.realtimeStatus).toBe("disconnected");
  });
});

describe("HermesClient: connecting", () => {
  it("subscribes to the channel for the token's subject when no id is given", () => {
    // The channel needs the internal id from `sub`. Making the argument optional means a
    // consumer cannot accidentally pass the external id and get a silently dead socket.
    const { hermes, transports } = client({ token: tokenFor("usr_from_claim") });
    return hermes.connect().then(() => {
      expect(transports[0].latest.channel).toBe("user#usr_from_claim");
    });
  });

  it("prefers an explicitly supplied user id", async () => {
    const { hermes, transports } = client({ token: tokenFor("usr_from_claim") });
    await hermes.connect("usr_explicit");
    expect(transports[0].latest.channel).toBe("user#usr_explicit");
  });

  it("rejects when the token carries no subject and no id was supplied", async () => {
    const { hermes } = client({ token: "not-a-jwt" });
    await expect(hermes.connect()).rejects.toThrow(/user id/i);
  });

  it("passes the current token to the socket", async () => {
    const token = tokenFor("usr_1");
    const { hermes, transports } = client({ token });
    await hermes.connect();
    expect(transports[0].options.token).toBe(token);
  });
});

describe("HermesClient: event fan-out", () => {
  it("delivers a realtime notification to notification handlers", async () => {
    const { hermes, transports } = client();
    const seen: unknown[] = [];
    hermes.on("notification", (event) => seen.push(event));
    await hermes.connect();

    transports[0].latest.publish({
      type: "notification.new",
      id: "n1",
      title: "T",
      body: "B",
      created_at: "2026-07-29T09:00:00.000Z",
    });

    expect(seen).toHaveLength(1);
    expect(seen[0]).toMatchObject({ id: "n1" });
  });

  it("delivers an update to update handlers and the count to count handlers", async () => {
    const { hermes, transports } = client();
    const updates: unknown[] = [];
    const counts: number[] = [];
    hermes.on("update", (event) => updates.push(event));
    hermes.on("unreadCountChange", (count) => counts.push(count));
    await hermes.connect();

    transports[0].latest.publish({
      type: "inbox.updated",
      notification_id: "n1",
      action: "read",
      unread_count: 4,
      timestamp: 1,
    });

    expect(updates).toHaveLength(1);
    expect(counts).toEqual([4]);
  });

  // Each event is registered through its own overload rather than a union, because the
  // overloads are the API — passing a union here would only compile if `on` were widened,
  // which would let a notification handler be registered for a count event.
  it.each([
    {
      name: "notification",
      register: (hermes: HermesClient) => hermes.on("notification", () => {}),
    },
    { name: "update", register: (hermes: HermesClient) => hermes.on("update", () => {}) },
    {
      name: "unreadCountChange",
      register: (hermes: HermesClient) => hermes.on("unreadCountChange", () => {}),
    },
  ])("returns a working unsubscribe for $name", ({ register }) => {
    const { hermes } = client();
    const unsubscribe = register(hermes);
    expect(hermes.handlerCount()).toBe(1);
    unsubscribe();
    expect(hermes.handlerCount()).toBe(0);
  });
});

describe("HermesClient: unread count", () => {
  it("notifies count handlers when the count is pushed in", () => {
    // Previously the count only ever changed on an inbox.updated event, so a badge built
    // on this callback read zero until the user's first mutation — even with unread rows
    // on screen. The store now pushes the count from the initial list() through here.
    const { hermes } = client();
    const counts: number[] = [];
    hermes.on("unreadCountChange", (count) => counts.push(count));

    hermes.setUnreadCount(3);

    expect(counts).toEqual([3]);
  });

  it("does not re-notify for an unchanged value", () => {
    const { hermes } = client();
    const counts: number[] = [];
    hermes.on("unreadCountChange", (count) => counts.push(count));

    hermes.setUnreadCount(3);
    hermes.setUnreadCount(3);

    expect(counts).toEqual([3]);
  });

  it("reports the last known count", () => {
    const { hermes } = client();
    hermes.setUnreadCount(5);
    expect(hermes.unreadCount).toBe(5);
  });

  it("fetches and publishes the count on refreshUnreadCount", async () => {
    // The path that makes a standalone bell badge correct on first paint. Without it a host
    // with no inbox panel mounted has nothing driving the count until the user's first
    // mutation, so the badge reads zero however many notifications are waiting.
    const requested: string[] = [];
    const fakeFetch: typeof fetch = async (input) => {
      // openapi-fetch hands the middleware a Request, not a URL string.
      const url = input instanceof Request ? input.url : String(input);
      requested.push(new URL(url).pathname);
      return new Response(JSON.stringify({ unread_count: 8 }), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    };
    const { hermes } = client({ fetch: fakeFetch });
    const counts: number[] = [];
    hermes.on("unreadCountChange", (count) => counts.push(count));

    await expect(hermes.refreshUnreadCount()).resolves.toBe(8);

    expect(requested).toEqual(["/v1/inbox/unread-count"]);
    expect(counts).toEqual([8]);
  });

  it("moves the count on an arrival that carries one", async () => {
    const { hermes, transports } = client();
    await hermes.connect("usr_1");
    const counts: number[] = [];
    hermes.on("unreadCountChange", (count) => counts.push(count));

    transports[0].latest.publish({
      type: "notification.new",
      id: "ntf_1",
      title: "T",
      body: "B",
      unread_count: 4,
    });

    expect(counts).toEqual([4]);
  });

  it("leaves the count alone on an arrival that carries none", () => {
    // The client must not invent a number here. The store's reducer owns the fallback
    // arithmetic, because it has the notification list needed to dedupe a redelivery first.
    const { hermes, transports } = client();
    return hermes.connect("usr_1").then(() => {
      const counts: number[] = [];
      hermes.on("unreadCountChange", (count) => counts.push(count));

      transports[0].latest.publish({
        type: "notification.new",
        id: "ntf_1",
        title: "T",
        body: "B",
      });

      expect(counts).toEqual([]);
    });
  });
});

describe("HermesClient: tokens", () => {
  it("uses the token current at request time", async () => {
    const requests: Request[] = [];
    const fetchImpl = async (input: RequestInfo | URL, init?: RequestInit) => {
      const request = input instanceof Request ? input : new Request(input, init);
      requests.push(request);
      return new Response(JSON.stringify({ data: [], unread_count: 0 }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    };
    const { hermes } = client({ token: "first", fetch: fetchImpl as typeof fetch });

    await hermes.inbox.list();
    hermes.setToken("second");
    await hermes.inbox.list();

    expect(requests[0].headers.get("Authorization")).toBe("Bearer first");
    expect(requests[1].headers.get("Authorization")).toBe("Bearer second");
  });

  it("refreshes through getToken and retries a REST call that 401s", async () => {
    // The gap this closes: getToken used to be consulted only when the socket
    // reconnected, so after expiry the inbox 401'd forever while the socket recovered.
    let calls = 0;
    const fetchImpl = async () => {
      calls++;
      const body = JSON.stringify(
        calls === 1 ? { detail: "invalid token" } : { data: [], unread_count: 0 }
      );
      return new Response(body, {
        status: calls === 1 ? 401 : 200,
        headers: { "Content-Type": "application/json" },
      });
    };
    let refreshes = 0;
    const { hermes } = client({
      token: "stale",
      getToken: async () => {
        refreshes++;
        return "fresh";
      },
      fetch: fetchImpl as typeof fetch,
    });

    await expect(hermes.inbox.list()).resolves.toMatchObject({ unreadCount: 0 });
    expect(refreshes).toBe(1);
    expect(calls).toBe(2);
  });
});

describe("HermesClient: teardown", () => {
  it("keeps handlers across disconnect so a reconnect still delivers", async () => {
    const { hermes, transports } = client();
    const seen: unknown[] = [];
    hermes.on("update", (event) => seen.push(event));

    await hermes.connect();
    hermes.disconnect();
    await hermes.connect();
    transports[1].latest.publish({
      type: "inbox.updated",
      notification_id: "n1",
      action: "read",
      unread_count: 0,
      timestamp: 1,
    });

    expect(seen).toHaveLength(1);
  });

  it("drops the handlers consumers registered, on dispose", async () => {
    const { hermes, transports } = client();
    const seen: unknown[] = [];
    hermes.on("update", (event) => seen.push(event));
    await hermes.connect();

    hermes.dispose();
    transports[0].latest.publish({
      type: "inbox.updated",
      notification_id: "n1",
      action: "read",
      unread_count: 0,
      timestamp: 1,
    });

    expect(seen).toEqual([]);
    expect(hermes.handlerCount()).toBe(0);
  });

  it("still delivers events after a dispose-then-reuse cycle", async () => {
    // React's StrictMode runs an effect's cleanup and then the effect again on the same instance, so
    // a client can be disposed and then used again. Before this, `dispose()` also tore down the
    // client's own wiring into the realtime connection, and nothing re-established it — producing a
    // client that connected, subscribed, received publications and delivered them to nobody. It took
    // a live browser to find, because from the outside the socket looked perfectly healthy.
    const { hermes, transports } = client();
    await hermes.connect();

    hermes.dispose();

    const seen: unknown[] = [];
    hermes.on("update", (event) => seen.push(event));
    await hermes.connect();
    transports[1].latest.publish({
      type: "inbox.updated",
      notification_id: "n1",
      action: "read",
      unread_count: 4,
      timestamp: 1,
    });

    expect(seen).toHaveLength(1);
  });

  it("still reports the unread count after a dispose-then-reuse cycle", async () => {
    const { hermes, transports } = client();
    await hermes.connect();
    hermes.dispose();

    const counts: number[] = [];
    hermes.on("unreadCountChange", (count) => counts.push(count));
    await hermes.connect();
    transports[1].latest.publish({
      type: "inbox.updated",
      notification_id: "n1",
      action: "read",
      unread_count: 7,
      timestamp: 1,
    });

    expect(counts).toEqual([7]);
  });
});
