// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import {
  badge,
  expect,
  failOnConsoleErrors,
  openPanel,
  test,
  trigger,
  waitForRealtimeReady,
} from "../fixtures/demo.js";

/**
 * Read this file first when the suite goes red.
 *
 * It asserts the contract everything else depends on: the app renders, the widget's bundle loaded
 * *and* registered, a fresh user's inbox is genuinely empty, and the socket connects. If any of this
 * fails, every other failure in the run is downstream noise.
 */
test.describe("smoke", () => {
  test("the host application renders", async ({ demoPage }) => {
    await expect(demoPage.getByRole("heading", { name: "Dashboard" })).toBeVisible();
    await expect(demoPage.getByRole("navigation", { name: "Main" })).toBeVisible();
  });

  test("the custom element is defined, not merely present in the markup", async ({ demoPage }) => {
    // The single likeliest local failure is an unbuilt or stale dist/. An undefined element renders
    // as an inert unknown tag: no shadow root, no bell, and every other spec fails obscurely.
    const defined = await demoPage.evaluate(() => Boolean(customElements.get("hermes-inbox")));
    expect(defined, "hermes-inbox is not registered — is dist/ built? run 'make demo-install'").toBe(
      true
    );
  });

  test("the widget upgraded and rendered its shadow root", async ({ demoPage }) => {
    await expect(trigger(demoPage)).toBeVisible();
    const hasShadow = await demoPage.evaluate(
      () => document.querySelector("hermes-inbox")?.shadowRoot !== null
    );
    expect(hasShadow).toBe(true);
  });

  test("a fresh user has an empty inbox and no badge", async ({ demoPage, hermesUser }) => {
    const page = await hermesUser.inbox();
    expect(page.data).toEqual([]);
    expect(page.unread_count).toBe(0);

    await expect(badge(demoPage)).toHaveCount(0);
    await openPanel(demoPage);
    await expect(demoPage.getByText("No notifications")).toBeVisible();
  });

  test("the session panel shows a decoded internal user id", async ({ demoPage, hermesUser }) => {
    // The browser needs `sub` for its Centrifugo channel and cannot derive it any other way, so a
    // blank here means realtime will silently never work.
    await expect(demoPage.getByTestId("session-hermes-id")).toHaveText(hermesUser.hermesUserId);
  });

  test("realtime reaches the connected state", async ({ demoPage }) => {
    await waitForRealtimeReady(demoPage);
  });

  test("the page logs no console errors on a clean load", async ({ page, hermesUser }) => {
    failOnConsoleErrors(page);
    const { loginAs } = await import("../fixtures/demo.js");
    await loginAs(page, hermesUser);
    await page.goto("/");
    await expect(trigger(page)).toBeVisible();
  });
});
