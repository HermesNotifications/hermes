// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { StrictMode, useEffect } from "react";
import { cleanup, render, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import type {
  HermesClient,
  NewNotificationEvent,
  RealtimeSubscriptionLike,
  RealtimeTransportLike,
} from "@hermes-notifications/client";
import { useHermesClient } from "./hooks.js";

/**
 * `useHermesClient` had no tests, and that is precisely how this shipped.
 *
 * It disposed the client from an effect cleanup. StrictMode runs cleanup between two effect
 * invocations on the same memoized instance, and `dispose()` is terminal — it drops every handler
 * any consumer registered. An embedded `<hermes-inbox client={...}>` registers its own during an
 * async start(), so whichever won the race decided whether realtime worked at all. When dispose
 * won, the socket still connected, still subscribed, still received publications, and delivered
 * them to nobody.
 *
 * Nothing below needs a network: constructing a client opens no socket, and the transport is
 * injected. What it does need is StrictMode, because without it the cleanup never runs and every
 * assertion here passes against the broken version too.
 */

class FakeSubscription implements RealtimeSubscriptionLike {
  private handlers = new Map<string, (ctx: never) => void>();
  on(event: string, handler: (ctx: never) => void): unknown {
    this.handlers.set(event, handler);
    return this;
  }
  subscribe(): void {}
  unsubscribe(): void {}
  /** Deliver a publication the way Centrifugo would. */
  publish(data: unknown): void {
    (this.handlers.get("publication") as ((ctx: { data: unknown }) => void) | undefined)?.({ data });
  }
  fire(event: string): void {
    (this.handlers.get(event) as ((ctx: unknown) => void) | undefined)?.({});
  }
}

class FakeTransport implements RealtimeTransportLike {
  readonly subscriptions: FakeSubscription[] = [];
  on(): unknown {
    return this;
  }
  newSubscription(): RealtimeSubscriptionLike {
    const sub = new FakeSubscription();
    this.subscriptions.push(sub);
    return sub;
  }
  removeSubscription(): void {}
  connect(): void {}
  disconnect(): void {}
  get latest(): FakeSubscription {
    const sub = this.subscriptions.at(-1);
    if (!sub) throw new Error("no subscription was created");
    return sub;
  }
}

/** A consumer that registers on the shared client from its own effect, as the widget does. */
function Consumer(props: { client: HermesClient; onArrival: (e: NewNotificationEvent) => void }) {
  const { client, onArrival } = props;
  useEffect(() => client.on("notification", onArrival), [client, onArrival]);
  return null;
}

function mount() {
  const transports: FakeTransport[] = [];
  const arrivals: NewNotificationEvent[] = [];
  let client!: HermesClient;

  function Harness() {
    client = useHermesClient({
      apiUrl: "http://localhost:8888",
      socketUrl: "http://localhost:8888/realtime",
      token: "test-token",
      transportFactory: () => {
        const transport = new FakeTransport();
        transports.push(transport);
        return transport;
      },
    });
    return <Consumer client={client} onArrival={(e) => arrivals.push(e)} />;
  }

  render(<Harness />, { wrapper: StrictMode });
  return { client: () => client, transports, arrivals };
}

afterEach(cleanup);

describe("useHermesClient under StrictMode", () => {
  it("leaves the client usable, rather than disposed, after the mount cycle", async () => {
    // The regression in one line: a disposed client refuses to connect. Before the fix this
    // rejected, and every consumer of the shared client was silently deaf from then on.
    const { client } = mount();

    await expect(client().connect("usr_1")).resolves.toBeUndefined();
  });

  it("keeps a handler a child registered during its effect", async () => {
    // The half a `disconnect()`-only fix would still get wrong. dispose() empties the handler
    // arrays, so the child's registration disappeared even though the child never unmounted —
    // and the client cannot restore it, because it has no idea who registered it.
    const { client, transports, arrivals } = mount();

    await client().connect("usr_1");
    await waitFor(() => expect(transports).toHaveLength(1));
    transports[0].latest.publish({
      type: "notification.new",
      id: "n1",
      title: "Arrived",
      body: "b",
    });

    expect(arrivals.map((a) => a.id)).toEqual(["n1"]);
  });

  it("closes the socket on cleanup, so a spurious teardown costs a reconnect and nothing more", async () => {
    // The cleanup still has to do something — an unmount must not leave a socket open. Asserting
    // it disconnects pins the reason `disconnect()` was the right verb: survivable, therefore
    // safe to run spuriously.
    const { client, transports } = mount();

    await client().connect("usr_1");
    await waitFor(() => expect(transports).toHaveLength(1));

    cleanup();

    expect(client().realtimeStatus).toBe("disconnected");
  });
});
