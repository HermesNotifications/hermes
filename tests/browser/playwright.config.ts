// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { defineConfig, devices } from "@playwright/test";

/**
 * Live end-to-end configuration.
 *
 * ## What this suite assumes, and what it owns
 *
 * It **assumes** a running `make dev-up`: the k3d cluster, ingress, Centrifugo, Postgres, NATS,
 * Redis and the Go services. `global-setup.ts` probes for those and fails with a message naming the
 * fix, rather than letting an absent cluster look like a widget bug.
 *
 * It **owns** the two processes it can cheaply own — the demo server and the Vite dev server —
 * through `webServer` below. Orchestrating k3d and Tilt from a test hook was considered and
 * rejected: a multi-minute cluster bootstrap inside `globalSetup` is unloggable and conflates
 * "the environment is broken" with "the code is broken". `make demo-e2e-full` composes them at the
 * shell level instead, where that belongs.
 *
 * ## Why Playwright rather than vitest browser mode
 *
 * This is a two-actor scenario: the test must act *server-side* with the API key (mint a token,
 * send a notification) and *observe* in a browser. Vitest browser mode runs the test code inside
 * the page, which would put the API key in browser-side code — precisely what the demo's whole
 * design avoids. Playwright's Node-side test process plus a browser page is exactly this shape, and
 * `page.on("request")` makes the suite's strongest assertion — that a notification arrived over the
 * websocket and *no* inbox refetch happened — a two-liner.
 *
 * Playwright's selector engines pierce open shadow roots, so the widget's internals are reachable
 * with ordinary `getByRole` queries and no special handling.
 */
export default defineConfig({
  testDir: "./tests",
  // Per-test isolation is by unique organization and user (see fixtures/hermes.ts), and the API's
  // rate limit is keyed by user, so parallel tests cannot contend with each other.
  fullyParallel: true,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 2 : 0,
  // The whole pipeline — NATS, JetStream ack, dispatch, inbox worker, Centrifugo publish — sits
  // between a send and an arrival, so assertions need a generous deadline. They still retry rather
  // than sleep.
  timeout: 90_000,
  expect: { timeout: 20_000 },
  reporter: process.env.CI
    ? [["list"], ["html", { open: "never" }]]
    : [["list"], ["html", { open: "never" }]],
  globalSetup: "./global-setup.ts",

  use: {
    baseURL: "http://localhost:5173",
    // A trace of a notification arriving is the deliverable, not just a debugging aid.
    trace: "on-first-retry",
    video: "retain-on-failure",
    screenshot: "only-on-failure",
  },

  // Chromium only to begin with. Firefox and WebKit differ in ::part and adopted-stylesheet
  // behaviour, which is exactly the class of bug cross-browser buys — but not before the suite is
  // green and quick on one engine.
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],

  webServer: [
    {
      command: "pnpm --filter @hermes/demo-server dev",
      url: "http://localhost:8899/healthz",
      reuseExistingServer: !process.env.CI,
      timeout: 90_000,
      cwd: "../..",
      stdout: "pipe",
      stderr: "pipe",
    },
    {
      command: "pnpm --filter @hermes/react-demo dev",
      url: "http://localhost:5173",
      reuseExistingServer: !process.env.CI,
      timeout: 90_000,
      cwd: "../..",
      stdout: "pipe",
      stderr: "pipe",
    },
  ],
});
