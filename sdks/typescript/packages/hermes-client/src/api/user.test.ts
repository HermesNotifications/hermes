// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { describe, expect, it } from "vitest";
import { HermesError } from "../errors.js";
import { UserAPI } from "./user.js";

function api(responder: (request: Request) => Response) {
  const requests: Request[] = [];
  const fetchImpl = async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = input instanceof Request ? input : new Request(input, init);
    requests.push(request);
    return responder(request);
  };
  const user = new UserAPI("http://localhost:8888", () => "tok", {
    fetch: fetchImpl as typeof fetch,
  });
  return { user, requests };
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

const profile = {
  id: "usr_1",
  organization_id: "org_1",
  external_id: "demo-user-1",
  created_at: "2026-07-29T09:00:00.000Z",
};

describe("UserAPI: profile", () => {
  it("fetches the current user", async () => {
    const { user, requests } = api(() => json(profile));
    await expect(user.getProfile()).resolves.toMatchObject({ id: "usr_1" });
    expect(new URL(requests[0].url).pathname).toBe("/v1/users/me");
  });

  it("sends contacts nested under a contacts key", async () => {
    // The handler takes {"contacts": {...}}, not a flat body. The integration guide
    // documented the flat shape, which would 400 — this test pins the real one.
    const { user, requests } = api(() => json(profile));
    await user.updateContacts({ email: "a@example.com" });
    expect(await requests[0].json()).toEqual({ contacts: { email: "a@example.com" } });
    expect(requests[0].method).toBe("PUT");
  });
});

describe("UserAPI: preference centre", () => {
  it("unwraps the categories array", async () => {
    const { user } = api(() =>
      json({
        categories: [
          {
            id: "sct_1",
            slug: "general",
            name: "General",
            default_channels: ["inbox"],
            default_state: "on",
            subscriptions: [],
          },
        ],
      })
    );
    const categories = await user.getPreferenceCenter();
    expect(categories.map((c) => c.slug)).toEqual(["general"]);
  });

  it("returns an empty array when the body carries no categories", async () => {
    const { user } = api(() => json({}));
    await expect(user.getPreferenceCenter()).resolves.toEqual([]);
  });

  it("sends the opt-in flag when setting a preference", async () => {
    const { user, requests } = api(() => json({ status: "ok" }));
    await user.setPreference("sub_default_general", false);
    expect(new URL(requests[0].url).pathname).toBe(
      "/v1/users/me/preferences/sub_default_general"
    );
    expect(await requests[0].json()).toEqual({ opted_in: false });
  });

  it("deletes a preference to revert it to the category default", async () => {
    const { user, requests } = api(() => json({ status: "ok" }));
    await user.deletePreference("sub_default_general");
    expect(requests[0].method).toBe("DELETE");
  });
});

describe("UserAPI: errors", () => {
  it.each([
    { status: 401, kind: "unauthorized" },
    // A required category cannot be opted out of; the server answers 403.
    { status: 403, kind: "forbidden" },
    { status: 404, kind: "not-found" },
    { status: 500, kind: "server" },
  ])("classifies a $status as $kind", async ({ status, kind }) => {
    const { user } = api(() => json({ detail: "nope" }, status));
    await expect(user.getProfile()).rejects.toMatchObject({ kind, status });
  });

  it("rejects with a HermesError naming the user surface", async () => {
    const { user } = api(() => json({ detail: "nope" }, 500));
    await expect(user.getProfile()).rejects.toBeInstanceOf(HermesError);
    await expect(user.getProfile()).rejects.toThrow(/User/);
  });
});
