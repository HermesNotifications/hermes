#!/usr/bin/env bash
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.
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
: "${LT_ORGANIZATIONS:=10}"
: "${LT_USERS:=10000}"
: "${ADMIN_URL:=http://hermes-admin.hermes.svc.cluster.local:8080}"
: "${SEND_URL:=http://hermes-send.hermes.svc.cluster.local:8088}"
: "${INBOX_URL:=http://hermes-inbox.hermes.svc.cluster.local:8086}"
: "${CENTRIFUGO_URL:=ws://hermes-centrifugo.hermes.svc.cluster.local:8000/connection/websocket}"

# Everything below this line is referenced by k8s/testrun.yaml and was NOT set here, so
# `envsubst` substituted the empty string for all nine. The failure is not loud: an empty
# `resources.requests.cpu` is a schema error the API server rejects, but an empty CONNECTIONS
# just means the scenario falls back to its own default, so a run asked for 100,000
# connections quietly measured 100 and reported success.
: "${CONNECTIONS:=${VUS}}"
: "${WS_SOCKETS_PER_VU:=25}"
: "${WS_RAMP_SECONDS:=0}"
# Must be >= DURATION or every socket closes and reopens mid-run, which measures reconnect
# churn rather than the ceiling of concurrently held connections. The lib default of 60s is
# wrong for every cluster run, so derive it from DURATION and pad. The soak scenario passes
# hours, so a minutes-only parse would produce a shell arithmetic error on the one run long
# enough for the difference to matter.
if [ -z "${WS_HOLD_SECONDS:-}" ]; then
  case "$DURATION" in
    *h) WS_HOLD_SECONDS=$(( ${DURATION%h} * 3600 + 300 )) ;;
    *m) WS_HOLD_SECONDS=$(( ${DURATION%m} * 60 + 300 )) ;;
    *s) WS_HOLD_SECONDS=$(( ${DURATION%s} + 300 )) ;;
    *)  WS_HOLD_SECONDS=1200 ;;
  esac
fi
: "${CHANNEL_WEIGHTS:=inbox:100}"
# Generator sizing. Holding sockets is cheap on CPU, so these are deliberately smaller than
# the k6-operator default of 2 cores -- that default caps how many runners fit on a node and
# so caps the connection count long before Hermes is stressed.
: "${RUNNER_CPU_REQ:=1}"
: "${RUNNER_MEM_REQ:=1Gi}"
: "${RUNNER_CPU_LIM:=3}"
: "${RUNNER_MEM_LIM:=3Gi}"

export SCENARIO PARALLELISM RUN_ID RUN_NAME LOADSEED_IMAGE \
  TARGET_RPS VUS SEND_RPS POLL_RPS DURATION LT_ORGANIZATIONS LT_USERS \
  ADMIN_URL SEND_URL INBOX_URL CENTRIFUGO_URL \
  CONNECTIONS WS_SOCKETS_PER_VU WS_RAMP_SECONDS WS_HOLD_SECONDS CHANNEL_WEIGHTS \
  RUNNER_CPU_REQ RUNNER_MEM_REQ RUNNER_CPU_LIM RUNNER_MEM_LIM

# Fail rather than render a TestRun with holes in it. envsubst has no strict mode, and every
# one of these has a failure mode that looks like a result instead of an error.
for v in CONNECTIONS WS_SOCKETS_PER_VU WS_RAMP_SECONDS WS_HOLD_SECONDS CHANNEL_WEIGHTS \
         RUNNER_CPU_REQ RUNNER_MEM_REQ RUNNER_CPU_LIM RUNNER_MEM_LIM; do
  [ -n "${!v}" ] || { echo "$v is empty; testrun.yaml would render a hole" >&2; exit 1; }
done

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
K8S_DIR="$SCRIPT_DIR/../k8s"
cd "$SCRIPT_DIR/../.."

