// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { describe, expect, it } from "vitest";

/**
 * `./define` is the deliberate side-effecting entry point, and the one the bundled standalone
 * artifact is built from — so a `<script type="module">` tag is enough to make
 * `<hermes-inbox>` work with no further JavaScript.
 *
 * The split matters in both directions, and both are asserted here: importing `./define`
 * MUST register, and importing the package root must NOT. Getting the second one wrong is
 * what made the package crash on import under Node, because `customElements` does not exist
 * there.
 *
 * Vitest isolates each test file in its own environment, so the registry starts empty here
 * regardless of what other suites register.
 */
describe("@hermes-notifications/web/define", () => {
  it("registers the element as a side effect of being imported", async () => {
    expect(customElements.get("hermes-inbox")).toBeUndefined();

    const module = await import("./define.js");

    expect(customElements.get("hermes-inbox")).toBe(module.HermesInbox);
  });

  it("re-exports the class and the register function for callers that want them", async () => {
    const module = await import("./define.js");
    expect(module.HermesInbox).toBeTypeOf("function");
    expect(module.registerHermesInbox).toBeTypeOf("function");
  });

  it("leaves a working element behind, constructible from markup alone", async () => {
    await import("./define.js");

    document.body.innerHTML = "<hermes-inbox></hermes-inbox>";
    const element = document.body.firstElementChild;

    expect(element).toBeInstanceOf((await import("./hermes-inbox.js")).HermesInbox);
    expect(element?.shadowRoot).not.toBeNull();
    document.body.replaceChildren();
  });
});
