import { textSummary } from 'https://jslib.k6.io/k6-summary/0.1.0/index.js';

export function handleSummary(data) {
  const out = {};
  const runID = __ENV.RUN_ID || 'local';
  out[`artifacts/${runID}/summary.json`] = JSON.stringify(data, null, 2);
  out['stdout'] = textSummary(data, { indent: ' ', enableColors: true });
  return out;
}
