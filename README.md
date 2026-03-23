# Hermes

Event-driven notification platform. Send notifications across email, SMS, and in-app inbox channels with real-time delivery via WebSocket.

## Architecture

Go monorepo with 8 microservices connected via NATS JetStream:

```
API Client ──> Admin Service ──> NATS ──> Dispatch ──> NATS ──> Workers ──> NATS ──> Event Writer ──> Postgres
                                           │
                                     ┌─────┼─────┐
                                     │     │     │
                                   Email  SMS  Inbox
                                  Worker Worker Worker
                                     │     │     │
                                  Webhook Webhook Centrifugo
                                                  (WebSocket)
```

**Write path (API key auth):** Admin Service validates and persists notifications, publishes to NATS. Dispatch resolves templates and channels, fans out to delivery workers. Workers deliver via webhooks (email/SMS) or Centrifugo push (inbox). Event Writer batch-inserts delivery events and updates notification status.

**Read path (JWT auth):** Inbox Service serves paginated inbox. User Service manages profiles and notification preferences. Centrifugo provides real-time WebSocket push.

### Services

| Service | Port | Description |
|---------|------|-------------|
| Admin | 8080 | Server-to-server API — tenants, groups, types, send |
| Dispatch | 8081 | Resolves channels and templates, fans out to workers |
| Event Writer | 8082 | Batch-inserts delivery events, updates notification status |
| Email Worker | 8083 | Delivers email notifications via webhook |
| SMS Worker | 8084 | Delivers SMS notifications via webhook |
| Inbox Worker | 8085 | Pushes inbox notifications via Centrifugo |
| Inbox Service | 8086 | User-facing inbox API (list, read, archive) |
| User Service | 8087 | User profiles and notification preferences |

### Infrastructure

| Component | Purpose |
|-----------|---------|
| PostgreSQL | Shared database for all services |
| NATS JetStream | Async messaging (3 streams: NOTIFICATIONS, DELIVERY, EVENTS) |
| Redis/Valkey | Template cache, idempotency dedup, Centrifugo engine |
| Centrifugo | Real-time WebSocket push to user inboxes |

## Quick Start

### Prerequisites

- Go 1.25+
- Docker & Docker Compose
- Make

### Local Development

```bash
# Start infrastructure (Postgres, NATS, Redis)
make infra-up

# Run database migrations and seed dev data
make migrate
make seed

# Build all services
make build

# Install git hooks (requires lefthook)
make hooks

# Run unit tests
make test

# Run integration tests (requires infra)
make test-integration

# Run E2E tests
make test-e2e

# Lint
make lint
```

### K8s Development (k3d + Tilt)

For a full local Kubernetes environment with hot reload:

```bash
# Requires: k3d, tilt, kubectl
make dev-up        # Creates k3d cluster, starts Tilt
make dev-status    # Show cluster and pod status
make dev-logs SERVICE=admin  # Tail service logs
make dev-ui        # Open Tilt dashboard
make dev-down      # Tear down
```

## CLI

The `hermes` CLI provides management commands and an interactive inbox viewer.

```bash
go install ./cmd/hermes/

# Configure
export HERMES_URL=http://localhost:8080
export HERMES_API_KEY=<your-api-key>

# Manage resources
hermes groups list
hermes types list
hermes notifications send --type welcome --user user123

# Interactive inbox (TUI with real-time updates)
hermes inbox open --user user123
```

## API

Two auth modes:

- **Admin API** (API key) — `Authorization: Bearer <api-key>` — for server-to-server operations
- **User API** (JWT) — `Authorization: Bearer <jwt>` — for inbox and user preference endpoints

OpenAPI specs are generated with `make swagger` and available in `api/admin/` and `api/user/`.

### Send a notification

```bash
curl -X POST http://localhost:8080/v1/send \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "welcome",
    "user_id": "user123",
    "data": {"name": "Alice"}
  }'
```

See [docs/integration-guide.md](docs/integration-guide.md) for the full API reference.

## Deployment

Hermes deploys to AWS EKS with ArgoCD (GitOps) and Kargo (promotion pipeline).

**Infrastructure:** Terraform provisions VPC, EKS, Aurora PostgreSQL, ElastiCache (Valkey), ECR, and Secrets Manager. All on Graviton (ARM) instances.

**Pipeline:** `git push` → GitHub Actions CI → CD pushes images to ECR → Kargo promotes to staging → ArgoCD syncs → operator approves production promotion.

```bash
# Provision infrastructure
make tf-bootstrap REGION=us-east-1
make tf-plan ENV=staging
make tf-apply ENV=staging

# Bootstrap EKS cluster
./infra/scripts/bootstrap-cluster.sh hermes-staging us-east-1 $ESO_ROLE_ARN $KARGO_ROLE_ARN
```

See [docs/deployment-guide.md](docs/deployment-guide.md) for the full deployment walkthrough.

## Project Structure

```
cmd/                    # Service entry points
  admin/                #   Admin API server
  dispatch/             #   Notification dispatch
  worker-{email,sms,inbox,events}/  # Delivery workers
  inbox/                #   User inbox API
  user/                 #   User service API
  hermes/               #   CLI tool
  migrate/              #   Database migration runner
internal/               # Shared packages
  store/                #   Database layer (all services)
  config/               #   Environment configuration
  nats/                 #   NATS message contracts
  models/               #   Shared data models
  middleware/           #   HTTP middleware (auth, logging)
  id/                   #   Crockford Base32 ID generation
migrations/             # SQL migrations
deploy/                 # Kubernetes manifests
  k8s/base/             #   Base Kustomize manifests
  k8s/overlays/         #   Staging & production overlays
  argocd/               #   ArgoCD application definitions
  kargo/                #   Kargo promotion pipeline
infra/                  # Infrastructure as code
  terraform/            #   AWS resources (EKS, Aurora, ElastiCache, ECR)
  scripts/              #   Cluster bootstrap scripts
api/                    # Generated OpenAPI specs
docs/                   # Integration & deployment guides
tests/e2e/              # End-to-end tests
```

## Make Targets

Run `make help` to see all available targets.

## License

Proprietary.
