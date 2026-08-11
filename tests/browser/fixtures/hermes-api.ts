// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { randomUUID } from "node:crypto";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

/**
 * Server-side Hermes access for the test process.
 *
 * Every call here goes **straight to the ingress**, never through the demo server's `/v1` proxy.
 * That is deliberate: if the tests reached the API the same way the browser does, a proxy bug and an
 * API bug would be indistinguishable, and the proxy could mask a regression in either.
 */

/**
 * The ingress, which routes every path. Not the :8080 admin port-forward.
 *
 * The three overrides below exist for a stack running without an ingress, where each service owns
 * its own port. Under `make dev-up` all four resolve to the same origin and nothing needs setting.
 */
export const HERMES_URL = process.env.HERMES_API_URL ?? "http://localhost:8888";
export const ADMIN_URL = process.env.HERMES_ADMIN_URL ?? HERMES_URL;
export const SEND_URL = process.env.HERMES_SEND_URL ?? HERMES_URL;
export const INBOX_URL = process.env.HERMES_INBOX_URL ?? HERMES_URL;
/** Base for the Centrifugo websocket, as the browser would reach it. */
export const SOCKET_ENDPOINT =
  process.env.HERMES_WS_URL ?? `${HERMES_URL.replace(/^http/, "ws")}/realtime/connection/websocket`;

/**
 * Centrifugo's health endpoint.
 *
 * Derived from {@link SOCKET_ENDPOINT} rather than assuming the ingress path, so it follows the
 * websocket wherever that actually is. Under an ingress that resolves to `/realtime/health`; talking
 * to Centrifugo directly it is `/health` on its own port.
 */
export const REALTIME_HEALTH_URL =
  process.env.HERMES_REALTIME_HEALTH_URL ??
  `${SOCKET_ENDPOINT.replace(/^ws/, "http").replace(/\/connection\/websocket$/, "")}/health`;

/** Read the dev API key from the environment, falling back to what `make seed` wrote. */
export function apiKey(): string | undefined {
  if (process.env.HERMES_API_KEY) return process.env.HERMES_API_KEY;
  try {
    const envLocal = fileURLToPath(new URL("../../../web/admin/.env.local", import.meta.url));
    const line = readFileSync(envLocal, "utf8")
      .split("\n")
      .find((candidate) => candidate.startsWith("HERMES_API_KEY="));
    return line?.slice("HERMES_API_KEY=".length).trim() || undefined;
  } catch {
    return undefined;
  }
}

function authHeaders(): Record<string, string> {
  return {
    Authorization: `Bearer ${apiKey() ?? ""}`,
    "Content-Type": "application/json",
  };
}

async function expectOk(response: Response, what: string): Promise<unknown> {
  if (!response.ok) {
    throw new Error(`${what} failed (${response.status}): ${await response.text()}`);
  }
  return response.json();
}

/** Decode the `sub` claim without verifying — the token came from a trusted mint. */
function subjectOf(token: string): string {
  const payload = token.split(".")[1];
  if (!payload) throw new Error("minted token is not a JWT");
  const json = Buffer.from(payload, "base64url").toString("utf8");
  const sub = (JSON.parse(json) as { sub?: string }).sub;
  if (!sub) throw new Error("minted token carries no sub claim");
  return sub;
}

/**
 * Mint a user token, which also creates the organization and user as a side effect.
 *
 * Note `organizationId` must be a UUID: `organizations.id` is a uuid column and
 * `EnsureOrganization` inserts the value directly, so anything else produces a Postgres type error
 * surfacing as a bare 500.
 */
export async function mintToken(identity: {
  organizationId: string;
  externalUserId: string;
}): Promise<{ token: string; expiresAt: string; sub: string }> {
  const body = (await expectOk(
    await fetch(`${ADMIN_URL}/v1/auth/token`, {
      method: "POST",
      headers: authHeaders(),
      body: JSON.stringify({
        user_id: identity.externalUserId,
        organization_id: identity.organizationId,
      }),
    }),
    "mint token"
  )) as { token: string; expires_at: string };

  return { token: body.token, expiresAt: body.expires_at, sub: subjectOf(body.token) };
}

/** Send a notification to the inbox channel. Returns once accepted, not once delivered. */
export async function sendNotification(input: {
  organizationId: string;
  externalUserId: string;
  title: string;
  body: string;
  actionUrl?: string;
  actionLabel?: string;
}): Promise<{ notificationId: string }> {
  const body = (await expectOk(
    await fetch(`${SEND_URL}/v1/send`, {
      method: "POST",
      headers: { ...authHeaders(), "X-Idempotency-Key": randomUUID() },
      body: JSON.stringify({
        to: { organization_id: input.organizationId, user_id: input.externalUserId },
        content: {
          title: input.title,
          body: input.body,
          ...(input.actionUrl ? { action_url: input.actionUrl } : {}),
          ...(input.actionLabel ? { action_label: input.actionLabel } : {}),
        },
        channels: ["inbox"],
      }),
    }),
    "send notification"
  )) as { notification_id: string };

  return { notificationId: body.notification_id };
}

export interface InboxPage {
  data: Array<{ id: string; title: string; read_at?: string; archived_at?: string }>;
  unread_count: number;
  cursor?: string;
}

/** Read a user's inbox with their own token. */
export async function listInbox(
  token: string,
  options: { archived?: boolean; limit?: number; cursor?: string } = {}
): Promise<InboxPage> {
  const url = new URL(`${INBOX_URL}/v1/inbox`);
  if (options.archived !== undefined) url.searchParams.set("archived", String(options.archived));
  if (options.limit !== undefined) url.searchParams.set("limit", String(options.limit));
  if (options.cursor) url.searchParams.set("cursor", options.cursor);

  return (await expectOk(
    await fetch(url, { headers: { Authorization: `Bearer ${token}` } }),
    "list inbox"
  )) as InboxPage;
}

/** Perform an inbox action as the user. Returns the status, since 200 is not always success. */
export async function inboxAction(
  token: string,
  method: "PUT" | "DELETE",
  path: string
): Promise<number> {
  const response = await fetch(`${INBOX_URL}/v1/inbox${path}`, {
    method,
    headers: { Authorization: `Bearer ${token}` },
  });
  return response.status;
}

/** Poll until `predicate` holds, or fail with the last page seen. */
export async function waitForInbox(
  token: string,
  predicate: (page: InboxPage) => boolean,
  options: { timeoutMs?: number; archived?: boolean; limit?: number } = {}
): Promise<InboxPage> {
  const deadline = Date.now() + (options.timeoutMs ?? 45_000);
  let last: InboxPage = { data: [], unread_count: 0 };

  for (;;) {
    last = await listInbox(token, {
      ...(options.archived !== undefined ? { archived: options.archived } : {}),
      ...(options.limit !== undefined ? { limit: options.limit } : {}),
    });
    if (predicate(last)) return last;
    if (Date.now() > deadline) {
      throw new Error(
        `inbox never satisfied the condition within ${options.timeoutMs ?? 45_000}ms; ` +
          `last saw ${last.data.length} rows and unread_count ${last.unread_count}`
      );
    }
    // Polling, never sleeping-then-asserting: /v1/send returns 202 before dispatch has created
    // the row, so there is no fixed delay that is both reliable and quick.
    await new Promise((resolve) => setTimeout(resolve, 400));
  }
}
