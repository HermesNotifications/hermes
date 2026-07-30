// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { createServer } from "node:http";
import { Hermes } from "@hermes-notifications/server";
import { toRequest, writeResponse } from "./http-adapter.js";
import { proxyToHermes, type UpstreamRoutes } from "./proxy.js";
import { createHandler, type DemoConfig } from "./server.js";

/**
 * Process entry point: read configuration, wire the real SDK and proxy into the handler, listen.
 *
 * Deliberately contains no logic. Everything with behaviour worth asserting was extracted so it
 * could be tested without a socket — routing and the identity rule in `server.ts`, token
 * assembly in `session.ts`, cookie signing in `cookies.ts`, the two proxy rules in `proxy.ts`,
 * the stream conversion in `http-adapter.ts`. This file is the adapter that binds them to a port,
 * and the Playwright suite boots it for real.
 */

function requireApiKey(): string {
  const value = process.env.HERMES_API_KEY;
  if (!value) {
    // Fail loudly and name the fix. The usual cause is a missing `make seed`, and a server that
    // starts and then 500s on every request is far harder to diagnose than one that refuses to.
    console.error(
      "\nHERMES_API_KEY is not set.\n\n" +
        "Run 'make seed' to generate a dev API key, then start the demo with 'make dev-demo',\n" +
        "which sources the key for you. See examples/demo-server/.env.example.\n"
    );
    process.exit(1);
  }
  return value;
}

const config: DemoConfig = {
  // The ingress, not the :8080 admin port-forward that `make seed` writes into
  // web/admin/.env.local — /v1/send is served by hermes-send, which the admin service does not
  // route. Taking the key from that file but the URL from here is deliberate.
  hermesApiUrl: process.env.HERMES_API_URL ?? "http://localhost:8888",
  socketUrl: process.env.HERMES_SOCKET_URL ?? "http://localhost:8888/realtime",
  cookieSecret: process.env.DEMO_COOKIE_SECRET ?? "hermes-demo-cookie-secret",
  port: Number(process.env.PORT ?? 8899),
};

const hermes = new Hermes({
  // Admin serves /v1/auth; under an ingress that is the same origin as everything else.
  baseUrl: process.env.HERMES_ADMIN_URL ?? config.hermesApiUrl,
  apiKey: requireApiKey(),
});

/**
 * With `make dev-up` a single ingress routes every path, so one origin suffices.
 *
 * These overrides exist for a stack without an ingress, where each service owns a port. Setting any
 * of them switches the proxy to per-path routing — see `upstreamFor`.
 */
const routeOverrides: Array<[string, string | undefined]> = [
  ["/v1/inbox", process.env.HERMES_INBOX_URL],
  ["/v1/users/me", process.env.HERMES_USER_URL],
  ["/v1/send", process.env.HERMES_SEND_URL],
];

const upstream: UpstreamRoutes = {
  default: config.hermesApiUrl,
  routes: Object.fromEntries(
    routeOverrides.filter((entry): entry is [string, string] => entry[1] !== undefined)
  ),
};

const handle = createHandler({
  config,
  hermes: hermes.auth,
  sendNotification: (input) => hermes.notifications.send(input),
  proxy: (request) => proxyToHermes(request, upstream),
});

createServer((incoming, outgoing) => {
  void (async () => {
    try {
      await writeResponse(await handle(await toRequest(incoming, config.port)), outgoing);
    } catch (cause) {
      console.error("demo-server: unhandled failure", cause);
      outgoing.writeHead(500, { "Content-Type": "application/json" });
      outgoing.end(JSON.stringify({ detail: "internal error" }));
    }
  })();
}).listen(config.port, () => {
  console.log(`Hermes demo server on http://localhost:${config.port}`);
  console.log(`  proxying /v1/* to ${config.hermesApiUrl}`);
  console.log(`  browsers will open their socket at ${config.socketUrl}`);
});
