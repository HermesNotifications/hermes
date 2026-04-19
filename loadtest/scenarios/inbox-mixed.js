import http from 'k6/http';
import { check } from 'k6';
import { tenants, pickTemplate, instanceRange } from '../lib/seed.js';
import { adminHeaders, userHeaders } from '../lib/auth.js';
import { buildSendBody, idempotencyKey } from '../lib/payloads.js';
import { connect, recordE2EOnPush } from '../lib/centrifugo.js';
import { sendAckLatency, sendErrors, inboxListLatency } from '../lib/metrics.js';
import { recordSent } from '../lib/shared.js';
export { handleSummary } from '../lib/summary.js';

const VUS        = parseInt(__ENV.VUS || '100', 10);
const SEND_RPS   = parseInt(__ENV.SEND_RPS || '50', 10);
const POLL_RPS   = parseInt(__ENV.POLL_RPS || '10', 10);
const DURATION   = __ENV.DURATION || '1m';
const SEND_URL   = __ENV.SEND_URL || __ENV.ADMIN_URL || 'http://localhost:8088';
const INBOX_URL  = __ENV.INBOX_URL || 'http://localhost:8086';

const instCount  = parseInt(__ENV.INSTANCE_COUNT || '1', 10);
const perPodVUs  = Math.max(1, Math.floor(VUS / instCount));
const perPodSend = Math.max(1, Math.floor(SEND_RPS / instCount));
const perPodPoll = Math.max(1, Math.floor(POLL_RPS / instCount));

// Flatten all (tenant, user) pairs for this instance's shard.
const allPairs = (function () {
  const pairs = [];
  for (const t of tenants) {
    for (const u of t.users) pairs.push({ tenant: t, user: u });
  }
  const [s, e] = instanceRange(pairs.length);
  return pairs.slice(s, e);
})();

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
    http_req_failed: ['rate<0.01'],
  },
  tags: {
    scenario: 'inbox-mixed',
    run_id: __ENV.RUN_ID || 'local',
    instance_id: __ENV.INSTANCE_ID || '0',
    parallelism: __ENV.INSTANCE_COUNT || '1',
  },
};

function vuPair() {
  return allPairs[(__VU + __ITER) % allPairs.length];
}

export function wsHold() {
  const p = allPairs[__VU % allPairs.length];
  connect(p.user, p.tenant.id, recordE2EOnPush);
}

export function drive() {
  const p = vuPair();
  const tpl = pickTemplate(p.tenant);
  const body = buildSendBody(p.tenant, p.user, tpl);
  const headers = adminHeaders({ 'X-Idempotency-Key': idempotencyKey() });
  const start = Date.now();
  const res = http.post(`${SEND_URL}/v1/send`, JSON.stringify(body), { headers });
  sendAckLatency.add(Date.now() - start);
  if (res.status !== 202) { sendErrors.add(1); return; }
  try {
    const parsed = JSON.parse(res.body);
    if (parsed.notification_id) recordSent(parsed.notification_id);
  } catch (e) {}
}

export function pollInbox() {
  const p = vuPair();
  const h = userHeaders(p.user, p.tenant.id);
  const start = Date.now();
  const res = http.get(`${INBOX_URL}/v1/inbox?limit=20`, { headers: h });
  inboxListLatency.add(Date.now() - start);
  check(res, { 'inbox 200': r => r.status === 200 });
}
