#!/usr/bin/env bash
set -euo pipefail

SCENARIO="${SCENARIO:?SCENARIO required}"
PARALLELISM="${PARALLELISM:-2}"
RUN_ID="${RUN_ID:-$(date +%Y%m%d-%H%M%S)-$RANDOM}"
RUN_NAME="loadtest-${RUN_ID}"
LOADSEED_IMAGE="${LOADSEED_IMAGE:?LOADSEED_IMAGE required (e.g., ghcr.io/…/loadseed:latest)}"

: "${TARGET_RPS:=500}"
: "${VUS:=1000}"
: "${SEND_RPS:=100}"
: "${POLL_RPS:=20}"
: "${DURATION:=10m}"
: "${LT_TENANTS:=10}"
: "${LT_USERS:=10000}"
: "${ADMIN_URL:=http://hermes-admin.hermes.svc.cluster.local:8080}"
: "${SEND_URL:=http://hermes-send.hermes.svc.cluster.local:8088}"
: "${INBOX_URL:=http://hermes-inbox.hermes.svc.cluster.local:8086}"
: "${CENTRIFUGO_URL:=ws://hermes-centrifugo.hermes.svc.cluster.local:8000/connection/websocket}"

export SCENARIO PARALLELISM RUN_ID RUN_NAME LOADSEED_IMAGE \
  TARGET_RPS VUS SEND_RPS POLL_RPS DURATION LT_TENANTS LT_USERS \
  ADMIN_URL SEND_URL INBOX_URL CENTRIFUGO_URL

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
K8S_DIR="$SCRIPT_DIR/../k8s"
cd "$SCRIPT_DIR/../.."

# 1) Build the scenarios ConfigMap from the repo's JS files.
kubectl -n loadtest create configmap loadtest-scenarios \
  --from-file=loadtest/scenarios/ \
  --from-file=loadtest/lib/ \
  --dry-run=client -o yaml | kubectl apply -f -

# 2) Seed the dataset (blocks until complete).
envsubst < "$K8S_DIR/loadseed-job.yaml" | kubectl apply -f -
kubectl -n loadtest wait --for=condition=complete job/loadseed --timeout=30m

# 3) Apply the TestRun.
envsubst < "$K8S_DIR/testrun.yaml" | kubectl apply -f -

# 4) Wait for completion (k6-operator sets .status.stage to "finished").
echo "Waiting for test run $RUN_NAME…"
until kubectl -n loadtest get testrun "$RUN_NAME" -o jsonpath='{.status.stage}' 2>/dev/null | grep -qE 'finished|error'; do
  sleep 10
done
STAGE=$(kubectl -n loadtest get testrun "$RUN_NAME" -o jsonpath='{.status.stage}')
echo "TestRun stage: $STAGE"

# 5) Collect per-pod logs.
mkdir -p "artifacts/$RUN_ID"
for pod in $(kubectl -n loadtest get pods -l app=k6,testrun="$RUN_NAME" -o name); do
  kubectl -n loadtest logs "$pod" > "artifacts/$RUN_ID/${pod##*/}.log" || true
done

# 6) Print Grafana URL (user runs kubectl port-forward separately).
echo ""
echo "Run complete: $RUN_ID ($STAGE)"
echo "Artifacts: artifacts/$RUN_ID/"
echo "Dashboard: kubectl -n loadtest port-forward svc/loadtest-grafana 3001:80 → http://localhost:3001/d/loadtest/load-test?var-run_id=$RUN_ID"

[ "$STAGE" = "finished" ] || exit 1
