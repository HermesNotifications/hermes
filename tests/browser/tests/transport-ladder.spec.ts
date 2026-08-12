// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import type { Page } from "@playwright/test";
import {
  badge,
  countInboxFetches,
  expect,
  loginAs,
  openPanel,
  test,
  waitForInboxLoaded,
  waitForRealtimeReady,
  type HermesUser,
} from "../fixtures/demo.js";
import { SOCKET_ENDPOINT } from "../fixtures/hermes-api.js";

/**
 * The transport ladder: websocket, then http_stream, then sse (ADR 0017).
 *
 * The bug this suite exists for is invisible by construction. A client behind a proxy that blocks
 * the websocket upgrade loads its first page over REST — so the inbox renders, looks healthy, and
 * reports no error — and then never updates again for the whole session, because a socket that
 * never opens never reconnects and nothing else triggers a refresh. It reproduces on one corporate
 * network and nowhere else: not in CI, not in monitoring, not in any developer's browser.
 *
 * So these tests manufacture that network. Each one blocks a prefix of the ladder and requires the
 * widget to deliver a live notification anyway, over the rung below.
 *
 * **How a rung is proved.** Not by trusting that blocking worked, and not by reading centrifuge's
 * internals — by counting requests to `/emulation`. That endpoint carries the client→server half of
 * http_stream and sse, which are unidirectional on their own. A websocket is bidirectional and
 * never touches it. So `emulation > 0` is positive, mechanism-level proof that a fallback transport
 * is carrying the connection, and the control test below asserts the converse on a healthy network.
 * Without that pair, every test here would pass just as happily over a websocket that was never
 * actually blocked, which is the failure mode that makes a fallback test worthless.
 */

/** The ladder's three endpoints, derived exactly as the SDK derives them from one base URL. */
const REALTIME_BASE = SOCKET_ENDPOINT.replace(/\/connection\/websocket$/, "");
const HTTP_BASE = REALTIME_BASE.replace(/^ws:/, "http:").replace(/^wss:/, "https:");

type TransportTally = {
  /** Requests to each fallback rung, and to the emulation endpoint they share. */
  httpStream: number;
  sse: number;
  emulation: number;
  /** URLs of every websocket the page opened, successful or not. */
  websockets: string[];
};

/**
 * Count realtime transport traffic from now on.
 *
 * Matches on pathname suffix rather than the full URL so this keeps working against a stack with no
 * ingress, where Centrifugo is on its own port and the paths lose the `/realtime` prefix.
 */
function watchTransports(page: Page): { get: () => TransportTally } {
  const tally: TransportTally = { httpStream: 0, sse: 0, emulation: 0, websockets: [] };

  page.on("websocket", (ws) => tally.websockets.push(ws.url()));
  page.on("request", (request) => {
    let path: string;
    try {
      path = new URL(request.url()).pathname.replace(/\/+$/, "");
    } catch {
      return;
    }
    if (path.endsWith("/connection/http_stream")) tally.httpStream++;
    else if (path.endsWith("/connection/sse")) tally.sse++;
    else if (path.endsWith("/emulation")) tally.emulation++;
  });

  return { get: () => tally };
}

/**
 * Make the websocket handshake fail, the way a middlebox that drops `Upgrade` does.
 *
 * Two approaches were tried before this one, and both were wrong in the same instructive way.
 * `page.route(...).abort()` does nothing at all: websockets bypass the HTTP request interceptor
 * entirely. `page.routeWebSocket(url, ws => ws.close())` looks right but is worse than useless
 * here — Playwright's mock *accepts* the handshake and then closes, so the page sees a socket that
 * opened and later dropped. centrifuge-js treats that as a lost connection and reconnects on the
 * same transport with backoff; it only steps down the ladder when a transport fails to establish.
 * The test then "proved" the ladder was broken when the simulation was.
 *
 * So: point the websocket at a closed port instead. The TCP connection is refused, the page gets a
 * genuine connection failure with no handshake at all, and that is exactly the signal a blocked
 * upgrade produces. No mocking semantics are involved, which is why this is the version that
 * reflects the real network.
 *
 * Only the realtime socket is diverted — anything else the page opens is left alone.
 */
