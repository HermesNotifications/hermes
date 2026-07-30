// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { expect, openPanel, rows, test } from "../fixtures/demo.js";

/**
 * Pagination, which until this work did not exist: the widget captured the cursor from every
 * response and never read it, so an inbox was permanently capped at its first page.
 *
 * Seeding 25 notifications is the slowest thing in the suite, so it happens once for the whole file
 * rather than per test.
 */
const SEED_COUNT = 25;
const PAGE_SIZE = 20;

test.describe("pagination", () => {
  // Deliberately one test rather than two. `hermesUser` and `demoPage` are per-test fixtures, so a
  // second test would get a fresh user with an empty inbox — `mode: "serial"` orders tests, it does
  // not share their fixtures. Seeding 25 notifications through the real pipeline is also the slowest
  // thing in the suite, so doing it once is worth more than the finer-grained reporting.
  test("pages through a seeded inbox with a cursor, without duplicating rows", async ({
    demoPage,
    hermesUser,
  }) => {
    for (let index = 0; index < SEED_COUNT; index++) {
      await hermesUser.send({ title: `Seeded ${index + 1}`, body: `row ${index + 1}` });
    }
    // /v1/send returns 202 before dispatch creates the row, so wait for the server to agree that
    // all 25 exist before asserting anything about pages.
    await hermesUser.waitFor((inbox) => inbox.unread_count === SEED_COUNT, {
      timeoutMs: 90_000,
    });

    await demoPage.reload();
    await openPanel(demoPage);

    await expect(rows(demoPage)).toHaveCount(PAGE_SIZE);

    const loadMore = demoPage.getByRole("button", { name: "Load more" });
    await expect(loadMore).toBeVisible();

    const nextPage = demoPage.waitForRequest(
      (request) =>
        request.url().includes("/v1/inbox") && new URL(request.url()).searchParams.has("cursor")
    );
    await loadMore.click();
    // The cursor must actually be sent; a "load more" that refetched page one would still grow the
    // list in some implementations.
    await nextPage;

    await expect(rows(demoPage)).toHaveCount(SEED_COUNT);
    await expect(loadMore).toHaveCount(0);

    // The classic cursor regression: an overlapping page appends rows already on screen.
    const titles = await rows(demoPage).allInnerTexts();
    expect(new Set(titles).size).toBe(titles.length);
  });
});
