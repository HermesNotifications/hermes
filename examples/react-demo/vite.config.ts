// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

/**
 * Dev-server config for the React demo.
 *
 * Note what is *not* here: the `/v1` proxy of record lives in `examples/demo-server`, not in this
 * file. Putting it here would make it dev-only and framework-specific — `vite preview`, a built
 * demo, and every future Vue or Svelte sibling would each need their own copy. In the server it is
 * one implementation, identical in dev and production, and a pattern an integrator can actually
 * copy into their own backend.
 *
 * These entries just forward to that server so the browser sees a single origin.
 */
/**
 * Ports come from the environment so that several worktrees can run the demo at once.
 *
 * `scripts/demo-env` derives both from the worktree name, which is what keeps two agents from
 * colliding without either having to pick a number. The defaults are the historical ones, so
 * running vite directly still behaves exactly as before.
 */
const webPort = Number(process.env.DEMO_WEB_PORT ?? 5173);
const serverPort = Number(process.env.DEMO_SERVER_PORT ?? 8899);
const target = `http://localhost:${serverPort}`;

export default defineConfig({
  plugins: [react()],
  server: {
    port: webPort,
    // Fail rather than silently picking another port. A demo that quietly moved to 5174 would
    // take the browser suite's baseURL with it and the failure would surface as "the app is
    // empty" several minutes later.
    strictPort: true,
    proxy: {
      "/api": { target, changeOrigin: false },
      "/v1": { target, changeOrigin: false },
    },
  },
  preview: {
    port: webPort,
    strictPort: true,
  },
});
