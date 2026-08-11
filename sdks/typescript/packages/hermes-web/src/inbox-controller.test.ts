// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { describe, expect, it, vi } from "vitest";
import type { HermesClientConfig, HermesError } from "@hermes-notifications/client";
import { FakeHermesClient, fakeNotification, fakePage } from "@hermes-notifications/client/testing";
import { InboxController, type ClientFactory } from "./inbox-controller.js";

/**
 * Everything network-facing in the widget used to be unreachable from a test: the element
 * constructed its own HermesClient inside `initClient`, so there was no way to observe the
 * lifecycle without opening a socket. The old suite said so in a comment and stopped at the
 * render boundary.
 *
 * Pulling the lifecycle into a controller with an injectable factory is what changes that.
 * These tests need no DOM at all — they use a three-method stub host — so they read like the
 * client's own suites rather than like widget tests.
 */

/** The slice of Lit's ReactiveControllerHost a controller actually uses. */
class StubHost {
  updateRequests = 0;
  readonly controllers: unknown[] = [];

  addController(controller: unknown): void {
    this.controllers.push(controller);
  }
  removeController(): void {}
  requestUpdate(): void {
    this.updateRequests++;
  }
  get updateComplete(): Promise<boolean> {
    return Promise.resolve(true);
  }
}

interface Harness {
  host: StubHost;
  controller: InboxController;
  /** Clients the factory produced, in order. */
  clients: FakeHermesClient[];
  /** Configs the factory was handed, in order. */
  configs: HermesClientConfig[];
  page: (page: ReturnType<typeof fakePage>) => void;
}

function harness(options?: {
  page?: ReturnType<typeof fakePage>;
  fetch?: typeof fetch;
}): Harness {
  const clients: FakeHermesClient[] = [];
  const configs: HermesClientConfig[] = [];
  let nextPage = options?.page ?? fakePage();

  const clientFactory: ClientFactory = (config) => {
    configs.push(config);
    const fake = new FakeHermesClient(nextPage);
    clients.push(fake);
    return fake.asClient();
  };

  const host = new StubHost();
  const controller = new InboxController(host, {
    clientFactory,
    now: () => "2026-07-29T10:00:00.000Z",
    ...(options?.fetch ? { fetch: options.fetch } : {}),
  });

  return {
    host,
    controller,
    clients,
    configs,
    page: (page) => {
      nextPage = page;
      for (const client of clients) client.page = page;
    },
  };
}

/** A config with everything the controller needs to build a client. */
const READY = {
  apiUrl: "http://localhost:8888",
  socketUrl: "http://localhost:8888/realtime",
  token: "jwt-token",
  userId: "usr_1",
} as const;

/** Let queued promise callbacks run. */
const settle = () => new Promise((resolve) => setTimeout(resolve, 0));

describe("InboxController: registration", () => {
  it("registers itself with its host", () => {
    const { host, controller } = harness();
    expect(host.controllers).toContain(controller);
  });

  it("builds nothing before it is configured", () => {
    const { clients } = harness();
    expect(clients).toHaveLength(0);
  });
});

