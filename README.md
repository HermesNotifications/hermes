# Hermes

Event-driven notification platform. Send notifications across email, SMS, and in-app inbox channels with real-time delivery via WebSocket.

## Architecture

Go monorepo of nine services connected via NATS JetStream:

```
API Client ──> Send ──> NATS ──> Dispatch ──> NATS ──> Workers ──> NATS ──> Event Writer ──> Postgres
                                           │
                                     ┌─────┼─────┐
                                     │     │     │
                                   Email  SMS  Inbox
                                  Worker Worker Worker
                                     │     │     │
                                  Webhook Webhook Centrifugo
                                                  (WebSocket)
```

**Write path (API key auth):** The Send service authenticates the request and publishes it to NATS (a thin ingestion layer). Dispatch persists the notification, resolves templates and channels, and fans out to delivery workers. Workers deliver via webhooks (email/SMS) or Centrifugo push (inbox). Event Writer batch-inserts delivery events and updates notification status. The Admin service is the separate management API (organizations, keys, categories, templates, JWT issuance).

**Read path (JWT auth):** Inbox Service serves paginated inbox. User Service manages profiles and notification preferences. Centrifugo provides real-time WebSocket push.

### Services

| Service | Port | Description |
|---------|------|-------------|
| Send | 8088 | Thin ingestion API — authenticates and publishes `POST /v1/send` to NATS |
| Admin | 8080 | Server-to-server management API — organizations, categories, templates, API keys, JWT issuance |
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

- Go 1.26+
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
export HERMES_URL=http://localhost:8888   # k3d ingress
export HERMES_API_KEY=<your-api-key>

# Manage resources
hermes categories list
hermes templates list
hermes notifications send --organization-id <uuid> --user-id user123 --template welcome --data '{"name":"Alice"}'

# Interactive inbox (TUI with real-time updates)
hermes inbox open --organization-id <uuid> --user-id user123
```

See [docs/cli.md](docs/cli.md) for the full CLI reference.

## API

Two auth modes:

- **Admin API** (API key) — `Authorization: Bearer <api-key>` — for server-to-server operations
- **User API** (JWT) — `Authorization: Bearer <jwt>` — for inbox and user preference endpoints

OpenAPI specs are generated with `make openapi` and available under `api/admin/`, `api/inbox/`, and `api/user/`. See [docs/api/README.md](docs/api/README.md).

### Send a notification

```bash
curl -X POST http://localhost:8888/v1/send \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "to": {"organization_id": "<uuid>", "user_id": "user123"},
    "template": "welcome",
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
cmd/                    # Service & tool entry points
  send/                 #   Ingestion API (POST /v1/send)
  admin/                #   Admin/management API server
  dispatch/             #   Notification dispatch
  worker-{email,sms,inbox,events}/  # Delivery workers + event writer
  inbox/                #   User inbox API
  user/                 #   User service API
  hermes/               #   CLI tool
  migrate/ seed/ cleanup/ loadseed/ openapi/  # One-shot tools
internal/               # Shared packages
  store/                #   Database layer (all services)
  config/               #   Environment configuration
  nats/                 #   NATS message contracts
  models/               #   Shared data models
  middleware/           #   HTTP middleware (auth, logging)
  id/v2/                #   Base62 sortable ID generation
migrations/             # SQL migrations
deploy/                 # Kubernetes manifests
  k8s/base/             #   Base Kustomize manifests
  k8s/overlays/         #   Staging & production overlays
  argocd/               #   ArgoCD application definitions
  kargo/                #   Kargo promotion pipeline
infra/                  # Infrastructure as code
  terraform/            #   AWS resources (EKS, Aurora, ElastiCache, ECR)
  scripts/              #   Cluster bootstrap scripts
api/                    # Generated OpenAPI specs + AsyncAPI contract
docs/                   # Documentation hub (see docs/README.md)
tests/e2e/              # End-to-end tests
```

## Documentation

The full documentation hub is **[docs/README.md](docs/README.md)**. Highlights:

- [Architecture](docs/architecture.md) · [Services](docs/services.md) · [Data Model](docs/data-model.md)
- [Development](docs/development.md) · [Testing](docs/testing.md) · [Contributing](CONTRIBUTING.md)
- [API Reference](docs/api/README.md) · [Integration Guide](docs/integration-guide.md) · [CLI](docs/cli.md)
- [Configuration](docs/configuration.md) · [Self-Hosting](docs/self-hosting/quickstart.md) · [Deployment](docs/deployment-guide.md) · [Observability](docs/observability/README.md)

## Make Targets

Run `make help` to see all available targets.

## Legal

Licensed under the Apache License 2.0 — see [LICENSE](./LICENSE). Important usage
disclaimers are in [DISCLAIMER.md](./DISCLAIMER.md).

Hermes is developed non-commercially and supplied free of charge. It is **not designed,
tested, or certified for use in safety-critical systems** and is **not intended for
distribution or use within the European Economic Area**. Users are responsible for
compliance with all applicable local laws. See [DISCLAIMER.md](./DISCLAIMER.md) for
important usage information and [LICENSE](./LICENSE) for the license terms.
