// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { randomUUID } from "node:crypto";
import {
  clearIdentityCookie,
  readIdentityCookie,
  serializeIdentityCookie,
} from "./cookies.js";
import { PROXY_PREFIX } from "./proxy.js";
import { createSession, type Identity, type TokenMinter } from "./session.js";

export interface DemoConfig {
  /** Origin of the Hermes ingress. Locally `http://localhost:8888`. */
  hermesApiUrl: string;
  /** Where the browser should open its websocket. */
  socketUrl: string;
  cookieSecret: string;
  port: number;
}

/** A test send, in the shape the admin SDK's notifications service takes. */
export interface SendInput {
  to: { organizationId: string; userId: string };
  content: { title: string; body: string; actionUrl?: string; actionLabel?: string };
  channels: string[];
  idempotencyKey: string;
}

export interface DemoDeps {
  config: DemoConfig;
  hermes: TokenMinter;
  sendNotification: (input: SendInput) => Promise<{ notificationId: string }>;
  proxy: (request: Request) => Promise<Response>;
}

const UUID_PATTERN =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** A stray count must not turn a demo click into a flood through the real pipeline. */
const MAX_SEND_COUNT = 50;

function json(body: unknown, init: ResponseInit = {}): Response {
  return new Response(JSON.stringify(body), {
    status: init.status ?? 200,
    headers: { "Content-Type": "application/json", ...(init.headers ?? {}) },
  });
}

function problem(status: number, detail: string): Response {
  return json({ detail }, { status });
}

async function readJson(request: Request): Promise<Record<string, unknown>> {
  try {
    const body = (await request.json()) as unknown;
    return typeof body === "object" && body !== null ? (body as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}

/**
 * The demo's HTTP surface, as a plain `Request -> Response` function.
 *
 * Kept independent of `node:http` so routing and — more importantly — the "identity comes from
 * the server, never from the request" rule are testable without a socket.
 *
 * ## The shape worth copying
 *
 * `POST /api/demo/login` exists only because a demo has no real users; it stands in for whatever
 * session your app already has. `POST /api/session` reads identity **from that session only**.
 * Collapsing the two into one `POST /api/session {userId}` would be shorter and would teach
 * every reader to let the browser choose whose token it gets.
 */
export function createHandler(deps: DemoDeps): (request: Request) => Promise<Response> {
  const { config, hermes, sendNotification, proxy } = deps;

  function identityOf(request: Request): Identity | null {
    return readIdentityCookie(request.headers.get("cookie") ?? undefined, config.cookieSecret);
  }

  async function handleLogin(request: Request): Promise<Response> {
    const body = await readJson(request);
    const organizationId = body.organizationId;
    const externalUserId = body.externalUserId;

    if (typeof organizationId !== "string" || typeof externalUserId !== "string") {
      return problem(400, "organizationId and externalUserId are required");
    }
    if (!UUID_PATTERN.test(organizationId)) {
      // Caught here rather than upstream: organizations.id is a uuid column, and the admin API
      // answers a bare 500 from a Postgres type error for anything else — which reads as a
      // Hermes bug rather than a bad input.
      return problem(
        400,
        "organizationId must be a UUID — the organizations table keys on a uuid column"
      );
    }

    return json(
      { organizationId, externalUserId },
      { headers: { "Set-Cookie": serializeIdentityCookie({ organizationId, externalUserId }, config.cookieSecret) } }
    );
  }

  async function handleSession(request: Request): Promise<Response> {
    const identity = identityOf(request);
    if (!identity) return problem(401, "no demo session; POST /api/demo/login first");

    const session = await createSession(hermes, identity, { socketUrl: config.socketUrl });

    // `expires_at` is duplicated in snake_case because that is what <hermes-inbox token-url>
    // reads, and this endpoint doubles as that url.
    return json({ ...session, expires_at: session.expiresAt });
  }

  async function handleTestSend(request: Request): Promise<Response> {
    const identity = identityOf(request);
    if (!identity) return problem(401, "no demo session; POST /api/demo/login first");

    const body = await readJson(request);
    const title = body.title;
    const text = body.body;
    if (typeof title !== "string" || title === "") return problem(400, "title is required");
    if (typeof text !== "string") return problem(400, "body must be a string");

    const requested = typeof body.count === "number" ? Math.floor(body.count) : 1;
    const count = Math.max(1, Math.min(requested, MAX_SEND_COUNT));
    const channels = Array.isArray(body.channels) ? (body.channels as string[]) : ["inbox"];

    const sent: string[] = [];
    for (let index = 0; index < count; index++) {
      const { notificationId } = await sendNotification({
        to: { organizationId: identity.organizationId, userId: identity.externalUserId },
        content: {
          title: count > 1 ? `${title} (${index + 1}/${count})` : title,
          body: text,
          ...(typeof body.actionUrl === "string" ? { actionUrl: body.actionUrl } : {}),
          ...(typeof body.actionLabel === "string" ? { actionLabel: body.actionLabel } : {}),
        },
        channels,
        // A repeated key inside the dedup window silently drops the send, which in a demo looks
        // like the pipeline losing messages.
        idempotencyKey: randomUUID(),
      });
      sent.push(notificationId);
    }

    // 202, matching /v1/send: the notification row is created later by dispatch, so this is an
    // acknowledgement rather than a confirmation. Anything waiting on arrival must poll.
    return json({ notificationIds: sent }, { status: 202 });
  }

  return async function handle(request: Request): Promise<Response> {
    const { pathname } = new URL(request.url);

    if (pathname.startsWith(PROXY_PREFIX)) return proxy(request);

    if (pathname === "/healthz") return json({ status: "ok" });

    if (pathname === "/api/demo/login") {
      if (request.method !== "POST") return problem(405, "use POST");
      return handleLogin(request);
    }

    if (pathname === "/api/demo/logout") {
      if (request.method !== "POST") return problem(405, "use POST");
      return json({ status: "ok" }, { headers: { "Set-Cookie": clearIdentityCookie() } });
    }

    if (pathname === "/api/session") {
      // GET as well as POST: <hermes-inbox token-url> issues a credentialed GET, and that is the
      // only token refresh a plain-HTML consumer can express.
      if (request.method !== "POST" && request.method !== "GET") return problem(405, "use GET or POST");
      return handleSession(request);
    }

    if (pathname === "/api/test-send") {
      if (request.method !== "POST") return problem(405, "use POST");
      return handleTestSend(request);
    }

    return problem(404, `no route for ${pathname}`);
  };
}
