# Quickstart

Get Hermes running on Kubernetes in 5 minutes for evaluation.

## Prerequisites

- Kubernetes 1.26+
- Helm 3.12+
- An ingress controller (e.g., ingress-nginx, Traefik)
- `kubectl` configured to talk to your cluster

## Install

Add the Hermes Helm chart from the OCI registry and install with minimal configuration:

```bash
helm install hermes oci://ghcr.io/hermesnotifications/charts/hermes \
  --namespace hermes --create-namespace \
  --set global.domain=hermes.example.com \
  --set hermes.jwt.secret="$(openssl rand -base64 32)" \
  --set hermes.apiKey.hmacSecret="$(openssl rand -base64 32)"
```

This deploys all Hermes services along with bundled PostgreSQL, NATS, and Redis sub-charts for a self-contained evaluation environment.

## Verify

Run the built-in Helm test to confirm all services are healthy:

```bash
helm test hermes -n hermes
```

You should see all tests pass, confirming that each service's health endpoint is reachable.

## Access the API

If you have an ingress controller, enable ingress:

```bash
helm upgrade hermes oci://ghcr.io/hermesnotifications/charts/hermes \
  --namespace hermes \
  --reuse-values \
  --set ingress.enabled=true \
  --set global.domain=hermes.example.com
```

Otherwise, port-forward to the admin service:

```bash
kubectl port-forward -n hermes svc/hermes-admin 8080:8080
```

## Next Steps

- [Production Hardening](production.md) -- external databases, TLS, HA, and secrets management.
- [Configuration Reference](configuration.md) -- full values reference with examples for email, SMS, observability, and more.
- [Upgrading](upgrading.md) -- how to upgrade between versions safely.
