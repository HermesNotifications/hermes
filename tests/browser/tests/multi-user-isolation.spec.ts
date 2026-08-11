// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { randomUUID } from "node:crypto";
import { Centrifuge } from "centrifuge";
import {
  badge,
  expect,
  loginAs,
  test,
  trigger,
  waitForInboxLoaded,
  waitForRealtimeReady,
} from "../fixtures/demo.js";
import { SOCKET_ENDPOINT, mintToken, sendNotification } from "../fixtures/hermes-api.js";

/**
 * Channel scoping, which is a security property rather than a feature.
 *
 * Centrifugo's `user#<sub>` convention plus `allow_user_limited_channels` means the *server* refuses
 * a subscription whose suffix does not match the token's subject. Nothing client-side enforces it, so
 * nothing client-side can test it — only a live run against a real Centrifugo can.
 */

test.describe("multi-user isolation", () => {
  test("a notification reaches only its own recipient", async ({ browser, hermesUser }) => {
    const other = {
      organizationId: randomUUID(),
      externalUserId: `e2e-other-${Date.now()}`,
    };
    // Mint for the second user too, so their org and user exist before their page loads.
    await mintToken(other);

    const contextA = await browser.newContext();
    const contextB = await browser.newContext();
    try {
      const pageA = await contextA.newPage();
      const pageB = await contextB.newPage();

      await loginAs(pageA, hermesUser);
      await loginAs(pageB, other);
      await pageA.goto("/");
      await pageB.goto("/");
      await waitForInboxLoaded(pageA);
      await waitForInboxLoaded(pageB);
      await waitForRealtimeReady(pageA);
      await waitForRealtimeReady(pageB);

      await sendNotification({
        organizationId: hermesUser.organizationId,
        externalUserId: hermesUser.externalUserId,
        title: "For A only",
        body: "should not reach B",
      });

      await expect(badge(pageA)).toHaveText("1");

      // B stays empty. Asserted only *after* A has received, so this is not a race B happened to
      // lose — the delivery demonstrably completed and still did not reach B.
      await expect(trigger(pageB)).toBeVisible();
      await expect(badge(pageB)).toHaveCount(0);
    } finally {
      await contextA.close();
      await contextB.close();
    }
  });

  test("Centrifugo refuses a subscription to another user's channel", async ({ hermesUser }) => {
    // The strongest assertion available here. A client able to subscribe to an arbitrary
    // `user#<id>` could read anyone's notifications in real time, whatever the REST API permits.
    //
    // Driven from Node rather than in-page: node 24 has a global WebSocket, so the real centrifuge
    // client works here, and it avoids depending on how the dev server happens to serve
    // node_modules.
    const victim = await mintToken({
      organizationId: randomUUID(),
      externalUserId: `e2e-victim-${Date.now()}`,
    });

    const outcome = await new Promise<"subscribed" | "error" | "timeout">((resolve) => {
      const client = new Centrifuge(SOCKET_ENDPOINT, {
        // A perfectly valid token — for a *different* user than the channel below.
        token: hermesUser.token,
        websocket: WebSocket,
      });
      const settle = (result: "subscribed" | "error" | "timeout") => {
        clearTimeout(timer);
        client.disconnect();
        resolve(result);
      };
      const timer = setTimeout(() => settle("timeout"), 15_000);

      const subscription = client.newSubscription(`user#${victim.sub}`);
      subscription.on("subscribed", () => settle("subscribed"));
      subscription.on("error", () => settle("error"));
      client.on("error", () => settle("error"));
      subscription.subscribe();
      client.connect();
    });

    expect(
      outcome,
      "Centrifugo permitted a cross-user subscription — check allow_user_limited_channels " +
        "and that the channel separator is '#'"
    ).not.toBe("subscribed");
  });

  test("a user's own channel still works, so the check above is meaningful", async ({
    hermesUser,
  }) => {
    // Without this, "refused" could simply mean the endpoint is unreachable and the previous test
    // would pass for the wrong reason.
    const outcome = await new Promise<"subscribed" | "error" | "timeout">((resolve) => {
      const client = new Centrifuge(SOCKET_ENDPOINT, {
        token: hermesUser.token,
        websocket: WebSocket,
      });
      const settle = (result: "subscribed" | "error" | "timeout") => {
        clearTimeout(timer);
        client.disconnect();
        resolve(result);
      };
      const timer = setTimeout(() => settle("timeout"), 15_000);

      const subscription = client.newSubscription(`user#${hermesUser.hermesUserId}`);
      subscription.on("subscribed", () => settle("subscribed"));
      subscription.on("error", () => settle("error"));
      subscription.subscribe();
      client.connect();
    });

    expect(outcome).toBe("subscribed");
  });
});
