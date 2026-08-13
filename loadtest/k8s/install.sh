#!/usr/bin/env bash
# Copyright Hermes Notifications
# SPDX-License-Identifier: Apache-2.0
# See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

# One-time installer for the load-test observability + runner stack.
# Assumes kubectl context points at the target cluster and you have cluster-admin.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

kubectl apply -f "$SCRIPT_DIR/namespace.yaml"

# Helm repos
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts >/dev/null 2>&1 || true
helm repo add grafana https://grafana.github.io/helm-charts >/dev/null 2>&1 || true
helm repo update >/dev/null

# k6-operator.
#
# namespace.create=false because namespace.yaml above already created it, and it carries a
# label of our own (purpose: load-testing). Left at the chart default, Helm refuses to adopt
# a namespace it did not create -- "invalid ownership metadata; missing key
# app.kubernetes.io/managed-by" -- and the install fails on a fresh cluster every time.
helm upgrade --install k6-operator grafana/k6-operator \
  --namespace loadtest \
  --set namespace.create=false \
  --set tolerations[0].key=loadtest \
  --set tolerations[0].operator=Equal \
  --set-string tolerations[0].value=true \
  --set tolerations[0].effect=NoSchedule \
  --set nodeSelector.pool=loadtest-generators

# Prometheus
helm upgrade --install loadtest-prometheus prometheus-community/prometheus \
  --namespace loadtest \
  -f "$SCRIPT_DIR/prometheus-values.yaml"

# Dashboard ConfigMap (sourced from loadtest/dashboards/)
kubectl -n loadtest create configmap loadtest-dashboards \
  --from-file="$SCRIPT_DIR/../dashboards/" \
  --dry-run=client -o yaml | kubectl apply -f -

# Grafana
helm upgrade --install loadtest-grafana grafana/grafana \
  --namespace loadtest \
  -f "$SCRIPT_DIR/grafana-values.yaml"

echo ""
echo "Install complete. Port-forward Grafana:"
echo "  kubectl -n loadtest port-forward svc/loadtest-grafana 3001:80"
