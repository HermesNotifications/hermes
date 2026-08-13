// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

// inbox-mixed's steady state, run while pods are deliberately restarted underneath it.
//
// This is the test that proves ADR 0012. Every other scenario measures a system nobody is
// disturbing, and a rolling restart is the single most common disruption Hermes experiences —
// every deploy is one. Before ADR 0012 a restart shed in-flight requests (no readiness drain,
// SIGTERM racing endpoint removal) and abandoned in-flight messages (consumers kept pulling
// throughout shutdown, and nothing waited for handlers). Both were invisible to every existing
// scenario, because none of them restarted anything.
//
// The pass criterion is therefore a *negative* one, and it is the whole point:
//
//     http_req_failed: ['rate<0.001']   DURING a rolling restart
//
// Run it against main before the ADR 0012 changes and it fails; run it after and it passes.
// A run where nothing was restarted proves nothing at all, which is why the harness fails fast
// if it cannot confirm a restart happened — see loadtest/scripts/run-churn.sh.
//
// Usage:
//   CHURN_TARGETS="deploy/hermes-inbox deploy/centrifugo" \
//   DURATION=5m VUS=100 ./loadtest/scripts/run-churn.sh

import http from 'k6/http';
import { check } from 'k6';
import exec from 'k6/execution';
import { pickTemplate, userAt, totalUsers } from '../lib/seed.js';
import { adminHeaders, userHeaders } from '../lib/auth.js';
import { buildSendBody, idempotencyKey } from '../lib/payloads.js';
import { connect, recordE2EOnPush } from '../lib/centrifugo.js';
import { sendAckLatency, sendErrors, inboxListLatency } from '../lib/metrics.js';
export { handleSummary } from '../lib/summary.js';

const VUS       = parseInt(__ENV.VUS || '100', 10);
const SEND_RPS  = parseInt(__ENV.SEND_RPS || '50', 10);
const POLL_RPS  = parseInt(__ENV.POLL_RPS || '10', 10);
const DURATION  = __ENV.DURATION || '5m';
const SEND_URL  = __ENV.SEND_URL || __ENV.ADMIN_URL || 'http://localhost:8088';
const INBOX_URL = __ENV.INBOX_URL || 'http://localhost:8086';

// Not divided by an instance count: k6-operator shards with --execution-segment and k6
// applies that per scenario. See the longer note in inbox-mixed.js.
const perPodVUs  = VUS;
const perPodSend = SEND_RPS;
const perPodPoll = POLL_RPS;

export const options = {
  scenarios: {
    ws: {
      executor: 'constant-vus',
      vus: perPodVUs,
      duration: DURATION,
      exec: 'wsHold',
    },
    send: {
      executor: 'constant-arrival-rate',
      rate: perPodSend,
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: Math.max(50, perPodSend),
      maxVUs: Math.max(100, perPodSend * 4),
      exec: 'drive',
    },
    poll: {
      executor: 'constant-arrival-rate',
      rate: perPodPoll,
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: Math.max(10, perPodPoll),
      maxVUs: Math.max(50, perPodPoll * 4),
      exec: 'pollInbox',
    },
  },
  thresholds: {
    // The assertion this scenario exists for. Ten times stricter than inbox-mixed's 1%,
    // because the claim is not "mostly survives a restart" — it is that a rolling restart is
    // invisible to callers. abortOnFail because once requests are being reset there is nothing
    // further to learn from the remaining minutes.
    http_req_failed: [{ threshold: 'rate<0.001', abortOnFail: true }],

    // Latency is allowed to degrade while a pod is out, but not without bound: a drain that
    // holds connections open without serving them would keep http_req_failed at zero while
    // being just as broken.
    inbox_list_latency: ['p(95)<500', 'p(99)<2000'],
    send_ack_latency: ['p(99)<1000'],

    // Sockets on a restarted pod do drop — that is unavoidable, the pod is going away. What
    // must not happen is a cascade, where reconnects overwhelm the survivors and drop those
    // too. Bounded rather than zero.
    ws_connection_drops: ['count<' + Math.ceil(perPodVUs * 0.5)],
    ws_reconnect_duration: ['p(95)<5000'],

    // End-to-end push must keep working across the restart, not merely resume after it.
    ws_push_e2e_latency: ['p(95)<3000'],
    // See inbox-mixed.js: a percentile over an empty trend passes, a count does not.
    ws_push_received: ['count>0'],
  },
  tags: {
    scenario: 'churn',
    run_id: __ENV.RUN_ID || 'local',
    instance_id: __ENV.INSTANCE_ID || '0',
    parallelism: __ENV.INSTANCE_COUNT || '1',
  },
};

// Same constraint as inbox-mixed.js: a push only exercises the realtime path if it is
// addressed to a user holding a socket, and __VU is per-instance and interleaved between
// scenarios, so it cannot be used to line the two up. Sockets spread by globally-unique
// id; sends sample the same window at random.
const connectedCount = Math.min(VUS, totalUsers);

function connectedPair(i) {
  return userAt(i % connectedCount);
}

function randomConnectedPair() {
  return userAt(Math.floor(Math.random() * connectedCount));
}

export function wsHold() {
  const p = connectedPair(exec.vu.idInTest);
  connect(p.user, p.organization.id, recordE2EOnPush);
}

export function drive() {
  const p = randomConnectedPair();
  const tpl = pickTemplate(p.organization);
  const body = buildSendBody(p.organization, p.user, tpl);
  const headers = adminHeaders({ 'X-Idempotency-Key': idempotencyKey() });
  const start = Date.now();
  const res = http.post(`${SEND_URL}/v1/send`, JSON.stringify(body), { headers });
  sendAckLatency.add(Date.now() - start);
  if (res.status !== 202) { sendErrors.add(1); return; }
}

export function pollInbox() {
  const p = randomConnectedPair();
  const h = userHeaders(p.user, p.organization.id);
  const start = Date.now();
  const res = http.get(`${INBOX_URL}/v1/inbox?limit=20`, { headers: h });
  inboxListLatency.add(Date.now() - start);
  check(res, { 'inbox 200': r => r.status === 200 });
}
