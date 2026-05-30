// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import { textSummary } from 'https://jslib.k6.io/k6-summary/0.1.0/index.js';

export function handleSummary(data) {
  const out = {};
  const runID = __ENV.RUN_ID || 'local';
  out[`artifacts/${runID}/summary.json`] = JSON.stringify(data, null, 2);
  out['stdout'] = textSummary(data, { indent: ' ', enableColors: true });
  return out;
}