# 1) Bundle scenarios into self-contained files, then build a ConfigMap.
loadtest/scripts/bundle.sh
kubectl -n loadtest create configmap loadtest-scenarios \
  --from-file=loadtest/dist/ \
  --dry-run=client -o yaml | kubectl apply -f -

# 2) Seed the dataset (blocks until complete) and extract the manifest.
kubectl -n loadtest delete job loadseed --ignore-not-found
envsubst < "$K8S_DIR/loadseed-job.yaml" | kubectl apply -f -
kubectl -n loadtest wait --for=condition=complete job/loadseed --timeout=30m

# Read the manifest out of the Job's logs, not off its filesystem.
#
# `kubectl cp` cannot work here and never could: it shells out to `tar` *inside* the
# container, and cmd/loadseed/Dockerfile builds FROM distroless/static, which has no tar, no
# shell, and no cat. `kubectl exec` fails for the same reason, and an ephemeral debug
# container cannot mount the target's volumes. So the Job writes the manifest to stdout
# (see k8s/loadseed-job.yaml) and we take it from there.
#
# Parse the JSON rather than pattern-match it. The previous `sed -n '/^{$/,/^}$/p'` assumed
# MarshalIndent's closing `}` sits alone on its own line, but loadseed writes the manifest
# without a trailing newline, so its own completion log lands on that same line:
#
#     }load-test seed complete: 10 organizations, 100000 users, run_seed_id=638a6c6e
#
# The range therefore never terminates, sed prints to EOF, and the summary lines end up
# inside the manifest. Both of the old guards -- non-empty, and contains "organizations" --
# pass on that output, so the failure surfaced four steps later as a k6 SyntaxError in the
# initializer rather than here. raw_decode stops at the end of the first complete object and
# ignores whatever follows it, so interleaved stdout/stderr cannot corrupt the result.
#
# Newest pod, not items[0]: an aborted earlier run can leave a completed loadseed pod behind,
# and jsonpath does not order its results.
#
# `|| POD=""` because jsonpath does not degrade gracefully on an empty list: `items[-1:]` on
# zero items exits 1 with a multi-line template dump, and under `set -e` that aborts the
# script before the readable message below can be printed.
POD=$(kubectl -n loadtest get pods -l app=loadseed \
  --sort-by=.metadata.creationTimestamp \
  -o jsonpath='{.items[-1:].metadata.name}' 2>/dev/null) || POD=""
[ -n "$POD" ] || { echo "no loadseed pod found"; exit 1; }
TMP_MANIFEST=$(mktemp)
kubectl -n loadtest logs "$POD" -c loadseed | python3 -c '
import json, sys
raw = sys.stdin.read()
start = raw.find("{")
if start < 0:
    sys.exit("manifest extraction failed: no JSON object in loadseed logs")
try:
    obj, _ = json.JSONDecoder().raw_decode(raw[start:])
except ValueError as e:
    sys.exit(f"manifest extraction failed: {e}")
if not obj.get("organizations"):
    sys.exit("manifest extraction produced no organizations")
json.dump(obj, sys.stdout, indent=2)
' > "$TMP_MANIFEST"
kubectl -n loadtest create secret generic loadtest-manifest \
  --from-file=seed-manifest.json="$TMP_MANIFEST" \
  --dry-run=client -o yaml | kubectl apply -f -
rm -f "$TMP_MANIFEST"

# 3) Apply the TestRun.
envsubst < "$K8S_DIR/testrun.yaml" | kubectl apply -f -

# 4) Wait for completion (k6-operator sets .status.stage to "finished").
# Braces are load-bearing. Bare `$RUN_NAME…` makes bash read the following multi-byte
# ellipsis as part of the identifier when the locale is not UTF-8, and `set -u` then aborts
# the script with `RUN_NAME…: unbound variable` -- after the TestRun has been applied, so the
# run is left going while the wait, log collection and summary never happen.
echo "Waiting for test run ${RUN_NAME}..."
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
