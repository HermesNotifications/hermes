.PHONY: dev-up dev-down dev-restart dev-status dev-logs dev-psql dev-migrate dev-ui dev-prereqs

CLUSTER_NAME := hermes-dev
K3D_CONFIG   := deploy/k3d/config.yaml

# --- Prerequisites ---

dev-prereqs:
	@command -v k3d >/dev/null   || { echo "k3d not found. Install: https://k3d.io"; exit 1; }
	@command -v tilt >/dev/null  || { echo "tilt not found. Install: https://docs.tilt.dev/install"; exit 1; }
	@command -v kubectl >/dev/null || { echo "kubectl not found."; exit 1; }

# --- Core Commands ---

## Start local K8s dev environment (creates cluster + starts Tilt)
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
