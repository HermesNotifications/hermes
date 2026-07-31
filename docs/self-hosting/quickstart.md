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

`global.domain` is required — the chart's schema rejects an install without it, because every
ingress rule is bound to that hostname. (There is no `ingress.host`.)

This deploys all Hermes services along with bundled PostgreSQL, NATS, Redis and Centrifugo
sub-charts. Two Jobs — the database migration and the NATS stream provisioner — are applied
alongside them, so nothing has to be run by hand.

> **Expect a minute or two of `CrashLoopBackOff` on a first install.** The two Jobs are
> ordinary resources rather than Helm hooks ([ADR 0008](../adr/0008-helm-chart-provisioning-jobs-are-not-hooks.md)),
> so they are created at the same time as the services rather than before them. The services
> come up while the database schema and the JetStream streams do not yet exist, and exit
> rather than run against infrastructure that is not ready. Kubernetes restarts them, the Jobs
> finish, and the pods settle on their own. A message like `stream NOTIFICATIONS is not
> available to hermes-send (has cmd/natsprovision run?)` in the logs during that window is
> normal.

Add `--wait` if you would rather the command block until everything is up — it takes about
70 seconds on this bundled install, and it makes a failed migration fail the install instead
of returning successfully. A flagless install returns before the Jobs have necessarily
finished.

> **This is an evaluation environment, and only that.** The bundled PostgreSQL, Redis and
> NATS are unauthenticated and unencrypted, the Postgres password is the committed string
> `hermes`, and the bundled Centrifugo uses the in-memory engine, so realtime push does not
> fan out beyond a single replica. That is a deliberate posture for getting started quickly,
> not a default to harden in place: production requires external datastores over TLS and
> `hermes.env: production`, which the bundled sub-charts cannot satisfy. See
> [Production Hardening](production.md).

## Verify

Wait for the pods to stop restarting first — see the note above — then run the built-in Helm
test to confirm all services are healthy:

```bash
kubectl get pods -n hermes -w    # wait until every pod is Running and Ready
helm test hermes -n hermes
```

You should see all tests pass, confirming that each service's health endpoint is reachable.
Running `helm test` while the migration and provisioner Jobs are still finishing will fail
for that reason rather than a real one.

## Access the API

Ingress is enabled by default (`ingress.enabled: true`), so if you have an ingress controller
the API is already served at `global.domain`. Set `ingress.className` to match your
controller:

```bash
helm upgrade hermes oci://ghcr.io/hermesnotifications/charts/hermes \
  --namespace hermes \
  --reuse-values \
  --set ingress.className=nginx
```

Otherwise, port-forward to the admin service:

```bash
kubectl port-forward -n hermes svc/hermes-admin 8080:8080
```

## Next Steps

- [Production Hardening](production.md) -- external databases, TLS, HA, and secrets management.
- [Configuration Reference](configuration.md) -- full values reference with examples for email, SMS, observability, and more.
- [Upgrading](upgrading.md) -- how to upgrade between versions safely.
