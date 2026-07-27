// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { uuidv4 } from 'https://jslib.k6.io/k6-utils/1.4.0/index.js';

// buildSendBody constructs a POST /v1/send request body.
// Channel selection is weighted by the env var CHANNEL_WEIGHTS (e.g., "inbox:70,email:30").
// Inline content path: pass opts.inline = true to bypass template lookup.
export function buildSendBody(organization, userID, template, opts) {
  const channel = pickChannel(template.channels);
  if (opts && opts.inline) {
    return {
      to: { organization_id: organization.id, user_id: userID },
      channels: [channel],
      content: {
        inbox: channel === 'inbox' ? { title: 'Load test', body: 'Inline body ' + uuidv4() } : undefined,
        email: channel === 'email' ? { subject: 'Load test', body: 'Inline body ' + uuidv4() } : undefined,
      },
    };
  }
  return {
    to: { organization_id: organization.id, user_id: userID },
    channels: [channel],
    template: template.slug,
    data: { subject: 'Load test ' + uuidv4().slice(0, 8), name: userID },
  };
}

function pickChannel(allowed) {
  const weights = parseWeights(__ENV.CHANNEL_WEIGHTS || 'inbox:70,email:30');
  const filtered = weights.filter(w => allowed.includes(w.channel));
  const total = filtered.reduce((s, w) => s + w.weight, 0);
  let r = Math.random() * total;
  for (const w of filtered) {
    r -= w.weight;
    if (r <= 0) return w.channel;
  }
  return filtered[0].channel;
}

function parseWeights(s) {
  return s.split(',').map(p => {
    const [c, w] = p.split(':');
    return { channel: c.trim(), weight: parseInt(w, 10) };
  });
}

export function idempotencyKey() {
  return uuidv4();
}
