// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import {
  badge,
  expect,
  failOnConsoleErrors,
  openPanel,
  rows,
  test,
  toasts,
  waitForRealtimeReady,
} from "../fixtures/demo.js";

/**
 * Toasts, end to end.
 *
 * The unit suite covers the routing and the dedupe against a fake client. What only a live run
 * proves is that `metadata` survives the whole pipeline — send, NATS, dispatch, Postgres, the
 * inbox worker's Centrifugo publish, and the client's wire parsing — and arrives shaped well
 * enough for the adapter to read `level` off it.
 */
test.describe("toasts", () => {
  test("a notification asking for a toast produces one", async ({ demoPage, hermesUser }) => {
    await waitForRealtimeReady(demoPage);

    await hermesUser.send({
      title: "Export complete",
      body: "1.2M rows are ready.",
      metadata: { toast: true, level: "success" },
    });

    await expect(toasts(demoPage)).toHaveCount(1);
    await expect(toasts(demoPage)).toContainText("Export complete");
  });

  test("a level with no toast flag stays in the panel", async ({ demoPage, hermesUser }) => {
    // The negative that matters: `level` and `toast` are independent, so styling a notification
    // as an error must not interrupt anyone. It still lands in the inbox.
    await waitForRealtimeReady(demoPage);

    await hermesUser.send({
      title: "Approaching your plan limit",
      body: "92% of this month's events used.",
      metadata: { level: "warning" },
    });

    await expect(badge(demoPage)).toHaveText("1");
    await expect(toasts(demoPage)).toHaveCount(0);
  });

  test("a toast with no level still appears", async ({ demoPage, hermesUser }) => {
    // Routed to the adapter's neutral `show`, never dropped: the sender asked to interrupt.
    await waitForRealtimeReady(demoPage);

    await hermesUser.send({
      title: "Something happened",
      body: "No level on this one.",
      metadata: { toast: true },
    });

    await expect(toasts(demoPage)).toHaveCount(1);
  });

  test("one send produces exactly one toast", async ({ demoPage, hermesUser }) => {
    // The dedupe, through a real socket. The client subscribes before it lists, so an arrival
    // can legitimately reach the reducer twice; the toast layer sits outside that reducer.
    await waitForRealtimeReady(demoPage);

    await hermesUser.send({
      title: "Only once",
      body: "Even though the row may arrive twice.",
      metadata: { toast: true, level: "info" },
    });

    await expect(toasts(demoPage)).toHaveCount(1);
    // Give a duplicate publication time to arrive and be suppressed.
    await demoPage.waitForTimeout(1500);
    await expect(toasts(demoPage)).toHaveCount(1);
  });

  test("an unrecognised level is tolerated, not thrown on", async ({ demoPage, hermesUser }) => {
    // A client older than the server must degrade. failOnConsoleErrors is what proves the
    // adapter did not throw on the way through.
    failOnConsoleErrors(demoPage);
    await waitForRealtimeReady(demoPage);

    await hermesUser.send({
      title: "From a newer server",
      body: "Level this client has never heard of.",
      // `level` is enum-validated at the send edge, so an unknown value cannot be sent through
      // the API. The forward-compatibility path is covered by the unit suite; here we assert
      // the neutral path an old client would take, with an opaque key alongside.
      metadata: { toast: true, futureField: "ignored" },
    });

    await expect(toasts(demoPage)).toHaveCount(1);
  });

  test("a declared level is still visible in the panel afterwards", async ({
    demoPage,
    hermesUser,
  }) => {
    // A severity that only existed for the few seconds a toast was on screen would be half a
    // feature. The row carries a level-* part token and a rail.
    await waitForRealtimeReady(demoPage);

    await hermesUser.send({
      title: "Payment failed",
      body: "Your card was declined.",
      metadata: { level: "error" },
    });
    await expect(badge(demoPage)).toHaveText("1");
    await openPanel(demoPage);

    const row = rows(demoPage).first();
    await expect(row).toHaveAttribute("data-level", "error");
    const shadow = await row.evaluate((node) => getComputedStyle(node).boxShadow);
    expect(shadow).not.toBe("none");
  });

  test("opaque metadata round-trips to the client untouched", async ({
    demoPage,
    hermesUser,
  }) => {
    // The passthrough promise, asserted where it is actually consumed rather than only in Go.
    await waitForRealtimeReady(demoPage);

    await hermesUser.send({
      title: "With your own data",
      body: "Keys Hermes does not read.",
      metadata: { toast: true, invoiceId: "1041", tab: "billing" },
    });
    await expect(toasts(demoPage)).toHaveCount(1);

    const inbox = await hermesUser.waitFor((page) => page.data.length === 1);
    const notification = inbox.data[0] as { metadata?: Record<string, unknown> };
    expect(notification.metadata?.invoiceId).toBe("1041");
    expect(notification.metadata?.tab).toBe("billing");
  });
});
