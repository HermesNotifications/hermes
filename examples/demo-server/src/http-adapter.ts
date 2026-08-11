// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import type { IncomingMessage, ServerResponse } from "node:http";

/**
 * Convert a node request into a WHATWG `Request`.
 *
 * The handler is written against the standard types so it can be tested without a socket; this
 * is the adapter that makes that possible.
 */
export async function toRequest(
  incoming: IncomingMessage,
  fallbackPort: number
): Promise<Request> {
  const host = incoming.headers.host ?? `localhost:${fallbackPort}`;
  const url = `http://${host}${incoming.url ?? "/"}`;

  const headers = new Headers();
  for (const [name, value] of Object.entries(incoming.headers)) {
    if (Array.isArray(value)) for (const item of value) headers.append(name, item);
    else if (value !== undefined) headers.set(name, value);
  }

  const method = incoming.method ?? "GET";
  // Constructing a Request with a body on GET or HEAD throws, so this branch is required rather
  // than merely an optimisation.
  if (method === "GET" || method === "HEAD") return new Request(url, { method, headers });

  const chunks: Buffer[] = [];
  for await (const chunk of incoming) chunks.push(chunk as Buffer);
  return new Request(url, { method, headers, body: Buffer.concat(chunks) });
}

/** Write a WHATWG `Response` out through a node response. */
export async function writeResponse(
  response: Response,
  outgoing: ServerResponse
): Promise<void> {
  const headers: Record<string, string | string[]> = {};

  response.headers.forEach((value, name) => {
    if (name.toLowerCase() === "set-cookie") {
      // Set-Cookie has list semantics. Flattening it to a comma-joined string produces a header
      // browsers reject, which would break the demo's entire identity mechanism.
      const existing = headers[name];
      headers[name] = Array.isArray(existing) ? [...existing, value] : [value];
    } else {
      headers[name] = value;
    }
  });

  outgoing.writeHead(response.status, headers);
  const body = response.body ? Buffer.from(await response.arrayBuffer()) : undefined;
  outgoing.end(body);
}
