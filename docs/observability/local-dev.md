# Local Development

How to run the observability stack on k3d via Tilt.

## Starting the stack

```bash
tilt up -- --observability
```

The flag enables the opt-in observability resources in `Tiltfile`. Without it, services run but no OSS stack exists (you see your service stdout, nothing more).

Combined with Datadog:

```bash
tilt up -- --datadog --observability
```

## First-run timing

On a cold `tilt up`, the observability stack takes 2–4 minutes to fully come up. The order you'll see in the Tilt UI:

1. `kube-prometheus-stack` CRDs installed (~30s)
2. Prometheus Operator pod ready (~30s)
3. Prometheus, Alertmanager, Grafana pods scheduled (~30s)
4. Loki, Tempo StatefulSets come up (~45s — StatefulSets are slower than Deployments)
5. OTel Collector Deployment ready (~30s)
6. Alloy DaemonSet scheduled on each node
7. Exporters come up last (dependent on NATS/Postgres/Redis being ready, which is what Tilt ensures)

If a pod stays `Pending` >2 min, check `kubectl describe pod -n observability` — most often it's **insufficient memory** on k3d. See "Common pitfalls" below.

## Port-forwards

Automatic via Tilt config:

| UI | URL | Creds |
|---|---|---|
| Grafana | http://localhost:3001 | admin / admin |
| Prometheus | http://localhost:9090 | — |
| Alertmanager | http://localhost:9093 | — |

## Verifying end-to-end

Once everything is green in Tilt:

1. Hit an endpoint on one of the instrumented services (e.g. `curl localhost:8088/healthz` if `send` is migrated yet).
2. Open Grafana → **Explore → Tempo**, enter `service.name = hermes-send`, click "Run query". The span should appear within 30s.
3. Open **Explore → Prometheus**, query `http_server_request_duration_seconds_count{service="hermes-send"}`. Non-zero = metrics flowing.
4. Open **Explore → Loki**, query `{service="hermes-send"}`. Logs should appear with `trace_id` in structured metadata.

If any of those fail, [operations.md](operations.md#debugging-common-issues) has the triage paths.

## Common pitfalls

### k3d out of memory

The defaults in `deploy/observability/overlays/local/` shrink PVs and resource requests aggressively, but Grafana + Prometheus + Loki + Tempo together still want ~2Gi+. If `tilt up` stalls on pending pods:

```bash
kubectl describe pod -n observability <pod> | grep -A3 Events
```

If you see `0/1 nodes are available: insufficient memory`, bump the k3d cluster size in `deploy/k3d/config.yaml` or skip `--observability` when you don't need it.

### Helm CRDs not installed

Symptom: `resource mapping not found for name ... kind "PrometheusRule"` during `tilt up`.

Cause: Tilt tried to apply `PrometheusRule` before kube-prometheus-stack's CRDs registered.

Fix: `tilt trigger kps-prometheus` (or just wait — Tilt retries, usually recovers within one cycle).

### macOS PVs stuck in `Pending`

k3d on macOS sometimes has flaky local-path PV provisioning. Symptoms: PVCs say `Pending` forever, pods stay `ContainerCreating`.

Fix:

```bash
kubectl get storageclass
# Confirm local-path is present
kubectl describe pvc -n observability <name>
# Look for provisioner errors
```

Usually a k3d restart (`tilt down && tilt up -- --observability`) clears it. If persistent, try `k3d cluster delete` and recreate.

### "trace_id not appearing in Loki logs"

Either the service isn't migrated to OTel yet (still on dd-trace-go), or the log was written without passing `ctx` to slog. Check:

```go
slog.Info("foo")                      // ❌ no ctx, no trace_id
slog.InfoContext(ctx, "foo")           // ✅ trace_id populated
```

Grep for plain `slog.Info(` calls in the codebase that should be `InfoContext`.

### "Collector logs flooded with `context deadline exceeded`"

Usually Tempo or Loki is slow to start and the Collector is retrying exports. Resolves once the downstream backends are ready. If it persists >5 min, check the respective pod logs.

## Stopping

```bash
tilt down
```

Cleans up everything including PVs (k3d local-path default is `Delete` reclaim policy). Next `tilt up -- --observability` starts fresh.

To keep PVs across restarts, edit `deploy/k3d/config.yaml` to use a storage class with `Retain` policy, but this is rarely worth it for dev.
