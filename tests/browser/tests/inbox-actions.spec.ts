// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { badge, expect, openPanel, rows, test, waitForRealtimeReady } from "../fixtures/demo.js";

/**
 * Actions driven through the widget's UI, verified against the real API.
 *
 * Every assertion polls the server rather than trusting what the widget rendered — the widget is
 * optimistic by design, so a mutation that never reached the API would still look correct on screen
 * for a moment.
 */
test.describe("inbox actions", () => {
  test("marking read round-trips to the server", async ({ demoPage, hermesUser }) => {
    await waitForRealtimeReady(demoPage);
    await hermesUser.send({ title: "Mark me", body: "m" });
    // Waited for server-side and then reloaded, rather than waiting for the realtime push. This
    // spec is about actions round-tripping; coupling it to delivery latency would make it fail for
    // reasons that have nothing to do with what it asserts. Realtime arrival has its own spec.
    await hermesUser.waitFor((inbox) => inbox.unread_count === 1);
    await demoPage.reload();
    await expect(badge(demoPage)).toHaveText("1");
    await openPanel(demoPage);

    await demoPage.getByRole("button", { name: /^Mark Mark me as read$/ }).click();

    const page = await hermesUser.waitFor((inbox) => inbox.data[0]?.read_at !== undefined);
    expect(page.data[0].read_at).toBeTruthy();
    expect(page.unread_count).toBe(0);
    await expect(badge(demoPage)).toHaveCount(0);
  });

  test("archiving removes the row and moves it to the archived view", async ({
    demoPage,
    hermesUser,
  }) => {
    await waitForRealtimeReady(demoPage);
    await hermesUser.send({ title: "Archive me", body: "a" });
    // Waited for server-side and then reloaded, rather than waiting for the realtime push. This
    // spec is about actions round-tripping; coupling it to delivery latency would make it fail for
    // reasons that have nothing to do with what it asserts. Realtime arrival has its own spec.
    await hermesUser.waitFor((inbox) => inbox.unread_count === 1);
    await demoPage.reload();
    await expect(badge(demoPage)).toHaveText("1");
    await openPanel(demoPage);

    await demoPage.getByRole("button", { name: /^Archive Archive me$/ }).click();

    await expect(rows(demoPage)).toHaveCount(0);
    const archived = await hermesUser.waitFor((inbox) => inbox.data.length === 1, {
      archived: true,
    });
    expect(archived.data[0].title).toBe("Archive me");
    // And it is genuinely gone from the active view, not merely hidden client-side.
    const active = await hermesUser.inbox();
    expect(active.data).toEqual([]);
  });

  test("mark all read clears every row and the badge", async ({ demoPage, hermesUser }) => {
    await waitForRealtimeReady(demoPage);
    await hermesUser.send({ title: "One", body: "1" });
    await hermesUser.send({ title: "Two", body: "2" });
    await hermesUser.waitFor((inbox) => inbox.unread_count === 2);
    await demoPage.reload();
    await expect(badge(demoPage)).toHaveText("2");
    await openPanel(demoPage);

    await demoPage.getByRole("button", { name: "Mark all read" }).click();

    const page = await hermesUser.waitFor((inbox) => inbox.unread_count === 0);
    expect(page.data.every((row) => row.read_at !== undefined)).toBe(true);
    await expect(badge(demoPage)).toHaveCount(0);
  });

  test("clicking a row without an action marks it read", async ({ demoPage, hermesUser }) => {
    await waitForRealtimeReady(demoPage);
    await hermesUser.send({ title: "Clickable", body: "c" });
    // Waited for server-side and then reloaded, rather than waiting for the realtime push. This
    // spec is about actions round-tripping; coupling it to delivery latency would make it fail for
    // reasons that have nothing to do with what it asserts. Realtime arrival has its own spec.
    await hermesUser.waitFor((inbox) => inbox.unread_count === 1);
    await demoPage.reload();
    await expect(badge(demoPage)).toHaveText("1");
    await openPanel(demoPage);

    await demoPage.locator("hermes-inbox").locator("css=button.row-target").click();

    await hermesUser.waitFor((inbox) => inbox.unread_count === 0);
    await expect(badge(demoPage)).toHaveCount(0);
  });

  test("rows are reachable and operable by keyboard", async ({ demoPage, hermesUser }) => {
    // Rows used to be plain divs with click handlers — entirely unreachable without a mouse.
    await waitForRealtimeReady(demoPage);
    await hermesUser.send({ title: "Keyboard", body: "k" });
    // Waited for server-side and then reloaded, rather than waiting for the realtime push. This
    // spec is about actions round-tripping; coupling it to delivery latency would make it fail for
    // reasons that have nothing to do with what it asserts. Realtime arrival has its own spec.
    await hermesUser.waitFor((inbox) => inbox.unread_count === 1);
    await demoPage.reload();
    await expect(badge(demoPage)).toHaveText("1");
    await openPanel(demoPage);

    const row = demoPage.locator("hermes-inbox").locator("css=button.row-target");
    await row.focus();
    await expect(row).toBeFocused();
    await demoPage.keyboard.press("Enter");

    await hermesUser.waitFor((inbox) => inbox.unread_count === 0);
  });

  test("Escape closes the panel and returns focus to the bell", async ({ demoPage }) => {
    await openPanel(demoPage);

    await demoPage.keyboard.press("Escape");

    await expect(demoPage.getByRole("dialog")).toHaveCount(0);
    const focusedIsTrigger = await demoPage.evaluate(() => {
      const host = document.querySelector("hermes-inbox");
      return host?.shadowRoot?.activeElement?.classList.contains("trigger") ?? false;
    });
    expect(focusedIsTrigger).toBe(true);
  });

  test("clicking outside closes the panel; clicking inside does not", async ({ demoPage }) => {
    // Across a shadow boundary the naive `contains(event.target)` check is true for every click on
    // the page, so this would close instantly. Only a real browser retargets events properly.
    await openPanel(demoPage);
    await demoPage.getByRole("dialog").click();
    await expect(demoPage.getByRole("dialog")).toBeVisible();

    await demoPage.getByRole("heading", { name: "Dashboard" }).click();

    await expect(demoPage.getByRole("dialog")).toHaveCount(0);
  });

  test("an action on an unknown id answers 200, not 404", async ({ hermesUser }) => {
    // Documented deliberately. The store reports changed=false and the handler ignores it, so the
    // API answers 200 for an id that does not exist or belongs to someone else. Nothing in this
    // suite may depend on a 404, and this test is why: anyone "fixing" it into a 404 gets a red
    // test and a reason.
    expect(await hermesUser.action("PUT", "/ntf_does_not_exist_at_all/read")).toBe(200);

    const page = await hermesUser.inbox();
    expect(page.unread_count).toBe(0);
  });

  test("a failed action leaves the list intact and surfaces an error", async ({
    demoPage,
    hermesUser,
  }) => {
    await waitForRealtimeReady(demoPage);
    await hermesUser.send({ title: "Rollback", body: "r" });
    // Waited for server-side and then reloaded, rather than waiting for the realtime push. This
    // spec is about actions round-tripping; coupling it to delivery latency would make it fail for
    // reasons that have nothing to do with what it asserts. Realtime arrival has its own spec.
    await hermesUser.waitFor((inbox) => inbox.unread_count === 1);
    await demoPage.reload();
    await expect(badge(demoPage)).toHaveText("1");
    await openPanel(demoPage);

    // Break the next archive at the network level, so the widget's rollback path runs for real.
    await demoPage.route("**/v1/inbox/*/archive", (route) =>
      route.fulfill({ status: 500, body: JSON.stringify({ detail: "boom" }) })
    );
    await demoPage.getByRole("button", { name: /^Archive Rollback$/ }).click();

    // Rolled back: the row is still there, and the failure is visible rather than silent.
    await expect(rows(demoPage)).toHaveCount(1);
    await expect(demoPage.locator("hermes-inbox").locator('css=[role="alert"]')).toBeVisible();
    const page = await hermesUser.inbox();
    expect(page.data).toHaveLength(1);
  });
});
