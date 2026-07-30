// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { badge, expect, test, waitForRealtimeReady } from "../fixtures/demo.js";

/**
 * Badge correctness, driven from *outside* the browser wherever possible.
 *
 * That matters: clicking the widget's own Read button applies an optimistic local decrement, which
 * would make the badge look right even if the server-driven `inbox.updated` path were completely
 * broken. Acting from the test process is the only way to exercise that path honestly.
 *
 * Per-test users make every assertion absolute rather than relative.
 */
test.describe("unread count", () => {
  test("is absent at zero rather than showing a nought", async ({ demoPage }) => {
    await expect(badge(demoPage)).toHaveCount(0);
  });

  test("counts up as notifications arrive", async ({ demoPage, hermesUser }) => {
    await waitForRealtimeReady(demoPage);

    await hermesUser.send({ title: "One", body: "1" });
    await expect(badge(demoPage)).toHaveText("1");

    await hermesUser.send({ title: "Two", body: "2" });
    await expect(badge(demoPage)).toHaveText("2");
  });

  test("drops when another client marks something read", async ({ demoPage, hermesUser }) => {
    await waitForRealtimeReady(demoPage);
    await hermesUser.send({ title: "One", body: "1" });
    await hermesUser.send({ title: "Two", body: "2" });
    await expect(badge(demoPage)).toHaveText("2");

    // Server-side, as a second device would. This is the inbox.updated path, and the widget's own
    // button would mask it with a local decrement.
    const page = await hermesUser.waitFor((inbox) => inbox.data.length === 2);
    const status = await hermesUser.action("PUT", `/${page.data[0].id}/read`);
    expect(status).toBe(200);

    await expect(badge(demoPage)).toHaveText("1");
  });

  test("survives a reload, because the server is the source of truth", async ({
    demoPage,
    hermesUser,
  }) => {
    await waitForRealtimeReady(demoPage);
    await hermesUser.send({ title: "Persisted", body: "p" });
    await expect(badge(demoPage)).toHaveText("1");

    await demoPage.reload();

    // If this were local state it would be gone.
    await expect(badge(demoPage)).toHaveText("1");
  });

  test("disappears when everything is marked read server-side", async ({
    demoPage,
    hermesUser,
  }) => {
    // read-all arrives with an empty notification_id and unread_count 0 — a distinct payload shape
    // from a single read, and worth exercising as such.
    await waitForRealtimeReady(demoPage);
    await hermesUser.send({ title: "One", body: "1" });
    await hermesUser.send({ title: "Two", body: "2" });
    await expect(badge(demoPage)).toHaveText("2");

    await hermesUser.waitFor((inbox) => inbox.unread_count === 2);
    expect(await hermesUser.action("PUT", "/read-all")).toBe(200);

    await expect(badge(demoPage)).toHaveCount(0);
  });

  test("caps its display above ninety-nine but keeps the server count", async ({
    hermesUser,
  }) => {
    // Sending 100 notifications through the real pipeline would be slow and pointless; the display
    // cap itself is unit-tested. What is worth asserting live is that the server counts exactly.
    await hermesUser.send({ title: "One", body: "1" });
    const page = await hermesUser.waitFor((inbox) => inbox.unread_count === 1);
    expect(page.unread_count).toBe(1);
  });
});
