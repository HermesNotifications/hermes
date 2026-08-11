// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { afterEach, beforeAll, describe, expect, it } from "vitest";
import type { Notification } from "@hermes-notifications/client";
import { FakeHermesClient, fakeNotification, fakePage } from "@hermes-notifications/client/testing";
import { HermesInbox } from "./hermes-inbox.js";
import { registerHermesInbox } from "./register.js";

/**
 * The previous version of this suite reached into the element's private `@state` fields with
 * an `as unknown as` cast, because the element built its own client and there was no way to
 * give it data. It documented that as a compromise and covered only rendering.
 *
 * Now the element takes an injected client, so these tests drive it the way a real page does
 * — real state, produced by a real store, from a fake transport — and the network-facing
 * half is reachable at last.
 */

beforeAll(() => {
  registerHermesInbox();
});

/** Mount an element wired to `fake`, and wait for the first load to render. */
async function mount(
  fake: FakeHermesClient = new FakeHermesClient(fakePage()),
  attributes: Record<string, string> = {}
): Promise<HermesInbox> {
  const el = document.createElement("hermes-inbox") as HermesInbox;
  for (const [name, value] of Object.entries(attributes)) el.setAttribute(name, value);
  el.client = fake.asClient();
  document.body.append(el);
  await el.updateComplete;
  // One extra microtask turn for the store's initial list() to resolve and re-render.
  await new Promise((resolve) => setTimeout(resolve, 0));
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

/** Click a shadow-DOM element and let the resulting render settle. */
async function click(el: HermesInbox, selector: string): Promise<void> {
  const target = shadow(el).querySelector<HTMLElement>(selector);
  if (!target) throw new Error(`no element matching ${selector}`);
  target.click();
  await new Promise((resolve) => setTimeout(resolve, 0));
  await el.updateComplete;
}

function text(el: HermesInbox, selector: string): string | null {
  return shadow(el).querySelector(selector)?.textContent?.trim() ?? null;
}

function unread(id: string, overrides: Partial<Notification> = {}): Notification {
  return fakeNotification(id, overrides);
}

function read(id: string, overrides: Partial<Notification> = {}): Notification {
  return fakeNotification(id, { read_at: "2026-07-01T00:00:00.000Z", ...overrides });
}

afterEach(() => {
  document.body.replaceChildren();
});

describe("<hermes-inbox>: opening and closing", () => {
  it("renders closed, with no panel, until the trigger is clicked", async () => {
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

  it("reflects the open state as an attribute so a host can style on it", async () => {
    const el = await mount();
    await clickTrigger(el);
    expect(el.hasAttribute("open")).toBe(true);
  });

  it("tracks the open state in aria-expanded", async () => {
    const el = await mount();
    expect(trigger(el).getAttribute("aria-expanded")).toBe("false");
    await clickTrigger(el);
    expect(trigger(el).getAttribute("aria-expanded")).toBe("true");
  });

  it("closes on Escape and returns focus to the trigger", async () => {
    const el = await mount();
    await clickTrigger(el);

    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    await el.updateComplete;

    expect(shadow(el).querySelector(".popover")).toBeNull();
    expect(shadow(el).activeElement).toBe(trigger(el));
  });

  it("closes when a pointer goes down outside the element", async () => {
    const el = await mount();
    await clickTrigger(el);

    document.body.dispatchEvent(new PointerEvent("pointerdown", { bubbles: true, composed: true }));
    await el.updateComplete;

    expect(shadow(el).querySelector(".popover")).toBeNull();
  });

  it("stays open when a pointer goes down inside the panel", async () => {
    // The reason this can regress: across a shadow boundary the event target is retargeted
    // to the host, so the obvious `this.contains(event.target)` check reports true for every
    // click on the page and the panel closes the instant you touch it. composedPath is what
    // distinguishes inside from outside.
    const el = await mount();
    await clickTrigger(el);

    shadow(el)
      .querySelector(".popover")
      ?.dispatchEvent(new PointerEvent("pointerdown", { bubbles: true, composed: true }));
    await el.updateComplete;

    expect(shadow(el).querySelector(".popover")).not.toBeNull();
  });

  it("stops listening for outside clicks once closed", async () => {
    const el = await mount();
    await clickTrigger(el);
    await clickTrigger(el);

    // Should be a no-op rather than a second open-change event.
    const events: unknown[] = [];
    document.addEventListener("hermes-open-change", (event) => events.push(event));
    document.body.dispatchEvent(new PointerEvent("pointerdown", { bubbles: true, composed: true }));
    await el.updateComplete;

    expect(events).toEqual([]);
  });
});

describe("<hermes-inbox>: the badge", () => {
  it.each([
    { name: "hides the badge when nothing is unread", count: 0, want: null },
    { name: "shows a single unread as its own number", count: 1, want: "1" },
    { name: "shows the exact count at the display cap", count: 99, want: "99" },
    // The cap is `> 99`, so 100 is the first value that collapses.
    { name: "collapses the first count above the cap", count: 100, want: "99+" },
    { name: "collapses counts far above the cap", count: 4321, want: "99+" },
  ])("$name", async ({ count, want }) => {
    const el = await mount(new FakeHermesClient(fakePage({ unreadCount: count })));
    expect(text(el, ".badge")).toBe(want);
  });

  it("hides the badge from assistive technology and announces via a live region instead", async () => {
    // A live region has to be in the DOM before its content changes. The badge is
    // conditionally rendered, so marking it aria-live would announce nothing on the 0 -> 1
    // transition — the only transition anyone cares about.
    const el = await mount(new FakeHermesClient(fakePage({ unreadCount: 2 })));

    expect(shadow(el).querySelector(".badge")?.getAttribute("aria-hidden")).toBe("true");
    const status = shadow(el).querySelector('[role="status"]');
    expect(status?.getAttribute("aria-live")).toBe("polite");
    expect(status?.textContent?.trim()).toBe("2 unread notifications");
  });

  it("keeps the live region mounted while the count is zero", async () => {
    const el = await mount();
    expect(shadow(el).querySelector('[role="status"]')).not.toBeNull();
  });

  it("names the unread count in the trigger's accessible label", async () => {
    const el = await mount(new FakeHermesClient(fakePage({ unreadCount: 3 })));
    expect(trigger(el).getAttribute("aria-label")).toBe("Notifications, 3 unread");
  });
});

describe("<hermes-inbox>: listing", () => {
  it("renders the loaded page", async () => {
    const fake = new FakeHermesClient(
      fakePage({ data: [unread("n1", { title: "Deploy done", body: "v2 is live" })], unreadCount: 1 })
    );
    const el = await mount(fake);
    await clickTrigger(el);

    expect(text(el, ".notification-title")).toBe("Deploy done");
    expect(text(el, ".notification-body")).toBe("v2 is live");
    expect(shadow(el).querySelector(".empty")).toBeNull();
  });

  it("shows the empty state when there is nothing to list", async () => {
    const el = await mount();
    await clickTrigger(el);
    expect(text(el, ".empty")).toBe("No notifications");
  });

  it("honours a custom heading and empty text", async () => {
    const el = await mount(new FakeHermesClient(fakePage()), {
      heading: "Alerts",
      "empty-text": "All clear",
    });
    await clickTrigger(el);

    expect(text(el, "#hermes-heading")).toBe("Alerts");
    expect(text(el, ".empty")).toBe("All clear");
  });

  it("renders a relative timestamp", async () => {
    const el = await mount(
      new FakeHermesClient(
        fakePage({ data: [unread("n1", { created_at: new Date().toISOString() })], unreadCount: 1 })
      )
    );
    await clickTrigger(el);
    expect(text(el, ".notification-time")).toBe("just now");
  });

  it.each([
    { name: "marks an unread row with the unread dot", row: unread("n1"), wantDot: true },
    { name: "marks a read row with the read dot", row: read("n1"), wantDot: false },
  ])("$name", async ({ row, wantDot }) => {
    const el = await mount(new FakeHermesClient(fakePage({ data: [row], unreadCount: 1 })));
    await clickTrigger(el);

    expect(shadow(el).querySelector(".unread-dot") !== null).toBe(wantDot);
    const actions = [...shadow(el).querySelectorAll(".action-btn")].map((b) => b.textContent?.trim());
    expect(actions.includes("Read")).toBe(wantDot);
    expect(actions).toContain("Archive");
  });

  it("makes each row keyboard reachable as a real interactive element", async () => {
    // Rows used to be plain divs with a click handler, so a keyboard user could not reach
    // them at all.
    const el = await mount(new FakeHermesClient(fakePage({ data: [unread("n1")], unreadCount: 1 })));
    await clickTrigger(el);

    const row = shadow(el).querySelector(".row-target");
    expect(row?.tagName).toBe("BUTTON");
  });

  it("offers 'mark all read' only while something is unread", async () => {
    const el = await mount(new FakeHermesClient(fakePage({ data: [read("n1")], unreadCount: 0 })));
    await clickTrigger(el);
    expect(shadow(el).querySelector(".mark-all-read")).toBeNull();

    const withUnread = await mount(
      new FakeHermesClient(fakePage({ data: [unread("n2")], unreadCount: 3 }))
    );
    await clickTrigger(withUnread);
    expect(shadow(withUnread).querySelector(".mark-all-read")).not.toBeNull();
  });
});

describe("<hermes-inbox>: actions reach the server", () => {
  it("marks a row read and updates the badge", async () => {
    const fake = new FakeHermesClient(fakePage({ data: [unread("n1")], unreadCount: 1 }));
    const el = await mount(fake);
    await clickTrigger(el);

    await click(el, ".action-btn");

    expect(fake.calls).toContain("markRead:n1");
    expect(text(el, ".badge")).toBeNull();
    expect(shadow(el).querySelector(".unread-dot")).toBeNull();
  });

  it("archives a row and removes it from the list", async () => {
    const fake = new FakeHermesClient(
      fakePage({ data: [unread("n1"), unread("n2")], unreadCount: 2 })
    );
    const el = await mount(fake);
    await clickTrigger(el);

    await click(el, '[aria-label="Archive Title n1"]');

    expect(fake.calls).toContain("archive:n1");
    expect(shadow(el).querySelectorAll(".notification")).toHaveLength(1);
  });

  it("marks everything read", async () => {
    const fake = new FakeHermesClient(
      fakePage({ data: [unread("n1"), unread("n2")], unreadCount: 2 })
    );
    const el = await mount(fake);
    await clickTrigger(el);

    await click(el, ".mark-all-read");

    expect(fake.calls).toContain("markAllRead");
    expect(text(el, ".badge")).toBeNull();
  });

  it("marks a row read when the row itself is activated", async () => {
    const fake = new FakeHermesClient(fakePage({ data: [unread("n1")], unreadCount: 1 }));
    const el = await mount(fake);
    await clickTrigger(el);

    await click(el, ".row-target");

    expect(fake.calls).toContain("markRead:n1");
  });

  it("restores the row and reports the error when the server rejects an action", async () => {
    const fake = new FakeHermesClient(fakePage({ data: [unread("n1")], unreadCount: 1 }));
    fake.fail("archive", new Error("boom"));
    const el = await mount(fake);
    await clickTrigger(el);
    const errors: unknown[] = [];
    document.addEventListener("hermes-error", (event) => errors.push(event));

    await click(el, '[aria-label="Archive Title n1"]');

    expect(shadow(el).querySelectorAll(".notification")).toHaveLength(1);
    expect(errors).toHaveLength(1);
    expect(shadow(el).querySelector('[role="alert"]')).not.toBeNull();
  });
});

describe("<hermes-inbox>: pagination", () => {
  it("offers 'load more' only when the server reported another page", async () => {
    const withMore = await mount(
      new FakeHermesClient(fakePage({ data: [unread("n1")], unreadCount: 1, cursor: "c1" }))
    );
    await clickTrigger(withMore);
    expect(shadow(withMore).querySelector(".load-more")).not.toBeNull();

    const lastPage = await mount(
      new FakeHermesClient(fakePage({ data: [unread("n2")], unreadCount: 1 }))
    );
    await clickTrigger(lastPage);
    expect(shadow(lastPage).querySelector(".load-more")).toBeNull();
  });

  it("appends the next page and sends the cursor", async () => {
    // The old element stored the cursor and never read it — there was no pagination at all.
    const fake = new FakeHermesClient(
      fakePage({ data: [unread("n1")], unreadCount: 1, cursor: "c1" })
    );
    const el = await mount(fake);
    await clickTrigger(el);

    fake.page = fakePage({ data: [unread("n2")], unreadCount: 1 });
    await click(el, ".load-more");

    expect(fake.listOptions[1]).toMatchObject({ cursor: "c1" });
    expect(shadow(el).querySelectorAll(".notification")).toHaveLength(2);
    expect(shadow(el).querySelector(".load-more")).toBeNull();
  });
});

describe("<hermes-inbox>: action urls", () => {
  it("renders a link to the action url with its label", async () => {
    // action_url and action_label have always been on the schema and on the realtime
    // payload, and the widget never rendered either.
    const el = await mount(
      new FakeHermesClient(
        fakePage({
          data: [
            unread("n1", {
              action_url: "https://example.com/invoices/1234",
              action_label: "View invoice",
            }),
          ],
          unreadCount: 1,
        })
      )
    );
    await clickTrigger(el);

    const link = shadow(el).querySelector<HTMLAnchorElement>("a.row-target");
    expect(link?.getAttribute("href")).toBe("https://example.com/invoices/1234");
    expect(text(el, ".action-link")).toBe("View invoice");
  });

  it("falls back to a generic label when the notification supplies none", async () => {
    const el = await mount(
      new FakeHermesClient(
        fakePage({ data: [unread("n1", { action_url: "https://example.com/x" })], unreadCount: 1 })
      )
    );
    await clickTrigger(el);
    expect(text(el, ".action-link")).toBe("View");
  });

  it("dispatches a cancellable hermes-action so a host can route internally", async () => {
    const el = await mount(
      new FakeHermesClient(
        fakePage({ data: [unread("n1", { action_url: "https://example.com/x" })], unreadCount: 1 })
      )
    );
    await clickTrigger(el);

    let received: CustomEvent | undefined;
    document.addEventListener("hermes-action", (event) => {
      received = event as CustomEvent;
      event.preventDefault();
    });
    const link = shadow(el).querySelector<HTMLAnchorElement>("a.row-target");
    const clickEvent = new MouseEvent("click", { bubbles: true, composed: true, cancelable: true });
    link?.dispatchEvent(clickEvent);
    await el.updateComplete;

    expect(received?.cancelable).toBe(true);
    expect(received?.detail.notification.id).toBe("n1");
    // Cancelled by the host, so the browser must not navigate away.
    expect(clickEvent.defaultPrevented).toBe(true);
  });
});

describe("<hermes-inbox>: the clientFactory escape hatch", () => {
  it("builds its client through a factory set on the element", async () => {
    // The property was declared and documented as an escape hatch for tests and wrappers,
    // but nothing read it: applyConfig never forwarded it, and the controller took a factory
    // only from its constructor — which runs in a field initializer, before any property is
    // assigned. Setting it silently did nothing and the element opened a real socket.
    const fake = new FakeHermesClient(
      fakePage({ data: [fakeNotification("from-factory")], unreadCount: 1 })
    );
    let calls = 0;

    const el = document.createElement("hermes-inbox") as HermesInbox;
    el.clientFactory = () => {
      calls++;
      return fake.asClient();
    };
    el.apiUrl = "http://localhost:8888";
    el.token = "jwt-token";
    document.body.append(el);
    await el.updateComplete;
    await new Promise((resolve) => setTimeout(resolve, 0));
    await el.updateComplete;

    expect(calls).toBe(1);
    await clickTrigger(el);
    expect(text(el, ".notification-title")).toBe("Title from-factory");
  });
});

describe("<hermes-inbox>: unsafe action urls", () => {
  /**
   * `action_url` is attacker-influenced: the send API takes it as an unvalidated string and
   * it also arrives over the websocket. lit does not sanitize `href`, so interpolating it
   * unchecked put script execution in the *host page* one click away — in a widget built to
   * be embedded in customer applications.
   */
  it.each([
    { name: "javascript", url: "javascript:alert(document.domain)" },
    { name: "javascript with mixed case", url: "JaVaScRiPt:alert(1)" },
    { name: "javascript padded with whitespace", url: "  javascript:alert(1)  " },
    { name: "javascript split by a tab", url: "java\tscript:alert(1)" },
    { name: "javascript split by a newline", url: "java\nscript:alert(1)" },
    { name: "data", url: "data:text/html,<script>alert(1)</script>" },
    { name: "vbscript", url: "vbscript:msgbox(1)" },
  ])("renders no link for a $name url", async ({ url }) => {
    const el = await mount(
      new FakeHermesClient(
        fakePage({ data: [unread("n1", { action_url: url })], unreadCount: 1 })
      )
    );
    await clickTrigger(el);

    expect(shadow(el).querySelector("a.row-target")).toBeNull();
    // Still an interactive row, so the notification stays reachable and keyboard-usable.
    expect(shadow(el).querySelector("button.row-target")).not.toBeNull();
    expect(shadow(el).innerHTML).not.toContain("alert(");
  });

  it.each([
    { name: "https", url: "https://example.com/invoices/1" },
    { name: "http", url: "http://example.com/invoices/1" },
    { name: "root-relative", url: "/invoices/1" },
    { name: "document-relative", url: "invoices/1" },
  ])("still renders a link for a $name url", async ({ url }) => {
    const el = await mount(
      new FakeHermesClient(
        fakePage({ data: [unread("n1", { action_url: url })], unreadCount: 1 })
      )
    );
    await clickTrigger(el);

    const link = shadow(el).querySelector<HTMLAnchorElement>("a.row-target");
    // The original string is preserved, so a host that routes internally sees what it sent.
    expect(link?.getAttribute("href")).toBe(url);
  });
});

describe("<hermes-inbox>: events cross the shadow boundary", () => {
  // Without composed: true a CustomEvent stops at the shadow root, so no ancestor listener
  // ever fires — which is what made the React wrapper impossible. Asserting on `document`
  // is what actually proves it.
  it.each([
    { name: "hermes-open-change", act: (el: HermesInbox) => clickTrigger(el) },
    {
      name: "hermes-notification",
      act: async (el: HermesInbox, fake: FakeHermesClient) => {
        fake.emit("notification", {
          type: "notification.new",
          id: "n9",
          title: "T",
          body: "B",
          createdAt: "2026-07-29T10:00:00.000Z",
        });
        await el.updateComplete;
      },
    },
    {
      name: "hermes-update",
      act: async (el: HermesInbox, fake: FakeHermesClient) => {
        fake.emit("update", {
          type: "inbox.updated",
          notificationId: "n1",
          action: "read",
          unreadCount: 0,
          timestamp: 1,
        });
        await el.updateComplete;
      },
    },
    {
      name: "hermes-connected",
      act: async (el: HermesInbox, fake: FakeHermesClient) => {
        fake.emitStatus("connected");
        await el.updateComplete;
      },
    },
  ])("$name reaches a document-level listener", async ({ name, act }) => {
    const fake = new FakeHermesClient(fakePage({ data: [unread("n1")], unreadCount: 1 }));
    const el = await mount(fake);
    const seen: Event[] = [];
    document.addEventListener(name, (event) => seen.push(event));

    await act(el, fake);

    expect(seen).toHaveLength(1);
    expect(seen[0].composed).toBe(true);
    expect(seen[0].bubbles).toBe(true);
  });

  it("carries the unread count on hermes-unread-count-change", async () => {
    const fake = new FakeHermesClient(fakePage({ data: [unread("n1")], unreadCount: 1 }));
    const el = await mount(fake);
    const counts: number[] = [];
    document.addEventListener("hermes-unread-count-change", (event) => {
      counts.push((event as CustomEvent<number>).detail);
    });

    fake.emit("update", {
      type: "inbox.updated",
      notificationId: "n1",
      action: "read",
      unreadCount: 7,
      timestamp: 1,
    });
    await el.updateComplete;

    expect(counts).toContain(7);
  });
});

describe("<hermes-inbox>: realtime arrivals", () => {
  it("prepends an arriving notification and raises the badge", async () => {
    const fake = new FakeHermesClient(fakePage({ data: [unread("n1")], unreadCount: 1 }));
    const el = await mount(fake);
    await clickTrigger(el);

    fake.emit("notification", {
      type: "notification.new",
      id: "n2",
      title: "Fresh",
      body: "Just arrived",
      createdAt: "2026-07-29T10:00:00.000Z",
    });
    await el.updateComplete;

    expect(text(el, ".notification-title")).toBe("Fresh");
    expect(text(el, ".badge")).toBe("2");
  });
});

describe("<hermes-inbox>: theming surface", () => {
  it("exposes the parts a host needs to restyle it from outside the shadow root", async () => {
    const fake = new FakeHermesClient(
      fakePage({ data: [unread("n1", { action_url: "https://example.com/x" })], unreadCount: 1, cursor: "c1" })
    );
    const el = await mount(fake);
    await clickTrigger(el);

    const parts = new Set(
      [...shadow(el).querySelectorAll("[part]")].flatMap((node) =>
        (node.getAttribute("part") ?? "").split(/\s+/)
      )
    );
    for (const expected of [
      "trigger",
      "badge",
      "status",
      "popover",
      "header",
      "mark-all-read",
      "list",
      "notification",
      "unread",
      "unread-dot",
      "title",
      "body",
      "time",
      "actions",
      "action-btn",
      "action-link",
      "footer",
      "load-more",
    ]) {
      expect(parts).toContain(expected);
    }
  });

  it("keeps part=notification matching every row while adding a read/unread token", async () => {
    const fake = new FakeHermesClient(
      fakePage({ data: [unread("n1"), read("n2")], unreadCount: 1 })
    );
    const el = await mount(fake);
    await clickTrigger(el);

    expect(shadow(el).querySelectorAll('[part~="notification"]')).toHaveLength(2);
    expect(shadow(el).querySelectorAll('[part~="unread"]')).toHaveLength(1);
    expect(shadow(el).querySelectorAll('[part~="read"]')).toHaveLength(1);
  });

  it("takes its stacking order from a custom property so a host modal cannot clip it", () => {
    // A hardcoded `z-index: 1000` is unfixable from outside a shadow root: any host with a
    // modal above that value clips the panel and the integrator has no lever.
    //
    // This asserts on the stylesheet text rather than a computed style because jsdom does not
    // resolve var() in getComputedStyle — it reports "auto" no matter what. The behavioural
    // assertion (setting the property actually moves the panel) needs a real browser and
    // lives in the Playwright theming spec.
    const css = HermesInbox.styles.toString();

    expect(css).toContain("z-index: var(--hermes-popover-z-index, 1000)");
    expect(css).not.toMatch(/z-index:\s*1000/);
  });

  it("routes every colour and metric through a custom property with a fallback", () => {
    // Spot-checks the theming contract the docs promise. A literal creeping in here is a
    // value an integrator cannot override.
    const css = HermesInbox.styles.toString();
    for (const property of [
      "--hermes-badge-bg",
      "--hermes-popover-bg",
      "--hermes-popover-width",
      "--hermes-accent-color",
      "--hermes-text-color",
      "--hermes-border-color",
      "--hermes-focus-ring",
    ]) {
      expect(css).toContain(`var(${property},`);
    }
  });
});
