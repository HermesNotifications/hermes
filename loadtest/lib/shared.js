// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

// Per-process in-memory map. k6 scenarios in the same test (send + ws) share a
// single JS runtime per VU, but do NOT share state across VUs. For e2e latency,
// the send scenario and ws scenario run in the SAME VU pool in inbox-mixed.js,
// so this map works for sends that happen on the same VU.
// For the current iteration of the design this is sufficient — we measure e2e
// on the subset of traffic where send and ws are paired by VU.
const sentAt = new Map();

export function recordSent(notificationID) {
  sentAt.set(notificationID, Date.now());
}

export function takeSent(notificationID) {
  const t = sentAt.get(notificationID);
  if (t !== undefined) sentAt.delete(notificationID);
  return t;
}
