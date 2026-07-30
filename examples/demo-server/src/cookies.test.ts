// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { describe, expect, it } from "vitest";
import { readIdentityCookie, serializeIdentityCookie, signValue } from "./cookies.js";

/**
 * The demo needs *some* server-side notion of "who is logged in", because the whole point of
 * the token endpoint is that identity comes from the server rather than from the request. A
 * signed cookie is the smallest thing that demonstrates that honestly.
 *
 * It is deliberately not a real session: no expiry, no revocation, no rotation. The comment in
 * cookies.ts says so, and these tests pin the one property that actually matters — a tampered
 * cookie is rejected, so a demo visitor cannot mint a token for an arbitrary organization by
 * editing a cookie in devtools.
 */

const SECRET = "demo-cookie-secret";
const IDENTITY = {
  organizationId: "3f4c1f52-0f8e-4a1c-9c1e-7c1f2b9a0d11",
  externalUserId: "demo-user-1",
};

describe("identity cookie round trip", () => {
  it("reads back what it wrote", () => {
    const header = serializeIdentityCookie(IDENTITY, SECRET);
    const cookie = header.split(";")[0];

    expect(readIdentityCookie(cookie, SECRET)).toEqual(IDENTITY);
  });

  it("survives values needing escaping", () => {
    const identity = { ...IDENTITY, externalUserId: "user with spaces=and;semicolons" };
    const header = serializeIdentityCookie(identity, SECRET);

    expect(readIdentityCookie(header.split(";")[0], SECRET)).toEqual(identity);
  });

  it("finds its cookie among others", () => {
    const header = serializeIdentityCookie(IDENTITY, SECRET);
    const mine = header.split(";")[0];

    expect(readIdentityCookie(`other=1; ${mine}; another=2`, SECRET)).toEqual(IDENTITY);
  });
});

describe("identity cookie hardening", () => {
  it("rejects a cookie signed with a different secret", () => {
    const header = serializeIdentityCookie(IDENTITY, "another-secret");
    expect(readIdentityCookie(header.split(";")[0], SECRET)).toBeNull();
  });

  it("rejects a tampered payload", () => {
    // The attack this prevents: edit the organization id in devtools and have the server mint a
    // token for someone else's organization.
    const header = serializeIdentityCookie(IDENTITY, SECRET);
    const cookie = header.split(";")[0];
    const [name, value] = cookie.split("=");
    const [payload, signature] = decodeURIComponent(value).split(".");
    const tampered = Buffer.from(
      JSON.stringify({ ...IDENTITY, organizationId: "someone-elses-org" })
    ).toString("base64url");

    const forged = `${name}=${encodeURIComponent(`${tampered}.${signature}`)}`;

    expect(readIdentityCookie(forged, SECRET)).toBeNull();
    expect(payload).not.toBe(tampered);
  });

  it.each([
    { name: "an absent header", header: undefined },
    { name: "an empty header", header: "" },
    { name: "a header without our cookie", header: "other=1" },
    { name: "a value with no signature", header: "hermes_demo_identity=justpayload" },
    { name: "a value that is not base64", header: "hermes_demo_identity=%21%21%21.sig" },
    { name: "a payload that is not JSON", header: `hermes_demo_identity=${Buffer.from("nope").toString("base64url")}.sig` },
  ])("returns null for $name", ({ header }) => {
    expect(readIdentityCookie(header, SECRET)).toBeNull();
  });

  it("rejects a payload missing a required field", () => {
    const payload = Buffer.from(JSON.stringify({ organizationId: "o" })).toString("base64url");
    const value = `${payload}.${signValue(payload, SECRET)}`;

    expect(readIdentityCookie(`hermes_demo_identity=${encodeURIComponent(value)}`, SECRET)).toBeNull();
  });
});

describe("cookie attributes", () => {
  it("is HttpOnly, so page scripts cannot read the identity", () => {
    expect(serializeIdentityCookie(IDENTITY, SECRET)).toContain("HttpOnly");
  });

  it("is SameSite=Lax and path-scoped to the whole app", () => {
    const header = serializeIdentityCookie(IDENTITY, SECRET);
    expect(header).toContain("SameSite=Lax");
    expect(header).toContain("Path=/");
  });
});