async function blockWebsocket(page: Page): Promise<void> {
  await page.addInitScript(() => {
    const Original = window.WebSocket;
    const Blocked = function (this: unknown, url: string | URL, protocols?: string | string[]) {
      const target = String(url).includes("/connection/websocket")
        ? // Port 1 is on Chrome's blocked-port list, so the connection is refused before a
          // socket is ever opened (ERR_UNSAFE_PORT). Chosen over a high unused port because
          // it fails deterministically — it cannot start passing because something happened
          // to bind the port on the machine running the suite.
          "ws://127.0.0.1:1/blocked"
        : String(url);
      return new Original(target, protocols);
    } as unknown as typeof WebSocket;

    // Carry over CONNECTING/OPEN/CLOSING/CLOSED and the prototype, so any `instanceof` or
    // readyState comparison in the SDK behaves exactly as it would untouched.
    Blocked.prototype = Original.prototype;
    Object.assign(Blocked, Original);
    window.WebSocket = Blocked;
  });
}

/** Make an HTTP rung fail outright. */
async function blockHttpRung(page: Page, rung: "http_stream" | "sse"): Promise<void> {
  await page.route(`**/connection/${rung}*`, (route) => route.abort());
}

/**
 * Log in, arm the blocks, and load the demo.
 *
 * The `demoPage` fixture cannot be used here: it navigates, and every block above has to be in
 * place *before* the widget opens its connection.
 */
async function openDemoWithBlocks(
  page: Page,
  user: HermesUser,
  block: (page: Page) => Promise<void>
): Promise<void> {
  await loginAs(page, user);
  await block(page);
  await page.goto("/");
  await waitForInboxLoaded(page);
}

/** Send a notification and require it to appear in the widget without a refetch. */
async function expectLiveArrival(page: Page, user: HermesUser, title: string): Promise<void> {
  const fetches = countInboxFetches(page);
  await user.send({ title, body: "delivered over a fallback transport" });

  await expect(badge(page)).toHaveText("1");
  await openPanel(page);
  // Scoped to the widget: the demo's activity log echoes the title, and an unscoped match would
  // fail strict mode without saying anything about what the widget rendered.
  await expect(page.locator("hermes-inbox").getByText(title)).toBeVisible();

  expect(
    fetches.get(),
    "the notification arrived via a refetch, so this proves nothing about realtime"
  ).toBe(0);
}

