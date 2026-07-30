// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { describe, expect, it } from "vitest";
import { subjectFromToken } from "./jwt.js";

/**
 * Why this exists: Centrifugo channels are named `user#<sub>`, where `sub` is the
 * *internal* Hermes user id carried in the JWT — not the external id a caller passed to
 * the token endpoint. Getting that wrong fails silently in the worst way: REST keeps
 * working, so the inbox loads normally, while Centrifugo rejects the subscription and no
 * update ever arrives. Decoding it here means a consumer never has to guess.
 *
 * This deliberately does NOT verify the signature. The token came from a trusted mint
 * over HTTPS and the client has no secret to verify it with, so a verification step here
 * would be theatre. It is a parser, not an authenticator — hence the name.
 */

/**
 * Build an unsigned token whose payload is `claims`.
 *
 * The JSON is UTF-8 encoded before base64, which is what a real signer does — `btoa` on
 * a raw JS string throws on anything outside latin-1, so encoding here is not incidental
 * to the multi-byte case below, it is the case.
 */
function base64url(value: unknown): string {
  const bytes = new TextEncoder().encode(JSON.stringify(value));
  const binary = String.fromCharCode(...bytes);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function token(claims: Record<string, unknown>): string {
  return `${base64url({ alg: "HS256", typ: "JWT" })}.${base64url(claims)}.signature`;
}

describe("subjectFromToken", () => {
  it("reads sub from the payload", () => {
    expect(subjectFromToken(token({ sub: "usr_2abc", organization_id: "org" }))).toBe("usr_2abc");
  });

  it("decodes base64url without padding", () => {
    // A payload whose base64 length is not a multiple of four is the common case, and
    // atob rejects it unless the padding is restored.
    const value = subjectFromToken(token({ sub: "a" }));
    expect(value).toBe("a");
  });

  it.each([
    { name: "an empty string", input: "" },
    { name: "a token with no dots", input: "notatoken" },
    { name: "a token with only one segment separator", input: "header.payload" },
    { name: "a payload that is not base64", input: "header.!!!!.sig" },
    { name: "a payload that is not JSON", input: `header.${btoa("nope")}.sig` },
  ])("returns undefined for $name", ({ input }) => {
    expect(subjectFromToken(input)).toBeUndefined();
  });

  it("returns undefined when the payload has no sub", () => {
    expect(subjectFromToken(token({ organization_id: "org" }))).toBeUndefined();
  });

  it("returns undefined when sub is not a string", () => {
    expect(subjectFromToken(token({ sub: 42 }))).toBeUndefined();
  });

  it("handles a payload containing multi-byte characters", () => {
    // Naive atob-only decoding mangles anything outside latin-1, which would corrupt
    // the channel name for a token carrying non-ASCII claims.
    expect(subjectFromToken(token({ sub: "usr_1", name: "Ådne Ωmega 😀" }))).toBe("usr_1");
  });
});
