// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import {
  badge,
  expandToggle,
  expect,
  openPanel,
  rows,
  test,
  waitForInboxLoaded,
  waitForRealtimeReady,
} from "../fixtures/demo.js";

/**
 * Expanding a clipped notification.
 *
 * This has to live in a real browser: the whole feature is driven by measuring whether text
 * actually overflows its box, and jsdom reports every box as zero. The unit suite covers the
 * markup, the ARIA wiring and the click semantics through an injected probe; what it cannot
 * assert is the thing the user sees — that the clamp lifts.
 */

const LONG_BODY =
  "Your export of 1.2M rows completed successfully and will be retained for thirty days. " +
  "This body is deliberately long so that the clamp, and the control that lifts it, are both " +
  "exercised by this test rather than assumed to work.";

const LONG_TITLE =
  "Your scheduled export of the full analytics warehouse has finished processing and is ready";

test.describe("expanding a long notification", () => {
  test("a clipped row offers a toggle, and pressing it shows the whole body", async ({
    demoPage,
    hermesUser,
  }) => {
    await waitForRealtimeReady(demoPage);
    await hermesUser.send({ title: "Data export finished", body: LONG_BODY });
    await expect(badge(demoPage)).toHaveText("1");
    await openPanel(demoPage);

    const body = demoPage.locator("hermes-inbox").locator("css=.notification-body");
    // Clipped to begin with: more content than box.
    const clipped = await body.evaluate((node) => node.scrollHeight > node.clientHeight + 1);
    expect(clipped).toBe(true);

    const toggle = expandToggle(demoPage);
    await expect(toggle).toBeVisible();
    await expect(toggle).toHaveAttribute("aria-expanded", "false");

    await toggle.click();

    await expect(toggle).toHaveAttribute("aria-expanded", "true");
    const expanded = await body.evaluate((node) => node.scrollHeight <= node.clientHeight + 1);
    expect(expanded).toBe(true);
  });

  test("a short row offers no toggle at all", async ({ demoPage, hermesUser }) => {
    // The negative no unit test can give: it depends entirely on layout. A toggle on every row
    // would add a tab stop to every row, which is the cost the measurement exists to avoid.
    await waitForRealtimeReady(demoPage);
    await hermesUser.send({ title: "Short", body: "One line." });
    await expect(badge(demoPage)).toHaveText("1");
    await openPanel(demoPage);

    await expect(rows(demoPage)).toHaveCount(1);
    await expect(expandToggle(demoPage)).toHaveCount(0);
  });

  test("expanding lifts the title's truncation too", async ({ demoPage, hermesUser }) => {
    await waitForRealtimeReady(demoPage);
    await hermesUser.send({ title: LONG_TITLE, body: LONG_BODY });
    await expect(badge(demoPage)).toHaveText("1");
    await openPanel(demoPage);

    const title = demoPage.locator("hermes-inbox").locator("css=.notification-title");
    expect(await title.evaluate((node) => node.scrollWidth > node.clientWidth + 1)).toBe(true);

    await expandToggle(demoPage).click();

    expect(await title.evaluate((node) => node.scrollWidth <= node.clientWidth + 1)).toBe(true);
  });

  test("expanding does not mark the notification read", async ({ demoPage, hermesUser }) => {
    // The guarantee that made the toggle a sibling of the row target rather than a child of it,
    // asserted at the level the user experiences: the badge does not move.
    await waitForRealtimeReady(demoPage);
    await hermesUser.send({ title: "Data export finished", body: LONG_BODY });
    await expect(badge(demoPage)).toHaveText("1");
    await openPanel(demoPage);

    await expandToggle(demoPage).click();
    await expect(expandToggle(demoPage)).toHaveAttribute("aria-expanded", "true");

    await expect(badge(demoPage)).toHaveText("1");
    const inbox = await hermesUser.inbox();
    expect(inbox.data[0]?.read_at).toBeUndefined();
  });

  test("the toggle keeps focus after being activated by keyboard", async ({
    demoPage,
    hermesUser,
  }) => {
    // If an expanded row were re-measured naively it would stop counting as overflowing, the
    // toggle would be removed from the DOM, and the focus would go with it. This is the test
    // that catches the rule being deleted.
    await waitForRealtimeReady(demoPage);
    await hermesUser.send({ title: "Data export finished", body: LONG_BODY });
    await expect(badge(demoPage)).toHaveText("1");
    await openPanel(demoPage);

    const toggle = expandToggle(demoPage);
    await toggle.focus();
    await demoPage.keyboard.press("Enter");

    await expect(toggle).toHaveAttribute("aria-expanded", "true");
    await expect(toggle).toBeFocused();
  });

  test("expanded state survives loading another page of results", async ({
    demoPage,
    hermesUser,
  }) => {
    // Expanded rows are keyed by notification id rather than by position, so appending a page
    // must not move the expansion onto a different row.
    //
    // Seeded server-side and then reloaded, rather than sent into an open page. `hasMore` comes
    // from the cursor on the *initial list response*: a widget that loaded an empty inbox and
    // then received 22 arrivals over the websocket has 22 rows and no cursor, so "Load more"
    // never renders and this test waits forever on a control that cannot appear.
    for (let i = 0; i < 22; i++) {
      await hermesUser.send({ title: `Seeded ${i}`, body: LONG_BODY });
    }
    await hermesUser.waitFor((page) => page.data.length >= 20, { limit: 25 });

    await demoPage.reload();
    await waitForInboxLoaded(demoPage);
    await openPanel(demoPage);
    // One page's worth, plus a cursor -- which is what makes the control exist.
    await expect(rows(demoPage)).toHaveCount(20);

    // Expand the oldest visible row, then page in more beneath it.
    const target = rows(demoPage).last();
    const targetId = await target.getAttribute("data-id");
    await target.locator("css=button.expand-toggle").click();

    await demoPage.locator("hermes-inbox").locator("css=button.load-more").click();
    await expect(rows(demoPage)).toHaveCount(22);

    const stillExpanded = demoPage
      .locator("hermes-inbox")
      .locator(`css=.notification[data-id="${targetId}"] button.expand-toggle`);
    await expect(stillExpanded).toHaveAttribute("aria-expanded", "true");
  });
});
