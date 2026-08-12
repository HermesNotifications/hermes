// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { describe, expect, it } from "vitest";
import { HermesError, type HermesErrorKind } from "./errors.js";

/**
 * Before this module every API failure was `throw new Error("Inbox API error (401)")`,
 * so a caller could not tell "your token expired, refresh it" from "the server is
 * broken, retry later" without parsing the message. These tests pin the status -> kind
 * mapping that decision now rests on.
 */
describe("HermesError.fromStatus", () => {
  const cases: Array<{ status: number; kind: HermesErrorKind; retryable: boolean }> = [
    { status: 401, kind: "unauthorized", retryable: true },
    { status: 403, kind: "forbidden", retryable: false },
    { status: 404, kind: "not-found", retryable: false },
    { status: 429, kind: "rate-limited", retryable: true },
    { status: 500, kind: "server", retryable: true },
    { status: 502, kind: "server", retryable: true },
    { status: 503, kind: "server", retryable: true },
  ];

  it.each(cases)("maps $status to $kind (retryable: $retryable)", ({ status, kind, retryable }) => {
    const err = HermesError.fromStatus("Inbox", status);
    expect(err.kind).toBe(kind);
    expect(err.status).toBe(status);
    expect(err.retryable).toBe(retryable);
  });

  it("is an instance of Error and of HermesError", () => {
    const err = HermesError.fromStatus("Inbox", 500);
    expect(err).toBeInstanceOf(Error);
    expect(err).toBeInstanceOf(HermesError);
    expect(err.name).toBe("HermesError");
  });

  it("names the surface and the status in the message", () => {
    const err = HermesError.fromStatus("Inbox", 503);
    expect(err.message).toContain("Inbox");
    expect(err.message).toContain("503");
  });

  it("treats an unmapped 4xx as a non-retryable client error", () => {
    const err = HermesError.fromStatus("Inbox", 418);
    expect(err.kind).toBe("client");
    expect(err.retryable).toBe(false);
  });

  describe("Retry-After", () => {
    it("surfaces the wait the server asked for", () => {
      const headers = new Headers({ "Retry-After": "3" });
      const err = HermesError.fromStatus("Inbox", 429, undefined, headers);
      expect(err.kind).toBe("rate-limited");
      expect(err.retryAfterSeconds).toBe(3);
    });

    it("is undefined when the header is absent", () => {
      const err = HermesError.fromStatus("Inbox", 429, undefined, new Headers());
      expect(err.retryAfterSeconds).toBeUndefined();
    });

    it("is undefined when no headers are supplied at all", () => {
      expect(HermesError.fromStatus("Inbox", 429).retryAfterSeconds).toBeUndefined();
    });

    /**
     * The header also permits an HTTP date. Returning NaN would be worse than
     * returning nothing: `setTimeout(fn, NaN)` fires immediately, so a client
     * honouring it would retry instantly — the opposite of the instruction.
     */
    it("ignores a non-numeric value rather than yielding NaN", () => {
      const headers = new Headers({ "Retry-After": "Wed, 21 Oct 2026 07:28:00 GMT" });
      expect(
        HermesError.fromStatus("Inbox", 429, undefined, headers).retryAfterSeconds
      ).toBeUndefined();
    });
  });

  it("classifies a 400 whose body reports an invalid cursor", () => {
    // The store recovers from this by discarding the cursor and re-requesting page 1,
    // which it can only do if the kind is distinguishable from any other 400.
    const err = HermesError.fromStatus("Inbox", 400, { detail: "invalid cursor supplied" });
    expect(err.kind).toBe("invalid-cursor");
    expect(err.retryable).toBe(false);
  });

  it("leaves an unrelated 400 as a client error", () => {
    const err = HermesError.fromStatus("Inbox", 400, { detail: "limit must be <= 100" });
    expect(err.kind).toBe("client");
  });

  it("retains the response body for diagnostics", () => {
    const body = { detail: "limit must be <= 100" };
    expect(HermesError.fromStatus("Inbox", 400, body).body).toEqual(body);
  });
});

describe("HermesError.network", () => {
  it("is retryable and carries no status", () => {
    const err = HermesError.network("Inbox", new TypeError("Failed to fetch"));
    expect(err.kind).toBe("network");
    expect(err.retryable).toBe(true);
    expect(err.status).toBeUndefined();
  });

  it("keeps the underlying failure as the cause", () => {
    const cause = new TypeError("Failed to fetch");
    expect(HermesError.network("Inbox", cause).cause).toBe(cause);
  });
});
