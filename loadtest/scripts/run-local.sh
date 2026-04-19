#!/usr/bin/env bash
set -euo pipefail

SCENARIO="${SCENARIO:?SCENARIO required (send|inbox-mixed|soak)}"
RUN_ID="${RUN_ID:-$(date +%Y%m%d-%H%M%S)-$RANDOM}"
export RUN_ID

cd "$(dirname "$0")/../.."

# Ensure Prom+Grafana up
docker compose -f docker-compose.yml -f loadtest/docker-compose.loadtest.yml up -d loadtest-prometheus loadtest-grafana

# Ensure a seed manifest exists
if [ ! -f loadtest/seed-manifest.json ]; then
  echo "Seeding (default sizes)…"
  go run ./cmd/loadseed
fi

mkdir -p "artifacts/$RUN_ID"

k6 run \
  --out experimental-prometheus-rw=http://localhost:9090/api/v1/write \
  --tag run_id="$RUN_ID" \
  "loadtest/scenarios/${SCENARIO}.js"

echo ""
echo "Run complete: $RUN_ID"
echo "Summary: artifacts/$RUN_ID/summary.json"
echo "Dashboard: http://localhost:3001/d/loadtest/load-test?var-run_id=$RUN_ID&from=now-10m&to=now"