describe("InboxController: idempotent configuration", () => {
  it("builds one client and loads once when configured twice with the same values", async () => {
    // This is the fix for the widget's double-mount bug. A parser-created element runs
    // attributeChangedCallback for every attribute *before* connectedCallback, so the
    // values are both set when connectedCallback fires AND present in the first
    // changedProperties map — the old code called initClient from both and did everything
    // twice, including two GET /v1/inbox and two socket connects on every single mount.
    //
    // The fix is convergence rather than event-ordering: configure() compares against what
    // is already applied and returns early. That also means there is no watch list to
    // forget to extend.
    const { controller, clients } = harness();

    controller.configure(READY);
    controller.configure(READY);
    await settle();

    // Counted across every client the factory produced, not just the first, so the
    // assertion holds whichever way a regression manifests — one client fetching twice, or
    // two clients each fetching once. Both are two GET /v1/inbox on a single mount.
    const allCalls = clients.flatMap((client) => client.calls);
    expect(allCalls.filter((call) => call === "list")).toHaveLength(1);
    expect(allCalls.filter((call) => call.startsWith("connect"))).toHaveLength(1);
    expect(clients).toHaveLength(1);
  });

  it("builds nothing while the api url is missing", async () => {
    const { controller, clients } = harness();
    controller.configure({ token: "jwt-token" });
    await settle();
    expect(clients).toHaveLength(0);
  });

  it("builds nothing while there is no way to obtain a token", async () => {
    const { controller, clients } = harness();
    controller.configure({ apiUrl: "http://localhost:8888" });
    await settle();
    expect(clients).toHaveLength(0);
  });

  it("builds as soon as the missing piece arrives", async () => {
    // configure() takes the *complete* current config every time — that convergence is
    // what makes it safe to call on every render. Passing only the changed field would
    // reintroduce exactly the partial-update problem the watch list had.
    const { controller, clients } = harness();
    controller.configure({ apiUrl: "http://localhost:8888" });
    await settle();
    controller.configure({ apiUrl: "http://localhost:8888", token: "jwt-token" });
    await settle();
    expect(clients).toHaveLength(1);
  });

  it.each([
    { name: "token", change: { token: "different-token" } },
    { name: "api url", change: { apiUrl: "http://localhost:9999" } },
    { name: "socket url", change: { socketUrl: "http://localhost:9999/realtime" } },
  ])("rebuilds the client when the $name changes", async ({ change }) => {
    const { controller, clients } = harness();
    controller.configure(READY);
    await settle();

    controller.configure({ ...READY, ...change });
    await settle();

    expect(clients).toHaveLength(2);
    expect(clients[0].calls).toContain("disconnect");
  });

  it("resubscribes when the user id changes", async () => {
    // The old element watched only token and apiUrl, so changing user-id after mount left
    // the socket on the previous user's channel while REST returned the new user's rows.
    const { controller, clients } = harness();
    controller.configure(READY);
    await settle();

    controller.configure({ ...READY, userId: "usr_2" });
    await settle();

    expect(clients[1].calls).toContain("connect:usr_2");
  });

  it("reloads without rebuilding the client when only the page size changes", async () => {
    const { controller, clients } = harness();
    controller.configure({ ...READY, pageSize: 20 });
    await settle();

    controller.configure({ ...READY, pageSize: 50 });
    await settle();

    expect(clients).toHaveLength(2);
    expect(clients[1].listOptions[0]).toMatchObject({ limit: 50 });
  });

  it("switches to the archived view when asked", async () => {
    const { controller, clients } = harness();
    controller.configure(READY);
    await settle();

    controller.configure({ ...READY, archived: true });
    await settle();

    expect(clients[1].listOptions[0]).toMatchObject({ archived: true });
  });

  it("uses an injected client instead of building one", async () => {
    const injected = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    const { controller, clients } = harness();

    controller.configure({ client: injected.asClient() });
    await settle();

    expect(clients).toHaveLength(0);
    expect(controller.state.notifications.map((n) => n.id)).toEqual(["a"]);
  });
});

describe("InboxController: state and host updates", () => {
  it("publishes the loaded page as state", async () => {
    const { controller } = harness({
      page: fakePage({ data: [fakeNotification("a")], unreadCount: 3 }),
    });
    controller.configure(READY);
    await settle();

    expect(controller.state.notifications.map((n) => n.id)).toEqual(["a"]);
    expect(controller.state.unreadCount).toBe(3);
  });

  it("asks the host to re-render when state changes", async () => {
    const { controller, host } = harness({
      page: fakePage({ data: [fakeNotification("a")], unreadCount: 1 }),
    });
    controller.configure(READY);
    await settle();

    expect(host.updateRequests).toBeGreaterThan(0);
  });

  it("does not ask the host to re-render for a no-op", async () => {
    const { controller, host } = harness();
    controller.configure(READY);
    await settle();
    const before = host.updateRequests;

    await controller.markRead("nonexistent");

    expect(host.updateRequests).toBe(before);
  });

  it("starts from the initial state before anything loads", () => {
    const { controller } = harness();
    expect(controller.state.notifications).toEqual([]);
    expect(controller.state.unreadCount).toBe(0);
    expect(controller.state.loading).toBe(false);
  });
});

