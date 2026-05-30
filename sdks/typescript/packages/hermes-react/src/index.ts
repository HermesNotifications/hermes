// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

export { useHermesClient, useHermesInbox, useUnreadCount } from "./hooks.js";
export type {
  HermesClientConfig,
  Notification,
  InboxPage,
  InboxUpdatedEvent,
  NewNotificationEvent,
} from "@hermes-notifications/client";
