// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

/**
 * Decode a base64url segment to a UTF-8 string.
 *
 * `atob` alone returns latin-1, which mangles any multi-byte character, so the bytes are
 * run through TextDecoder. Padding is restored because JWT segments omit it.
 */
function decodeSegment(segment: string): string {
  const base64 = segment.replace(/-/g, "+").replace(/_/g, "/");
  const padded = base64.padEnd(Math.ceil(base64.length / 4) * 4, "=");
  const binary = atob(padded);
  const bytes = Uint8Array.from(binary, (character) => character.charCodeAt(0));
  return new TextDecoder().decode(bytes);
}

/**
 * Read the `sub` claim — the internal Hermes user id — from a JWT.
 *
 * This is a parser, not a verifier: it does not check the signature, because the client
 * has no secret to check it with and the token was minted by a trusted backend. Its only
 * job is to save consumers from hand-decoding a JWT to discover the id that Centrifugo's
 * `user#<sub>` channel requires.
 *
 * Returns `undefined` for anything unparseable rather than throwing, so a malformed token
 * degrades to "no realtime" instead of taking down the caller.
 */
export function subjectFromToken(token: string): string | undefined {
  const segments = token.split(".");
  if (segments.length !== 3) return undefined;

  try {
    const payload = JSON.parse(decodeSegment(segments[1])) as unknown;
    if (typeof payload !== "object" || payload === null) return undefined;
    const sub = (payload as { sub?: unknown }).sub;
    return typeof sub === "string" ? sub : undefined;
  } catch {
    return undefined;
  }
}