describe("InboxController: forwarding client events", () => {
  it("forwards a realtime notification to its callback", async () => {
    const seen: unknown[] = [];
    const host = new StubHost();
    const clients: FakeHermesClient[] = [];
    const controller = new InboxController(host, {
      clientFactory: () => {
        const fake = new FakeHermesClient(fakePage());
        clients.push(fake);
        return fake.asClient();
      },
      onNotification: (event) => seen.push(event),
    });

    controller.configure(READY);
    await settle();
    clients[0].emit("notification", {
      type: "notification.new",
      id: "n1",
      title: "T",
      body: "B",
      createdAt: "2026-07-29T10:00:00.000Z",
    });

    expect(seen).toHaveLength(1);
  });

  it("forwards an update and a status change", async () => {
    const updates: unknown[] = [];
    const statuses: string[] = [];
    const clients: FakeHermesClient[] = [];
    const controller = new InboxController(new StubHost(), {
      clientFactory: () => {
        const fake = new FakeHermesClient(fakePage());
        clients.push(fake);
        return fake.asClient();
      },
      onUpdate: (event) => updates.push(event),
      onStatusChange: (status) => statuses.push(status),
    });

    controller.configure(READY);
    await settle();
    clients[0].emit("update", {
      type: "inbox.updated",
      notificationId: "n1",
      action: "read",
      unreadCount: 2,
      timestamp: 1,
    });
    clients[0].emitStatus("connected");

    expect(updates).toHaveLength(1);
    expect(statuses).toContain("connected");
  });

  it("reports an error through its callback", async () => {
    const errors: HermesError[] = [];
    const clients: FakeHermesClient[] = [];
    const controller = new InboxController(new StubHost(), {
      clientFactory: () => {
        const fake = new FakeHermesClient(fakePage());
        fake.fail("markRead", new Error("boom"));
        clients.push(fake);
        return fake.asClient();
      },
      onError: (error) => errors.push(error),
      now: () => "2026-07-29T10:00:00.000Z",
    });

    controller.configure(READY);
    await settle();
    clients[0].page = fakePage({ data: [fakeNotification("a")], unreadCount: 1 });
    await controller.refresh();
    await controller.markRead("a");

    expect(errors).toHaveLength(1);
  });
});

