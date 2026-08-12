// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { StrictMode, createElement, type ReactNode } from "react";
import { render } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { FakeHermesClient } from "@hermes-notifications/client/testing";
import type { NewNotificationEvent } from "@hermes-notifications/client";
import {
  useHermesToasts,
  type HermesToastAdapter,
  type HermesToastPayload,
  type UseHermesToastsOptions,
} from "./toasts.js";

/**
 * A hand-written adapter typed against the real interface, rather than a module mock — the
 * repo's stated preference, and the thing that makes the adapter contract itself testable.
 */
function recorder(): HermesToastAdapter & {
  calls: Array<{ method: string; payload: HermesToastPayload }>;
  dismissed: unknown[];
} {
  const calls: Array<{ method: string; payload: HermesToastPayload }> = [];
  const dismissed: unknown[] = [];
  const record = (method: string) => (payload: HermesToastPayload) => {
    calls.push({ method, payload });
    return `handle:${method}:${payload.id}`;
  };
  return {
    calls,
    dismissed,
    info: record("info"),
    success: record("success"),
    warning: record("warning"),
    error: record("error"),
    show: record("show"),
    dismiss: (handle) => void dismissed.push(handle),
  };
}

/**
 * An arriving event.
 *
 * `metadata` is deliberately widened: the generated type constrains `level` to the four known
 * strings and `toast` to a boolean, but this data arrives over a websocket from a server that
 * may be newer than this client, so the values a test needs to cover are exactly the ones the
 * type forbids. Widening here keeps the type honest at every real call site.
 */
function arrival(
  overrides: Partial<Omit<NewNotificationEvent, "metadata">> & { metadata?: Record<string, unknown> } = {}
): NewNotificationEvent {
  return {
    type: "notification.new",
    id: "n1",
    title: "Invoice ready",
    body: "Invoice #1041",
    createdAt: "2026-08-11T00:00:00.000Z",
    ...overrides,
  } as NewNotificationEvent;
}

/** Mount one component driving the hook. */
function mount(fake: FakeHermesClient, options: UseHermesToastsOptions, strict = false) {
  function Harness() {
    useHermesToasts(fake.asClient(), options);
    return null;
  }
  const tree: ReactNode = strict
    ? createElement(StrictMode, null, createElement(Harness))
    : createElement(Harness);
  return render(tree);
}

afterEach(() => {
  document.body.replaceChildren();
});

describe("useHermesToasts: which arrivals toast", () => {
  it("toasts an arrival that asked for one", () => {
    const fake = new FakeHermesClient();
    const toast = recorder();
    mount(fake, { toast });

    fake.emit("notification", arrival({ metadata: { toast: true, level: "success" } }));

    expect(toast.calls).toHaveLength(1);
    expect(toast.calls[0]?.method).toBe("success");
    expect(toast.calls[0]?.payload.title).toBe("Invoice ready");
    expect(toast.calls[0]?.payload.body).toBe("Invoice #1041");
  });

  it("stays silent when metadata.toast is absent or not true", () => {
    const fake = new FakeHermesClient();
    const toast = recorder();
    mount(fake, { toast });

    // A level alone is a presentation hint, not a request to interrupt.
    fake.emit("notification", arrival({ id: "a", metadata: { level: "error" } }));
    fake.emit("notification", arrival({ id: "b" }));
    fake.emit("notification", arrival({ id: "c", metadata: { toast: "true" } }));

    expect(toast.calls).toHaveLength(0);
  });

  it("routes each level to its own adapter method", () => {
    const fake = new FakeHermesClient();
    const toast = recorder();
    mount(fake, { toast });

    for (const level of ["info", "success", "warning", "error"] as const) {
      fake.emit("notification", arrival({ id: level, metadata: { toast: true, level } }));
    }

    expect(toast.calls.map((call) => call.method)).toEqual([
      "info",
      "success",
      "warning",
      "error",
    ]);
  });

  it("falls back to show() for a missing or unrecognised level", () => {
    // Never dropped: the sender explicitly asked to interrupt, and discarding that because of
    // one unknown string would be the worse failure. `show` is the default path.
    const fake = new FakeHermesClient();
    const toast = recorder();
    mount(fake, { toast });

    fake.emit("notification", arrival({ id: "a", metadata: { toast: true } }));
    fake.emit("notification", arrival({ id: "b", metadata: { toast: true, level: "banana" } }));

    expect(toast.calls.map((call) => call.method)).toEqual(["show", "show"]);
    expect(toast.calls[1]?.payload.level).toBeUndefined();
  });

  it("carries the reducer's own row on the payload", () => {
    // Built with notificationFromEvent, so the toast and the list row can never disagree.
    const fake = new FakeHermesClient();
    const toast = recorder();
    mount(fake, { toast });

    fake.emit(
      "notification",
      arrival({ metadata: { toast: true, level: "warning", invoiceId: "1041" } })
    );

    const { notification } = toast.calls[0]!.payload;
    expect(notification.id).toBe("n1");
    expect(notification.title).toBe("Invoice ready");
    expect(notification.metadata?.invoiceId).toBe("1041");
  });
});

