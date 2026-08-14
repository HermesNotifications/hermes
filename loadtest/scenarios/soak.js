// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

// Soak is inbox-mixed at ~30% of capacity levels for a long duration.
// We re-export inbox-mixed's exec functions so the scenario code stays in one place.
export { wsHold, drive, pollInbox } from './inbox-mixed.js';
export { handleSummary } from '../lib/summary.js';

const VUS      = parseInt(__ENV.VUS || '1000', 10);
const SEND_RPS = parseInt(__ENV.SEND_RPS || '100', 10);
const POLL_RPS = parseInt(__ENV.POLL_RPS || '20', 10);
const DURATION = __ENV.DURATION || '4h';

// Not divided by an instance count: k6-operator shards a TestRun with --execution-segment
// and k6 applies that to each scenario itself. See the same note in inbox-mixed.js, whose
// exec functions this scenario re-exports.
const perPodVUs  = VUS;
const perPodSend = SEND_RPS;
const perPodPoll = POLL_RPS;

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
    // See inbox-mixed.js: a percentile over an empty trend passes, a count does not.
    ws_push_received: ['count>0'],
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
