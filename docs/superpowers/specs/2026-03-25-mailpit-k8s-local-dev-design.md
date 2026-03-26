# Mailpit in Local K8s Dev Environment

**Date:** 2026-03-25
**Status:** Draft

## Problem

The email worker runs in the local k8s dev environment (Tilt + k3d), but there is no mail capture service deployed in the cluster. Mailpit exists in Docker Compose for `make test-e2e`, but not in the k8s overlay. This means email delivery cannot be manually tested end-to-end in the local k8s environment.

## Solution

Deploy Mailpit into the local k8s overlay and configure the email worker to route SMTP traffic to it.

## Changes

### 1. `deploy/k8s/overlays/local/mailpit.yaml` (new)

Deployment + Service following the same pattern as `postgres.yaml` and `redis.yaml`:

- **Image:** `axllent/mailpit:latest`
- **Ports:** 1025 (SMTP), 8025 (HTTP UI)
- **Readiness probe:** HTTP GET on port 8025 at `/livez`
- **Service:** ClusterIP exposing both ports
- **Labels:** `app.kubernetes.io/name: mailpit`, `app.kubernetes.io/component: email`

### 2. `deploy/k8s/overlays/local/kustomization.yaml` (modified)

- Add `mailpit.yaml` to `resources:`
- Add `HERMES_EMAIL_SMTP_HOST=mailpit` to the `configMapGenerator` `hermes-config` literals

### 3. `Tiltfile` (modified)

Add Mailpit as an infra resource with a port-forward for the web UI:

```python
k8s_resource("mailpit", labels=["infra"], port_forwards=["8025:8025"])
```

## What stays the same

- Email worker already defaults to SMTP port 1025 and provider `smtp` — no change needed
- `HERMES_EMAIL_FROM` default (`noreply@example.com`) works fine for local dev
- Docker Compose Mailpit for `make test-e2e` is unchanged
- No base manifest changes — Mailpit is local-dev-only (production/staging use real email providers)

## Usage

After `make dev-up`:
1. Send a notification with an email channel via the Admin API
2. Open `http://localhost:8025` to view captured emails in the Mailpit web UI
