// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { randomUUID } from "node:crypto";
import {
  HERMES_URL,
  INBOX_URL,
  apiKey,
  listInbox,
  mintToken,
  sendNotification,
} from "./fixtures/hermes-api.js";

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
  const realtime = await probe(`${HERMES_URL}/realtime/health`);
  if (!realtime.ok) {
    fail(
      `GET ${HERMES_URL}/realtime/health returned ${realtime.status}`,
      "check Centrifugo is running and hermes-realtime-ingress rewrites /realtime/* correctly"
    );
  }

  // 4. One real notification, end to end. /v1/send returns 202 before the row exists — dispatch
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
