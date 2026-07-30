// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

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
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    // Fail rather than silently picking another port: the browser suite's baseURL is fixed.
    strictPort: true,
    proxy: {
      "/api": { target: "http://localhost:8899", changeOrigin: false },
      "/v1": { target: "http://localhost:8899", changeOrigin: false },
    },
  },
  preview: {
    port: 5173,
    strictPort: true,
  },
});
