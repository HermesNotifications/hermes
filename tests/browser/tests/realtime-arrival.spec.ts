// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import {
  badge,
  countInboxFetches,
  expect,
  openPanel,
  rows,
  test,
  waitForRealtimeReady,
} from "../fixtures/demo.js";

/**
 * The centrepiece of the suite, and the success criterion for this work: a notification sent by a
 * process the browser knows nothing about appears in the widget, without a reload.
 *
 * The assertion that makes it meaningful is the *negative* one — that no `GET /v1/inbox` was issued
 * while the row appeared. Without it, "the notification showed up" is equally satisfied by a
 * background refetch on an interval, which would prove nothing about realtime at all.
 */
test.describe("realtime arrival", () => {
  test("a notification appears with no refetch", async ({ demoPage, hermesUser }) => {
    await waitForRealtimeReady(demoPage);

    const fetches = countInboxFetches(demoPage);
    const title = `Invoice ${Date.now()} is ready`;

    // Sent from the test process, using the API key. The browser has no idea this happened.
    await hermesUser.send({ title, body: "Payment due in 14 days." });

    await expect(badge(demoPage)).toHaveText("1");
    await openPanel(demoPage);
    await expect(demoPage.getByText(title)).toBeVisible();

    // The whole point.
    expect(
      fetches.get(),
      "the notification arrived via a refetch, not over the websocket"
    ).toBe(0);
  });

  test("an arrival lands at the top of an already-open panel", async ({ demoPage, hermesUser }) => {
    await waitForRealtimeReady(demoPage);
    await hermesUser.send({ title: "First", body: "one" });
    await expect(badge(demoPage)).toHaveText("1");

    await openPanel(demoPage);
    await hermesUser.send({ title: "Second", body: "two" });

    await expect(rows(demoPage)).toHaveCount(2);
    // Newest first: an arrival prepends.
    await expect(rows(demoPage).first()).toContainText("Second");
  });

  test("three sends produce a count of three, in order", async ({ demoPage, hermesUser }) => {
    await waitForRealtimeReady(demoPage);

    await hermesUser.send({ title: "Alpha", body: "1" });
    await expect(badge(demoPage)).toHaveText("1");
    await hermesUser.send({ title: "Bravo", body: "2" });
    await expect(badge(demoPage)).toHaveText("2");
    await hermesUser.send({ title: "Charlie", body: "3" });
    await expect(badge(demoPage)).toHaveText("3");

    await openPanel(demoPage);
    await expect(rows(demoPage)).toHaveCount(3);
    const titles = await rows(demoPage).allInnerTexts();
    expect(titles[0]).toContain("Charlie");
    expect(titles[2]).toContain("Alpha");
  });

  test("an arriving notification carries its action url through", async ({
    demoPage,
    hermesUser,
  }) => {
    // The realtime payload carries the action, and the old synthesizers dropped it — so a
    // notification that arrived live could never show its call to action, while the same
    // notification after a reload could.
    await waitForRealtimeReady(demoPage);

    await hermesUser.send({
      title: "Action arrival",
      body: "has an action",
      actionUrl: "/invoices/9001",
      actionLabel: "Open invoice",
    });

    await expect(badge(demoPage)).toHaveText("1");
    await openPanel(demoPage);

    const link = demoPage.locator("hermes-inbox").locator("css=a.row-target");
    await expect(link).toHaveAttribute("href", "/invoices/9001");
    await expect(demoPage.getByText("Open invoice")).toBeVisible();
  });

  test("the widget records the arrival in the host app's own log", async ({
    demoPage,
    hermesUser,
  }) => {
    // Proves the CustomEvent crossed the shadow boundary into React. Without composed: true the
    // host's onNotification callback never fires, and the widget would be an island.
    await waitForRealtimeReady(demoPage);

    await hermesUser.send({ title: "Bridged event", body: "b" });

    await expect(demoPage.getByTestId("activity-log")).toContainText(
      "arrived over websocket: Bridged event"
    );
  });

  test("a standalone badge elsewhere on the page agrees with the widget", async ({
    demoPage,
    hermesUser,
  }) => {
    // The page renders a second count from useUnreadCount over the shared client. It used to read
    // zero until the user's first mutation, because the client only learned the count from a
    // server-side update event.
    await waitForRealtimeReady(demoPage);

    await hermesUser.send({ title: "Count check", body: "c" });

    await expect(badge(demoPage)).toHaveText("1");
    await expect(demoPage.getByTestId("hook-unread")).toHaveText("1");
  });
});
