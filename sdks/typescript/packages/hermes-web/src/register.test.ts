// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { describe, expect, it } from "vitest";
import { HermesInbox } from "./hermes-inbox.js";
import { registerHermesInbox } from "./register.js";

/**
 * Registration used to be a side effect of `@customElement`, which runs
 * `customElements.define` at module-import time. That made the package impossible to import
 * in Node — `customElements` is undefined there — so any Next.js server component that
 * transitively reached it crashed on import, before rendering anything.
 *
 * Making registration an explicit, guarded call is what fixes that, and it is also what
 * lets the package declare an honest `sideEffects` field.
 */
describe("registerHermesInbox", () => {
  it("defines the element under its default tag name", () => {
    registerHermesInbox();
    expect(customElements.get("hermes-inbox")).toBe(HermesInbox);
  });

  it("is idempotent — a second call does not throw", () => {
    // Two independent consumers (say the React wrapper and a direct import) may both
    // register. customElements.define throws on a duplicate name, so the guard is what
    // stops the second one taking the page down.
    registerHermesInbox();
    expect(() => registerHermesInbox()).not.toThrow();
  });

  it("registers under a custom tag name when asked", () => {
    // The registry allows one constructor per name, so an alias is registered as a subclass
    // rather than the base class — a bare define() with the same constructor throws
    // NotSupportedError. Instances still satisfy `instanceof HermesInbox`, which is the
    // contract that actually matters to a consumer.
    registerHermesInbox();
    registerHermesInbox("my-inbox");

    const aliased = customElements.get("my-inbox");
    expect(aliased).toBeTypeOf("function");
    expect(document.createElement("my-inbox")).toBeInstanceOf(HermesInbox);
  });

  it("gives an aliased element its own working shadow root", async () => {
    registerHermesInbox();
    registerHermesInbox("second-inbox");

    const element = document.createElement("second-inbox");
    document.body.append(element);
    await (element as HermesInbox).updateComplete;

    expect(element.shadowRoot?.querySelector("button.trigger")).not.toBeNull();
    document.body.replaceChildren();
  });

  it("reports whether it registered anything", () => {
    expect(registerHermesInbox("fresh-inbox-tag")).toBe(true);
    expect(registerHermesInbox("fresh-inbox-tag")).toBe(false);
  });

  it("does nothing and does not throw where there is no custom element registry", () => {
    // This is the server-rendering case. Deleting the global is the closest jsdom can come
    // to Node's environment; the companion assertion runs for real in the React package's
    // node-environment SSR suite.
    const registry = globalThis.customElements;
    // @ts-expect-error deliberately removing a global to simulate a non-browser runtime
    delete globalThis.customElements;
    try {
      expect(registerHermesInbox("ssr-inbox-tag")).toBe(false);
    } finally {
      globalThis.customElements = registry;
    }
  });
});

describe("importing the class alone", () => {
  it("does not register the element", async () => {
    // hermes-inbox.js must stay free of import-time side effects; only ./define and an
    // explicit registerHermesInbox() call may touch the registry.
    const tag = "side-effect-probe-inbox";
    await import("./hermes-inbox.js");
    expect(customElements.get(tag)).toBeUndefined();
  });
});
