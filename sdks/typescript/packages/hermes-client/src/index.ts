// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

export { HermesClient } from "./client.js";
export { InboxAPI } from "./api/inbox.js";
export { UserAPI } from "./api/user.js";
export type {
  HermesClientConfig,
  Notification,
  User,
  PreferenceCategory,
  PreferenceSubscription,
  InboxPage,
  InboxUpdatedEvent,
  NewNotificationEvent,
  HermesEvent,
} from "./types.js";
