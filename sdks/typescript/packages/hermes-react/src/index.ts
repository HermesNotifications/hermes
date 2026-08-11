// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

// The widget: a React binding over the <hermes-inbox> custom element.
export { HermesInbox, type HermesInboxProps } from "./hermes-inbox.js";

// Configure once near the root so every widget and hook below shares one client, and
// therefore one Centrifugo connection.
export { HermesProvider, useHermes, type HermesProviderProps } from "./context.js";

// The headless path, for teams rendering their own markup.
export {
  useHermesClient,
  useHermesInbox,
  useUnreadCount,
  type UseHermesInboxOptions,
  type UseHermesInboxResult,
} from "./hooks.js";

export { HermesError, initialInboxState } from "@hermes-notifications/client";
export type {
  HermesClient,
  HermesClientConfig,
  HermesErrorKind,
  InboxPage,
  InboxState,
  InboxUpdatedEvent,
  NewNotificationEvent,
  Notification,
  RealtimeStatus,
} from "@hermes-notifications/client";
