// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { HermesInbox } from "./hermes-inbox.js";

/**
 * Register `<hermes-inbox>` with the browser's custom element registry.
 *
 * This is an explicit call rather than an import side effect, for two reasons:
 *
 * 1. **Server rendering.** `customElements` does not exist in Node, so defining the element
 *    at module scope made the package unimportable from any server component. Guarding here
 *    means the module is safe to import anywhere and simply does nothing on a server.
 * 2. **Honest tree-shaking.** With registration confined to this function and `./define`,
 *    the package root can declare itself side-effect free without risking a bundler dropping
 *    the registration.
 *
 * Idempotent: registering a name that is already taken is a no-op rather than the
 * `NotSupportedError` that `customElements.define` would otherwise throw when two consumers
 * both register.
 *
 * @param tagName Tag to register under. Defaults to `hermes-inbox`.
 * @returns Whether this call performed the registration.
 */
export function registerHermesInbox(tagName = "hermes-inbox"): boolean {
  if (typeof customElements === "undefined") return false;
  if (customElements.get(tagName)) return false;

  // The registry rejects the same constructor under a second name, so an alias needs its own
  // class. Registering under two names is a real need — two versions of the widget on one
  // page, or a host that already owns the default tag — and a bare `define` would throw
  // NotSupportedError instead. Instances remain `instanceof HermesInbox`.
  const constructor = baseIsRegistered() ? class extends HermesInbox {} : HermesInbox;
  customElements.define(tagName, constructor);
  return true;
}

/** Whether the base class itself has already been claimed by some tag name. */
function baseIsRegistered(): boolean {
  return customElements.getName !== undefined
    ? customElements.getName(HermesInbox) !== null
    : // Older engines lack getName; fall back to the default tag, which is the only name
      // this package registers the base class under.
      customElements.get("hermes-inbox") === HermesInbox;
}