test.describe("transport ladder", () => {
  test("the server offers every rung, not just the websocket", async ({ request }) => {
    // The cheapest guard against the single most likely silent regression in this feature.
    //
    // Centrifugo changed these config keys between majors — v5 takes a flat `"sse": true`, v6 takes
    // `"sse": {"enabled": true}` — and this repo deploys BOTH (kustomize runs v5, the Helm chart
    // runs v6). An unrecognised key is ignored rather than rejected, so the wrong shape yields a
    // server that starts healthy, passes every other test in this suite, and 404s only the rungs
    // nobody exercises until a user on a hostile network needs them.
    //
    // Any status but 404 means the endpoint is registered. 400/405 are the correct answers to a
    // bare GET here and are what a working server returns.
    for (const path of ["/connection/http_stream", "/connection/sse", "/emulation"]) {
      const response = await request.get(`${HTTP_BASE}${path}`, { failOnStatusCode: false });
      expect(
        response.status(),
        `${path} is not registered — the Centrifugo transport config key is wrong for this image's major version`
      ).not.toBe(404);
    }
  });

  test("prefers the websocket, and touches no fallback, on a healthy network", async ({
    page,
    hermesUser,
  }) => {
    // The control. Without it the fallback tests are unfalsifiable: they would pass identically if
    // blocking silently did nothing and every connection were really a websocket. This asserts the
    // converse of the /emulation signal those tests rely on.
    const transports = watchTransports(page);
    await openDemoWithBlocks(page, hermesUser, async () => {});
    await waitForRealtimeReady(page);
    await expectLiveArrival(page, hermesUser, `Healthy network ${Date.now()}`);

    const tally = transports.get();
    expect(
      tally.websockets.some((url) => url.includes("/connection/websocket")),
      "no websocket was opened on a network where nothing is blocked"
    ).toBe(true);
    expect(
      { httpStream: tally.httpStream, sse: tally.sse, emulation: tally.emulation },
      "a fallback rung was used even though the websocket was available — the ladder is not ordered"
    ).toEqual({ httpStream: 0, sse: 0, emulation: 0 });
  });

  test("falls through to http_stream when the websocket is blocked", async ({
    page,
    hermesUser,
  }) => {
    const transports = watchTransports(page);
    await openDemoWithBlocks(page, hermesUser, blockWebsocket);

    // The whole point: still connected, on a network where the websocket cannot be.
    await waitForRealtimeReady(page);
    await expectLiveArrival(page, hermesUser, `Websocket blocked ${Date.now()}`);

    const tally = transports.get();
    expect(tally.httpStream, "http_stream was never attempted").toBeGreaterThan(0);
    expect(
      tally.emulation,
      "no /emulation traffic, so the connection was not actually carried by a fallback transport"
    ).toBeGreaterThan(0);
    // Stopped at rung 2 rather than running all the way down.
    expect(tally.sse, "fell past http_stream to sse, so rung 2 is not working").toBe(0);
  });

  test("falls through to sse when the websocket and http_stream are both blocked", async ({
    page,
    hermesUser,
  }) => {
    const transports = watchTransports(page);
    await openDemoWithBlocks(page, hermesUser, async (target) => {
      await blockWebsocket(target);
      await blockHttpRung(target, "http_stream");
    });

    await waitForRealtimeReady(page);
    await expectLiveArrival(page, hermesUser, `Only SSE left ${Date.now()}`);

    const tally = transports.get();
    expect(tally.sse, "sse was never attempted").toBeGreaterThan(0);
    expect(
      tally.emulation,
      "no /emulation traffic, so the connection was not actually carried by sse"
    ).toBeGreaterThan(0);
  });

  test("reports itself disconnected, and does not poll, when every rung is blocked", async ({
    page,
    hermesUser,
  }) => {
    // The honest negative. ADR 0017 rejected a polling rung below sse, so a client with no usable
    // transport must say so rather than quietly degrade into a refetch loop. This pins that: if
    // someone later adds polling without an ADR, this test fails and asks why.
    //
    // The inbox itself must still work — realtime is an enhancement, and REST is the source of
    // truth. A dead ladder may not take the list down with it.
    const title = `Sent before the page loaded ${Date.now()}`;
    await hermesUser.send({ title, body: "REST must still show it" });
    await hermesUser.waitFor((inbox) => inbox.data.some((n) => n.title === title));

    await openDemoWithBlocks(page, hermesUser, async (target) => {
      await blockWebsocket(target);
      await blockHttpRung(target, "http_stream");
      await blockHttpRung(target, "sse");
    });

    // REST carried both the list and the count, with realtime dead throughout.
    await expect(badge(page)).toHaveText("1");
    await openPanel(page);
    await expect(page.locator("hermes-inbox").getByText(title)).toBeVisible();
    await expect(page.getByTestId("realtime-status")).not.toHaveText("connected");

    const fetches = countInboxFetches(page);
    await hermesUser.send({ title: "Sent while offline", body: "must not appear by polling" });
    // Long enough that any plausible poll interval would have fired at least once.
    await page.waitForTimeout(5_000);
    expect(
      fetches.get(),
      "the client polled /v1/inbox with realtime down — ADR 0017 deliberately has no polling rung"
    ).toBe(0);
  });
});
