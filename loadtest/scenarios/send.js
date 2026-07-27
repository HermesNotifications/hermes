// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import http from 'k6/http';
import { check } from 'k6';
import { adminHeaders } from '../lib/auth.js';
import { pickOrganization, pickUser, pickTemplate } from '../lib/seed.js';
import { buildSendBody, idempotencyKey } from '../lib/payloads.js';
import { sendAckLatency, sendErrors } from '../lib/metrics.js';
import { recordSent } from '../lib/shared.js';
export { handleSummary } from '../lib/summary.js';

const TARGET_RPS = parseInt(__ENV.TARGET_RPS || '100', 10);
const DURATION   = __ENV.DURATION || '1m';
const PREALLOC   = parseInt(__ENV.PREALLOC_VUS || String(Math.max(50, TARGET_RPS / 10)), 10);
const MAX_VUS    = parseInt(__ENV.MAX_VUS || String(PREALLOC * 4), 10);
// Send service runs on :8088; admin service (:8080) does not expose /v1/send.
// Override with SEND_URL or the legacy ADMIN_URL env var.
const SEND_URL   = __ENV.SEND_URL || __ENV.ADMIN_URL || 'http://localhost:8088';

// Shard the target rate across pods when running under k6-operator.
const instanceCount = parseInt(__ENV.INSTANCE_COUNT || '1', 10);
const perPodRate    = Math.max(1, Math.floor(TARGET_RPS / instanceCount));

export const options = {
  scenarios: {
    send: {
      executor: 'constant-arrival-rate',
      rate: perPodRate,
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: PREALLOC,
      maxVUs: MAX_VUS,
    },
  },
  thresholds: {
    send_ack_latency: ['p(99)<200'],
    http_req_failed: ['rate<0.01'],
  },
  tags: {
    scenario: 'send',
    run_id: __ENV.RUN_ID || 'local',
    instance_id: __ENV.INSTANCE_ID || '0',
    parallelism: __ENV.INSTANCE_COUNT || '1',
  },
};

export default function () {
  const t = pickOrganization();
  const u = pickUser(t);
  const tpl = pickTemplate(t);
  const body = buildSendBody(t, u, tpl);
  const headers = adminHeaders({ 'X-Idempotency-Key': idempotencyKey() });

  const start = Date.now();
  const res = http.post(`${SEND_URL}/v1/send`, JSON.stringify(body), { headers });
  sendAckLatency.add(Date.now() - start);

  const ok = check(res, {
    'status 202': r => r.status === 202,
  });
  if (!ok) {
    sendErrors.add(1);
    return;
  }

  try {
    const parsed = JSON.parse(res.body);
    if (parsed.notification_id) recordSent(parsed.notification_id);
  } catch (e) { /* body not JSON — already failed above */ }
}
