// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import type { HermesInbox as HermesInboxElement } from "@hermes-notifications/web";
import { FakeHermesClient, fakeNotification, fakePage } from "@hermes-notifications/client/testing";
import { HermesInbox, ensureHermesInboxRegistered } from "./hermes-inbox.js";

/**
 * The wrapper exists because React 18 has no custom-element support worth the name: it
 * stringifies every prop and wires no CustomEvent listeners. Even on React 19, `on*` props are
 * not connected to CustomEvents. So `<hermes-inbox pageSize={20} onNotification={fn}>` in raw
 * JSX would set the attribute to the string "20" and never call the handler.
 *
 * These tests assert the two things that makes the wrapper worth having: props arrive as real
 * *properties* with their real types, and events arrive as callbacks.
 */

afterEach(cleanup);

/**
 * Render the wrapper and return the upgraded custom element.
 *
 * Registration is a browser-only dynamic import, so it resolves asynchronously — that is the
 * price of keeping the element class out of the server's module graph. Lit re-applies own
 * properties assigned before upgrade, so a consumer never has to think about it; a test
 * reaching for the element directly does.
 */
async function mount(ui: React.ReactElement): Promise<HermesInboxElement> {
  const { container } = render(ui);
  await ensureHermesInboxRegistered();
  await customElements.whenDefined("hermes-inbox");
  const element = container.querySelector("hermes-inbox") as HermesInboxElement | null;
  if (!element) throw new Error("hermes-inbox was not rendered");
  await element.updateComplete;
  return element;
}

describe("<HermesInbox />: registration", () => {
  it("registers the custom element in a browser", async () => {
    await ensureHermesInboxRegistered();
    expect(customElements.get("hermes-inbox")).toBeDefined();
  });

  it("renders the custom element tag even before the class has loaded", () => {
    // The markup is produced by createElement("hermes-inbox"), not by the element class, which
    // is what makes the wrapper renderable on a server.
    const { container } = render(<HermesInbox apiUrl="http://localhost:8888" token="tok" />);
    expect(container.querySelector("hermes-inbox")).not.toBeNull();
  });

  it("registers only once even with two instances mounted", async () => {
    // customElements.define throws on a duplicate name, so a wrapper that registered per
    // render would break the second widget on the page.
    render(
      <>
        <HermesInbox apiUrl="http://localhost:8888" token="a" />
        <HermesInbox apiUrl="http://localhost:8888" token="b" />
      </>
    );
    await expect(ensureHermesInboxRegistered()).resolves.toBeUndefined();
  });
});

describe("<HermesInbox />: props become element properties", () => {
  it("passes numbers as numbers, not strings", async () => {
    // The whole point of the wrapper. Raw JSX would set the attribute to "25" and
    // `element.pageSize` would be a string.
    const element = await mount(
      <HermesInbox apiUrl="http://localhost:8888" token="tok" pageSize={25} />
    );

    expect(element.pageSize).toBe(25);
    expect(element.pageSize).toBeTypeOf("number");
  });

  it("passes booleans as booleans", async () => {
    const element = await mount(<HermesInbox apiUrl="http://localhost:8888" token="tok" archived />);
    expect(element.archived).toBe(true);
  });

  it("passes strings through", async () => {
    const element = await mount(
      <HermesInbox
        apiUrl="http://localhost:8888"
        socketUrl="http://localhost:8888/realtime"
        token="tok"
        userId="usr_1"
        heading="Alerts"
        emptyText="All clear"
      />
    );

    expect(element.apiUrl).toBe("http://localhost:8888");
    expect(element.socketUrl).toBe("http://localhost:8888/realtime");
    expect(element.userId).toBe("usr_1");
    expect(element.heading).toBe("Alerts");
    expect(element.emptyText).toBe("All clear");
  });

  it("passes a function prop through unmangled", async () => {
    // A callback cannot survive being stringified into an attribute, so this only works
    // because the wrapper assigns it as a property.
    const getToken = async () => "fresh";
    const element = await mount(
      <HermesInbox apiUrl="http://localhost:8888" tokenUrl="/api/session" getToken={getToken} />
    );

    expect(element.getToken).toBe(getToken);
  });

  it("passes an injected client through", async () => {
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    const element = await mount(<HermesInbox client={fake.asClient()} />);

    expect(element.client).toBe(fake.asClient());
  });

  it("applies className and style to the host element", async () => {
    const element = await mount(
      <HermesInbox
        apiUrl="http://localhost:8888"
        token="tok"
        className="ml-auto"
        style={{ marginLeft: "8px" }}
      />
    );

    expect(element.className).toContain("ml-auto");
    expect(element.style.marginLeft).toBe("8px");
  });
});

