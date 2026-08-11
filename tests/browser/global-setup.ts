// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { randomBytes, randomUUID } from "node:crypto";
import http from "node:http";
import https from "node:https";
import {
  INBOX_URL,
  REALTIME_HEALTH_URL,
  SOCKET_ENDPOINT,
  apiKey,
  listInbox,
  mintToken,
  sendNotification,
} from "./fixtures/hermes-api.js";

/**
 * The origin the browser will present on the websocket handshake.
 *
 * Must match `use.baseURL` in playwright.config.ts — it is what Centrifugo validates against
 * `allowed_origins`, and the whole point of the check below.
 */
const DEMO_ORIGIN = process.env.HERMES_DEMO_ORIGIN ?? "http://localhost:5173";

/**
 * Assert the environment contract before a single browser starts.
 *
 * The point is attribution. Without this, a cluster that is not running produces a wall of failing
 * browser specs that all look like widget bugs. Each check below fails with a message naming the
 * specific thing that is missing and the command that fixes it.
 *
 * The final step is a real send polled through to arrival. That proves NATS, dispatch and the inbox
 * worker are alive *before* any test can blame the widget for their absence, and it warms JetStream
 * so the first real spec is not also the slowest.
 */

function fail(what: string, fix: string): never {
  throw new Error(
    `\n\nLive E2E environment check failed: ${what}\n\n  Fix: ${fix}\n\n` +
      `  This suite needs the full stack running. See docs/testing.md.\n`
  );
}

/**
 * Perform a real websocket handshake, carrying an `Origin` header, and report the status.
 *
 * Hand-rolled over `node:http` rather than done with a websocket client, because the status code
 * *is* the result: a client library reports "connection failed" and swallows the 403 that says
 * why. Resolves 101 on a successful upgrade, otherwise whatever the server answered.
 */
export async function websocketHandshake(endpoint: string, origin: string): Promise<number> {
  const url = new URL(endpoint.replace(/^ws/, "http"));
  const transport = url.protocol === "https:" ? https : http;

  return new Promise<number>((resolve, reject) => {
    const request = transport.request({
      hostname: url.hostname,
      port: url.port,
      path: `${url.pathname}${url.search}`,
      method: "GET",
      headers: {
        Connection: "Upgrade",
        Upgrade: "websocket",
        "Sec-WebSocket-Version": "13",
        "Sec-WebSocket-Key": randomBytes(16).toString("base64"),
        Origin: origin,
      },
    });

    request.on("upgrade", (_response, socket) => {
      socket.destroy();
      resolve(101);
    });
    request.on("response", (response) => {
      response.resume();
      resolve(response.statusCode ?? 0);
    });
    request.on("error", reject);
    request.setTimeout(10_000, () => request.destroy(new Error("handshake timed out")));
    request.end();
  });
}

async function probe(url: string): Promise<Response> {
  try {
    return await fetch(url);
  } catch (cause) {
    fail(
      `cannot reach ${url} (${cause instanceof Error ? cause.message : String(cause)})`,
      "run 'make dev-up' and wait for Tilt to go green"
    );
  }
}

export default async function globalSetup(): Promise<void> {
  const key = apiKey();
  if (!key) {
    fail(
      "HERMES_API_KEY is not set",
      "run 'make seed' (it writes the key to web/admin/.env.local), then use 'make demo-e2e'"
    );
  }

  // 1. Ingress up, /v1/inbox routed to hermes-inbox, JWT middleware active. A 404 here means the
  //    ingress path rule is wrong; a connection refused means there is no cluster.
  const inbox = await probe(`${INBOX_URL}/v1/inbox`);
  if (inbox.status !== 401) {
    fail(
      `GET ${INBOX_URL}/v1/inbox returned ${inbox.status}, expected 401`,
      inbox.status === 404
        ? "the ingress is up but /v1/inbox is not routed — check deploy/k8s/base/ingress.yaml"
        : "check that hermes-inbox is running ('tilt get uiresources')"
    );
  }

  // 2. Admin routed, and the key carries organizations:manage.
  let probeToken: { token: string; sub: string };
  try {
    probeToken = await mintToken({
      organizationId: randomUUID(),
      externalUserId: `e2e-probe-${Date.now()}`,
    });
  } catch (cause) {
    fail(
      `could not mint a token: ${cause instanceof Error ? cause.message : String(cause)}`,
      "check hermes-admin is running and the API key has the organizations:manage permission"
    );
  }

  // 3. Realtime ingress, its rewrite rule, and Centrifugo itself.
  const realtime = await probe(REALTIME_HEALTH_URL);
  if (!realtime.ok) {
    fail(
      `GET ${REALTIME_HEALTH_URL} returned ${realtime.status}`,
      "check Centrifugo is running and hermes-realtime-ingress rewrites /realtime/* correctly"
    );
  }

  // 4. The same endpoint again, but as a *browser* would reach it: with an Origin header.
  //
  //    Check 3 cannot see this. It is a plain fetch, so it sends no Origin, and Centrifugo permits
  //    origin-less connections by design — "they typically originate from non-browser
  //    environments". So Centrifugo can be perfectly healthy, reachable and correctly routed while
  //    refusing every connection the widget will ever make.
  //
  //    That is not hypothetical: it cost a 45-minute run in which 24 specs failed, each waiting 30s
  //    for a realtime status that a 403 had already made impossible. This is the check that turns
  //    that into a few seconds and one sentence.
  let handshake: number;
  try {
    handshake = await websocketHandshake(SOCKET_ENDPOINT, DEMO_ORIGIN);
  } catch (cause) {
    fail(
      `websocket handshake to ${SOCKET_ENDPOINT} failed: ` +
        `${cause instanceof Error ? cause.message : String(cause)}`,
      "check the realtime ingress forwards Upgrade headers to Centrifugo"
    );
  }
  if (handshake !== 101) {
    fail(
      `a websocket handshake to ${SOCKET_ENDPOINT} from Origin ${DEMO_ORIGIN} returned ` +
        `${handshake}, expected 101`,
      handshake === 403
        ? `Centrifugo refused the browser's Origin. Add ${DEMO_ORIGIN} to "allowed_origins" in ` +
          `deploy/k8s/overlays/local/centrifugo-config.json, then 'tilt trigger centrifugo'. ` +
          `Centrifugo validates Origin only for browser clients, which is why every non-browser ` +
          `check above passes.`
        : "check the realtime ingress rewrites /realtime/* and forwards Upgrade headers"
    );
  }

  // 5. One real notification, end to end. /v1/send returns 202 before the row exists — dispatch
  //    creates it later — so this polls rather than trusting the status code.
  const organizationId = randomUUID();
  const externalUserId = `e2e-warmup-${Date.now()}`;
  const warm = await mintToken({ organizationId, externalUserId });
  await sendNotification({
    organizationId,
    externalUserId,
    title: "e2e warm-up",
    body: "confirming the pipeline is alive",
  });

  const deadline = Date.now() + 60_000;
  for (;;) {
    const page = await listInbox(warm.token);
    if (page.data.length > 0) break;
    if (Date.now() > deadline) {
      fail(
        "a notification sent through /v1/send never reached the inbox within 60s",
        "check hermes-dispatch and hermes-worker-inbox are running, and that NATS is healthy " +
          "('tilt logs hermes-dispatch')"
      );
    }
    await new Promise((resolve) => setTimeout(resolve, 500));
  }

  console.log(`live E2E environment OK (probe sub ${probeToken.sub})`);
}
