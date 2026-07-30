// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { describe, expect, it } from "vitest";
import { HermesError } from "../errors.js";
import { createSender } from "./request.js";

/**
 * The retry-and-classify policy shared by InboxAPI and UserAPI. It lives here rather
 * than in each API class because a second copy is how the two drift — the same reason
 * the inbox reducer was hoisted out of the widget and the hooks.
 */

function ok<T>(data: T) {
  return { data, response: new Response(null, { status: 200 }) };
}

function fail(status: number, error: unknown = { detail: "nope" }) {
  return { error, response: new Response(null, { status }) };
}

describe("createSender", () => {
  it("returns the result untouched on success", async () => {
    const send = createSender("Inbox");
    await expect(send(async () => ok({ value: 1 }))).resolves.toMatchObject({
      data: { value: 1 },
    });
  });

  it("classifies a failure by status, naming the surface", async () => {
    const send = createSender("User");
    await expect(send(async () => fail(403))).rejects.toMatchObject({
      kind: "forbidden",
      status: 403,
    });
    await expect(send(async () => fail(403))).rejects.toThrow(/User/);
  });

  it("wraps a thrown transport failure as a network error", async () => {
    const send = createSender("Inbox");
    await expect(
      send(async () => {
        throw new TypeError("Failed to fetch");
      })
    ).rejects.toMatchObject({ kind: "network" });
  });

  it("rejects with HermesError in every failure mode", async () => {
    const send = createSender("Inbox");
    await expect(send(async () => fail(500))).rejects.toBeInstanceOf(HermesError);
  });

  it("refreshes and retries exactly once on a 401", async () => {
    let attempts = 0;
    let refreshes = 0;
    const send = createSender("Inbox", async () => {
      refreshes++;
    });

    const result = await send(async () => {
      attempts++;
      return attempts === 1 ? fail(401) : ok({ value: 2 });
    });

    expect(result).toMatchObject({ data: { value: 2 } });
    expect(attempts).toBe(2);
    expect(refreshes).toBe(1);
  });

  it("stops after one retry when the 401 persists", async () => {
    let attempts = 0;
    const send = createSender("Inbox", async () => {});

    await expect(
      send(async () => {
        attempts++;
        return fail(401);
      })
    ).rejects.toMatchObject({ kind: "unauthorized" });
    expect(attempts).toBe(2);
  });

  it("does not retry when no refresh hook was supplied", async () => {
    let attempts = 0;
    const send = createSender("Inbox");

    await expect(
      send(async () => {
        attempts++;
        return fail(401);
      })
    ).rejects.toMatchObject({ kind: "unauthorized" });
    expect(attempts).toBe(1);
  });

  it("does not retry a status other than 401", async () => {
    let attempts = 0;
    const send = createSender("Inbox", async () => {});

    await expect(
      send(async () => {
        attempts++;
        return fail(500);
      })
    ).rejects.toMatchObject({ kind: "server" });
    expect(attempts).toBe(1);
  });

  it("surfaces a failure thrown by the refresh hook itself", async () => {
    // If refreshing the token fails, that is the error worth reporting — not the 401
    // that prompted it.
    const send = createSender("Inbox", async () => {
      throw new Error("token endpoint unreachable");
    });

    await expect(send(async () => fail(401))).rejects.toThrow(/token endpoint unreachable/);
  });
});
