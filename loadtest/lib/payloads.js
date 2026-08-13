// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { uuidv4 } from 'https://jslib.k6.io/k6-utils/1.4.0/index.js';

// buildSendBody constructs a POST /v1/send request body.
// Channel selection is weighted by the env var CHANNEL_WEIGHTS (e.g., "inbox:70,email:30").
// Inline content path: pass opts.inline = true to bypass template lookup.
//
// `user` is a {id, external_id} pair from the seed manifest. `to.user_id` MUST be the
// external id: dispatch resolves it via EnsureUser(organization, external_id), so passing
// the internal id here creates a brand-new user on every send and routes the inbox push to
// a channel no VU is subscribed to. The internal id belongs on the JWT and the subscription,
// not in this body.
export function buildSendBody(organization, user, template, opts) {
  const channel = pickChannel(template.channels);
  const to = { organization_id: organization.id, user_id: user.external_id };
  // The send timestamp rides along on the notification so the socket that receives the
  // push can compute end-to-end latency by itself.
  //
  // It cannot be handed over in-process: k6 gives each scenario its own VU pool, and a
  // wsHold VU is parked inside ws.connect() for the whole run, so it never executes the
  // send function. A module-scope Map between them is always written by one VU and read by
  // another, and therefore never hits. Metadata is persisted by dispatch and echoed
  // verbatim in the inbox push (internal/delivery/inbox.go), which makes it the one channel
  // that actually spans the two.
  const metadata = { lt_sent_ms: Date.now() };
  if (opts && opts.inline) {
    return {
      to: to,
      channels: [channel],
      metadata: metadata,
      content: {
        inbox: channel === 'inbox' ? { title: 'Load test', body: 'Inline body ' + uuidv4() } : undefined,
        email: channel === 'email' ? { subject: 'Load test', body: 'Inline body ' + uuidv4() } : undefined,
      },
    };
  }
  return {
    to: to,
    channels: [channel],
    metadata: metadata,
    template: template.slug,
    data: { subject: 'Load test ' + uuidv4().slice(0, 8), name: user.external_id },
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
