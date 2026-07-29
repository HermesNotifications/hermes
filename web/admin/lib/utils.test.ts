// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { describe, expect, it } from "vitest";
import { slugify } from "./utils";

describe("slugify", () => {
  it.each([
    { name: "lowercases and joins words with a hyphen", input: "Order Updates", want: "order-updates" },
    { name: "leaves an existing slug untouched", input: "order-updates", want: "order-updates" },
    { name: "collapses a run of spaces into one hyphen", input: "Order   Updates", want: "order-updates" },
    { name: "collapses a run of hyphens into one", input: "order---updates", want: "order-updates" },
    { name: "trims leading and trailing whitespace", input: "  Order Updates  ", want: "order-updates" },
    { name: "trims leading and trailing hyphens", input: "-order-updates-", want: "order-updates" },
    { name: "drops punctuation rather than encoding it", input: "Order & Shipping!", want: "order-shipping" },
    { name: "keeps digits", input: "Tier 2 Alerts", want: "tier-2-alerts" },
    { name: "normalises tabs and newlines like spaces", input: "Order\tUpdates\nDaily", want: "order-updates-daily" },
    { name: "returns empty for input with nothing sluggable", input: "!!!", want: "" },
    { name: "returns empty for hyphens alone", input: "---", want: "" },
    { name: "returns empty for the empty string", input: "", want: "" },

    // Characterisation, not endorsement: the allowlist is ASCII-only, so accented
    // letters are dropped outright rather than transliterated. "Ünïcödé" becoming
    // "ncd" is a silent near-collision risk if categories are ever named in a
    // non-English language. Pinned here so the behaviour cannot change unnoticed.
    { name: "drops non-ascii letters instead of transliterating them", input: "Ünïcödé", want: "ncd" },
  ])("$name", ({ input, want }) => {
    expect(slugify(input)).toBe(want);
  });
});
