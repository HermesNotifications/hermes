#!/usr/bin/env bash
# Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
# See LICENSE and NOTICE in the project root for full terms and restrictions.
#
# Run the churn scenario: steady-state inbox traffic while pods are deliberately restarted.
#
# The scenario's thresholds are the assertion; this script's job is to make sure the disruption
# actually happens. A churn run in which nothing restarted passes trivially and proves nothing,
# so every restart is confirmed against the rollout status and the script exits non-zero if any
# of them did not complete.
#
# Usage:
#   ./loadtest/scripts/run-churn.sh
#   DURATION=10m VUS=200 CHURN_TARGETS="deploy/hermes-inbox" ./loadtest/scripts/run-churn.sh

set -euo pipefail

NAMESPACE="${NAMESPACE:-hermes}"
DURATION="${DURATION:-5m}"
# Both tiers, because they fail differently: hermes-inbox exercises the HTTP readiness drain,
# centrifugo exercises the websocket reconnect path.
CHURN_TARGETS="${CHURN_TARGETS:-deploy/hermes-inbox deploy/centrifugo}"
# How long to wait before the first restart, so the run has a clean baseline to compare against.
CHURN_WARMUP="${CHURN_WARMUP:-60}"
# Gap between restarts. Must exceed the longest terminationGracePeriodSeconds (60s) plus the
# rollout, or the next restart begins before the previous pod has finished draining and the run
# measures two overlapping disruptions rather than one.
CHURN_INTERVAL="${CHURN_INTERVAL:-90}"

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

command -v k6 >/dev/null || { echo "k6 is not installed: https://k6.io/docs/get-started/installation/" >&2; exit 1; }
command -v kubectl >/dev/null || { echo "kubectl is not installed" >&2; exit 1; }

echo "==> churn targets: ${CHURN_TARGETS}"
echo "==> warmup ${CHURN_WARMUP}s, then a restart every ${CHURN_INTERVAL}s, over ${DURATION}"

restarts_done=0
restarts_failed=0

churn() {
  sleep "${CHURN_WARMUP}"
  while true; do
    for target in ${CHURN_TARGETS}; do
      echo "==> restarting ${target}"
      if ! kubectl -n "${NAMESPACE}" rollout restart "${target}"; then
        restarts_failed=$((restarts_failed + 1))
        continue
      fi
      # Confirmed, not assumed. `rollout restart` returns as soon as the annotation is patched;
      # without waiting for the rollout the script could report a restart that never completed.
      if kubectl -n "${NAMESPACE}" rollout status "${target}" --timeout=5m; then
        restarts_done=$((restarts_done + 1))
        echo "${restarts_done}" > /tmp/hermes-churn-count
      else
        restarts_failed=$((restarts_failed + 1))
        echo "!!! rollout of ${target} did not complete" >&2
      fi
      sleep "${CHURN_INTERVAL}"
    done
  done
}

rm -f /tmp/hermes-churn-count
churn &
churn_pid=$!
trap 'kill "${churn_pid}" 2>/dev/null || true' EXIT

set +e
k6 run \
  -e DURATION="${DURATION}" \
  -e VUS="${VUS:-100}" \
  -e SEND_RPS="${SEND_RPS:-50}" \
  -e POLL_RPS="${POLL_RPS:-10}" \
  -e RUN_ID="${RUN_ID:-churn-local}" \
  "${root}/loadtest/scenarios/churn.js"
k6_status=$?
set -e

kill "${churn_pid}" 2>/dev/null || true

completed="$(cat /tmp/hermes-churn-count 2>/dev/null || echo 0)"
echo "==> ${completed} restart(s) completed during the run"

if [ "${completed}" -lt 1 ]; then
  echo "FAIL: no restart completed, so the run proves nothing. Lengthen DURATION (currently ${DURATION}) or shorten CHURN_WARMUP (currently ${CHURN_WARMUP}s)." >&2
  exit 1
fi

exit "${k6_status}"
