// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

/**
 * Test helpers, published at `@hermes-notifications/client/testing`.
 *
 * Kept on a subpath rather than the package root so that nothing here is reachable from
 * a production import, while still being a single shared implementation instead of a
 * copy per consuming package.
 */
export {
  FakeHermesClient,
  fakeNotification,
  fakePage,
  type FakeInboxMethod,
  type FakeInboxSurface,
  type FakeListOptions,
} from "./fake-client.js";
