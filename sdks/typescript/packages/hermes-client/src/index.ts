// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

export { HermesClient } from "./client.js";
export { InboxAPI } from "./api/inbox.js";
export { UserAPI } from "./api/user.js";
export type {
  HermesClientConfig,
  Notification,
  User,
  UserPreference,
  InboxPage,
  InboxUpdatedEvent,
  NewNotificationEvent,
  HermesEvent,
} from "./types.js";
