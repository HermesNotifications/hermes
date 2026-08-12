// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { toast as sonnerToast } from "sonner";
import type { HermesToastAdapter, HermesToastHandle, HermesToastPayload } from "./toasts.js";

/**
 * A Sonner adapter for {@link useHermesToasts}.
 *
 * **This module is deliberately not re-exported from the package root.** A hard `import "sonner"`
 * in the root would make sonner a mandatory dependency of everyone using
 * `@hermes-notifications/react`, including hosts that render their own toasts and hosts
 * importing the package from a server component. It is reachable only as
 * `@hermes-notifications/react/sonner`, and `sonner` is an optional peer dependency.
 */

/** The slice of sonner's `toast` this adapter drives. */
export interface SonnerToastLike {
  info(message: string, options?: SonnerOptions): string | number;
  success(message: string, options?: SonnerOptions): string | number;
  warning(message: string, options?: SonnerOptions): string | number;
  error(message: string, options?: SonnerOptions): string | number;
  (message: string, options?: SonnerOptions): string | number;
  dismiss(id?: string | number): void;
}

interface SonnerOptions {
  id?: string | number;
  description?: string;
}

function optionsFor(payload: HermesToastPayload): SonnerOptions {
  return {
    // The notification id doubles as the toast id. Sonner treats a repeat as an update rather
    // than a second toast, which puts a second line of defence behind the hook's own dedupe.
    id: payload.id,
    ...(payload.body ? { description: payload.body } : {}),
  };
}

/**
 * Build an adapter over a sonner-shaped `toast`.
 *
 * The parameter exists so this is testable against a hand-written fake typed as
 * {@link SonnerToastLike}, rather than by mocking the module.
 */
export function createSonnerAdapter(
  toast: SonnerToastLike = sonnerToast as unknown as SonnerToastLike
): HermesToastAdapter {
  return {
    info: (payload) => toast.info(payload.title, optionsFor(payload)),
    success: (payload) => toast.success(payload.title, optionsFor(payload)),
    warning: (payload) => toast.warning(payload.title, optionsFor(payload)),
    error: (payload) => toast.error(payload.title, optionsFor(payload)),
    // No level: sonner's bare call is its neutral toast.
    show: (payload) => toast(payload.title, optionsFor(payload)),
    dismiss: (handle: HermesToastHandle) => toast.dismiss(handle as string | number),
  };
}

/** Ready to use, over the real sonner. */
export const sonnerAdapter: HermesToastAdapter = createSonnerAdapter();