describe("useHermesToasts: deduplication", () => {
  it("toasts once when the same arrival is delivered twice", () => {
    const fake = new FakeHermesClient();
    const toast = recorder();
    mount(fake, { toast });

    const event = arrival({ metadata: { toast: true } });
    fake.emit("notification", event);
    fake.emit("notification", event);

    expect(toast.calls).toHaveLength(1);
  });

  it("toasts once across two components sharing one client", () => {
    // The realistic double: `client.on` registers both handlers and both fire. This is the
    // case the per-client WeakMap exists for — a per-hook ref would toast twice.
    const fake = new FakeHermesClient();
    const toast = recorder();
    function Harness() {
      useHermesToasts(fake.asClient(), { toast });
      return null;
    }
    render(createElement("div", null, createElement(Harness), createElement(Harness)));

    fake.emit("notification", arrival({ metadata: { toast: true } }));

    expect(toast.calls).toHaveLength(1);
  });

  it("lets each instance toast independently under dedupeScope: hook", () => {
    const fake = new FakeHermesClient();
    const toast = recorder();
    function Harness() {
      useHermesToasts(fake.asClient(), { toast, dedupeScope: "hook" });
      return null;
    }
    render(createElement("div", null, createElement(Harness), createElement(Harness)));

    fake.emit("notification", arrival({ metadata: { toast: true } }));

    expect(toast.calls).toHaveLength(2);
  });

  it("toasts once under StrictMode's double-invoked effects", () => {
    // The demo runs in StrictMode. Effect -> cleanup -> effect must leave exactly one
    // registered handler, and no event may land in the gap.
    const fake = new FakeHermesClient();
    const toast = recorder();
    mount(fake, { toast, dedupeScope: "hook" }, true);

    fake.emit("notification", arrival({ metadata: { toast: true } }));

    expect(toast.calls).toHaveLength(1);
  });

  it("bounds what it remembers, so a long session cannot grow without limit", () => {
    const fake = new FakeHermesClient();
    const toast = recorder();
    mount(fake, { toast, dedupeSize: 2 });

    for (const id of ["a", "b", "c"]) {
      fake.emit("notification", arrival({ id, metadata: { toast: true } }));
    }
    // "a" has been evicted by now, so it is treated as new again.
    fake.emit("notification", arrival({ id: "a", metadata: { toast: true } }));

    expect(toast.calls.map((call) => call.payload.id)).toEqual(["a", "b", "c", "a"]);
  });

  it("unsubscribes on unmount", () => {
    const fake = new FakeHermesClient();
    const toast = recorder();
    const { unmount } = mount(fake, { toast });

    expect(fake.handlerCount()).toBeGreaterThan(0);
    unmount();
    expect(fake.handlerCount()).toBe(0);
  });
});

describe("useHermesToasts: options", () => {
  it("does nothing when disabled", () => {
    const fake = new FakeHermesClient();
    const toast = recorder();
    mount(fake, { toast, enabled: false });

    fake.emit("notification", arrival({ metadata: { toast: true } }));

    expect(toast.calls).toHaveLength(0);
  });

  it("lets shouldToast replace the default gate", () => {
    const fake = new FakeHermesClient();
    const toast = recorder();
    mount(fake, { toast, shouldToast: (payload) => payload.title.startsWith("Invoice") });

    // Would not toast by default — no metadata.toast at all.
    fake.emit("notification", arrival({ id: "a" }));
    fake.emit("notification", arrival({ id: "b", title: "Something else" }));

    expect(toast.calls.map((call) => call.payload.id)).toEqual(["a"]);
  });

  it("dismisses a toast when its notification is read elsewhere", () => {
    const fake = new FakeHermesClient();
    const toast = recorder();
    mount(fake, { toast, dismissOnRead: true });

    fake.emit("notification", arrival({ metadata: { toast: true, level: "warning" } }));
    fake.emit("update", { notificationId: "n1", action: "read" });

    expect(toast.dismissed).toEqual(["handle:warning:n1"]);
  });

  it("clears every held toast on read-all", () => {
    const fake = new FakeHermesClient();
    const toast = recorder();
    mount(fake, { toast, dismissOnRead: true });

    fake.emit("notification", arrival({ id: "a", metadata: { toast: true } }));
    fake.emit("notification", arrival({ id: "b", metadata: { toast: true } }));
    fake.emit("update", { notificationId: "", action: "read-all" });

    expect(toast.dismissed).toHaveLength(2);
  });

  it("does nothing with a null client", () => {
    const toast = recorder();
    function Harness() {
      useHermesToasts(null, { toast });
      return null;
    }
    expect(() => render(createElement(Harness))).not.toThrow();
  });
});
