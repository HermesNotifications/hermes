// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

/**
 * Package root — deliberately free of side effects, so it is safe to import from a server
 * component. Call {@link registerHermesInbox}, or import
 * `@hermes-notifications/web/define`, to register the element.
 */
export { HermesInbox } from "./hermes-inbox.js";
export { registerHermesInbox } from "./register.js";
export {
  InboxController,
  type ClientFactory,
  type InboxControllerConfig,
  type InboxControllerOptions,
} from "./inbox-controller.js";
