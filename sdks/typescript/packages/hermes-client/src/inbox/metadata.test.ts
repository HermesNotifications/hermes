// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { describe, expect, it } from "vitest";
import { NOTIFICATION_LEVELS, notificationLevel, toastRequested } from "./metadata.js";

describe("notificationLevel", () => {
  it("reads each level Hermes defines", () => {
    for (const level of NOTIFICATION_LEVELS) {
      expect(notificationLevel({ metadata: { level } })).toBe(level);
    }
  });

  it("is undefined when there is no level", () => {
    expect(notificationLevel({ metadata: { toast: true } })).toBeUndefined();
    expect(notificationLevel({ metadata: {} })).toBeUndefined();
    expect(notificationLevel({})).toBeUndefined();
    expect(notificationLevel(null)).toBeUndefined();
    expect(notificationLevel(undefined)).toBeUndefined();
  });

  it("treats an unrecognised level as absent rather than passing it through", () => {
    // Forward compatibility: the server may add levels, and a client that predates one must
    // stay renderable. Passing "critical" through would put a value into a host's switch that
    // none of its branches handle.
    expect(notificationLevel({ metadata: { level: "critical" } })).toBeUndefined();
    expect(notificationLevel({ metadata: { level: "" } })).toBeUndefined();
  });

  it("ignores a level that is not a string", () => {
    expect(notificationLevel({ metadata: { level: 3 } })).toBeUndefined();
    expect(notificationLevel({ metadata: { level: null } })).toBeUndefined();
    expect(notificationLevel({ metadata: { level: ["error"] } })).toBeUndefined();
  });

  it("ignores metadata that is not a plain object", () => {
    // `typeof [] === "object"`, so an array reaches the same branch as a record and every
    // property read on it silently yields undefined. This arrives unvalidated from a socket.
    expect(notificationLevel({ metadata: [{ level: "error" }] })).toBeUndefined();
    expect(notificationLevel({ metadata: "error" })).toBeUndefined();
    expect(notificationLevel({ metadata: null })).toBeUndefined();
  });
});

describe("toastRequested", () => {
  it("is true only for a boolean true", () => {
    expect(toastRequested({ metadata: { toast: true } })).toBe(true);
  });

  it("is false for anything else", () => {
    // A JSON string "true" is a caller's type error, not a request to interrupt their users.
    expect(toastRequested({ metadata: { toast: "true" } })).toBe(false);
    expect(toastRequested({ metadata: { toast: 1 } })).toBe(false);
    expect(toastRequested({ metadata: { toast: false } })).toBe(false);
    expect(toastRequested({ metadata: {} })).toBe(false);
    expect(toastRequested({})).toBe(false);
    expect(toastRequested(null)).toBe(false);
  });

  it("is independent of level", () => {
    // Two separate concerns: an error you do not want to interrupt someone with, and an info
    // toast you do.
    expect(toastRequested({ metadata: { level: "error" } })).toBe(false);
    expect(toastRequested({ metadata: { level: "info", toast: true } })).toBe(true);
  });
});
