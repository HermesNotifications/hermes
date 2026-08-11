// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

/**
 * Format an ISO timestamp as a short relative time: "just now", "5m ago", "3h ago",
 * "2d ago", "4mo ago".
 *
 * Buckets truncate rather than round, and months are fixed 30-day units, so a calendar
 * year reads as "12mo ago". Future timestamps clamp to "just now" instead of reporting
 * a negative age.
 *
 * `now` is a parameter rather than a `Date.now()` call so that callers — and tests —
 * control the clock. The widget passes nothing and gets the current time.
 */
export function relativeTime(timestamp: string, now: number = Date.now()): string {
  const then = new Date(timestamp).getTime();
  if (Number.isNaN(then)) return "";

  const seconds = Math.floor((now - then) / 1000);
  if (seconds < 60) return "just now";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  if (days < 30) return `${days}d ago`;
  return `${Math.floor(days / 30)}mo ago`;
}
