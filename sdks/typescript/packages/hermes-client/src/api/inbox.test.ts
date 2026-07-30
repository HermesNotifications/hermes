// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { describe, expect, it } from "vitest";
import { HermesError } from "../errors.js";
import { InboxAPI } from "./inbox.js";

/**
 * A fetch stand-in. `openapi-fetch` takes a `fetch` in its options, which is the seam
 * these tests use — no network, no module mocking.
 */
function fakeFetch(
  responder: (request: Request) => Response | Promise<Response> | never
): { fetch: typeof fetch; requests: Request[] } {
  const requests: Request[] = [];
  const fetchImpl = async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = input instanceof Request ? input : new Request(input, init);
    requests.push(request);
    return await responder(request);
  };
  return { fetch: fetchImpl as typeof fetch, requests };
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function api(
  responder: (request: Request) => Response | Promise<Response>,
  options?: { token?: () => string; onUnauthorized?: () => Promise<void> }
) {
  const { fetch, requests } = fakeFetch(responder);
  const inbox = new InboxAPI("http://localhost:8888", options?.token ?? (() => "tok"), {
    fetch,
    onUnauthorized: options?.onUnauthorized,
  });
  return { inbox, requests };
}

const emptyPage = { data: [], unread_count: 0 };

describe("InboxAPI: requests", () => {
  it("sends the bearer token", async () => {
    const { inbox, requests } = api(() => json(emptyPage));
    await inbox.list();
    expect(requests[0].headers.get("Authorization")).toBe("Bearer tok");
  });

  it("reads the token at call time, not at construction", async () => {
    // The widget re-sets its token when one is refreshed; if the header were captured in
    // the constructor every later request would carry the expired one.
    let token = "first";
    const { inbox, requests } = api(() => json(emptyPage), { token: () => token });
    await inbox.list();
    token = "second";
    await inbox.list();
    expect(requests[1].headers.get("Authorization")).toBe("Bearer second");
  });

  it("passes pagination and filter options as query parameters", async () => {
    const { inbox, requests } = api(() => json(emptyPage));
    await inbox.list({ limit: 20, cursor: "c1", archived: true });
    const url = new URL(requests[0].url);
    expect(url.pathname).toBe("/v1/inbox");
    expect(url.searchParams.get("limit")).toBe("20");
    expect(url.searchParams.get("cursor")).toBe("c1");
    expect(url.searchParams.get("archived")).toBe("true");
  });

  it.each([
    { name: "markRead", call: (i: InboxAPI) => i.markRead("n1"), method: "PUT", path: "/v1/inbox/n1/read" },
    { name: "markUnread", call: (i: InboxAPI) => i.markUnread("n1"), method: "DELETE", path: "/v1/inbox/n1/read" },
    { name: "archive", call: (i: InboxAPI) => i.archive("n1"), method: "PUT", path: "/v1/inbox/n1/archive" },
    { name: "unarchive", call: (i: InboxAPI) => i.unarchive("n1"), method: "DELETE", path: "/v1/inbox/n1/archive" },
    { name: "delete", call: (i: InboxAPI) => i.delete("n1"), method: "DELETE", path: "/v1/inbox/n1" },
    { name: "markAllRead", call: (i: InboxAPI) => i.markAllRead(), method: "PUT", path: "/v1/inbox/read-all" },
  ])("$name issues $method $path", async ({ call, method, path }) => {
    const { inbox, requests } = api(() => json({ status: "ok" }));
    await call(inbox);
    expect(requests[0].method).toBe(method);
    expect(new URL(requests[0].url).pathname).toBe(path);
  });
});

