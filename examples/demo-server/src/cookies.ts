// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { createHmac, timingSafeEqual } from "node:crypto";
import type { Identity } from "./session.js";

const COOKIE_NAME = "hermes_demo_identity";

/**
 * A signed cookie standing in for the host application's real session.
 *
 * **This is demo scaffolding, not a session implementation.** There is no expiry, no
 * revocation, and no rotation. Its only job is to make the demo structurally honest: the token
 * endpoint derives identity from the server side rather than from a request parameter, which is
 * the part an integrator must copy. In your own app, replace this entirely with whatever
 * session you already have.
 *
 * The signature is still real, because without it a visitor could edit the organization id in
 * devtools and have the server mint a token for someone else's organization.
 */
export function signValue(payload: string, secret: string): string {
  return createHmac("sha256", secret).update(payload).digest("base64url");
}

function signaturesMatch(a: string, b: string): boolean {
  const left = Buffer.from(a);
  const right = Buffer.from(b);
  // Length must be compared first: timingSafeEqual throws on a mismatch.
  return left.length === right.length && timingSafeEqual(left, right);
}

/** Build a `Set-Cookie` header carrying `identity`. */
export function serializeIdentityCookie(identity: Identity, secret: string): string {
  const payload = Buffer.from(JSON.stringify(identity)).toString("base64url");
  const value = encodeURIComponent(`${payload}.${signValue(payload, secret)}`);
  return `${COOKIE_NAME}=${value}; Path=/; HttpOnly; SameSite=Lax`;
}

/** A `Set-Cookie` header that clears the identity. */
export function clearIdentityCookie(): string {
  return `${COOKIE_NAME}=; Path=/; HttpOnly; SameSite=Lax; Max-Age=0`;
}

/** Read and verify the identity from a `Cookie` header, or null if absent or tampered with. */
export function readIdentityCookie(
  header: string | undefined,
  secret: string
): Identity | null {
  if (!header) return null;

  const raw = header
    .split(";")
    .map((part) => part.trim())
    .find((part) => part.startsWith(`${COOKIE_NAME}=`))
    ?.slice(COOKIE_NAME.length + 1);
  if (!raw) return null;

  const [payload, signature] = decodeURIComponent(raw).split(".");
  if (!payload || !signature) return null;
  if (!signaturesMatch(signature, signValue(payload, secret))) return null;

  try {
    const parsed = JSON.parse(Buffer.from(payload, "base64url").toString("utf8")) as unknown;
    if (typeof parsed !== "object" || parsed === null) return null;
    const { organizationId, externalUserId } = parsed as Partial<Identity>;
    if (typeof organizationId !== "string" || typeof externalUserId !== "string") return null;
    if (organizationId === "" || externalUserId === "") return null;
    return { organizationId, externalUserId };
  } catch {
    return null;
  }
}
