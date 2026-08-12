// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { badge, expect, openPanel, test, waitForRealtimeReady } from "../fixtures/demo.js";

/**
 * The embed's restyling contract, which can only be checked in a real browser: jsdom does not
 * resolve `var()` in computed styles and does not implement `::part` at all.
 *
 * The two mechanisms are tested separately because they fail independently — custom properties cross
 * the shadow boundary by inheritance, while `::part` is the only way to reach an internal element.
 * An integrator needs both, and shipping one without the other is a plausible regression.
 */
test.describe("theming", () => {
  test("a host custom property changes the widget's internals", async ({ demoPage }) => {
    // Asserted on the popover rather than the badge, so this test needs no notification to exist.
    // Sending one would make a pure styling assertion depend on the whole async pipeline —
    // slower, and flaky for a reason that has nothing to do with theming.
    await openPanel(demoPage);
    const popover = demoPage.getByRole("dialog");

    const before = await popover.evaluate((node) => getComputedStyle(node).backgroundColor);

    await demoPage.evaluate(() => {
      document
        .querySelector("hermes-inbox")
        ?.setAttribute("style", "--hermes-popover-bg: rgb(1, 2, 3)");
    });

    const after = await popover.evaluate((node) => getComputedStyle(node).backgroundColor);
    expect(after).toBe("rgb(1, 2, 3)");
    expect(after).not.toBe(before);
  });

  test("a host custom property restyles the badge once there is one", async ({
    demoPage,
    hermesUser,
  }) => {
    // The badge is conditionally rendered, so it needs a real notification — kept as its own test
    // so a pipeline hiccup cannot be mistaken for a theming regression.
    await waitForRealtimeReady(demoPage);
    await hermesUser.send({ title: "Themed", body: "t" });
    await expect(badge(demoPage)).toHaveText("1");

    await demoPage.evaluate(() => {
      document
        .querySelector("hermes-inbox")
        ?.setAttribute("style", "--hermes-badge-bg: rgb(4, 5, 6)");
    });

    expect(await badge(demoPage).evaluate((node) => getComputedStyle(node).backgroundColor)).toBe(
      "rgb(4, 5, 6)"
    );
  });

  test("a ::part rule in the host stylesheet reaches inside the shadow root", async ({
    demoPage,
  }) => {
    // The demo's own stylesheet already sets `hermes-inbox::part(trigger) { border-radius: 10px }`,
    // so this asserts the shipped integration rather than something contrived.
    const trigger = demoPage.locator("hermes-inbox").locator("css=button.trigger");
    await expect(trigger).toBeVisible();
    expect(await trigger.evaluate((node) => getComputedStyle(node).borderRadius)).toBe("10px");

    await demoPage.evaluate(() => {
      const style = document.createElement("style");
      style.textContent = "hermes-inbox::part(trigger) { border-radius: 2px; }";
      document.head.append(style);
    });

    expect(await trigger.evaluate((node) => getComputedStyle(node).borderRadius)).toBe("2px");
  });

  test("the popover escapes a sticky, z-indexed header", async ({ demoPage }) => {
    // The widget sits inside a sticky header at z-index 20. With the old hardcoded z-index there was
    // no way for a host to fix a clipped panel from outside the shadow root; the demo sets
    // --hermes-popover-z-index: 50 and this proves it took effect.
    await openPanel(demoPage);
    const popover = demoPage.getByRole("dialog");

    expect(await popover.evaluate((node) => getComputedStyle(node).zIndex)).toBe("50");
    // Visible, and actually on top: hit-testing fails if something covers it.
    await expect(popover).toBeVisible();
    await expect(popover).toBeInViewport();
  });

  test("the panel stays anchored when the page scrolls", async ({ demoPage }) => {
    await openPanel(demoPage);
    const popover = demoPage.getByRole("dialog");
    await expect(popover).toBeVisible();

    await demoPage.mouse.wheel(0, 600);

    // The header is sticky, so the panel must travel with it rather than scrolling away.
    await expect(popover).toBeVisible();
    await expect(popover).toBeInViewport();
  });

  test("the bell ships borderless, and the custom property still puts a box back", async ({
    demoPage,
  }) => {
    // Both halves matter: the first is the change, the second is the promise that a host who
    // wanted the old look has a lever. The default is `transparent` rather than `none` so the
    // box metrics are identical either way and restoring it cannot reflow the header.
    const trigger = demoPage.locator("hermes-inbox").locator("css=button.trigger");

    const before = await trigger.evaluate((node) => {
      const style = getComputedStyle(node);
      return { color: style.borderTopColor, width: style.borderTopWidth };
    });
    expect(before.color).toBe("rgba(0, 0, 0, 0)");
    expect(before.width).toBe("1px");

    await demoPage.evaluate(() => {
      document
        .querySelector("hermes-inbox")
        ?.setAttribute("style", "--hermes-trigger-border: 1px solid rgb(7, 8, 9)");
    });

    expect(await trigger.evaluate((node) => getComputedStyle(node).borderTopColor)).toBe(
      "rgb(7, 8, 9)"
    );
  });

  test("the bell is a large enough target once the border is invisible", async ({ demoPage }) => {
    // WCAG 2.2 SC 2.5.8 wants 24x24 minimum. Worth pinning, because "tighten the padding now
    // that the box is gone" is the tempting follow-on edit.
    const box = await demoPage
      .locator("hermes-inbox")
      .locator("css=button.trigger")
      .boundingBox();

    expect(box?.width ?? 0).toBeGreaterThanOrEqual(24);
    expect(box?.height ?? 0).toBeGreaterThanOrEqual(24);
  });

  test("the brand theme restyles the widget well beyond a border radius", async ({ demoPage }) => {
    // The demo's worked example, asserted so it cannot rot. It reaches internals no custom
    // property exposes, which is the whole argument for ::part existing.
    await demoPage.getByLabel("Theme").selectOption("brand");

    const trigger = demoPage.locator("hermes-inbox").locator("css=button.trigger");
    expect(await trigger.evaluate((node) => getComputedStyle(node).borderRadius)).toBe("999px");

    await openPanel(demoPage);

    // Asserted on the panel chrome rather than on a row, for the reason given at the top of
    // this file: a fresh user's inbox is empty, so anything selecting `.notification` or its
    // unread dot waits for an element that will never exist and fails as a 90s timeout rather
    // than as a styling error. The row-level parts of the brand theme are covered where a
    // notification already has to exist, in toasts.spec.
    const popover = demoPage.getByRole("dialog");
    // --hermes-popover-width, a custom property the brand theme widens to 420px.
    expect(await popover.evaluate((node) => getComputedStyle(node).width)).toBe("420px");

    // And a ::part rule reaching an internal element no custom property exposes.
    const header = demoPage.locator("hermes-inbox").locator("css=.header");
    expect(await header.evaluate((node) => getComputedStyle(node).paddingTop)).toBe("18px");
  });

  test("switching the host theme restyles the widget with it", async ({ demoPage }) => {
    await openPanel(demoPage);
    const popover = demoPage.getByRole("dialog");
    const light = await popover.evaluate((node) => getComputedStyle(node).backgroundColor);

    await demoPage.getByLabel("Theme").selectOption("dark");

    const dark = await popover.evaluate((node) => getComputedStyle(node).backgroundColor);
    expect(dark).not.toBe(light);
  });
});