describe("InboxAPI: response mapping", () => {
  it("maps the wire body onto InboxPage", async () => {
    const { inbox } = api(() =>
      json({
        data: [
          {
            id: "n1",
            organization_id: "o",
            user_id: "u",
            category_id: "c",
            title: "T",
            body: "B",
            status: "delivered",
            channels: ["inbox"],
            created_at: "2026-07-29T09:00:00.000Z",
          },
        ],
        unread_count: 3,
        cursor: "c1",
      })
    );
    const result = await inbox.list();
    expect(result.data).toHaveLength(1);
    expect(result.unreadCount).toBe(3);
    expect(result.cursor).toBe("c1");
  });

  it("normalises the empty-string last-page cursor to undefined", async () => {
    // The API sends `cursor: ""` on the last page. hasMore in the reducer keys off
    // undefined, so this normalisation is what makes "no further pages" detectable.
    const { inbox } = api(() => json({ data: [], unread_count: 0, cursor: "" }));
    expect((await inbox.list()).cursor).toBeUndefined();
  });

  it("turns a null data array into an empty one", async () => {
    // The generated type is `Notification[] | null`.
    const { inbox } = api(() => json({ data: null, unread_count: 0 }));
    expect((await inbox.list()).data).toEqual([]);
  });
});

describe("InboxAPI: error classification", () => {
  it.each([
    { status: 401, kind: "unauthorized" },
    { status: 403, kind: "forbidden" },
    { status: 404, kind: "not-found" },
    { status: 429, kind: "rate-limited" },
    { status: 500, kind: "server" },
    { status: 503, kind: "server" },
  ])("rejects a $status with kind $kind", async ({ status, kind }) => {
    const { inbox } = api(() => json({ detail: "nope" }, status));
    await expect(inbox.list()).rejects.toMatchObject({ kind, status });
  });

  it("rejects with a HermesError, not a bare Error", async () => {
    const { inbox } = api(() => json({ detail: "nope" }, 500));
    await expect(inbox.list()).rejects.toBeInstanceOf(HermesError);
  });

  it("classifies a rejected cursor so the store can recover from it", async () => {
    const { inbox } = api(() => json({ detail: "invalid cursor" }, 400));
    await expect(inbox.list({ cursor: "stale" })).rejects.toMatchObject({
      kind: "invalid-cursor",
    });
  });

  it("reports a transport failure as a network error", async () => {
    const { inbox } = api(() => {
      throw new TypeError("Failed to fetch");
    });
    await expect(inbox.list()).rejects.toMatchObject({ kind: "network", retryable: true });
  });

  it("classifies failures from the action endpoints too", async () => {
    const { inbox } = api(() => json({ detail: "nope" }, 401));
    await expect(inbox.markRead("n1")).rejects.toMatchObject({ kind: "unauthorized" });
  });
});

describe("InboxAPI: unauthorized recovery", () => {
  it("refreshes once and retries the request on a 401", async () => {
    let tokens = ["stale", "fresh"];
    let token = tokens[0];
    let calls = 0;
    const { inbox, requests } = api(
      () => {
        calls++;
        return calls === 1 ? json({ detail: "invalid token" }, 401) : json(emptyPage);
      },
      {
        token: () => token,
        onUnauthorized: async () => {
          token = tokens[1];
        },
      }
    );

    await expect(inbox.list()).resolves.toMatchObject({ unreadCount: 0 });
    expect(requests).toHaveLength(2);
    expect(requests[1].headers.get("Authorization")).toBe("Bearer fresh");
  });

  it("gives up after one retry rather than looping", async () => {
    // A permanently-rejected token must surface as an error, not spin.
    let calls = 0;
    const { inbox, requests } = api(
      () => {
        calls++;
        return json({ detail: "invalid token" }, 401);
      },
      { onUnauthorized: async () => {} }
    );

    await expect(inbox.list()).rejects.toMatchObject({ kind: "unauthorized" });
    expect(requests).toHaveLength(2);
    expect(calls).toBe(2);
  });

  it("does not retry when no refresh hook is configured", async () => {
    const { inbox, requests } = api(() => json({ detail: "invalid token" }, 401));
    await expect(inbox.list()).rejects.toMatchObject({ kind: "unauthorized" });
    expect(requests).toHaveLength(1);
  });

  it("does not retry a non-401", async () => {
    let calls = 0;
    const { inbox } = api(
      () => {
        calls++;
        return json({ detail: "boom" }, 500);
      },
      { onUnauthorized: async () => {} }
    );
    await expect(inbox.list()).rejects.toMatchObject({ kind: "server" });
    expect(calls).toBe(1);
  });
});
