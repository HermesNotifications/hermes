// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { randomUUID } from "node:crypto";
import { test as base, expect, type Locator, type Page } from "@playwright/test";
import {
  inboxAction,
  listInbox,
  mintToken,
  sendNotification,
  waitForInbox,
  type InboxPage,
} from "./hermes-api.js";

/**
 * A Hermes identity that exists only for one test.
 *
 * Per-test rather than per-run, which buys three things at once:
 *
 * - **Absolute assertions.** The badge goes 0 → 1 → 2, never "one more than whatever was there".
 * - **Safe parallelism.** The inbox API's rate limit is keyed by user id, so tests cannot contend.
 * - **No ordering coupling.** No test can leave state that changes another's result.
 */
export interface HermesUser {
  /** Must be a UUID — organizations.id is a uuid column. */
  organizationId: string;
  externalUserId: string;
  /** The internal id, and therefore the Centrifugo channel suffix. */
  hermesUserId: string;
  token: string;

  send(input: {
    title: string;
    body: string;
    actionUrl?: string;
    actionLabel?: string;
  }): Promise<{ notificationId: string }>;
  inbox(options?: { archived?: boolean; limit?: number; cursor?: string }): Promise<InboxPage>;
  waitFor(
    predicate: (page: InboxPage) => boolean,
    options?: { timeoutMs?: number; archived?: boolean; limit?: number }
  ): Promise<InboxPage>;
  action(method: "PUT" | "DELETE", path: string): Promise<number>;
}

export interface DemoFixtures {
  hermesUser: HermesUser;
  /** The demo page, already logged in as `hermesUser` and finished loading. */
  demoPage: Page;
}

export const test = base.extend<DemoFixtures>({
  hermesUser: async ({}, use, testInfo) => {
    const organizationId = randomUUID();
    const externalUserId = `e2e-${testInfo.testId}-${Date.now()}`;
    // Minting also runs EnsureOrganization and EnsureUser, so the user provably exists before any
    // send. /v1/send validates nothing, so without this the first send could race user creation.
    const { token, sub } = await mintToken({ organizationId, externalUserId });

    await use({
      organizationId,
      externalUserId,
      hermesUserId: sub,
      token,
      send: (input) => sendNotification({ organizationId, externalUserId, ...input }),
      inbox: (options) => listInbox(token, options ?? {}),
      waitFor: (predicate, options) => waitForInbox(token, predicate, options ?? {}),
      action: (method, path) => inboxAction(token, method, path),
    });

    // Nothing is torn down: there is no API to delete an organization, a user or a notification
    // (inbox DELETE is a soft delete). Per-test ids make accumulation harmless, and
    // `make dev-restart` is the reset. Said out loud rather than left implied.
  },

  demoPage: async ({ page, hermesUser }, use) => {
    await loginAs(page, hermesUser);
    await page.goto("/");
    await waitForInboxLoaded(page);
    await use(page);
  },
});

export { expect };

/**
 * Become an identity in the browser, by setting the demo's session cookie through its login route.
 *
 * Takes only the identity fields rather than a whole `HermesUser`, so a test can log in as an
 * arbitrary second user without fabricating one.
 */
export async function loginAs(
  page: Page,
  identity: { organizationId: string; externalUserId: string }
): Promise<void> {
  const response = await page.request.post("http://localhost:8899/api/demo/login", {
    data: {
      organizationId: identity.organizationId,
      externalUserId: identity.externalUserId,
    },
  });
  expect(response.ok(), `demo login failed: ${await response.text()}`).toBe(true);
}

/** The widget's bell. Playwright's engines pierce the open shadow root, so no special selector. */
export function trigger(page: Page): Locator {
  return page.getByRole("button", { name: /notifications/i }).first();
}

/** The unread badge, which is absent rather than empty at zero. */
export function badge(page: Page): Locator {
  return page.locator("hermes-inbox").locator("css=.badge");
}

export function panel(page: Page): Locator {
  return page.getByRole("dialog");
}

export function rows(page: Page): Locator {
  return page.locator("hermes-inbox").locator("css=.notification");
}

/** Wait for the widget's first page to land. */
export async function waitForInboxLoaded(page: Page): Promise<void> {
  await expect(trigger(page)).toBeVisible();
  await page.waitForResponse(
    (response) => response.url().includes("/v1/inbox") && response.request().method() === "GET"
  );
}

/**
 * Wait until realtime is genuinely live.
 *
 * **Not optional.** The widget subscribes *after* its initial fetch, and a publication that lands
 * before the channel subscription completes is lost permanently — Centrifugo has nowhere to deliver
 * it. Without this gate the suite is flaky by construction, and every arrival assertion is a race.
 *
 * The widget emits `hermes-connected` on the subscribed event precisely so this is one line rather
 * than a websocket-frame-counting hack — and so integrators have the same signal.
 */
export async function waitForRealtimeReady(page: Page): Promise<void> {
  await expect(page.getByTestId("realtime-status")).toHaveText("connected", { timeout: 30_000 });
}

/** Open the panel and wait for it. */
export async function openPanel(page: Page): Promise<void> {
  await trigger(page).click();
  await expect(panel(page)).toBeVisible();
}

/** Count `GET /v1/inbox` requests from now on. */
export function countInboxFetches(page: Page): { get: () => number } {
  let count = 0;
  page.on("request", (request) => {
    if (request.url().includes("/v1/inbox") && request.method() === "GET") count++;
  });
  return { get: () => count };
}

/** Fail a test on any unexpected console error, so a silent SDK exception cannot hide. */
export function failOnConsoleErrors(page: Page, allow: RegExp[] = []): void {
  page.on("console", (message) => {
    if (message.type() !== "error") return;
    const text = message.text();
    if (allow.some((pattern) => pattern.test(text))) return;
    throw new Error(`unexpected console error: ${text}`);
  });
}