describe("<HermesInbox />: props that go away", () => {
  /**
   * The sync effect used to skip `undefined`, which conflates "this prop was never set"
   * with "this prop was just cleared". Only the second needs action — and it is exactly
   * what a host signing out does, dropping its client, token and userId in one render. The
   * element kept the previous values and carried on showing the signed-out user's inbox.
   */
  it("clears a string prop that becomes undefined", async () => {
    const { container, rerender } = render(
      <HermesInbox apiUrl="http://localhost:8888" token="tok" userId="usr_1" />
    );
    await ensureHermesInboxRegistered();
    await customElements.whenDefined("hermes-inbox");
    const element = container.querySelector("hermes-inbox") as HermesInboxElement;
    await element.updateComplete;
    expect(element.userId).toBe("usr_1");

    rerender(<HermesInbox apiUrl="http://localhost:8888" token="tok" />);
    await element.updateComplete;

    expect(element.userId).toBeUndefined();
  });

  it("clears an injected client when the host signs out", async () => {
    const fake = new FakeHermesClient(fakePage({ data: [fakeNotification("a")], unreadCount: 1 }));
    const { container, rerender } = render(
      <HermesInbox client={fake.asClient()} userId="usr_1" />
    );
    await ensureHermesInboxRegistered();
    await customElements.whenDefined("hermes-inbox");
    const element = container.querySelector("hermes-inbox") as HermesInboxElement;
    await element.updateComplete;
    expect(element.client).toBe(fake.asClient());

    rerender(<HermesInbox />);
    await element.updateComplete;

    expect(element.client).toBeUndefined();
    expect(element.userId).toBeUndefined();
  });

  it("leaves properties it never set alone", async () => {
    // Only properties this component assigned are cleared. Anything the host set directly
    // on the element stays its own.
    const { container, rerender } = render(<HermesInbox apiUrl="http://localhost:8888" token="tok" />);
    await ensureHermesInboxRegistered();
    await customElements.whenDefined("hermes-inbox");
    const element = container.querySelector("hermes-inbox") as HermesInboxElement;
    await element.updateComplete;
    element.heading = "Set by the host";

    rerender(<HermesInbox apiUrl="http://localhost:8888" token="tok" archived />);
    await element.updateComplete;

    expect(element.heading).toBe("Set by the host");
  });
});

describe("<HermesInbox />: events become callbacks", () => {
  it("calls onNotification when the element emits one", async () => {
    const fake = new FakeHermesClient(fakePage());
    const seen: unknown[] = [];
    const element = await mount(
      <HermesInbox client={fake.asClient()} onNotification={(event) => seen.push(event)} />
    );
    await new Promise((resolve) => setTimeout(resolve, 0));

    fake.emit("notification", {
      type: "notification.new",
      id: "n1",
      title: "T",
      body: "B",
      createdAt: "2026-07-29T10:00:00.000Z",
    });

    expect(seen).toHaveLength(1);
    expect(seen[0]).toMatchObject({ id: "n1" });
  });

  it("calls onUnreadCountChange with the count itself, not the event", async () => {
    const fake = new FakeHermesClient(fakePage());
    const counts: number[] = [];
    const element = await mount(
      <HermesInbox client={fake.asClient()} onUnreadCountChange={(count) => counts.push(count)} />
    );
    await new Promise((resolve) => setTimeout(resolve, 0));

    fake.setUnreadCount(5);

    expect(counts).toContain(5);
  });

  it("calls onOpenChange when the panel is toggled", async () => {
    const fake = new FakeHermesClient(fakePage());
    const opens: boolean[] = [];
    const element = await mount(
      <HermesInbox client={fake.asClient()} onOpenChange={(open) => opens.push(open)} />
    );

    element.shadowRoot?.querySelector<HTMLButtonElement>("button.trigger")?.click();
    await element.updateComplete;

    expect(opens).toEqual([true]);
  });

  it("stops calling handlers after unmount", async () => {
    const fake = new FakeHermesClient(fakePage());
    const seen: unknown[] = [];
    const view = render(
      <HermesInbox client={fake.asClient()} onNotification={(event) => seen.push(event)} />
    );
    const element = view.container.querySelector("hermes-inbox") as HermesInboxElement;
    await new Promise((resolve) => setTimeout(resolve, 0));

    view.unmount();
    fake.emit("notification", {
      type: "notification.new",
      id: "n1",
      title: "T",
      body: "B",
      createdAt: "2026-07-29T10:00:00.000Z",
    });

    expect(seen).toEqual([]);
  });
});
