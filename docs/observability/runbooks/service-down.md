# Runbook: `ServiceDown`

## What this alert means

Prometheus has been unable to successfully scrape the service's `/metrics` endpoint for 2 minutes. Either the pod is not running, the container is crash-looping, the readiness probe is failing, or the Kubernetes Service selector isn't matching any healthy pods.

## Immediate triage

```bash
kubectl -n hermes get pods -l app.kubernetes.io/name=$service
kubectl -n hermes describe pod -l app.kubernetes.io/name=$service
kubectl -n hermes logs -l app.kubernetes.io/name=$service --tail=200
```

Dashboard: **kube-prometheus-stack → Kubernetes / Pods** — filter by the service's pod label.

## Common causes (ranked by frequency)

1. **Recent deploy failed readiness probe.** Service is running but `/readyz` returns non-200 (often DB connection not established).
2. **Image pull error.** Look for `ImagePullBackOff` in pod status. Usually registry auth or a bad tag from Kargo.
3. **OOM kill.** `kubectl get events -n hermes | grep OOM`. Pod is getting killed and restarting.
4. **Config error.** Env var missing, panic on startup. Check logs for `log.Fatal` / panics.
5. **Kube Service selector mismatch.** Labels on Pod vs Service don't match. Newly introduced via a bad PR.

## Mitigations

### If recent deploy

Roll back: in ArgoCD, revert the relevant Kustomize commit, or via Kargo UI promote the previous Freight.

### If OOM

Short-term: edit the overlay to increase memory limits. Redeploy.
Medium-term: open a ticket to profile the service — usually there's a recently-added code path that leaks.

### If image pull

Confirm the image tag exists in ECR. If it does, check the service's `imagePullSecrets` and the `ecr-credentials` ExternalSecret. Re-sync the overlay.

## Escalation

- Page the on-call engineer for the service's owning team (check `CLAUDE.md` or service tag in the deploy manifest).
- If multiple services are down simultaneously: suspect infrastructure (nodes, network, NATS/Postgres). Escalate to platform on-call.

## Post-incident

- Add a regression test if the failure mode can be caught in CI.
- If this alert fired and a human didn't act within 5 minutes during business hours, consider why — threshold too loose? Destination too easy to miss?
