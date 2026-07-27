# Operations

How to deploy, upgrade, and debug the observability stack.

## Prerequisites

### ArgoCD must have `--enable-helm`

The Kustomize overlays use the `helmCharts` block, which requires Kustomize's Helm plugin:

```yaml
# argocd-cm ConfigMap
data:
  kustomize.buildOptions: --enable-helm
```

Also, the `argocd-repo-server` deployment must have `helm` available on PATH. The default ArgoCD image ships with it, but custom images may need it added.

### CRDs install order

kube-prometheus-stack installs the Prometheus Operator CRDs. Everything else (our `ServiceMonitor`, `PrometheusRule`, etc.) depends on them. ArgoCD's `ServerSideApply=true` option + `ApplyOutOfSyncOnly=true` handles the ordering. On a fresh cluster, the first sync may report errors for 30–60s while CRDs propagate — subsequent auto-sync rounds succeed.

## Deployment

### Local (k3d)

```bash
tilt up -- --observability
```

The Tiltfile picks up `deploy/observability/overlays/local` and port-forwards Grafana to `localhost:3001` (admin/admin), Prometheus to `9090`, Alertmanager to `9093`.

See [local-dev.md](local-dev.md) for the common pitfalls.

### Staging / production

Syncing is handled by ArgoCD Applications:

- `deploy/argocd/observability-staging.yaml` — auto-sync
- `deploy/argocd/observability-production.yaml` — manual sync only

Production changes land via PR. When merged to `main`, ArgoCD shows "Out of sync" on the production app; an operator clicks Sync in the UI after reviewing the diff.

### Optional SigNoz fan-out

SigNoz is enabled at the Collector, not in Hermes services. The default staging
and production overlays remain LGTM + Datadog only. Use
`deploy/observability/overlays/staging-signoz` or
`deploy/observability/overlays/production-signoz` only after the corresponding
SigNoz backend and secret are ready.

The SigNoz overlays expect an `otel-collector-signoz` Secret populated by
External Secrets from:

- `hermes/staging/signoz`
- `hermes/production/signoz`

Each secret must provide `otlp-endpoint` and `ingestion-key`. The local stack
stays LGTM-only with `tilt up -- --observability`; local SigNoz export requires
`tilt up -- --observability --signoz` and assumes an OTLP/gRPC endpoint is
reachable at `host.k3d.internal:4317`.

## Upgrading a Helm chart

1. Bump the `version` in `deploy/observability/base/<component>/kustomization.yaml`.
2. Review the chart's changelog for breaking changes.
3. PR → merge → staging ArgoCD auto-syncs.
4. Monitor `observability-health` dashboard and let it run at least 1 hour in staging before promoting.
5. Manual sync in production ArgoCD when ready.

Production upgrades are **sequenced, not parallel** — upgrade kube-prometheus-stack in one PR, then Loki in another, etc. This contains blast radius and makes rollbacks cleaner.

## Restoring from backup

Phase 1 has **no off-cluster backup** — all data lives on the PVs. This is acceptable for the storage classes we use: if a PV is lost, we lose that window's data. The tradeoff is conscious.

Recovery from PV corruption:

1. `kubectl delete pvc <pvc> -n observability` (careful — this is destructive).
2. ArgoCD re-syncs, a fresh PV is provisioned, the statefulset pod restarts empty.
3. The window of data up to the failure is gone. Historical data in Datadog during dual-emit phase is unaffected.

Phase 2 moves long-term storage to S3 (Crossplane-provisioned), which eliminates this class of failure.

## Debugging common issues

### "Traces aren't showing up in Tempo"

Ordered triage:

