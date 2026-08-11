// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

export { HermesClient } from "./client.js";
export { InboxAPI, type InboxAPIOptions } from "./api/inbox.js";
export { UserAPI, type UserAPIOptions } from "./api/user.js";
export { HermesError, type HermesErrorKind } from "./errors.js";
export { relativeTime } from "./format/relative-time.js";
export { subjectFromToken } from "./jwt.js";

// The inbox state layer. Both the custom element and the React hooks drive these rather
// than keeping their own copies — that shared implementation is the whole point.
export {
  inboxReducer,
  initialInboxState,
  notificationFromEvent,
  type InboxAction,
  type InboxState,
} from "./inbox/state.js";
export { InboxStore, type InboxStoreOptions } from "./inbox/store.js";

export {
  eventFromPublication,
  type RealtimeStatus,
  type StatusHandler,
  type TransportFactory,
  type RealtimeSubscriptionLike,
  type RealtimeTransportLike,
} from "./realtime/connection.js";

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
