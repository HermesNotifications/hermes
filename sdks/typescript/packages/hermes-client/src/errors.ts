// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

/**
 * Why a failure happened, at the granularity a caller can act on.
 *
 * - `unauthorized` — the token is expired or invalid. Refresh it and retry once.
 * - `forbidden` — authenticated but not permitted. Retrying will not help.
 * - `not-found` — no such resource.
 * - `invalid-cursor` — the pagination cursor was rejected. Cursors are backend-specific
 *   and are invalidated when an operator switches store backends, so the correct
 *   recovery is to discard it and re-request the first page.
 * - `rate-limited` — slow down. The inbox and user services allow 50 rps per user.
 * - `client` — some other 4xx; the request itself is wrong.
 * - `server` — a 5xx. Transient often enough to be worth retrying.
 * - `network` — the request never produced a response at all.
 */
export type HermesErrorKind =
  | "unauthorized"
  | "forbidden"
  | "not-found"
  | "invalid-cursor"
  | "rate-limited"
  | "client"
  | "server"
  | "network";

const RETRYABLE: ReadonlySet<HermesErrorKind> = new Set<HermesErrorKind>([
  "unauthorized",
  "rate-limited",
  "server",
  "network",
]);

/** Whether a 400's body indicates the cursor rather than some other bad parameter. */
function looksLikeInvalidCursor(body: unknown): boolean {
  if (typeof body !== "object" || body === null) return false;
  const values = Object.values(body as Record<string, unknown>);
  return values.some(
    (value) => typeof value === "string" && /cursor/i.test(value)
  );
}

function kindFromStatus(status: number, body?: unknown): HermesErrorKind {
  if (status === 400 && looksLikeInvalidCursor(body)) return "invalid-cursor";
  if (status === 401) return "unauthorized";
  if (status === 403) return "forbidden";
  if (status === 404) return "not-found";
  if (status === 429) return "rate-limited";
  if (status >= 500) return "server";
  return "client";
}

/**
 * A typed API failure.
 *
 * Every rejection from `InboxAPI` and `UserAPI` is one of these, so callers branch on
 * `kind` instead of on a message string.
 */
export class HermesError extends Error {
  readonly kind: HermesErrorKind;
  readonly status?: number;
  readonly retryable: boolean;
  readonly body?: unknown;

  constructor(
    message: string,
    kind: HermesErrorKind,
    options?: { status?: number; body?: unknown; cause?: unknown }
  ) {
    super(message, { cause: options?.cause });
    this.name = "HermesError";
    this.kind = kind;
    this.status = options?.status;
    this.body = options?.body;
    this.retryable = RETRYABLE.has(kind);
  }

  /** Build an error from an HTTP status, classifying it by status and body. */
  static fromStatus(surface: string, status: number, body?: unknown): HermesError {
    const kind = kindFromStatus(status, body);
    return new HermesError(`${surface} API error (${status}): ${kind}`, kind, {
      status,
      body,
    });
  }

  /** Build an error for a request that never produced a response. */
  static network(surface: string, cause: unknown): HermesError {
    return new HermesError(`${surface} API request failed: network error`, "network", {
      cause,
    });
  }
}
