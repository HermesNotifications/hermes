// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { Readable } from "node:stream";
import type { IncomingMessage, ServerResponse } from "node:http";
import { describe, expect, it } from "vitest";
import { toRequest, writeResponse } from "./http-adapter.js";

/**
 * The bridge between node's streams and the WHATWG `Request`/`Response` the handler speaks.
 *
 * Small, but not trivial: cookies and `Set-Cookie` both have list semantics that a naive
 * object-based conversion silently collapses, and the demo's login flow depends on `Set-Cookie`
 * surviving. Hence tests rather than trust.
 */

/** A stand-in for node's IncomingMessage, which is an async-iterable stream plus headers. */
function incoming(options: {
  method?: string;
  url?: string;
  headers?: Record<string, string | string[]>;
  body?: string;
}): IncomingMessage {
  const stream = Readable.from(options.body ? [Buffer.from(options.body)] : []);
  return Object.assign(stream, {
    method: options.method ?? "GET",
    url: options.url ?? "/",
    headers: { host: "localhost:8899", ...(options.headers ?? {}) },
  }) as unknown as IncomingMessage;
}

/** Captures what was written, standing in for ServerResponse. */
function outgoing() {
  const captured = {
    status: 0,
    headers: {} as Record<string, string | string[]>,
    body: undefined as Buffer | undefined,
  };
  const response = {
    writeHead(status: number, headers: Record<string, string | string[]>) {
      captured.status = status;
      captured.headers = headers;
      return response;
    },
    end(body?: Buffer) {
      captured.body = body;
    },
  } as unknown as ServerResponse;
  return { response, captured };
}

describe("toRequest", () => {
  it("builds an absolute url from the host header and path", async () => {
    const request = await toRequest(incoming({ url: "/v1/inbox?limit=20" }), 8899);
    expect(request.url).toBe("http://localhost:8899/v1/inbox?limit=20");
  });

  it("falls back to the configured port when there is no host header", async () => {
    const message = incoming({ url: "/healthz" });
    delete (message.headers as Record<string, unknown>).host;
    const request = await toRequest(message, 8899);
    expect(request.url).toBe("http://localhost:8899/healthz");
  });

  it("carries the method", async () => {
    const request = await toRequest(incoming({ method: "PUT", url: "/v1/inbox/n1/read" }), 8899);
    expect(request.method).toBe("PUT");
  });

  it("carries headers, including the cookie the session depends on", async () => {
    const request = await toRequest(
      incoming({ headers: { cookie: "hermes_demo_identity=abc.def", authorization: "Bearer t" } }),
      8899
    );
    expect(request.headers.get("cookie")).toBe("hermes_demo_identity=abc.def");
    expect(request.headers.get("authorization")).toBe("Bearer t");
  });

  it("joins a repeated header rather than dropping all but one", async () => {
    const request = await toRequest(
      incoming({ headers: { "accept-encoding": ["gzip", "br"] } }),
      8899
    );
    expect(request.headers.get("accept-encoding")).toContain("gzip");
    expect(request.headers.get("accept-encoding")).toContain("br");
  });

  it("reads the body of a POST", async () => {
    const request = await toRequest(
      incoming({ method: "POST", url: "/api/test-send", body: '{"title":"T"}' }),
      8899
    );
    await expect(request.json()).resolves.toEqual({ title: "T" });
  });

  it.each(["GET", "HEAD"])("does not attempt to read a body for %s", async (method) => {
    // Constructing a Request with a body on GET throws, so this is a real constraint rather
    // than an optimisation.
    const request = await toRequest(incoming({ method }), 8899);
    expect(request.body).toBeNull();
  });
});

describe("writeResponse", () => {
  it("writes the status and body", async () => {
    const { response, captured } = outgoing();

    await writeResponse(new Response(JSON.stringify({ ok: true }), { status: 202 }), response);

    expect(captured.status).toBe(202);
    expect(captured.body?.toString()).toBe('{"ok":true}');
  });

  it("writes headers", async () => {
    const { response, captured } = outgoing();

    await writeResponse(
      new Response("{}", { headers: { "Content-Type": "application/json" } }),
      response
    );

    expect(captured.headers["content-type"]).toBe("application/json");
  });

  it("writes Set-Cookie as a list, so the login cookie survives", async () => {
    // Set-Cookie is the one header that must never be flattened into a comma-joined string:
    // browsers reject the joined form, and the demo's whole identity mechanism is a cookie.
    const { response, captured } = outgoing();
    const headers = new Headers();
    headers.append("Set-Cookie", "a=1; Path=/");
    headers.append("Set-Cookie", "b=2; Path=/");

    await writeResponse(new Response(null, { headers }), response);

    expect(captured.headers["set-cookie"]).toEqual(["a=1; Path=/", "b=2; Path=/"]);
  });

  it("ends with no body for an empty response", async () => {
    const { response, captured } = outgoing();
    await writeResponse(new Response(null, { status: 204 }), response);
    expect(captured.status).toBe(204);
    expect(captured.body).toBeUndefined();
  });
});
