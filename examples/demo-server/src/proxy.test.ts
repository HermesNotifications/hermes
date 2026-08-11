// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { describe, expect, it } from "vitest";
import { buildProxyRequest, PROXY_PREFIX, upstreamFor } from "./proxy.js";

/**
 * Why a proxy exists at all: the Hermes services ship no CORS headers, so a browser on the
 * demo's origin cannot call the inbox API directly. Proxying `/v1/*` through the app's own
 * origin is the supported integration today, and it is a pattern an integrator copies rather
 * than a build-tool trick — which is why it lives here and not in a Vite config.
 *
 * Two rules are the actual security content of this module, and both are asserted below:
 * forward the caller's user token untouched, and never attach the API key.
 */

const UPSTREAM = "http://localhost:8888";

function request(
  url: string,
  init: { method?: string; headers?: Record<string, string> } = {}
): Request {
  return new Request(`http://localhost:5173${url}`, {
    method: init.method ?? "GET",
    headers: init.headers,
  });
}

describe("buildProxyRequest: routing", () => {
  it("rewrites the path onto the upstream origin", () => {
    const proxied = buildProxyRequest(request("/v1/inbox"), UPSTREAM);
    expect(proxied.url).toBe(`${UPSTREAM}/v1/inbox`);
  });

  it("preserves the query string, including pagination", () => {
    const proxied = buildProxyRequest(
      request("/v1/inbox?limit=20&archived=false&cursor=abc"),
      UPSTREAM
    );
    const url = new URL(proxied.url);
    expect(url.searchParams.get("limit")).toBe("20");
    expect(url.searchParams.get("archived")).toBe("false");
    expect(url.searchParams.get("cursor")).toBe("abc");
  });

  it("preserves the method", () => {
    const proxied = buildProxyRequest(request("/v1/inbox/n1/read", { method: "PUT" }), UPSTREAM);
    expect(proxied.method).toBe("PUT");
  });

  it("tolerates an upstream with a trailing slash", () => {
    const proxied = buildProxyRequest(request("/v1/inbox"), `${UPSTREAM}/`);
    expect(proxied.url).toBe(`${UPSTREAM}/v1/inbox`);
  });

  it("keeps a path component on the upstream instead of dropping it", () => {
    // The incoming pathname is root-absolute, so resolving it as a relative reference used
    // to replace the upstream's path entirely — an upstream behind a gateway prefix would
    // silently proxy to the wrong place. The defaults have no path, so nothing local caught
    // it, but this file is reference code integrators copy.
    const proxied = buildProxyRequest(request("/v1/inbox"), "https://hermes.example.com/gateway");
    expect(proxied.url).toBe("https://hermes.example.com/gateway/v1/inbox");
  });

  it("keeps a path component whether or not it ends in a slash", () => {
    const proxied = buildProxyRequest(request("/v1/inbox"), "https://hermes.example.com/gateway/");
    expect(proxied.url).toBe("https://hermes.example.com/gateway/v1/inbox");
  });

  it("keeps a nested path component, with the query string intact", () => {
    const proxied = buildProxyRequest(
      request("/v1/inbox?limit=5"),
      "https://hermes.example.com/a/b"
    );
    expect(proxied.url).toBe("https://hermes.example.com/a/b/v1/inbox?limit=5");
  });

  it("only claims the /v1 prefix", () => {
    // /api/* is the demo's own surface and must not be forwarded upstream.
    expect(PROXY_PREFIX).toBe("/v1/");
  });
});

describe("upstreamFor", () => {
  const INGRESS = { default: "http://localhost:8888" };
  const PER_SERVICE = {
    default: "http://localhost:8080",
    routes: {
      "/v1/inbox": "http://localhost:8086",
      "/v1/users": "http://localhost:8080",
      "/v1/users/me": "http://localhost:8087",
      "/v1/send": "http://localhost:8088",
    },
  };

  it("sends everything to one origin when an ingress fronts the services", () => {
    expect(upstreamFor("/v1/inbox", INGRESS)).toBe("http://localhost:8888");
    expect(upstreamFor("/v1/send", INGRESS)).toBe("http://localhost:8888");
  });

  it("routes each path to its own service when there is no ingress", () => {
    expect(upstreamFor("/v1/inbox", PER_SERVICE)).toBe("http://localhost:8086");
    expect(upstreamFor("/v1/send", PER_SERVICE)).toBe("http://localhost:8088");
  });

  it("prefers the longest matching prefix, as the real ingress does", () => {
    // /v1/users/me belongs to the user service while /v1/users belongs to admin. A first-match
    // implementation would send both to whichever was declared first.
    expect(upstreamFor("/v1/users/me/preferences", PER_SERVICE)).toBe("http://localhost:8087");
    expect(upstreamFor("/v1/users", PER_SERVICE)).toBe("http://localhost:8080");
  });

  it("falls back to the default for an unrouted path", () => {
    expect(upstreamFor("/v1/templates", PER_SERVICE)).toBe("http://localhost:8080");
  });
});

describe("buildProxyRequest: credentials", () => {
  it("forwards the caller's bearer token unchanged", () => {
    const proxied = buildProxyRequest(
      request("/v1/inbox", { headers: { authorization: "Bearer user-jwt" } }),
      UPSTREAM
    );
    expect(proxied.headers.get("authorization")).toBe("Bearer user-jwt");
  });

  it("sends no authorization at all when the caller sent none", () => {
    // Substituting a credential here would turn an unauthenticated request into an
    // authenticated one — the proxy must stay a relay, so an anonymous call gets the 401 it
    // deserves.
    const proxied = buildProxyRequest(request("/v1/inbox"), UPSTREAM);
    expect(proxied.headers.get("authorization")).toBeNull();
  });

  it("never carries anything resembling an API key", () => {
    const proxied = buildProxyRequest(
      request("/v1/inbox", { headers: { authorization: "Bearer user-jwt" } }),
      UPSTREAM
    );
    const names = [...proxied.headers.keys()];
    expect(names).not.toContain("x-api-key");
    expect(proxied.headers.get("authorization")).not.toContain("hk_");
  });

  it("forwards the content type so a JSON body is parsed upstream", () => {
    const proxied = buildProxyRequest(
      request("/v1/inbox", { method: "PUT", headers: { "content-type": "application/json" } }),
      UPSTREAM
    );
    expect(proxied.headers.get("content-type")).toBe("application/json");
  });

  it("drops hop-by-hop and origin headers rather than confusing the upstream", () => {
    const proxied = buildProxyRequest(
      request("/v1/inbox", {
        headers: {
          authorization: "Bearer user-jwt",
          host: "localhost:5173",
          origin: "http://localhost:5173",
          connection: "keep-alive",
          cookie: "hermes_demo_identity=secret",
        },
      }),
      UPSTREAM
    );

    expect(proxied.headers.get("host")).toBeNull();
    expect(proxied.headers.get("connection")).toBeNull();
    // The demo's own session cookie is meaningless to Hermes, and forwarding it would leak the
    // signed identity to another service for no reason.
    expect(proxied.headers.get("cookie")).toBeNull();
  });
});