describe("InboxController: token-url refresh", () => {
  it("fetches a token from the url when none is supplied directly", async () => {
    // This is the only refresh mechanism expressible in plain HTML, which is exactly the
    // framework-agnostic case the widget exists for. Without it a script-tag consumer's
    // inbox simply stops working when the token expires.
    const requests: string[] = [];
    const fetchImpl = (async (input: RequestInfo | URL) => {
      requests.push(String(input));
      return new Response(
        JSON.stringify({ token: "minted-token", expires_at: "2026-07-29T14:00:00.000Z" }),
        { status: 200, headers: { "Content-Type": "application/json" } }
      );
    }) as typeof fetch;

    const { controller, configs } = harness({ fetch: fetchImpl });
    controller.configure({
      apiUrl: "http://localhost:8888",
      tokenUrl: "/api/session",
      userId: "usr_1",
    });
    await settle();

    expect(requests).toEqual(["/api/session"]);
    expect(configs[0].token).toBe("minted-token");
  });

  it("sends credentials so the host app's session cookie is included", async () => {
    let init: RequestInit | undefined;
    const fetchImpl = (async (_input: RequestInfo | URL, options?: RequestInit) => {
      init = options;
      return new Response(JSON.stringify({ token: "t" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as typeof fetch;

    const { controller } = harness({ fetch: fetchImpl });
    controller.configure({ apiUrl: "http://localhost:8888", tokenUrl: "/api/session" });
    await settle();

    expect(init?.credentials).toBe("include");
  });

  it("reports an error and builds nothing when the token endpoint fails", async () => {
    const errors: HermesError[] = [];
    const fetchImpl = (async () => new Response("nope", { status: 500 })) as typeof fetch;
    const clients: FakeHermesClient[] = [];
    const controller = new InboxController(new StubHost(), {
      clientFactory: () => {
        const fake = new FakeHermesClient(fakePage());
        clients.push(fake);
        return fake.asClient();
      },
      fetch: fetchImpl,
      onError: (error) => errors.push(error),
    });

    controller.configure({ apiUrl: "http://localhost:8888", tokenUrl: "/api/session" });
    await settle();

    expect(clients).toHaveLength(0);
    expect(errors).toHaveLength(1);
  });

  it("prefers an explicit token over the url", async () => {
    const fetchImpl = vi.fn();
    const { controller, configs } = harness({ fetch: fetchImpl as unknown as typeof fetch });
    controller.configure({ ...READY, tokenUrl: "/api/session" });
    await settle();

    expect(fetchImpl).not.toHaveBeenCalled();
    expect(configs[0].token).toBe("jwt-token");
  });

  it("gives the client a getToken that re-fetches from the url", async () => {
    let minted = 0;
    const fetchImpl = (async () => {
      minted++;
      return new Response(JSON.stringify({ token: `token-${minted}` }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as typeof fetch;

    const { controller, configs } = harness({ fetch: fetchImpl });
    controller.configure({ apiUrl: "http://localhost:8888", tokenUrl: "/api/session" });
    await settle();

    await expect(configs[0].getToken?.()).resolves.toBe("token-2");
  });

  it("passes an explicitly supplied getToken straight through", async () => {
    const getToken = async () => "from-callback";
    const { controller, configs } = harness();
    controller.configure({ ...READY, getToken });
    await settle();

    await expect(configs[0].getToken?.()).resolves.toBe("from-callback");
  });
});

describe("InboxController: actions", () => {
  it("delegates each action to the store", async () => {
    const { controller, clients } = harness({
      page: fakePage({ data: [fakeNotification("a")], unreadCount: 1 }),
    });
    controller.configure(READY);
    await settle();

    await controller.markRead("a");
    await controller.archive("a");
    await controller.markAllRead();

    expect(clients[0].calls).toContain("markRead:a");
    expect(clients[0].calls).toContain("archive:a");
    expect(clients[0].calls).toContain("markAllRead");
  });

  it("ignores actions before it is configured", async () => {
    const { controller } = harness();
    await expect(controller.markRead("a")).resolves.toBeUndefined();
    await expect(controller.markAllRead()).resolves.toBeUndefined();
    await expect(controller.loadMore()).resolves.toBeUndefined();
  });

  it("loads the next page", async () => {
    const { controller, clients, page } = harness({
      page: fakePage({ data: [fakeNotification("a")], unreadCount: 1, cursor: "c1" }),
    });
    controller.configure(READY);
    await settle();

    page(fakePage({ data: [fakeNotification("b")], unreadCount: 1 }));
    await controller.loadMore();

    expect(clients[0].listOptions[1]).toMatchObject({ cursor: "c1" });
    expect(controller.state.notifications.map((n) => n.id)).toEqual(["a", "b"]);
  });
});

describe("InboxController: teardown", () => {
  it("disconnects when the host leaves the document", async () => {
    const { controller, clients } = harness();
    controller.configure(READY);
    await settle();

    controller.hostDisconnected();

    expect(clients[0].calls).toContain("disconnect");
    expect(clients[0].handlerCount()).toBe(0);
  });

  it("reconfigures from scratch when the host is reconnected", async () => {
    // Lit moves elements between parents by disconnecting and reconnecting them, so a
    // controller that could not recover from teardown would leave a moved widget dead.
    const { controller, clients } = harness();
    controller.configure(READY);
    await settle();
    controller.hostDisconnected();

    controller.hostConnected();
    controller.configure(READY);
    await settle();

    expect(clients).toHaveLength(2);
    expect(controller.state.loading).toBe(false);
  });

  it("clears a recorded error on request", async () => {
    const { controller, clients } = harness({
      page: fakePage({ data: [fakeNotification("a")], unreadCount: 1 }),
    });
    controller.configure(READY);
    await settle();
    clients[0].fail("markRead", new Error("boom"));
    await controller.markRead("a");
    expect(controller.state.error).toBeDefined();

    controller.clearError();

    expect(controller.state.error).toBeUndefined();
  });
});

describe("InboxController: injected client ownership", () => {
  /**
   * A client passed in through `config.client` belongs to the caller, who is very likely
   * sharing it — the React provider owns one client, hands it to the widget, and also
   * feeds a standalone unread badge from it. `dispose()` clears *every* handler on the
   * client, so disposing an injected one here left those siblings permanently deaf, with
   * nothing to resubscribe them because the client identity never changed.
   */
  function shared() {
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 3 }));
    const seen: number[] = [];
    // A sibling consumer of the same client, registered before the widget exists.
    fake.on("unreadCountChange", ((count: number) => seen.push(count)) as never);
    return { fake, seen };
  }

  it("does not dispose a client it was handed", async () => {
    const { fake, seen } = shared();
    const { controller } = harness();
    controller.configure({ client: fake.asClient(), userId: "usr_1" });
    await settle();

    controller.hostDisconnected();

    fake.emit("unreadCountChange", 9);
    expect(seen).toContain(9);
  });

  it("keeps a sibling's subscription alive across a rebuild", async () => {
    // The exact demo path: becomeUser() changes userId, which is part of the client
    // identity, so the controller rebuilds — and used to dispose the shared client on its
    // way through teardown, freezing the host's own unread badge for good.
    const { fake, seen } = shared();
    const { controller } = harness();
    controller.configure({ client: fake.asClient(), userId: "usr_1" });
    await settle();

    controller.configure({ client: fake.asClient(), userId: "usr_2" });
    await settle();

    fake.emit("unreadCountChange", 4);
    expect(seen).toContain(4);
  });

  it("does not close a shared socket on teardown", async () => {
    const { fake } = shared();
    const { controller } = harness();
    controller.configure({ client: fake.asClient(), userId: "usr_1" });
    await settle();

    controller.hostDisconnected();

    expect(fake.calls).not.toContain("disconnect");
  });

  it("removes its own handlers from a shared client, so rebuilds do not stack them", async () => {
    const { fake, seen } = shared();
    const received: number[] = [];
    const host = new StubHost();
    const controller = new InboxController(host, {
      onUnreadCountChange: (count) => received.push(count),
    });

    controller.configure({ client: fake.asClient(), userId: "usr_1" });
    await settle();
    controller.configure({ client: fake.asClient(), userId: "usr_2" });
    await settle();
    controller.configure({ client: fake.asClient(), userId: "usr_3" });
    await settle();

    fake.emit("unreadCountChange", 5);

    // Once, not once per rebuild.
    expect(received.filter((c) => c === 5)).toHaveLength(1);
    expect(seen).toContain(5);
  });

  it("still disposes a client it built itself", async () => {
    const { controller, clients } = harness();
    controller.configure(READY);
    await settle();

    controller.hostDisconnected();

    expect(clients[0].calls).toContain("disconnect");
    expect(clients[0].handlerCount()).toBe(0);
  });
});

describe("InboxController: losing a usable config", () => {
  it("tears the inbox down when the host drops its credentials", async () => {
    // A host signing out clears client, token and userId together. configure() used to
    // return early on an unusable config, leaving the previous store running — so the
    // widget kept showing, and live-updating, the signed-out user's inbox.
    const { controller, clients } = harness({
      page: fakePage({ data: [fakeNotification("a")], unreadCount: 2 }),
    });
    controller.configure(READY);
    await settle();
    expect(controller.state.notifications).toHaveLength(1);

    controller.configure({ apiUrl: READY.apiUrl });
    await settle();

    expect(controller.state.notifications).toHaveLength(0);
    expect(controller.state.unreadCount).toBe(0);
    expect(clients[0].calls).toContain("disconnect");
  });

  it("reconfigures cleanly when credentials come back", async () => {
    const { controller, clients } = harness();
    controller.configure(READY);
    await settle();
    controller.configure({ apiUrl: READY.apiUrl });
    await settle();

    controller.configure(READY);
    await settle();

    expect(clients).toHaveLength(2);
    expect(controller.state.loading).toBe(false);
  });

  it("stays quiet when it never had a usable config to begin with", () => {
    const { controller, host, clients } = harness();
    const updatesBefore = host.updateRequests;

    controller.configure({ apiUrl: READY.apiUrl });

    expect(clients).toHaveLength(0);
    expect(host.updateRequests).toBe(updatesBefore);
  });
});

describe("InboxController: retrying after a failed token mint", () => {
  /** A fetch that fails `failures` times, then serves a token. */
  function flakyTokenFetch(failures: number) {
    let calls = 0;
    return vi.fn(async () => {
      calls++;
      if (calls <= failures) return new Response("nope", { status: 503 });
      return new Response(JSON.stringify({ token: "minted-jwt" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }) as unknown as typeof fetch;
  }

  const WITH_TOKEN_URL = {
    apiUrl: "http://localhost:8888",
    tokenUrl: "/api/hermes-token",
    userId: "usr_1",
  } as const;

  it("retries after a transient token-endpoint failure", async () => {
    // configure() recorded the config as applied *before* rebuild ran, and rebuild bailed
    // out after teardown when the mint failed. The widget was left with no store and no
    // client, while every later configure() — the element fires one on each render —
    // short-circuited on the applied config. A single 503 at mount stranded it empty.
    const { controller, clients } = harness({ fetch: flakyTokenFetch(1) });

    controller.configure(WITH_TOKEN_URL);
    await settle();
    expect(clients).toHaveLength(0);

    // The element re-renders and configures again with the same values.
    controller.configure(WITH_TOKEN_URL);
    await settle();

    expect(clients).toHaveLength(1);
  });

  it("reports the failure rather than swallowing it", async () => {
    const errors: HermesError[] = [];
    const host = new StubHost();
    const controller = new InboxController(host, {
      clientFactory: () => new FakeHermesClient(fakePage()).asClient(),
      fetch: flakyTokenFetch(1),
      onError: (error) => errors.push(error),
    });

    controller.configure(WITH_TOKEN_URL);
    await settle();

    expect(errors).toHaveLength(1);
  });

  it("leaves a superseding config's bookkeeping alone", async () => {
    // Only the current generation may clear `applied`; a newer configure() owns it now.
    const { controller, clients } = harness({ fetch: flakyTokenFetch(1) });

    controller.configure(WITH_TOKEN_URL);
    controller.configure(READY);
    await settle();

    // The direct-token config still built its client, and the failed mint did not undo it.
    expect(clients).toHaveLength(1);
    controller.configure(READY);
    await settle();
    expect(clients).toHaveLength(1);
  });
});

describe("InboxController: config-supplied client factory", () => {
  it("prefers a factory passed through the config over the constructor's", async () => {
    // This is what makes the element's documented `clientFactory` property mean anything:
    // the controller is constructed in a field initializer, long before properties are set,
    // so a factory that only arrived via the constructor could never come from the host.
    const fromConfig: FakeHermesClient[] = [];
    const { controller, clients } = harness();

    controller.configure({
      ...READY,
      clientFactory: () => {
        const fake = new FakeHermesClient(fakePage());
        fromConfig.push(fake);
        return fake.asClient();
      },
    });
    await settle();

    expect(fromConfig).toHaveLength(1);
    expect(clients).toHaveLength(0);
  });

  it("falls back to the constructor factory when the config supplies none", async () => {
    const { controller, clients } = harness();
    controller.configure(READY);
    await settle();
    expect(clients).toHaveLength(1);
  });
});