1. **Is the Collector receiving?** Check the `observability-health` dashboard → "OTel Collector — spans received / sec". If zero, the app isn't emitting.
2. **Is the app emitting?** Exec into the pod, check `/debug/tracez` (if enabled) or look at stdout for `observability: init`. Verify `OTEL_EXPORTER_OTLP_ENDPOINT` env var is set.
3. **Is the Collector exporting?** Same dashboard → "exporter send failures" — if `otlp/tempo` is failing, check Tempo pod logs for ingester errors (disk full, retention evicting hot blocks).
4. **Is Tempo healthy?** `kubectl logs -n observability tempo-0` — look for compaction errors, filesystem errors.

### "Metrics show up for some services, not others"

Almost always one of:

- Service isn't calling `observability.Init` yet (it's still on `tracing.Start` / dd-trace-go). Check `internal/tracing` imports.
- Resource attr `service.name` is wrong — search Prometheus for the actual series: `group by (service) ({__name__!=""})`.
- Collector's `attributes/metrics` processor stripped a label you needed. Check the processor config — it drops user_id/notification_id/organization_id by design.

### "Loki is slow / ingestion stalled"

1. Check PV free space (`DiskPressure` alert fires at 80%).
2. Check Loki pod logs for `rate limit exceeded` — means we're ingesting above the per-organization limit. Tune `limits_config` in `deploy/observability/base/loki/values.yaml`.
3. Check Alloy DaemonSet — if it's busy shipping all of `/var/log/containers` including noisy pods, add a drop rule in the Alloy config.

### "Collector is back-pressuring / OOM"

The `memory_limiter` processor is set to 80% soft / 25% spike. If it's triggering:

1. Confirm via the Collector's own metrics (`otelcol_processor_refused_*`).
2. Short-term: bump the Deployment's memory limit in an overlay patch.
3. Medium-term: add more replicas (the Service is headless-balanced).
4. Long-term: tail-based sampling (Phase 2) drops the firehose.

### "SigNoz export is failing"

SigNoz failures should stay isolated to the Collector. Hermes pods should remain
healthy because they still emit only to the in-cluster Collector.

1. Check Collector self-metrics for `otelcol_exporter_send_failed_*{exporter="otlp/signoz"}` and queue growth.
2. Check Collector logs for TLS, DNS, timeout, or authentication errors.
3. Confirm `SIGNOZ_OTLP_ENDPOINT` is present in the Collector pod and resolves from the `observability` namespace.
4. Confirm the SigNoz ingestion key Secret exists and the remote backend expects the `signoz-ingestion-key` header.
5. Confirm the metrics pipeline still includes `prometheusremotewrite`; SigNoz export must not remove Prometheus alerts or dashboards.

## Retention / PV sizing

Configured in the Helm values. To change:

1. Edit `deploy/observability/base/<component>/values.yaml`.
2. For PV size changes, you may need to delete and recreate the StatefulSet's PVC — growing a PVC depends on the storage class supporting expansion. Check your cloud's CSI driver docs.
3. Retention changes apply on next pod restart (Prometheus, Loki) or next compaction cycle (Tempo).

Current sizes:

| Component | Prod PV | Prod retention | Local PV | Local retention |
|---|---|---|---|---|
| Prometheus | 50Gi | 15d | 10Gi | 3d |
| Loki | 100Gi | 14d | 10Gi | (default) |
| Tempo | 50Gi | 14d | 10Gi | (default) |
| Grafana | 10Gi | — | 10Gi | — |
| Alertmanager | 1Gi × 2 | 120h | 1Gi × 1 | (default) |

## Removing Datadog (future, Phase 2)

When the team is confident in OSS signal:

1. Remove the `datadog` exporter from `deploy/observability/base/otel-collector/values.yaml` pipelines.
2. Remove the `ExternalSecret` for `otel-collector-dd`.
3. Delete `deploy/k8s/base/datadog/` and `deploy/k8s/overlays/*/datadog/`.
4. Remove the DD Agent DaemonSet include from the base Kustomization.
5. Remove Datadog log forwarding from Alloy config (if added).

Do this as separate PRs, at least a week apart. Monitor for regressions each time.
