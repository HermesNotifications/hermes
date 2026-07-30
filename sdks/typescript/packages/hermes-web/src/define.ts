// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

/**
 * Side-effecting entry point: importing this module registers `<hermes-inbox>`.
 *
 * Kept separate from `index.ts` so the package root stays safe to import on a server and
 * honest about its `sideEffects` field. This is also the entry the bundled standalone build
 * is produced from, so a single `<script type="module">` tag is enough to make the element
 * work in any page, with no framework and no build step.
 */
import { registerHermesInbox } from "./register.js";

registerHermesInbox();

export { HermesInbox } from "./hermes-inbox.js";
export { registerHermesInbox } from "./register.js";
