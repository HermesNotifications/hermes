// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { describe, expect, it } from "vitest";
import { refreshDelayMs, REFRESH_MARGIN_MS } from "./session.js";

/**
 * Token refresh scheduling, extracted from the component so it can be asserted directly.
 *
 * This matters more than it looks. Tokens are minted with a multi-hour TTL plus jitter, and the
 * admin API refuses `expires_in` below 3600 — so there is no way to make a token expire quickly
 * enough to test refresh by waiting. If the arithmetic here is wrong, nothing catches it until a
 * real session has been open for hours and the inbox starts returning 401.
 */

const NOW = new Date("2026-07-29T10:00:00.000Z").getTime();
const MINUTE = 60_000;
const HOUR = 60 * MINUTE;

function at(offsetMs: number): string {
  return new Date(NOW + offsetMs).toISOString();
}

describe("refreshDelayMs", () => {
  it("schedules a refresh one margin before expiry", () => {
    expect(refreshDelayMs(at(4 * HOUR), NOW)).toBe(4 * HOUR - REFRESH_MARGIN_MS);
  });

  it("uses a margin generous enough to survive a slow round trip", () => {
    // Refreshing one second before expiry would race the request itself.
    expect(REFRESH_MARGIN_MS).toBeGreaterThanOrEqual(60_000);
  });

  it("refreshes immediately-ish when the token is already inside the margin", () => {
    // Clamped rather than negative: a negative delay would make setTimeout fire in a tight loop.
    const delay = refreshDelayMs(at(10_000), NOW);
    expect(delay).toBeGreaterThan(0);
    expect(delay).toBeLessThanOrEqual(REFRESH_MARGIN_MS);
  });

  it("still returns a positive delay for an already-expired token", () => {
    expect(refreshDelayMs(at(-HOUR), NOW)).toBeGreaterThan(0);
  });

  it.each([
    { name: "an empty string", value: "" },
    { name: "an unparseable date", value: "not a date" },
    { name: "an absent value", value: undefined },
  ])("falls back to a fixed interval for $name", ({ value }) => {
    // A missing expiry must not disable refresh entirely; the session would then die silently.
    const delay = refreshDelayMs(value, NOW);
    expect(delay).toBeGreaterThan(0);
    expect(Number.isFinite(delay)).toBe(true);
  });

  it("never returns a delay a browser timer would overflow", () => {
    // setTimeout treats anything *above* 2^31-1 as zero and fires immediately, which would spin.
    // 2^31-1 itself is the largest valid delay, so the bound is inclusive.
    expect(refreshDelayMs(at(400 * 24 * HOUR), NOW)).toBeLessThanOrEqual(2 ** 31 - 1);
  });
});
