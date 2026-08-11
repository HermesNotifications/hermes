// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

/** Requests under this prefix are relayed to Hermes; everything else is the demo's own. */
export const PROXY_PREFIX = "/v1/";

/**
 * Headers that must not be relayed.
 *
 * `host` and `origin` describe the browser's view of *this* server and would only confuse the
 * upstream. `cookie` carries the demo's own signed identity, which is meaningless to Hermes and
 * should not be handed to another service. The rest are hop-by-hop.
 */
const STRIPPED = new Set([
  "host",
  "origin",
  "referer",
  "cookie",
  "connection",
  "keep-alive",
  "proxy-authorization",
  "te",
  "trailer",
  "transfer-encoding",
  "upgrade",
  "content-length",
]);

/**
 * Which upstream serves a given path.
 *
 * Under `make dev-up` there is a single ingress that routes everything, so one origin is enough and
 * `routes` stays empty. Without an ingress each service is its own port — `/v1/inbox` on 8086,
 * `/v1/users/me` on 8087, `/v1/send` on 8088, the rest on admin's 8080 — and these overrides let the
 * same proxy serve that topology. Longest prefix wins, mirroring how the real ingress resolves
 * `/v1/users/me` ahead of `/v1/users`.
 */
export interface UpstreamRoutes {
  /** Fallback for anything not matched below. */
  default: string;
  /** Path prefix to origin, e.g. `{"/v1/inbox": "http://localhost:8086"}`. */
  routes?: Record<string, string>;
}

/** Pick the origin for `pathname`, preferring the longest matching prefix. */
export function upstreamFor(pathname: string, upstream: UpstreamRoutes): string {
  const matches = Object.keys(upstream.routes ?? {})
    .filter((prefix) => pathname.startsWith(prefix))
    .sort((a, b) => b.length - a.length);
  return matches.length > 0 ? (upstream.routes as Record<string, string>)[matches[0]] : upstream.default;
}

/**
 * Build the upstream request for a proxied `/v1/*` call.
 *
 * ## The two rules
 *
 * 1. **The caller's `Authorization` header is forwarded verbatim, never substituted.** This proxy
 *    is an unauthenticated relay for user-scoped endpoints. If the browser sends no token, the
 *    upstream must return 401 — quietly supplying one here would let any visitor read any
 *    inbox.
 * 2. **The Hermes API key is never attached.** It belongs only to the token-minting and
 *    test-send routes, which are separate handlers in this server. An API key on a proxied
 *    request would give the browser full administrative reach over the organization.
 *
 * Split out as a pure function so both rules are assertable without a network.
 */
export function buildProxyRequest(request: Request, upstreamOrigin: string): Request {
  const incoming = new URL(request.url);
  // The incoming pathname is root-absolute, so resolving it against the upstream as a
  // relative reference would discard any path the upstream carries: an upstream of
  // `https://hermes.example.com/gateway` would proxy `/v1/inbox` to
  // `https://hermes.example.com/v1/inbox`. The defaults have no path, so this never bit
  // locally — but this file is reference code integrators copy. Join the two explicitly.
  const base = new URL(upstreamOrigin);
  const basePath = base.pathname.endsWith("/") ? base.pathname.slice(0, -1) : base.pathname;
  const target = new URL(`${basePath}${incoming.pathname}${incoming.search}`, base.origin);

  const headers = new Headers();
  for (const [name, value] of request.headers) {
    if (!STRIPPED.has(name.toLowerCase())) headers.set(name, value);
  }

  return new Request(target, {
    method: request.method,
    headers,
    // GET and HEAD carry no body; anything else streams through untouched.
    body: request.method === "GET" || request.method === "HEAD" ? undefined : request.body,
    // Required by undici whenever a body is a stream.
    ...(request.method === "GET" || request.method === "HEAD" ? {} : { duplex: "half" }),
  } as RequestInit);
}

/** Relay a `/v1/*` request to Hermes and return its response. */
export async function proxyToHermes(
  request: Request,
  upstream: string | UpstreamRoutes
): Promise<Response> {
  const origin =
    typeof upstream === "string"
      ? upstream
      : upstreamFor(new URL(request.url).pathname, upstream);
  const response = await fetch(buildProxyRequest(request, origin));

  // Rebuild rather than returning the upstream response directly, so the body streams while any
  // connection-specific headers are dropped.
  const headers = new Headers(response.headers);
  headers.delete("transfer-encoding");
  headers.delete("connection");

  return new Response(response.body, {
    status: response.status,
    statusText: response.statusText,
    headers,
  });
}
