// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package models

// UnreadCountCap bounds every unread count Hermes reports. A count at the cap means "at least
// this many"; below it the number is exact.
//
// The cap is not a display concern -- the widget already collapses anything above 99 to "99+",
// so 1000 is invisible in the UI for every real user. It exists to bound the *pathological*
// case, and it is load-bearing on both stores for different reasons:
//
//   - Postgres: COUNT(*) has no early exit, so a user with 400k unread rows scans 400k index
//     entries. Counting through a LIMIT subquery stops at the cap.
//   - DynamoDB: UnreadCount is a paginated Query with a FilterExpression, which is billed for
//     every item *scanned*, not every item counted. Without a bound it can burn arbitrary RCU
//     on a single badge read.
//
// It lives here, rather than in the inbox service, because four packages have to agree on it:
// both stores clamp their queries to it, the inbox service caps the cached value, and the
// delivery worker clamps its increment. A second opinion about the ceiling is how a badge and
// its own list end up disagreeing.
const UnreadCountCap = 1000
