// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

// Soak is inbox-mixed at ~30% of capacity levels for a long duration.
// We re-export inbox-mixed's exec functions so the scenario code stays in one place.
export { wsHold, drive, pollInbox } from './inbox-mixed.js';
export { handleSummary } from '../lib/summary.js';

const VUS      = parseInt(__ENV.VUS || '1000', 10);
const SEND_RPS = parseInt(__ENV.SEND_RPS || '100', 10);
const POLL_RPS = parseInt(__ENV.POLL_RPS || '20', 10);
const DURATION = __ENV.DURATION || '4h';

const instCount  = parseInt(__ENV.INSTANCE_COUNT || '1', 10);
const perPodVUs  = Math.max(1, Math.floor(VUS / instCount));
const perPodSend = Math.max(1, Math.floor(SEND_RPS / instCount));
const perPodPoll = Math.max(1, Math.floor(POLL_RPS / instCount));

export const options = {
  scenarios: {
    ws:   { executor: 'constant-vus', vus: perPodVUs, duration: DURATION, exec: 'wsHold' },
    send: {
      executor: 'constant-arrival-rate', rate: perPodSend, timeUnit: '1s', duration: DURATION,
      preAllocatedVUs: Math.max(50, perPodSend), maxVUs: Math.max(100, perPodSend * 4), exec: 'drive',
    },
    poll: {
      executor: 'constant-arrival-rate', rate: perPodPoll, timeUnit: '1s', duration: DURATION,
      preAllocatedVUs: Math.max(10, perPodPoll), maxVUs: Math.max(50, perPodPoll * 4), exec: 'pollInbox',
    },
  },
  thresholds: {
    send_ack_latency: ['p(99)<200'],
    http_req_failed: ['rate<0.005'],
    ws_connection_drops: ['count<' + Math.max(10, Math.floor(VUS * 0.05))],
  },
  tags: {
    scenario: 'soak',
    run_id: __ENV.RUN_ID || 'local',
    instance_id: __ENV.INSTANCE_ID || '0',
    parallelism: __ENV.INSTANCE_COUNT || '1',
  },
};
