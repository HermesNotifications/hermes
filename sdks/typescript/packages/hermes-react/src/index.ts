// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

export { useHermesClient, useHermesInbox, useUnreadCount } from "./hooks.js";
export type {
  HermesClientConfig,
  Notification,
  InboxPage,
  InboxUpdatedEvent,
  NewNotificationEvent,
} from "@hermes-notifications/client";
