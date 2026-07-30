// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { badge, expect, test, waitForRealtimeReady } from "../fixtures/demo.js";

/**
 * Token refresh, forced rather than waited for.
 *
 * The admin API refuses an `expires_in` below 3600 seconds, so there is no way to mint a token that
 * expires quickly enough to test by waiting. The demo therefore exposes
 * `window.__hermesDemo.refreshSession()`, and this is the only automated check on the refresh path —
 * which matters, because the failure mode is a session that works perfectly for hours and then 401s
 * forever.
 */
test.describe("session refresh", () => {
  test("a forced refresh issues a new token and later requests use it", async ({ demoPage }) => {
    const bearers: string[] = [];
    demoPage.on("request", (request) => {
      if (!request.url().includes("/v1/inbox")) return;
      const header = request.headers()["authorization"];
      if (header) bearers.push(header);
    });

    // The fixture has already loaded the first page by the time this listener is attached, so a
    // reload is what gives it something to observe. Attaching earlier is not an option — the
    // listener needs a page, and the page is what the fixture returns.
    await demoPage.reload();
    await waitForRealtimeReady(demoPage);
    await expect
      .poll(() => bearers.length, { message: "no /v1/inbox request carried a bearer token" })
      .toBeGreaterThan(0);
    const original = bearers[0];

    const sessionRequest = demoPage.waitForResponse(
      (response) => response.url().includes("/api/session") && response.status() === 200
    );
    await demoPage.evaluate(async () => {
      await (window as unknown as { __hermesDemo: { refreshSession: () => Promise<void> } })
        .__hermesDemo.refreshSession();
    });
    await sessionRequest;

    // Force a REST call with whatever token is now current.
    await demoPage.reload();
    await waitForRealtimeReady(demoPage);

    const latest = bearers.at(-1);
    expect(latest).toBeTruthy();
    // Tokens carry jitter and a fresh iat, so a genuinely re-minted token differs.
    expect(latest).not.toBe(original);
  });

  test("realtime still delivers after a refresh", async ({ demoPage, hermesUser }) => {
    // A refresh that silently killed the socket would leave the inbox looking fine until the next
    // notification failed to arrive — exactly the kind of thing only a live test notices.
    await waitForRealtimeReady(demoPage);

    await demoPage.evaluate(async () => {
      await (window as unknown as { __hermesDemo: { refreshSession: () => Promise<void> } })
        .__hermesDemo.refreshSession();
    });
    await waitForRealtimeReady(demoPage);

    await hermesUser.send({ title: "After refresh", body: "still live" });

    await expect(badge(demoPage)).toHaveText("1");
  });

  test("the rotate button in the demo UI works the same way", async ({ demoPage }) => {
    await waitForRealtimeReady(demoPage);

    const sessionRequest = demoPage.waitForResponse(
      (response) => response.url().includes("/api/session") && response.status() === 200
    );
    await demoPage.getByTestId("rotate-token").click();
    await sessionRequest;

    await expect(demoPage.getByTestId("activity-log")).toContainText("session for");
  });

  test("the widget recovers when the API rejects a stale token", async ({
    demoPage,
    hermesUser,
  }) => {
    // Simulates expiry: fail the next inbox read with a 401 once, and assert the client refreshes
    // and retries rather than surfacing a dead inbox. This is the gap where getToken used to be
    // consulted only on socket reconnect while every REST call kept failing.
    await waitForRealtimeReady(demoPage);

    await hermesUser.send({ title: "Recovered", body: "r" });
    await hermesUser.waitFor((inbox) => inbox.unread_count === 1);

    let rejected = 0;
    await demoPage.route("**/v1/inbox?*", async (route) => {
      if (rejected === 0) {
        rejected = 1;
        await route.fulfill({
          status: 401,
          contentType: "application/json",
          body: JSON.stringify({ detail: "invalid token" }),
        });
        return;
      }
      await route.continue();
    });

    // A reload, not the rotate button: rotating re-mints the token but does not re-read the inbox,
    // so nothing would hit the intercepted route and the retry path would never run. This is the
    // gap that mattered — `getToken` used to be consulted only on socket reconnect, so after expiry
    // the inbox 401'd forever while realtime looked perfectly healthy.
    await demoPage.reload();

    await expect
      .poll(() => rejected, { message: "the intercepted 401 never fired" })
      .toBe(1);
    // Recovered: the client refreshed and retried, so the row is on screen despite the first 401.
    await expect(badge(demoPage)).toHaveText("1");
  });
});
