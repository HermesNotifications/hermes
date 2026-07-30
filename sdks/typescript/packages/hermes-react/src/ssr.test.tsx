// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

// @vitest-environment node

import { renderToString } from "react-dom/server";
import { describe, expect, it } from "vitest";

/**
 * The node environment is the whole point of this file, and the reason it carries its own
 * `@vitest-environment` docblock while the rest of the package runs in jsdom.
 *
 * Before registration was made explicit, `@customElement("hermes-inbox")` ran
 * `customElements.define` at module-import time. `customElements` does not exist in Node, so
 * *importing* the package threw — which meant any Next.js server component that transitively
 * reached it crashed before rendering anything. That is a total outage for the most common
 * React deployment shape, and no jsdom test could ever have caught it, because jsdom provides
 * the global that Node lacks.
 *
 * There is nothing to implement here. This suite exists to hold Phase 2's design decision in
 * place.
 */
describe("server rendering", () => {
  it("imports the package without a custom element registry present", async () => {
    expect(globalThis.customElements).toBeUndefined();

    await expect(import("./index.js")).resolves.toBeDefined();
  });

  it("renders the widget to markup without throwing", async () => {
    const { HermesInbox } = await import("./index.js");

    const html = renderToString(
      <HermesInbox apiUrl="http://localhost:8888" token="tok" userId="usr_1" />
    );

    expect(html).toContain("<hermes-inbox");
  });

  it("emits no shadow content, so hydration has nothing to mismatch on", async () => {
    // The shadow root is created by the browser on upgrade and is never part of React's tree,
    // which is why this renders as an empty custom element and hydrates cleanly. The widget is
    // invisible until hydration — acceptable for a notification bell, and documented rather
    // than papered over.
    const { HermesInbox } = await import("./index.js");

    const html = renderToString(<HermesInbox apiUrl="http://localhost:8888" token="tok" />);

    expect(html).not.toContain("button");
    expect(html).not.toContain("Notifications");
  });

  it("registers nothing on the server", async () => {
    await import("./index.js");
    expect(globalThis.customElements).toBeUndefined();
  });

  it("serves the initial inbox state as the server snapshot", async () => {
    // useSyncExternalStore requires a server snapshot, and requires it to be stable between
    // calls. Returning a fresh object would throw "The result of getServerSnapshot should be
    // cached".
    const { initialInboxState } = await import("@hermes-notifications/client");
    expect(initialInboxState.notifications).toEqual([]);
    expect(initialInboxState.loading).toBe(false);
  });
});
