# --- Variables ---
SERVICES := admin router worker-events worker-email worker-sms worker-inbox inbox user migrate seed
DB_URL   := postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable

# --- Build ---
.PHONY: build build-%
build: $(addprefix build-,$(SERVICES))   ## Build all services
build-%:                                  ## Build a single service (e.g. make build-admin)
	go build -o bin/$*/service ./cmd/$*/

# --- Test ---
.PHONY: test test-integration test-e2e
test:              ## Run unit tests (no infra needed)
	go test ./... -count=1
test-integration:  ## Run all tests including integration (requires make infra-up)
	go test ./... -tags=integration -race -timeout=120s -count=1
test-e2e:          ## Run E2E tests only (requires make infra-up)
	go test ./tests/e2e/... -tags=integration -v -timeout=30s

# --- Lint ---
.PHONY: lint
lint:              ## Run golangci-lint
	golangci-lint run

# --- API Docs ---
.PHONY: openapi openapi-check asyncapi-check
openapi:           ## Generate OpenAPI 3.1 specs from huma
	go run ./cmd/openapi -service admin -format yaml -out api/admin/openapi.yaml
	go run ./cmd/openapi -service admin -format json -out api/admin/openapi.json
	go run ./cmd/openapi -service inbox -format yaml -out api/inbox/openapi.yaml
	go run ./cmd/openapi -service inbox -format json -out api/inbox/openapi.json
	go run ./cmd/openapi -service user -format yaml -out api/user/openapi.yaml
	go run ./cmd/openapi -service user -format json -out api/user/openapi.json
openapi-check:     ## Verify specs are up to date (for CI)
	$(MAKE) openapi
	git diff --exit-code api/
asyncapi-check:    ## Validate AsyncAPI spec
	npx --yes @asyncapi/cli validate api/async/asyncapi.yaml

# --- SDKs ---
.PHONY: sdk-ts-generate sdk-ts-build sdk-generate
sdk-ts-generate:   ## Generate TypeScript types from OpenAPI specs
	pnpm --filter @hermes-notifications/server generate
	pnpm --filter @hermes-notifications/client generate
sdk-ts-build:      ## Build TypeScript SDKs
	pnpm --filter @hermes-notifications/server build
sdk-generate: openapi sdk-ts-generate sdk-ts-build  ## Full pipeline: specs → types → build

# --- Infrastructure ---
.PHONY: infra-up infra-down migrate seed
infra-up:          ## Start local Postgres, NATS, Redis via Docker Compose
	docker compose up -d
infra-down:        ## Stop local infrastructure
	docker compose down
migrate:           ## Run database migrations
	go run ./cmd/migrate/ -database-url "$(DB_URL)" -migrations-path ./migrations
seed:              ## Seed dev API key (run after migrate)
	go run ./cmd/seed/ -database-url "$(DB_URL)"

# --- Docker ---
.PHONY: docker-%
docker-%:          ## Build Docker image for a service (e.g. make docker-admin)
	docker build --build-arg SERVICE=$* -t hermes-$* -f deploy/docker/Dockerfile .

# --- Helpers ---
.PHONY: help
help:              ## Show available targets
	@grep -E '^[a-zA-Z0-9_%-]+:.*##' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

# =============================================================================
# K8s Dev Environment (requires k3d, tilt, kubectl)
# =============================================================================

.PHONY: dev-up dev-down dev-restart dev-status dev-logs dev-psql dev-migrate dev-ui dev-prereqs

CLUSTER_NAME := hermes-dev
K3D_CONFIG   := deploy/k3d/config.yaml

# --- Prerequisites ---

dev-prereqs:
	@command -v k3d >/dev/null   || { echo "k3d not found. Install: https://k3d.io"; exit 1; }
	@command -v tilt >/dev/null  || { echo "tilt not found. Install: https://docs.tilt.dev/install"; exit 1; }
	@command -v kubectl >/dev/null || { echo "kubectl not found."; exit 1; }

# --- Core Commands ---

