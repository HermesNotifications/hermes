# Runbook: `PrometheusTargetDown`

## What this alert means

Prometheus cannot scrape one of its targets for 10 minutes (`up == 0`). This is self-monitoring of the observability stack.

## Immediate triage

1. Dashboard: **Observability — stack health** → "Prometheus scrape success rate".
2. Prometheus UI → Status → Targets: find the down target, look at the error message.
3. Find the target's pod: `kubectl get pods -A -l <selector>` based on the `job` label.

## Common causes (ranked by frequency)

1. **Pod restart in progress.** Target is briefly unavailable during rollout. Usually self-resolves within a minute or two.
2. **NetworkPolicy blocking scrape.** Prometheus in `observability` trying to reach a pod in `hermes` but denied. Check network policies.
3. **Exporter OOM / crashlooping.** Check pod status. The NATS/Postgres/Redis exporter pods are small and can OOM.
4. **Metrics endpoint not listening.** Service's process isn't exposing `/metrics` (likely a config error or the service hasn't been migrated to OTel yet).
5. **Bad ServiceMonitor label selector.** Prometheus finding zero targets for a job, so it can't scrape.

## Mitigations

### If pod rolling

Wait. If alert doesn't clear in 5 minutes, something's wrong.

### If NetworkPolicy

- In `deploy/k8s/overlays/production/network-policies/`, confirm there's an ingress rule allowing traffic from `observability` namespace to `hermes` namespace on the metrics port.
- If missing, add it:

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-prometheus-scrape
  namespace: hermes
spec:
  podSelector: {}
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: observability
      ports:
        - port: 9100  # or whichever metrics port
          protocol: TCP
```

### If exporter OOM

Bump the memory limit in the exporter's manifest under `deploy/observability/base/exporters/`. Redeploy.

### If metrics endpoint missing

- Confirm the service is expected to expose metrics (all migrated services should).
- If the service isn't migrated yet, the alert is expected — silence it with a temporary Alertmanager inhibition.

### If ServiceMonitor selector

- Check the `ServiceMonitor` resource: `kubectl get servicemonitor -A`.
- Confirm its `spec.selector` matches the target Service's labels.

## Escalation

- Almost always the platform/observability on-call.

## Post-incident

- If it was a NetworkPolicy, check whether newer services are affected too.
- If it was a ServiceMonitor labeling issue, confirm the template / generator code is correct.
