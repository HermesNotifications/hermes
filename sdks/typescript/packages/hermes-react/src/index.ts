// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

export { useHermesClient, useHermesInbox, useUnreadCount } from "./hooks.js";
export type {
  HermesClientConfig,
  Notification,
  InboxPage,
  InboxUpdatedEvent,
  NewNotificationEvent,
} from "@hermes-notifications/client";
