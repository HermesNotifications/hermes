// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { HermesError } from "../errors.js";

/** The shape `openapi-fetch` returns from every operation. */
export interface ApiResult {
  error?: unknown;
  response: Response;
}

/** Options shared by the API surfaces. */
export interface ApiOptions {
  /** Injected for tests; defaults to the global fetch. */
  fetch?: typeof fetch;
  /**
   * Called once on a 401 before a single retry. Should refresh whatever token the
   * request's `Authorization` header is built from. Without it, a 401 is surfaced to the
   * caller unchanged.
   */
  onUnauthorized?: () => Promise<void>;
}

/**
 * Build the run-classify-retry wrapper used by every API call.
 *
 * Two behaviours worth stating explicitly:
 *
 * - **One retry, never a loop.** A token the server keeps refusing has to surface as an
 *   error; retrying until it works would hang the caller forever.
 * - **Only a 401 is retried.** A 500 might succeed on a second attempt, but retrying it
 *   here would hide server trouble behind doubled latency, and the caller can see
 *   `retryable` on the error and decide for itself.
 */
export function createSender(surface: string, onUnauthorized?: () => Promise<void>) {
  async function attempt<T extends ApiResult>(request: () => Promise<T>): Promise<T> {
    try {
      return await request();
    } catch (cause) {
      throw HermesError.network(surface, cause);
    }
  }

  return async function send<T extends ApiResult>(request: () => Promise<T>): Promise<T> {
    const result = await attempt(request);
    if (!result.error) return result;

    if (result.response.status === 401 && onUnauthorized) {
      await onUnauthorized();
      const retried = await attempt(request);
      if (!retried.error) return retried;
      throw HermesError.fromStatus(surface, retried.response.status, retried.error);
    }

    throw HermesError.fromStatus(surface, result.response.status, result.error);
  };
}
