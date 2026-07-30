// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { describe, expect, it } from "vitest";
import { createHandler, type DemoDeps } from "./server.js";
import { serializeIdentityCookie } from "./cookies.js";

/**
 * The routing layer, tested through a plain `Request -> Response` handler so no socket is
 * needed. The pieces underneath (session assembly, cookie signing, proxy request building) have
 * their own suites; this file is about which route does what, and — more importantly — which
 * routes refuse to act without a server-side identity.
 */

const SECRET = "demo-cookie-secret";
const ORG = "3f4c1f52-0f8e-4a1c-9c1e-7c1f2b9a0d11";

function tokenFor(sub: string): string {
  const encode = (value: unknown) => {
    const bytes = new TextEncoder().encode(JSON.stringify(value));
    return btoa(String.fromCharCode(...bytes))
      .replace(/\+/g, "-")
      .replace(/\//g, "_")
      .replace(/=+$/, "");
  };
  return `${encode({ alg: "HS256" })}.${encode({ sub, organization_id: ORG })}.sig`;
}

interface Harness {
  handler: (request: Request) => Promise<Response>;
  sends: unknown[];
  proxied: Request[];
}

function harness(options: { proxyStatus?: number } = {}): Harness {
  const sends: unknown[] = [];
  const proxied: Request[] = [];

  const deps: DemoDeps = {
    config: {
      hermesApiUrl: "http://localhost:8888",
      socketUrl: "http://localhost:8888/realtime",
      cookieSecret: SECRET,
      port: 8899,
    },
    hermes: {
      exchangeToken: async () => ({
        token: tokenFor("usr_internal"),
        expiresAt: "2026-07-29T14:00:00.000Z",
      }),
    },
    sendNotification: async (input) => {
      sends.push(input);
      return { notificationId: "ntf_1" };
    },
    proxy: async (request) => {
      proxied.push(request);
      return new Response(JSON.stringify({ data: [], unread_count: 0 }), {
        status: options.proxyStatus ?? 200,
        headers: { "Content-Type": "application/json" },
      });
    },
  };

  return { handler: createHandler(deps), sends, proxied };
}

/** A request carrying a valid signed identity cookie. */
function authed(
  path: string,
  init: { method?: string; body?: unknown } = {}
): Request {
  const cookie = serializeIdentityCookie(
    { organizationId: ORG, externalUserId: "demo-user-1" },
    SECRET
  ).split(";")[0];

  return new Request(`http://localhost:8899${path}`, {
    method: init.method ?? "GET",
    headers: {
      cookie,
      ...(init.body ? { "content-type": "application/json" } : {}),
    },
    ...(init.body ? { body: JSON.stringify(init.body) } : {}),
  });
}

function anonymous(path: string, init: { method?: string; body?: unknown } = {}): Request {
  return new Request(`http://localhost:8899${path}`, {
    method: init.method ?? "GET",
    ...(init.body
      ? { headers: { "content-type": "application/json" }, body: JSON.stringify(init.body) }
      : {}),
  });
}

describe("POST /api/demo/login", () => {
  it("sets a signed identity cookie", async () => {
    const { handler } = harness();

    const response = await handler(
      anonymous("/api/demo/login", {
        method: "POST",
        body: { organizationId: ORG, externalUserId: "demo-user-1" },
      })
    );

    expect(response.status).toBe(200);
    expect(response.headers.get("set-cookie")).toContain("hermes_demo_identity=");
    expect(response.headers.get("set-cookie")).toContain("HttpOnly");
  });

  it("rejects a body missing either field", async () => {
    const { handler } = harness();
    const response = await handler(
      anonymous("/api/demo/login", { method: "POST", body: { organizationId: ORG } })
    );
    expect(response.status).toBe(400);
  });

  it("rejects a non-UUID organization id with a message naming the reason", async () => {
    // The organizations table keys on a uuid column, so a friendly-looking id like "demo-org"
    // makes the admin API fail with a Postgres type error — a 500 that looks like a Hermes bug.
    // Catching it here is worth the few lines.
    const { handler } = harness();

    const response = await handler(
      anonymous("/api/demo/login", {
        method: "POST",
        body: { organizationId: "demo-org", externalUserId: "u" },
      })
    );

    expect(response.status).toBe(400);
    await expect(response.text()).resolves.toMatch(/uuid/i);
  });
});

describe("POST /api/session", () => {
  it("mints a token for the cookie's identity", async () => {
    const { handler } = harness();

    const response = await handler(authed("/api/session", { method: "POST" }));

    expect(response.status).toBe(200);
    await expect(response.json()).resolves.toMatchObject({
      token: tokenFor("usr_internal"),
      hermesUserId: "usr_internal",
      externalUserId: "demo-user-1",
      organizationId: ORG,
      socketUrl: "http://localhost:8888/realtime",
    });
  });

  it("refuses without an identity cookie", async () => {
    // The important assertion in this file. Identity comes from the server-side session and
    // never from the request, so an anonymous caller cannot mint a token for an arbitrary user.
    const { handler } = harness();

    const response = await handler(anonymous("/api/session", { method: "POST" }));

    expect(response.status).toBe(401);
  });

  it("ignores an identity supplied in the request body", async () => {
    const { handler } = harness();

    const request = authed("/api/session", {
      method: "POST",
      body: { organizationId: "attacker-org", externalUserId: "someone-else" },
    });
    const response = await handler(request);

    await expect(response.json()).resolves.toMatchObject({
      organizationId: ORG,
      externalUserId: "demo-user-1",
    });
  });

  it("is also reachable by GET, so the widget's token-url can point at it", async () => {
    // <hermes-inbox token-url> issues a GET with credentials, and that is the only refresh
    // mechanism a plain-HTML consumer has.
    const { handler } = harness();

    const response = await handler(authed("/api/session"));

    expect(response.status).toBe(200);
    await expect(response.json()).resolves.toMatchObject({ token: tokenFor("usr_internal") });
  });

  it("returns expires_at in snake_case too, matching the widget's token-url contract", async () => {
    const { handler } = harness();
    const body = (await (await handler(authed("/api/session"))).json()) as Record<string, unknown>;
    expect(body.expires_at).toBe("2026-07-29T14:00:00.000Z");
    expect(body.expiresAt).toBe("2026-07-29T14:00:00.000Z");
  });
});

describe("POST /api/test-send", () => {
  it("sends to the cookie's identity on the inbox channel", async () => {
    const { handler, sends } = harness();

    const response = await handler(
      authed("/api/test-send", {
        method: "POST",
        body: { title: "Invoice ready", body: "Invoice #1234" },
      })
    );

    expect(response.status).toBe(202);
    expect(sends).toHaveLength(1);
    expect(sends[0]).toMatchObject({
      to: { organizationId: ORG, userId: "demo-user-1" },
      content: { title: "Invoice ready", body: "Invoice #1234" },
      channels: ["inbox"],
    });
  });

  it("passes an action url and label through", async () => {
    const { handler, sends } = harness();

    await handler(
      authed("/api/test-send", {
        method: "POST",
        body: {
          title: "T",
          body: "B",
          actionUrl: "http://localhost:5173/invoices/1",
          actionLabel: "View invoice",
        },
      })
    );

    expect(sends[0]).toMatchObject({
      content: { actionUrl: "http://localhost:5173/invoices/1", actionLabel: "View invoice" },
    });
  });

  it("generates a distinct idempotency key per send", async () => {
    // Repeating a key inside the dedup window silently drops the second notification, which in
    // a demo looks like the pipeline losing messages.
    const { handler, sends } = harness();

    await handler(authed("/api/test-send", { method: "POST", body: { title: "A", body: "B" } }));
    await handler(authed("/api/test-send", { method: "POST", body: { title: "A", body: "B" } }));

    const keys = sends.map((send) => (send as { idempotencyKey: string }).idempotencyKey);
    expect(keys[0]).toBeTypeOf("string");
    expect(keys[0]).not.toBe(keys[1]);
  });

  it("sends a requested count as separate notifications", async () => {
    const { handler, sends } = harness();

    await handler(
      authed("/api/test-send", { method: "POST", body: { title: "T", body: "B", count: 3 } })
    );

    expect(sends).toHaveLength(3);
  });

  it("caps the count so a stray value cannot flood the pipeline", async () => {
    const { handler, sends } = harness();

    await handler(
      authed("/api/test-send", { method: "POST", body: { title: "T", body: "B", count: 9999 } })
    );

    expect(sends.length).toBeLessThanOrEqual(50);
  });

  it("refuses without an identity cookie", async () => {
    const { handler } = harness();
    const response = await handler(
      anonymous("/api/test-send", { method: "POST", body: { title: "T", body: "B" } })
    );
    expect(response.status).toBe(401);
  });

  it("rejects a body with no title", async () => {
    const { handler } = harness();
    const response = await handler(
      authed("/api/test-send", { method: "POST", body: { body: "B" } })
    );
    expect(response.status).toBe(400);
  });
});

describe("/v1/* proxying", () => {
  it("relays an inbox request upstream", async () => {
    const { handler, proxied } = harness();

    const response = await handler(anonymous("/v1/inbox?limit=20"));

    expect(response.status).toBe(200);
    expect(proxied).toHaveLength(1);
    expect(proxied[0].url).toContain("/v1/inbox");
  });

  it("relays without needing the demo's own session", async () => {
    // The proxy is a relay: authorization is the caller's bearer token, which Hermes validates.
    const { handler, proxied } = harness();
    await handler(anonymous("/v1/inbox"));
    expect(proxied).toHaveLength(1);
  });

  it("passes an upstream error status straight through", async () => {
    const { handler } = harness({ proxyStatus: 401 });
    const response = await handler(anonymous("/v1/inbox"));
    expect(response.status).toBe(401);
  });

  it("does not proxy the demo's own /api routes", async () => {
    const { handler, proxied } = harness();
    await handler(anonymous("/api/session", { method: "POST" }));
    expect(proxied).toEqual([]);
  });
});

describe("unknown routes", () => {
  it("answers 404 for an unknown api path", async () => {
    const { handler } = harness();
    expect((await handler(anonymous("/api/nope"))).status).toBe(404);
  });

  it("answers 405 when the method is wrong for a known route", async () => {
    const { handler } = harness();
    expect((await handler(anonymous("/api/demo/login"))).status).toBe(405);
  });

  it("serves a health check", async () => {
    // The browser test suite waits on this before starting, so it must need no session.
    const { handler } = harness();
    const response = await handler(anonymous("/healthz"));
    expect(response.status).toBe(200);
  });
});
