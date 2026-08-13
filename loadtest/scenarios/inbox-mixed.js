// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import http from 'k6/http';
import { check } from 'k6';
import exec from 'k6/execution';
import { pickTemplate, userAt, totalUsers } from '../lib/seed.js';
import { adminHeaders, userHeaders } from '../lib/auth.js';
import { buildSendBody, idempotencyKey } from '../lib/payloads.js';
import { connect, recordE2EOnPush } from '../lib/centrifugo.js';
import { sendAckLatency, sendErrors, inboxListLatency } from '../lib/metrics.js';
export { handleSummary } from '../lib/summary.js';

const VUS        = parseInt(__ENV.VUS || '100', 10);
const SEND_RPS   = parseInt(__ENV.SEND_RPS || '50', 10);
const POLL_RPS   = parseInt(__ENV.POLL_RPS || '10', 10);
const DURATION   = __ENV.DURATION || '1m';
const SEND_URL   = __ENV.SEND_URL || __ENV.ADMIN_URL || 'http://localhost:8088';
const INBOX_URL  = __ENV.INBOX_URL || 'http://localhost:8086';

// VUs and rates are NOT divided by an instance count here. k6-operator splits a TestRun
// across pods with --execution-segment, and k6 applies that to every scenario itself, so
// dividing again would halve the load a second time. INSTANCE_ID/INSTANCE_COUNT are passed
// by the operator as k6 *tags*, not environment variables, so the sharding this file used
// to do was reading unset variables and silently doing nothing.
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
    send_ack_latency: ['p(99)<200'],
    ws_push_e2e_latency: ['p(95)<1000'],
    // The one threshold that cannot be satisfied by measuring nothing.
    //
    // A percentile over an empty trend passes: k6 reports p(95)=0 and a green tick. Three
    // separate defects -- the internal/external id mix-up, disjoint user selection, and a
    // cross-VU Map that never hit -- each produced a run where not one push arrived, and
    // every one of them was reported as a pass. A bare count>0 is what turns that class of
    // failure from a silent green into a red.
    ws_push_received: ['count>0'],
    http_req_failed: ['rate<0.01'],
    // The read path this scenario spends most of its requests on had no objective at all.
    // Set after ADR 0011 took the uncached COUNT(*) off it; see docs/loadtest/slo.md for
    // which of these numbers are measured and which are still estimates.
    inbox_list_latency: ['p(95)<150', 'p(99)<400'],
    ws_connect_latency: ['p(95)<500'],
  },
  tags: {
    scenario: 'inbox-mixed',
    run_id: __ENV.RUN_ID || 'local',
    instance_id: __ENV.INSTANCE_ID || '0',
    parallelism: __ENV.INSTANCE_COUNT || '1',
  },
};

// A notification only exercises the realtime path if it is addressed to a user who has a
// socket open, so both scenarios are confined to the same window of the population.
//
// Getting here took three attempts, all of which measured zero while reporting success.
// The trap is VU numbering: under execution segments __VU is per-instance and starts at 1
// on every pod, and the ws and send VUs are *interleaved* in one per-instance pool rather
// than occupying separate blocks (ws might hold 1,2,3,5,8 while send holds 4,6,7). So any
// index derived from __VU gives each scenario a sparse, unpredictable subset, and whether
// they overlap is luck.
//
// Hence: the socket side spreads over the window by globally-unique id, and the send side
// samples the window uniformly at random. Random beats clever here -- a miss costs one
// unrecorded sample, whereas a systematic offset costs the entire metric.
const connectedCount = Math.min(VUS, totalUsers);

function connectedPair(i) {
  return userAt(i % connectedCount);
}

function randomConnectedPair() {
  return userAt(Math.floor(Math.random() * connectedCount));
}

export function wsHold() {
  // idInTest, not __VU: unique across every pod, so two pods do not both claim user 1.
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
