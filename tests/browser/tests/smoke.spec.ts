// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

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

  test("the shell grid holds only the sidebar and the content column", async ({ demoPage }) => {
    // A regression guard with a specific history. sonner's <Toaster> renders a real in-flow
    // <section> — only the <ol> inside it is fixed — and it used to sit inside .shell, where it
    // became the grid's first item: it took the 232px sidebar column, the sidebar slid into the
    // 1fr column, and the entire app dropped to row two at 232px wide. Nothing threw and no test
    // noticed, because every element was still present and clickable.
    //
    // Asserting on the geometry rather than the child count, so any future stray grid item is
    // caught however it arrives.
    const layout = await demoPage.evaluate(() => {
      const shell = document.querySelector(".shell");
      const sidebar = shell?.querySelector(":scope > .sidebar");
      if (!shell || !sidebar) return null;
      return {
        children: shell.children.length,
        sidebarX: Math.round(sidebar.getBoundingClientRect().x),
        contentX: Math.round(
          (shell.children[1] as HTMLElement).getBoundingClientRect().x
        ),
        sidebarWidth: Math.round(sidebar.getBoundingClientRect().width),
      };
    });

    expect(layout, ".shell or its sidebar is missing").not.toBeNull();
    expect(layout!.children).toBe(2);
    expect(layout!.sidebarX).toBe(0);
    expect(layout!.sidebarWidth).toBe(232);
    expect(layout!.contentX).toBe(232);
  });

  test("the page logs no console errors on a clean load", async ({ page, hermesUser }) => {
    failOnConsoleErrors(page);
    const { loginAs } = await import("../fixtures/demo.js");
    await loginAs(page, hermesUser);
    await page.goto("/");
    await expect(trigger(page)).toBeVisible();
  });
});
