// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { afterEach, describe, expect, it } from "vitest";
import type { Notification } from "@hermes-notifications/client";
import { HermesInbox } from "./hermes-inbox.js";

/**
 * The component builds its own HermesClient from `api-url`/`token`, so these
 * tests deliberately leave both unset: `initClient` returns early, no network or
 * WebSocket is touched, and what remains is the rendering and interaction
 * behaviour this suite is about.
 *
 * `unreadCount` and `notifications` are `@state`, i.e. private to the element's
 * own rendering. Tests drive them through bracket access rather than through a
 * client, because the alternative is standing up a fake transport to assert on a
 * badge. Bracket access is checked by the compiler against the real field names,
 * so a rename still breaks this file.
 */
type InternalState = {
  unreadCount: number;
  notifications: Notification[];
};

function internals(el: HermesInbox): InternalState {
  return el as unknown as InternalState;
}

function notification(id: string, overrides: Partial<Notification> = {}): Notification {
  return {
    id,
    title: `Title ${id}`,
    body: `Body ${id}`,
    status: "delivered",
    channels: ["inbox"],
    created_at: new Date().toISOString(),
    organization_id: "org_1",
    user_id: "usr_1",
    category_id: "cat_1",
    ...overrides,
  };
}

/** Attach a fresh element and wait for its first render. */
async function mount(): Promise<HermesInbox> {
  const el = document.createElement("hermes-inbox") as HermesInbox;
  document.body.append(el);
  await el.updateComplete;
  return el;
}

function shadow(el: HermesInbox): ShadowRoot {
  if (!el.shadowRoot) throw new Error("element has no shadow root");
  return el.shadowRoot;
}

function trigger(el: HermesInbox): HTMLButtonElement {
  const button = shadow(el).querySelector<HTMLButtonElement>("button.trigger");
  if (!button) throw new Error("trigger button not rendered");
  return button;
}

async function clickTrigger(el: HermesInbox): Promise<void> {
  trigger(el).click();
  await el.updateComplete;
}

afterEach(() => {
  document.body.replaceChildren();
});

describe("<hermes-inbox>", () => {
  it("registers itself under the hermes-inbox tag name", async () => {
    expect(customElements.get("hermes-inbox")).toBe(HermesInbox);
    expect(await mount()).toBeInstanceOf(HermesInbox);
  });

  it("renders closed, with no popover, until the trigger is clicked", async () => {
    const el = await mount();
    expect(shadow(el).querySelector(".popover")).toBeNull();

    await clickTrigger(el);
    expect(shadow(el).querySelector(".popover")).not.toBeNull();
  });

  it("closes again on a second click", async () => {
    const el = await mount();
    await clickTrigger(el);
    await clickTrigger(el);

    expect(shadow(el).querySelector(".popover")).toBeNull();
  });

  it("shows the empty state when there is nothing to list", async () => {
    const el = await mount();
    await clickTrigger(el);

    expect(shadow(el).querySelector(".empty")?.textContent).toBe("No notifications");
  });

  it("lists a notification's title and body when open", async () => {
    const el = await mount();
    internals(el).notifications = [notification("n1", { title: "Deploy done", body: "v2 is live" })];
    await clickTrigger(el);

    expect(shadow(el).querySelector(".notification-title")?.textContent).toBe("Deploy done");
    expect(shadow(el).querySelector(".notification-body")?.textContent).toBe("v2 is live");
    expect(shadow(el).querySelector(".empty")).toBeNull();
  });

  it.each([
    { name: "hides the badge when nothing is unread", count: 0, want: null },
    { name: "shows a single unread as its own number", count: 1, want: "1" },
    { name: "shows the exact count at the display cap", count: 99, want: "99" },
    // The cap is `> 99`, so 100 is the first value that collapses to "99+".
    { name: "collapses the first count above the cap", count: 100, want: "99+" },
    { name: "collapses counts far above the cap", count: 4321, want: "99+" },
  ])("$name", async ({ count, want }) => {
    const el = await mount();
    internals(el).unreadCount = count;
    await el.updateComplete;

    expect(shadow(el).querySelector(".badge")?.textContent ?? null).toBe(want);
  });

  it("offers 'mark all read' only while something is unread", async () => {
    const el = await mount();
    await clickTrigger(el);
    expect(shadow(el).querySelector(".mark-all-read")).toBeNull();

    internals(el).unreadCount = 3;
    await el.updateComplete;
    expect(shadow(el).querySelector(".mark-all-read")).not.toBeNull();
  });

  it.each([
    { name: "marks an unread notification with the unread dot", readAt: undefined, wantDot: true },
    {
      name: "marks a read notification with the read dot",
      readAt: "2026-07-01T00:00:00.000Z",
      wantDot: false,
    },
  ])("$name", async ({ readAt, wantDot }) => {
    const el = await mount();
    internals(el).notifications = [notification("n1", { read_at: readAt })];
    await clickTrigger(el);

    expect(shadow(el).querySelector(".unread-dot") !== null).toBe(wantDot);
    // The Read action is offered only where there is something to read.
    const actions = [...shadow(el).querySelectorAll(".action-btn")].map((b) => b.textContent);
    expect(actions.includes("Read")).toBe(wantDot);
    expect(actions).toContain("Archive");
  });
});
