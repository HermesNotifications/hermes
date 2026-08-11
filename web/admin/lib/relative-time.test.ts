// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { relativeTime } from "./relative-time";

// relativeTime reads Date.now(), so every case is pinned to a fixed clock. NOW is
// arbitrary but must be far enough from the epoch that subtracting a year stays
// positive.
const NOW = new Date("2026-07-28T12:00:00.000Z");

const SECOND = 1000;
const MINUTE = 60 * SECOND;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/** An ISO timestamp `ms` milliseconds before NOW. */
function ago(ms: number): string {
  return new Date(NOW.getTime() - ms).toISOString();
}

describe("relativeTime", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it.each([
    { name: "reports the present as 'just now'", ms: 0, want: "just now" },
    // The three boundaries below are the ones the implementation's `<` comparisons
    // sit on. One second either side of each is what an off-by-one would move.
    { name: "still says 'just now' one second before a minute", ms: 59 * SECOND, want: "just now" },
    { name: "switches to minutes at exactly one minute", ms: MINUTE, want: "1m ago" },
    { name: "stays in minutes one minute before an hour", ms: 59 * MINUTE, want: "59m ago" },
    { name: "switches to hours at exactly one hour", ms: HOUR, want: "1h ago" },
    { name: "stays in hours one hour before a day", ms: 23 * HOUR, want: "23h ago" },
    { name: "switches to days at exactly one day", ms: DAY, want: "1d ago" },
    { name: "stays in days on the last day before a month", ms: 29 * DAY, want: "29d ago" },
    { name: "switches to months at exactly thirty days", ms: 30 * DAY, want: "1mo ago" },

    // Truncation, not rounding: 119 minutes is "1h ago", never "2h ago".
    { name: "truncates a partial hour downwards", ms: 119 * MINUTE, want: "1h ago" },
    { name: "truncates a partial day downwards", ms: 47 * HOUR, want: "1d ago" },

    // Months are fixed 30-day buckets, so a calendar year reports as 12mo.
    { name: "counts a year as twelve thirty-day months", ms: 365 * DAY, want: "12mo ago" },
  ])("$name", ({ ms, want }) => {
    expect(relativeTime(ago(ms))).toBe(want);
  });

  it("treats a future timestamp as 'just now' rather than going negative", () => {
    expect(relativeTime(new Date(NOW.getTime() + HOUR).toISOString())).toBe("just now");
  });
});
