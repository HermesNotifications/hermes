// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { describe, expect, it } from "vitest";
import { createSession, type TokenMinter } from "./session.js";

/**
 * The session endpoint is the piece an integrator copies most directly, so what it teaches
 * matters as much as what it does.
 *
 * Two properties are load-bearing:
 *
 * 1. **The API key never leaves this process.** Minting happens here; the browser receives only
 *    a short-lived user JWT.
 * 2. **The response carries the decoded `sub`.** Centrifugo channels are `user#<sub>`, where
 *    `sub` is the *internal* Hermes id — not the external id the caller passed in. Getting that
 *    wrong fails silently in the worst way: REST keeps working so the inbox loads, while the
 *    subscription is rejected and no update ever arrives. Returning it means no integrator has
 *    to discover that.
 */

const SUB = "usr_2xKp9Qm";

/** An unsigned token carrying `sub`, shaped like what the mint returns. */
function tokenFor(sub: string): string {
  const encode = (value: unknown) => {
    const bytes = new TextEncoder().encode(JSON.stringify(value));
    return btoa(String.fromCharCode(...bytes))
      .replace(/\+/g, "-")
      .replace(/\//g, "_")
      .replace(/=+$/, "");
  };
  return `${encode({ alg: "HS256" })}.${encode({ sub, organization_id: "org" })}.sig`;
}

/** A stand-in for the admin SDK's auth service, typed against the real signature. */
function minter(
  token = tokenFor(SUB),
  expiresAt = "2026-07-29T14:00:00.000Z"
): TokenMinter & { calls: Array<{ userId: string; organizationId: string }> } {
  const calls: Array<{ userId: string; organizationId: string }> = [];
  return {
    calls,
    exchangeToken: async (options) => {
      calls.push(options);
      return { token, expiresAt };
    },
  };
}

const IDENTITY = {
  organizationId: "3f4c1f52-0f8e-4a1c-9c1e-7c1f2b9a0d11",
  externalUserId: "demo-user-1",
};

const SOCKET_URL = "http://localhost:8888/realtime";

describe("createSession", () => {
  it("mints a token for the identity it was given", async () => {
    const hermes = minter();

    await createSession(hermes, IDENTITY, { socketUrl: SOCKET_URL });

    expect(hermes.calls).toEqual([
      { userId: "demo-user-1", organizationId: IDENTITY.organizationId },
    ]);
  });

  it("returns the internal user id decoded from the token", async () => {
    const session = await createSession(minter(), IDENTITY, { socketUrl: SOCKET_URL });
    expect(session.hermesUserId).toBe(SUB);
  });

  it("returns the token and its expiry for the browser to schedule a refresh", async () => {
    const session = await createSession(minter(), IDENTITY, { socketUrl: SOCKET_URL });
    expect(session.token).toBe(tokenFor(SUB));
    expect(session.expiresAt).toBe("2026-07-29T14:00:00.000Z");
  });

  it("echoes the identity so the demo UI can display who it is acting as", async () => {
    const session = await createSession(minter(), IDENTITY, { socketUrl: SOCKET_URL });
    expect(session.externalUserId).toBe("demo-user-1");
    expect(session.organizationId).toBe(IDENTITY.organizationId);
  });

  it("tells the browser where the websocket is", async () => {
    // Centrifugo is a different origin from the API in every real deployment, and websockets
    // are not subject to CORS, so the browser talks to it directly rather than through the
    // proxy. It has to be told where.
    const session = await createSession(minter(), IDENTITY, { socketUrl: SOCKET_URL });
    expect(session.socketUrl).toBe(SOCKET_URL);
  });

  it("never includes anything resembling a credential", async () => {
    // A guard against someone helpfully adding the API key to the payload for debugging.
    const session = await createSession(minter(), IDENTITY, { socketUrl: SOCKET_URL });
    expect(Object.keys(session).sort()).toEqual([
      "expiresAt",
      "externalUserId",
      "hermesUserId",
      "organizationId",
      "socketUrl",
      "token",
    ]);
  });

  it("fails loudly when the minted token carries no subject", async () => {
    // Better a 500 the operator can see than a browser that subscribes to `user#undefined` and
    // silently receives nothing.
    const hermes = minter("not-a-jwt");

    await expect(
      createSession(hermes, IDENTITY, { socketUrl: SOCKET_URL })
    ).rejects.toThrow(/sub/i);
  });

  it("propagates a mint failure rather than returning a broken session", async () => {
    const hermes: TokenMinter = {
      exchangeToken: async () => {
        throw new Error("401 from admin API");
      },
    };

    await expect(
      createSession(hermes, IDENTITY, { socketUrl: SOCKET_URL })
    ).rejects.toThrow(/401/);
  });
});