## Start local K8s dev environment (API gateway at http://localhost:8888)
dev-up: dev-prereqs
	@if k3d cluster list 2>/dev/null | grep -q $(CLUSTER_NAME); then \
		echo "Cluster $(CLUSTER_NAME) already exists, starting Tilt..."; \
	else \
		echo "Creating k3d cluster $(CLUSTER_NAME)..."; \
		k3d cluster create --config $(K3D_CONFIG); \
		echo "Cluster ready."; \
	fi
	tilt up

## Start in CI/headless mode (no browser, exits when resources are ready)
dev-up-ci: dev-prereqs
	@if k3d cluster list 2>/dev/null | grep -q $(CLUSTER_NAME); then \
		echo "Cluster $(CLUSTER_NAME) already exists"; \
	else \
		k3d cluster create --config $(K3D_CONFIG); \
	fi
	tilt ci

## Tear down the dev environment
dev-down:
	-tilt down 2>/dev/null
	k3d cluster delete $(CLUSTER_NAME) 2>/dev/null || true

## Restart the dev environment from scratch
dev-restart: dev-down dev-up

# --- Utilities ---

## Show cluster and pod status
dev-status:
	@echo "=== Cluster ==="
	@k3d cluster list 2>/dev/null || echo "No clusters found"
	@echo ""
	@echo "=== Pods ==="
	@kubectl get pods -n hermes -o wide 2>/dev/null || echo "No pods found"
	@echo ""
	@echo "=== Services ==="
	@kubectl get svc -n hermes 2>/dev/null || echo "No services found"

## Tail logs for a service (usage: make dev-logs SERVICE=admin)
dev-logs:
	@test -n "$(SERVICE)" || { echo "Usage: make dev-logs SERVICE=admin"; exit 1; }
	kubectl logs -n hermes -l app=hermes-$(SERVICE) -f --tail=100

## Connect to the dev Postgres database
dev-psql:
	psql "postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable"

## Re-run database migrations
dev-migrate:
	tilt trigger migrate

## Open the Tilt dashboard
dev-ui:
	@open http://localhost:10350 2>/dev/null || xdg-open http://localhost:10350 2>/dev/null || echo "Open http://localhost:10350"

# =============================================================================
# Terraform (requires AWS credentials)
# =============================================================================

.PHONY: tf-plan tf-apply tf-destroy tf-bootstrap

tf-plan:         ## Plan infra changes (usage: make tf-plan ENV=staging)
	@test -n "$(ENV)" || { echo "Usage: make tf-plan ENV=staging"; exit 1; }
	./infra/terraform/scripts/tfenv.sh $(ENV) plan

tf-apply:        ## Apply infra changes (usage: make tf-apply ENV=staging)
	@test -n "$(ENV)" || { echo "Usage: make tf-apply ENV=staging"; exit 1; }
	./infra/terraform/scripts/tfenv.sh $(ENV) apply

tf-destroy:      ## Destroy infra (usage: make tf-destroy ENV=staging)
	@test -n "$(ENV)" || { echo "Usage: make tf-destroy ENV=staging"; exit 1; }
	./infra/terraform/scripts/tfenv.sh $(ENV) destroy

tf-bootstrap:    ## Bootstrap Terraform backend (one-time, usage: make tf-bootstrap REGION=us-east-1)
	./infra/terraform/scripts/bootstrap-backend.sh $(or $(REGION),us-east-1)

.PHONY: hooks hooks-check
hooks:           ## Install git hooks via lefthook (one-time setup)
	lefthook install

hooks-check:     ## Run all hook checks manually
	lefthook run pre-commit && lefthook run pre-push

configure-registry: ## Set ECR registry in K8s overlays (usage: make configure-registry REGISTRY=123456.dkr.ecr.us-east-1.amazonaws.com)
	@test -n "$(REGISTRY)" || { echo "Usage: make configure-registry REGISTRY=<ecr-url>"; echo "  Get it via: cd infra/terraform && terraform output -raw ecr_registry_url"; exit 1; }
	sed -i'' -e 's|REGISTRY/|$(REGISTRY)/|g' deploy/k8s/overlays/staging/images/kustomization.yaml deploy/k8s/overlays/production/images/kustomization.yaml deploy/kargo/warehouse.yaml
	@echo "Registry set to $(REGISTRY) in staging, production, and kargo overlays"
