# Runbook: `DiskPressure`

## What this alert means

A PV in the `observability` namespace is >80% full. Continued growth will lead to ingestion stalls (Loki, Tempo) or retention violations (Prometheus).

## Immediate triage

```bash
kubectl get pvc -n observability
kubectl -n observability exec -it <pod> -- df -h /var/loki  # or /var/tempo/traces or /prometheus
```

Dashboard: **Observability — stack health** plus the kube-prom-stack default "Persistent Volumes" dashboard.

## Common causes (ranked by frequency)

1. **Retention not collecting fast enough.** Most common for Loki. Compactor may be lagging.
2. **Unexpected ingestion rate.** New service emitting high-cardinality metrics, or a log flood.
3. **PV sized wrong for the retention.** The Phase 1 sizing was a guess; real usage may exceed it.

## Mitigations

### Immediate: reduce retention

Edit the component's values:

- Loki: `loki.limits_config.retention_period` lower (e.g. 168h = 7d).
- Tempo: `tempo.retention` lower.
- Prometheus: `retention` flag lower.

Apply via PR → ArgoCD sync. Storage releases on the next compaction pass.

### Grow the PV

Only works if the storage class supports volume expansion.

```bash
kubectl patch pvc -n observability <pvc> --patch '{"spec":{"resources":{"requests":{"storage":"<new-size>"}}}}'
```

StatefulSet pods may need to be deleted (not the PVC — just the Pod) to pick up the new size.

### Investigate high ingestion

- Loki: query `{service="$culprit"}` → see which service is writing the most.
- Prometheus: the `cardinality` explorer in the Prometheus UI shows top metrics by series count. A runaway metric is usually an unbounded label sneak-in (see `semantic-conventions.md`).
- Tempo: less actionable — rarely the cause.

## Escalation

- On-call observability engineer.

## Post-incident

- If the cause was a runaway metric, add a cardinality guardrail test to CI.
- If retention was too generous, right-size it based on actual 30-day usage.
- If the PV was undersized from the start, adjust Phase 1 sizing assumptions in `deploy/observability/base/*/values.yaml`.
