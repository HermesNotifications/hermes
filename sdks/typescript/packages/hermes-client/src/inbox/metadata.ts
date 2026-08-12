// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

/**
 * The two keys Hermes reserves inside a notification's `metadata`.
 *
 * These readers live here, in the client package, rather than in the widget or the React
 * bindings, for the same reason `inboxReducer` does: both consumers need them, and a second
 * copy is how the two implementations diverged before ADR 0013 consolidated them.
 */

/** Levels Hermes understands, in presentation order. */
export const NOTIFICATION_LEVELS = ["info", "success", "warning", "error"] as const;

export type NotificationLevel = (typeof NOTIFICATION_LEVELS)[number];

/** Anything carrying Hermes metadata: a REST `Notification` or a `notification.new` event. */
interface WithMetadata {
  metadata?: unknown;
}

function metadataRecord(source: WithMetadata | null | undefined): Record<string, unknown> | undefined {
  const metadata = source?.metadata;
  // Arrays are objects in JavaScript, and `metadata` is attacker-influenced input that reaches
  // here unvalidated from the websocket.
  return typeof metadata === "object" && metadata !== null && !Array.isArray(metadata)
    ? (metadata as Record<string, unknown>)
    : undefined;
}

/**
 * The notification's declared level, or `undefined`.
 *
 * An unrecognised value is reported the same as an absent one, deliberately. The server may add
 * levels, and it must be safe for a client that predates one to meet it: degrading to "no level"
 * keeps the notification renderable, whereas passing the raw string through would put a value
 * into a host's `switch` that none of its branches handle.
 */
export function notificationLevel(
  source: WithMetadata | null | undefined
): NotificationLevel | undefined {
  const raw = metadataRecord(source)?.level;
  return typeof raw === "string" && (NOTIFICATION_LEVELS as readonly string[]).includes(raw)
    ? (raw as NotificationLevel)
    : undefined;
}

/**
 * Whether the sender asked for this to be surfaced transiently.
 *
 * Strictly `true`. The string `"true"` is not a request to interrupt someone, and coercing it
 * would make a caller's type error into an interruption for their users.
 */
export function toastRequested(source: WithMetadata | null | undefined): boolean {
  return metadataRecord(source)?.toast === true;
}
