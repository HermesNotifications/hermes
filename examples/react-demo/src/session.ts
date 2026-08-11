// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

/** Renew this long before expiry, so a slow round trip cannot race the token running out. */
export const REFRESH_MARGIN_MS = 5 * 60_000;

/** Used when the server did not tell us when the token expires. */
const FALLBACK_INTERVAL_MS = 30 * 60_000;

/** setTimeout clamps above this and fires immediately, which would spin. */
const MAX_TIMEOUT_MS = 2 ** 31 - 1;

/** Never schedule zero or a negative delay; that is a tight loop, not a refresh. */
const MIN_DELAY_MS = 1_000;

/** What the demo server's `/api/session` returns. */
export interface DemoSession {
  token: string;
  expiresAt: string;
  /** The internal Hermes id — the Centrifugo channel is `user#<this>`. */
  hermesUserId: string;
  externalUserId: string;
  organizationId: string;
  socketUrl: string;
}

/**
 * How long to wait before renewing a token that expires at `expiresAt`.
 *
 * Extracted from the component because it cannot be tested by waiting: the admin API refuses an
 * `expires_in` below 3600 seconds, so no token can be made to expire quickly. Get this wrong and
 * nothing notices until a session has been open for hours and the inbox starts 401ing.
 */
export function refreshDelayMs(expiresAt: string | undefined, now: number = Date.now()): number {
  if (!expiresAt) return FALLBACK_INTERVAL_MS;

  const expiry = new Date(expiresAt).getTime();
  if (Number.isNaN(expiry)) return FALLBACK_INTERVAL_MS;

  const delay = expiry - now - REFRESH_MARGIN_MS;
  return Math.min(Math.max(delay, MIN_DELAY_MS), MAX_TIMEOUT_MS);
}

/** Fetch a session from the demo backend. Credentials carry the demo's identity cookie. */
export async function fetchSession(): Promise<DemoSession> {
  const response = await fetch("/api/session", {
    method: "POST",
    credentials: "include",
    headers: { Accept: "application/json" },
  });
  if (!response.ok) {
    const detail = await response.text().catch(() => "");
    throw new Error(`session request failed (${response.status}): ${detail}`);
  }
  return (await response.json()) as DemoSession;
}

/** Become a given org/user. Stands in for whatever login the host app already has. */
export async function login(identity: {
  organizationId: string;
  externalUserId: string;
}): Promise<void> {
  const response = await fetch("/api/demo/login", {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(identity),
  });
  if (!response.ok) {
    const detail = await response.text().catch(() => "");
    throw new Error(`login failed (${response.status}): ${detail}`);
  }
}

export interface TestSendInput {
  title: string;
  body: string;
  actionUrl?: string;
  actionLabel?: string;
  count?: number;
}

/** Ask the demo backend to send a notification to the current user. */
export async function testSend(input: TestSendInput): Promise<{ notificationIds: string[] }> {
  const response = await fetch("/api/test-send", {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  if (!response.ok) {
    const detail = await response.text().catch(() => "");
    throw new Error(`send failed (${response.status}): ${detail}`);
  }
  return (await response.json()) as { notificationIds: string[] };
}
