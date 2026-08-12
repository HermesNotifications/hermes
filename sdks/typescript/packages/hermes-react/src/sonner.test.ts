// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { describe, expect, it } from "vitest";
import { createSonnerAdapter, type SonnerToastLike } from "./sonner.js";
import type { HermesToastPayload } from "./toasts.js";

/**
 * A fake typed as {@link SonnerToastLike}, rather than a module mock. The injectable parameter
 * on createSonnerAdapter exists precisely so this is possible — and it means the structural
 * type is checked against what the adapter actually calls.
 */
function fakeSonner(): SonnerToastLike & {
  calls: Array<{ method: string; message: string; options?: { id?: string | number; description?: string } }>;
  dismissed: Array<string | number | undefined>;
} {
  const calls: Array<{
    method: string;
    message: string;
    options?: { id?: string | number; description?: string };
  }> = [];
  const dismissed: Array<string | number | undefined> = [];

  const base = (message: string, options?: { id?: string | number; description?: string }) => {
    calls.push({ method: "toast", message, options });
    return options?.id ?? "generated";
  };
  const level =
    (method: string) =>
    (message: string, options?: { id?: string | number; description?: string }) => {
      calls.push({ method, message, options });
      return options?.id ?? "generated";
    };

  return Object.assign(base, {
    calls,
    dismissed,
    info: level("info"),
    success: level("success"),
    warning: level("warning"),
    error: level("error"),
    dismiss: (id?: string | number) => void dismissed.push(id),
  });
}

function payload(overrides: Partial<HermesToastPayload> = {}): HermesToastPayload {
  return {
    id: "n1",
    title: "Invoice ready",
    body: "Invoice #1041",
    level: undefined,
    toastRequested: true,
    notification: { id: "n1" } as HermesToastPayload["notification"],
    event: { type: "notification.new" } as HermesToastPayload["event"],
    ...overrides,
  };
}

describe("createSonnerAdapter", () => {
  it("maps each level to the matching sonner method", () => {
    const sonner = fakeSonner();
    const adapter = createSonnerAdapter(sonner);

    adapter.info(payload());
    adapter.success(payload());
    adapter.warning(payload());
    adapter.error(payload());

    expect(sonner.calls.map((call) => call.method)).toEqual([
      "info",
      "success",
      "warning",
      "error",
    ]);
  });

  it("uses sonner's bare call for a notification with no level", () => {
    const sonner = fakeSonner();
    createSonnerAdapter(sonner).show(payload());

    expect(sonner.calls[0]?.method).toBe("toast");
  });

  it("passes the title as the message and the body as the description", () => {
    const sonner = fakeSonner();
    createSonnerAdapter(sonner).info(payload());

    expect(sonner.calls[0]?.message).toBe("Invoice ready");
    expect(sonner.calls[0]?.options?.description).toBe("Invoice #1041");
  });

  it("omits the description when there is no body", () => {
    const sonner = fakeSonner();
    createSonnerAdapter(sonner).info(payload({ body: "" }));

    expect(sonner.calls[0]?.options?.description).toBeUndefined();
  });

  it("uses the notification id as the toast id", () => {
    // Sonner treats a repeated id as an update rather than a second toast, which is a second
    // line of defence behind the hook's own dedupe.
    const sonner = fakeSonner();
    createSonnerAdapter(sonner).error(payload({ id: "ntf_abc" }));

    expect(sonner.calls[0]?.options?.id).toBe("ntf_abc");
  });

  it("returns sonner's handle and forwards it to dismiss", () => {
    const sonner = fakeSonner();
    const adapter = createSonnerAdapter(sonner);

    const handle = adapter.warning(payload({ id: "ntf_abc" }));
    adapter.dismiss?.(handle);

    expect(handle).toBe("ntf_abc");
    expect(sonner.dismissed).toEqual(["ntf_abc"]);
  });
});
