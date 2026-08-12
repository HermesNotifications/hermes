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

// Toasts, provider-agnostic. The Sonner adapter lives at "@hermes-notifications/react/sonner"
// and is deliberately NOT re-exported here: a hard `import "sonner"` in the package root would
// turn an optional peer dependency into a mandatory one for every consumer.
export {
  useHermesToasts,
  type HermesToastAdapter,
  type HermesToastHandle,
  type HermesToastPayload,
  type UseHermesToastsOptions,
} from "./toasts.js";

export {
  HermesError,
  initialInboxState,
  NOTIFICATION_LEVELS,
  notificationLevel,
  toastRequested,
} from "@hermes-notifications/client";
export type {
  HermesClient,
  HermesClientConfig,
  HermesErrorKind,
  InboxPage,
  InboxState,
  InboxUpdatedEvent,
  NewNotificationEvent,
  Notification,
  NotificationLevel,
  RealtimeStatus,
} from "@hermes-notifications/client";
