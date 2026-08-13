// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import http from 'k6/http';
import { check } from 'k6';
import exec from 'k6/execution';
import { pickTemplate, userAt, totalUsers } from '../lib/seed.js';
import { adminHeaders, userHeaders } from '../lib/auth.js';
import { buildSendBody, idempotencyKey } from '../lib/payloads.js';
import { openSocket, recordE2EOnPush } from '../lib/centrifugo.js';
import { sendAckLatency, sendErrors, inboxListLatency } from '../lib/metrics.js';
export { handleSummary } from '../lib/summary.js';

// CONNECTIONS is the thing under test. VUs are just how many event loops carry them:
// with the async websockets module one VU holds WS_SOCKETS_PER_VU sockets, so the two
// numbers are no longer the same and only the first one is a property of Hermes.
const CONNECTIONS   = parseInt(__ENV.CONNECTIONS || __ENV.VUS || '100', 10);
const SOCKETS_PER_VU = parseInt(__ENV.WS_SOCKETS_PER_VU || '1', 10);
// Seconds to spend opening connections before the steady-state hold. 0 opens them all at once.
const WS_RAMP        = parseInt(__ENV.WS_RAMP_SECONDS || '0', 10);
const VUS        = Math.max(1, Math.ceil(CONNECTIONS / SOCKETS_PER_VU));
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

// VU pool for the arrival-rate scenarios, sized by throughput per VU rather than one VU
// per request per second.
//
// A VU whose request completes in ~2ms finishes hundreds of iterations a second, so one
// VU per rps over-allocates by two orders of magnitude. It is invisible at low rates and
// fatal at high ones: an 8000/s step preallocated 8000 VUs per pod and was OOMKilled 14
// seconds in, which reads exactly like the system under test refusing the load.
//
// ITERS_PER_VU is deliberately pessimistic (50ms per iteration) so the pool still covers a
// slow server; override with SEND_VUS/POLL_VUS to pin it.
const ITERS_PER_VU = 20;
const sendVUs = parseInt(__ENV.SEND_VUS || '0', 10) ||
  Math.min(2000, Math.max(50, Math.ceil(perPodSend / ITERS_PER_VU)));
const pollVUs = parseInt(__ENV.POLL_VUS || '0', 10) ||
  Math.min(1000, Math.max(10, Math.ceil(perPodPoll / ITERS_PER_VU)));

export const options = {
  scenarios: {
    // Ramped rather than all-at-once when WS_RAMP_SECONDS is set.
    //
    // `constant-vus` starts every VU simultaneously, which at high connection counts is a
    // thundering herd rather than a load test: 250k connections opened at once drove the
    // generator hosts to 80% CPU in the first seconds, ~35% of sockets never established,
    // and the run plateaued at 162k while the server side sat at 3-10%. Nothing retries,
    // because a socket that fails to open does not get a second attempt inside its
    // iteration -- so the shortfall is permanent and looks exactly like a server limit.
    //
    // A real system does not receive its entire user base in one instant either. It does
    // receive them all at once after an outage, which is what the churn scenario is for.
    ws: WS_RAMP > 0
      ? {
          executor: 'ramping-vus',
          startVUs: 0,
          stages: [
            { duration: `${WS_RAMP}s`, target: perPodVUs },
            { duration: DURATION, target: perPodVUs },
          ],
          gracefulRampDown: '0s',
          exec: 'wsHold',
        }
      : {
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
      preAllocatedVUs: sendVUs,
      maxVUs: sendVUs * 2,
      exec: 'drive',
    },
    poll: {
      executor: 'constant-arrival-rate',
      rate: perPodPoll,
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: pollVUs,
      maxVUs: pollVUs * 2,
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
const connectedCount = Math.min(CONNECTIONS, totalUsers);

function connectedPair(i) {
  return userAt(i % connectedCount);
}

function randomConnectedPair() {
  return userAt(Math.floor(Math.random() * connectedCount));
}

export function wsHold() {
  // idInTest, not __VU: unique across every pod, so two pods do not both claim the same
  // block of users. Each VU takes a contiguous run of SOCKETS_PER_VU users from it.
  const base = (exec.vu.idInTest - 1) * SOCKETS_PER_VU;
  for (let i = 0; i < SOCKETS_PER_VU; i++) {
    const p = connectedPair(base + i);
    openSocket(p.user, p.organization.id, recordE2EOnPush);
  }
  // Returns immediately. k6 holds the iteration open while the sockets and their close
  // timers are outstanding, so the iteration lasts WS_HOLD_SECONDS rather than a moment.
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
