# Phase 5: Infrastructure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create Dockerfiles, Kubernetes manifests, Centrifugo configuration, and ingress routing — making the entire Hermes platform deployable to any Kubernetes cluster.

**Architecture:** Multi-stage Docker builds for minimal images. Single K8s namespace `hermes`. Nginx ingress controller for path-based routing. Centrifugo deployed with NATS broker + Redis engine. NATS 3-node JetStream cluster. All manifests are vanilla K8s YAML (no Helm for now — keeps it simple and portable).

**Tech Stack:** Docker multi-stage builds, Kubernetes Deployments/Services/ConfigMaps, Nginx Ingress, Centrifugo, NATS, Redis, PostgreSQL (managed).

**Spec:** `docs/superpowers/specs/2026-03-20-hermes-notification-service-design.md`

---

## File Structure

```
hermes/
├── deploy/
│   ├── docker/
│   │   └── Dockerfile              # Shared multi-stage Dockerfile for all services
│   ├── k8s/
│   │   ├── namespace.yaml
│   │   ├── configmap.yaml           # Shared env config
│   │   ├── secrets.yaml             # Template for secrets (not committed with real values)
│   │   ├── admin.yaml               # Deployment + Service
│   │   ├── router.yaml
│   │   ├── worker-events.yaml
│   │   ├── worker-email.yaml
│   │   ├── worker-sms.yaml
│   │   ├── worker-inbox.yaml
│   │   ├── inbox.yaml
│   │   ├── user.yaml
│   │   ├── centrifugo.yaml          # Deployment + Service + ConfigMap
│   │   ├── nats.yaml                # StatefulSet + Service (3-node JetStream)
│   │   ├── redis.yaml               # Deployment + Service
│   │   ├── ingress.yaml             # Nginx Ingress rules
│   │   └── migration-job.yaml       # Job that runs migrations before deployments
│   └── centrifugo/
│       └── config.json              # Centrifugo configuration
```

---

### Task 1: Shared Dockerfile

**Files:**
- Create: `deploy/docker/Dockerfile`

Single multi-stage Dockerfile that builds any service via build arg.

```dockerfile
# Build stage
FROM golang:1.22-alpine AS builder

ARG SERVICE
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /service ./cmd/${SERVICE}/

# Runtime stage
FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata
COPY --from=builder /service /service
COPY migrations/ /migrations/

EXPOSE 8080
ENTRYPOINT ["/service"]
```

Build any service: `docker build --build-arg SERVICE=admin -t hermes-admin -f deploy/docker/Dockerfile .`

### Task 2: Kubernetes Namespace + ConfigMap + Secrets

**Files:**
- Create: `deploy/k8s/namespace.yaml`
- Create: `deploy/k8s/configmap.yaml`
- Create: `deploy/k8s/secrets.yaml`

### Task 3: Migration Job

**Files:**
- Create: `deploy/k8s/migration-job.yaml`

A Kubernetes Job that runs `database.RunMigrations` before service deployments. Uses the admin image (which includes migrations in /migrations/).

Actually — we need a small CLI tool for running migrations. Let me add that.

- Create: `cmd/migrate/main.go` — tiny binary that just runs migrations and exits

### Task 4: Hermes Service Deployments

**Files:**
- Create: `deploy/k8s/admin.yaml`
- Create: `deploy/k8s/router.yaml`
- Create: `deploy/k8s/worker-events.yaml`
- Create: `deploy/k8s/worker-email.yaml`
- Create: `deploy/k8s/worker-sms.yaml`
- Create: `deploy/k8s/worker-inbox.yaml`
- Create: `deploy/k8s/inbox.yaml`
- Create: `deploy/k8s/user.yaml`

Each has: Deployment (with replicas, resources, health probes, env from configmap/secrets) + Service (ClusterIP).

### Task 5: Infrastructure Services (NATS, Redis, Centrifugo)

**Files:**
- Create: `deploy/k8s/nats.yaml` — 3-node StatefulSet with JetStream
- Create: `deploy/k8s/redis.yaml` — Single-node Deployment
- Create: `deploy/k8s/centrifugo.yaml` — Deployment + ConfigMap
- Create: `deploy/centrifugo/config.json`

### Task 6: Ingress

**Files:**
- Create: `deploy/k8s/ingress.yaml`

Nginx ingress with path-based routing per the spec.

### Task 7: Migration CLI + Docker Build Verification

**Files:**
- Create: `cmd/migrate/main.go`
- Verify Docker build works for at least one service

### Task 8: Tidy

Final verification — all manifests are valid YAML, Docker builds work, all Go tests pass.

---

## Phase 5 Completion Criteria

- [ ] Shared multi-stage Dockerfile builds any service
- [ ] Migration CLI tool
- [ ] K8s namespace, configmap, secrets template
- [ ] Deployments + Services for all 8 Hermes services
- [ ] NATS 3-node JetStream StatefulSet
- [ ] Redis single-node Deployment
- [ ] Centrifugo Deployment with config
- [ ] Nginx Ingress with path-based routing
- [ ] Migration Job
- [ ] Docker build verified
- [ ] All Go tests still pass
