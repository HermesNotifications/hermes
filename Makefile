# --- Variables ---
# natsprovision is a Job like migrate, not a long-running service: ADR 0005 phase 4 made it
# the only identity that may declare JetStream streams.
SERVICES := admin send dispatch worker-events worker-email worker-sms worker-inbox inbox user migrate natsprovision seed cleanup
DB_URL   := postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable

# The manifest gates in scripts/ parse rendered YAML, so every one of them needs PyYAML.
# Nothing used to declare that. On a machine without it each gate printed SKIP and exited 0 --
# six controls off at once, inside a target whose whole job is catching controls that are
# present and do nothing. CI passed for an unrelated reason: GitHub's ubuntu-latest image
# happens to ship PyYAML for the system python3, so a runner image change would have turned
# every gate off with no failing step. The gates now exit non-zero when it is missing, and
# this venv is what stops that being a daily annoyance.
VENV   := .venv
PYTHON := $(VENV)/bin/python

# --- Build ---
GO_BUILD := go run github.com/DataDog/orchestrion go build
ifdef FAST
GO_BUILD := go build
endif

.PHONY: build build-%
build: $(addprefix build-,$(SERVICES))   ## Build all services (FAST=1 to skip orchestrion)
build-%:                                  ## Build a single service (e.g. make build-admin)
	$(GO_BUILD) -o bin/$*/service ./cmd/$*/

# --- Test ---
.PHONY: test test-integration test-e2e
test:              ## Run unit tests (no infra needed)
	go test ./... -count=1
test-integration:  ## Run all tests including integration (requires make infra-up)
	go test ./... -tags=integration -race -p 1 -timeout=120s -count=1
test-e2e:          ## Run E2E tests only (requires make infra-up)
	go test ./tests/e2e/... -tags=integration -v -timeout=30s

# --- Lint ---
.PHONY: lint
lint:              ## Run golangci-lint
	golangci-lint run

# SPDX license headers (policy in .licenserc.yaml). Pinned so local and CI agree.
LICENSE_EYE := go run github.com/apache/skywalking-eyes/cmd/license-eye@v0.8.0

.PHONY: license-check
license-check:     ## Check that covered source files carry the SPDX license header
	$(LICENSE_EYE) header check

.PHONY: license-fix
license-fix:       ## Insert the SPDX license header into files missing it
	$(LICENSE_EYE) header fix

# --- Verify ---
# The completion gate for parallel agent work (.claude/ownership.json). Everything
# here must run without cluster or cloud credentials, so it proves manifests and
# compositions are well-formed -- not that they are correct against a live API.
#
# `tf-check` is deliberately NOT part of this target; it runs in CI instead. The
# reasoning is recorded in full above that target, under "Terraform".
# Provisions the interpreter every manifest gate runs on. Depending on requirements.txt
# rather than order-only is deliberate: a pinned bump must rebuild the venv, and `touch`
# updates the directory mtime make compares against.
$(VENV): scripts/requirements.txt
	python3 -m venv $(VENV)
	$(VENV)/bin/pip install --quiet --disable-pip-version-check -r scripts/requirements.txt
	@touch $(VENV)

.PHONY: verify verify-manifests
verify:            ## Full local verification gate (no infra needed)
	go build ./...
	go test ./... -count=1
	golangci-lint run
	$(MAKE) verify-manifests
verify-manifests: $(VENV)  ## Static validation of k8s overlays, Crossplane and CI YAML
	kubectl kustomize deploy/k8s/overlays/local > /dev/null
	kubectl kustomize deploy/k8s/overlays/staging > /dev/null
	kubectl kustomize deploy/k8s/overlays/production > /dev/null
	$(PYTHON) -c "import yaml, glob; [list(yaml.safe_load_all(open(p))) for p in glob.glob('infra/crossplane/**/*.yaml', recursive=True) + glob.glob('deploy/kargo/**/*.yaml', recursive=True) + glob.glob('.github/workflows/*.yml')]"
	@# Finding 47: a NetworkPolicy whose podSelector matches nothing is silently inert.
	@# kustomize build and kubectl apply both accept it, so only this catches it.
	$(PYTHON) -m unittest discover -s scripts -p 'test_*.py' -t scripts
	kubectl kustomize deploy/k8s/overlays/staging | $(PYTHON) scripts/check_networkpolicy_selectors.py -
	kubectl kustomize deploy/k8s/overlays/production | $(PYTHON) scripts/check_networkpolicy_selectors.py -
	@# ADR 0005 phase 4: the CA private key must not render into the application namespace.
	@# One misplaced `namespace:` puts it back and nothing about the behaviour changes.
	kubectl kustomize deploy/k8s/overlays/staging | $(PYTHON) scripts/check_ca_key_location.py - --namespace hermes
	kubectl kustomize deploy/k8s/overlays/production | $(PYTHON) scripts/check_ca_key_location.py - --namespace hermes
	@# ADR 0006. A Job's spec.template is immutable and Kargo rewrites its image tag on every
	@# promotion, so a Job that is not an ArgoCD hook applies once and fails the SECOND
	@# promotion with `field is immutable` -- while the Application still reports Healthy.
	@# Established by stashing the fix rather than assuming: every other step in this target
	@# passes identically with that defect present and absent.
	kubectl kustomize deploy/k8s/overlays/staging | $(PYTHON) scripts/check_job_hooks.py -
	kubectl kustomize deploy/k8s/overlays/production | $(PYTHON) scripts/check_job_hooks.py -
	@# Finding 36. A one-character typo in a PDB selector (`hermes-sned`) took expectedPods
	@# from 3 to 0 with no error from kustomize, kubectl or the API server.
	kubectl kustomize deploy/k8s/overlays/production | $(PYTHON) scripts/check_pdb_selectors.py -
	@# Finding 8. hermes-send rendered with no resource requests, no HPA and no PDB and
	@# nothing objected. An HPA whose target declares no request for the resource it measures
	@# reports ScalingActive=False / FailedGetResourceMetric and silently never scales.
	@# The local overlay is deliberately exempt -- see the module docstring for why.
	kubectl kustomize deploy/k8s/overlays/staging | $(PYTHON) scripts/check_workload_resources.py -
	kubectl kustomize deploy/k8s/overlays/production | $(PYTHON) scripts/check_workload_resources.py - --require-hpa
	@# Finding 53. An EMPTY $$HERMES_CENTRIFUGO_NATS_PASSWORD is not a parse error: the server
	@# starts and accepts a `centrifugo` client presenting no credential at all. The guard is
	@# an initContainer, so its absence has no runtime signal -- the cluster looks healthy.
	@# Conditional on the server reading `-c nats.conf`, which is why local is checked too and
	@# legitimately passes without the guard.
	kubectl kustomize deploy/k8s/overlays/local | $(PYTHON) scripts/check_nats_password_guard.py -
	kubectl kustomize deploy/k8s/overlays/staging | $(PYTHON) scripts/check_nats_password_guard.py -
	kubectl kustomize deploy/k8s/overlays/production | $(PYTHON) scripts/check_nats_password_guard.py -
	@# ADR 0005 phase 4's named residual: a `ca` ClusterIssuer can be referenced from ANY
	@# namespace, and a leaf it signs is trusted by every Hermes service. The policy that
	@# closes it has two silent-inert shapes -- unbound, or bound with Warn instead of Deny.
	kubectl kustomize deploy/k8s/overlays/staging | $(PYTHON) scripts/check_ca_issuer_policy.py -
	kubectl kustomize deploy/k8s/overlays/production | $(PYTHON) scripts/check_ca_issuer_policy.py -
	@# infra/scripts/lib.sh derives the database and Redis URLs that config.Validate accepts
	@# or rejects. It shipped with 17 passing tests that nothing ran.
	./infra/scripts/test-lib.sh
	$(MAKE) verify-chart

# The chart's only previous control was `helm template ... > /dev/null` in CI, which
# proves the templates parse and asserts nothing about what they produce. That is how the
# chart reached main missing the natsprovision Job (six services crash-loop at boot),
# routing /v1/types and /v1/groups (no handler since the rename), having no rule at all
# for /v1/templates, /v1/apikeys, /v1/organizations or /v1/subscriptions, and rendering
# the bundled NATS images under the Hermes registry. All of it renders cleanly.
.PHONY: verify-chart
verify-chart: $(VENV)  ## Check the rendered Helm chart against the Go source it deploys
	@# Absent helm must fail loudly. A gate that skips when its tool is missing is the
	@# same class of defect as the one this target exists to close.
	@command -v helm >/dev/null 2>&1 || { \
	  echo "ERROR: helm not found on PATH."; \
	  echo "  The Helm chart gate cannot run without it, and skipping it is how the chart"; \
	  echo "  drifted in the first place. Install Helm v3:"; \
	  echo "    https://helm.sh/docs/intro/install/"; \
	  exit 1; }
	helm dependency build charts/hermes/
	helm template verify charts/hermes/ \
	  --set hermes.jwt.secret=verify --set hermes.apiKey.hmacSecret=verify \
	  --set global.domain=verify.example.com \
	  | $(PYTHON) scripts/check_helm_render.py - --source-root=.
	@# Optional features render into workloads the default install never produces, so they
	@# need their own pass -- hermes-cleanup was missing from the cd.yml publish matrix and
	@# only shows up when the CronJob renders.
	helm template verify charts/hermes/ \
	  --set hermes.jwt.secret=verify --set hermes.apiKey.hmacSecret=verify \
	  --set global.domain=verify.example.com \
	  --set hermes.cleanup.enabled=true --set networkPolicy.enabled=true \
	  --set observability.enabled=true \
	  | $(PYTHON) scripts/check_helm_render.py - --source-root=.
	@# The production posture must be refused at render time, not discovered as a
	@# crash-loop. Bundled sub-charts cannot satisfy config.Validate(), so this must fail.
	@if helm template verify charts/hermes/ \
	     --set hermes.jwt.secret=verify --set hermes.apiKey.hmacSecret=verify \
	     --set global.domain=verify.example.com --set hermes.env=production >/dev/null 2>&1; then \
	  echo "ERROR: hermes.env=production rendered with the bundled sub-charts."; \
	  echo "  It must fail: _validate.tpl should refuse a combination whose workloads all"; \
	  echo "  exit at startup on config.Validate()."; \
	  exit 1; \
	fi
	@echo "ok: production install with bundled sub-charts is refused at render time"
	@# No hermes-admin-portal image exists and nothing here can build one, so enabling it
	@# on chart defaults must be refused rather than deferred to ImagePullBackOff.
	@if helm template verify charts/hermes/ \
	     --set hermes.jwt.secret=verify --set hermes.apiKey.hmacSecret=verify \
	     --set global.domain=verify.example.com --set adminPortal.enabled=true >/dev/null 2>&1; then \
	  echo "ERROR: adminPortal.enabled=true rendered against the unpublished default image."; \
	  echo "  It must fail: _validate.tpl should require adminPortal.image.repository to be"; \
	  echo "  overridden with an image the operator built themselves."; \
	  exit 1; \
	fi
	@echo "ok: admin portal on the unpublished default image is refused at render time"

# --- Helm ---
.PHONY: helm-lint
helm-lint:         ## Lint the Helm chart
	helm dependency build charts/hermes/
	helm lint charts/hermes/ --set hermes.jwt.secret=test --set hermes.apiKey.hmacSecret=test --set global.domain=test.example.com
	jq . charts/hermes/values.schema.json > /dev/null

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
	pnpm --filter @hermes-notifications/client build
sdk-python:        ## Generate Python server SDK
	npx @openapitools/openapi-generator-cli generate \
		-i api/admin/openapi.yaml -g python \
		-o sdks/python/hermes-server-sdk \
		--additional-properties=packageName=hermes_server_sdk,projectName=hermes-server-sdk \
		--global-property=skipFormModel=true
# The inner double quotes on licenseName are literal, and load-bearing. The npx wrapper joins
# its argv back into a command string and re-splits it, so ordinary shell quoting is already
# gone by the time the generator parses anything: `licenseName="Apache License 2.0"` arrives as
# three arguments and the run dies with `Found unexpected parameters: [License, 2.0]`. Quoting
# the whole key=value, so the quotes survive *into* the wrapper, is what keeps the space
# intact. This is the only additional-property here whose value contains a space, which is why
# it is the only one that needs it.
sdk-java:          ## Generate Java server SDK
	npx @openapitools/openapi-generator-cli generate \
		-i api/admin/openapi.yaml -g java \
		-o sdks/java/hermes-server-sdk \
		--additional-properties=artifactId=hermes-server-sdk,groupId=com.hermes,invokerPackage=com.hermes.sdk,apiPackage=com.hermes.sdk.api,modelPackage=com.hermes.sdk.model,hideGenerationTimestamp=true \
		--additional-properties='"licenseName=Apache License 2.0"' \
		--additional-properties=licenseUrl=https://www.apache.org/licenses/LICENSE-2.0.txt \
		--global-property=skipFormModel=true
sdk-dotnet:        ## Generate .NET server SDK
	npx @openapitools/openapi-generator-cli generate \
		-i api/admin/openapi.yaml -g csharp \
		-o sdks/dotnet/Hermes.ServerSdk \
		--additional-properties=packageName=Hermes.ServerSdk,targetFramework=net8.0,hideGenerationTimestamp=true,packageGuid='{102EB2C0-41DB-427A-A9EF-333D033706BE}' \
		--global-property=skipFormModel=true
sdk-generate: openapi sdk-ts-generate sdk-ts-build sdk-python sdk-java sdk-dotnet  ## Full pipeline: specs → types → build

# --- Parallel development sandboxes (one namespace per worker) ---
#
# For running several agents or developers against one k3s cluster without them
# colliding. Each worker gets its own namespace holding Postgres, Redis, NATS with
# JetStream, and Mailpit. See docs/development.md.
#
# WORKER defaults to your username so `make devworker-up` is safe to type without
# thinking; CI and agents should pass it explicitly.
WORKER ?= $(USER)
DEVWORKER_NS := hermes-dev-$(WORKER)

.PHONY: devworker-up devworker-down devworker-list devworker-env
devworker-up:      ## Create an isolated dev sandbox namespace (WORKER=name)
	@kubectl create namespace $(DEVWORKER_NS) --dry-run=client -o yaml | kubectl apply -f - >/dev/null
	@# Labelled so devworker-list can find sandboxes without pattern-matching names, and
	@# so a stray sandbox is identifiable as disposable rather than someone's real work.
	@kubectl label namespace $(DEVWORKER_NS) hermes.io/devworker=true --overwrite >/dev/null
	@# deploy/k8s/devworker deliberately sets no `namespace:` anywhere, so `-n` places
	@# everything. That is what makes this parallel-safe: no shared file is edited, and
	@# no throwaway overlay is needed (kustomize also rejects absolute `resources` paths).
	kubectl kustomize deploy/k8s/devworker | kubectl apply -n $(DEVWORKER_NS) -f -
	@echo "waiting for $(DEVWORKER_NS) to become ready..."
	@kubectl -n $(DEVWORKER_NS) wait --for=condition=Ready pod --all --timeout=180s
	@$(MAKE) --no-print-directory devworker-env WORKER=$(WORKER)

devworker-down:    ## Delete a dev sandbox namespace and its volumes (WORKER=name)
	kubectl delete namespace $(DEVWORKER_NS) --wait=false

devworker-list:    ## List all dev sandbox namespaces
	@kubectl get ns -l hermes.io/devworker=true --no-headers 2>/dev/null || \
		kubectl get ns --no-headers | grep '^hermes-dev-' || echo "no sandboxes"

devworker-env:     ## Print eval-able env vars pointing at a sandbox (WORKER=name)
	@# Emits ClusterIPs, not .svc DNS names. Cluster DNS does not resolve from the host,
	@# but on a k3s node the host CAN route to both service and pod CIDRs — verified. So
	@# these URLs work directly from a shell, with no port-forward.
	@#
	@# Node-local by nature: these addresses are only reachable from this machine.
	@set -e; ns=$(DEVWORKER_NS); \
	ip() { kubectl -n $$ns get svc $$1 -o jsonpath='{.spec.clusterIP}'; }; \
	nats_ip() { kubectl -n $$ns get pod nats-0 -o jsonpath='{.status.podIP}'; }; \
	echo "# eval \"\$$(make devworker-env WORKER=$(WORKER))\""; \
	echo "export HERMES_ENV=development"; \
	echo "export HERMES_DATABASE_URL='postgres://hermes:hermes@$$(ip postgres):5432/hermes?sslmode=disable'"; \
	echo "export HERMES_REDIS_URL='redis://$$(ip redis):6379'"; \
	echo "# NATS is a headless Service (no ClusterIP), so this is the pod IP and it"; \
	echo "# changes if nats-0 restarts. Re-run this target after a restart."; \
	echo "export HERMES_NATS_URL='nats://$$(nats_ip):4222'"; \
	echo "export HERMES_EMAIL_SMTP_HOST='$$(ip mailpit)'"; \
	echo "export MAILPIT_SMTP_HOST='$$(ip mailpit)'"; \
	echo "export MAILPIT_API_URL='http://$$(ip mailpit):8025'"; \
	echo "export HERMES_DYNAMO_ENDPOINT='http://$$(ip dynamodb-local):8000'"; \
	echo "export HERMES_DYNAMO_REGION=us-east-1"; \
	echo "export AWS_ACCESS_KEY_ID=dummy AWS_SECRET_ACCESS_KEY=dummy"

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
cleanup:           ## Run event retention cleanup (delete events older than HERMES_EVENT_RETENTION_DAYS)
	go run ./cmd/cleanup/ -database-url "$(DB_URL)"

# --- Admin Portal ---
.PHONY: dev-admin admin-install
admin-install:     ## Install admin portal dependencies
	cd web/admin && pnpm install
dev-admin:         ## Start the admin portal dev server (port 3000)
	cd web/admin && pnpm dev

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

.PHONY: tf-plan tf-apply tf-destroy tf-bootstrap tf-check

# Credential-free Terraform checks. ADR 0007 records that `terraform validate`,
# `terraform fmt -check` and variable-validation truth tables WERE executed during that
# work -- by hand, once, and then never again, because Terraform appeared in neither
# `make verify` nor CI. That is this repository's signature defect: a control that ran
# once and does not run.
#
# This target is called by the `terraform` job in .github/workflows/ci.yml, so it now runs
# on every merge. It fails loudly when terraform is absent rather than skipping: a gate
# that goes quiet when its tool is missing is the same defect as no gate at all, and it is
# the reason `verify-chart` has the identical guard for helm.
#
# WHY THIS IS NOT IN `make verify`, deliberately and against the usual rule that every
# gate belongs in both. `verify` is the completion gate every agent and developer runs, and
# it is documented above as needing no cluster and no cloud credentials. `terraform init`
# needs neither of those but does need the network and a ~700MB provider download, and a
# hard failure on a missing binary would have made `verify` red for every unit in this
# remediation batch -- none of which had terraform installed, and none of which touched
# infra/terraform. The predictable response to that is that people stop running `verify`,
# which costs more than the gap it closes. CI has terraform unconditionally, so the check
# genuinely runs on the merge path, which is the half that was actually missing.
#
# Run it locally with `make tf-check` when you touch infra/terraform.
tf-check:        ## Credential-free terraform fmt/validate (runs in CI; needs terraform)
	@command -v terraform >/dev/null 2>&1 || { \
	  echo "ERROR: terraform not found on PATH."; \
	  echo "  This gate cannot run without it, and skipping it is how infra/terraform came"; \
	  echo "  to have no automated check at all. Install Terraform:"; \
	  echo "    https://developer.hashicorp.com/terraform/install"; \
	  exit 1; }
	terraform -chdir=infra/terraform fmt -check -recursive
	@# -backend=false: no S3 backend, no credentials, no state. This still resolves the
	@# module graph and the provider schemas, which is what makes `validate` mean anything.
	terraform -chdir=infra/terraform init -backend=false -input=false
	terraform -chdir=infra/terraform validate

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

# --- Load testing ---
.PHONY: loadseed loadseed-clean
loadseed:          ## Seed load-test dataset (default: 10 organizations, 10k users each)
	go run ./cmd/loadseed \
	  --organizations $(or $(LT_ORGANIZATIONS),10) \
	  --users-per-organization $(or $(LT_USERS),10000) \
	  --output loadtest/seed-manifest.json

loadseed-clean:    ## Delete all entities from the current seed manifest
	go run ./cmd/loadseed --cleanup --output loadtest/seed-manifest.json

.PHONY: loadtest-local loadtest-local-clean
loadtest-local:    ## Run a local load test (SCENARIO=send|inbox-mixed|soak TARGET_RPS=... DURATION=...)
	SCENARIO=$(or $(SCENARIO),send) \
	TARGET_RPS=$(or $(TARGET_RPS),50) \
	VUS=$(or $(VUS),50) \
	DURATION=$(or $(DURATION),30s) \
	SEND_URL=$(or $(SEND_URL),http://localhost:8088) \
	ADMIN_URL=$(or $(ADMIN_URL),http://localhost:8080) \
	INBOX_URL=$(or $(INBOX_URL),http://localhost:8086) \
	CENTRIFUGO_URL=$(or $(CENTRIFUGO_URL),ws://localhost:8000/connection/websocket) \
	loadtest/scripts/run-local.sh

loadtest-local-clean: ## Tear down local load-test infra and clean seed
	docker compose -f docker-compose.yml -f loadtest/docker-compose.loadtest.yml down -v
	[ -f loadtest/seed-manifest.json ] && go run ./cmd/loadseed --cleanup || true
	rm -f loadtest/seed-manifest.json

.PHONY: dispatchbench
dispatchbench:     ## Run the dispatch concurrency sweep (requires make infra-up; pool_max_conns >= max workers). BACKENDS=postgres|dynamo, HERMES_DYNAMO_ENDPOINT for dynamo.
	go run ./cmd/dispatchbench \
	  --db "$(or $(HERMES_DATABASE_URL),postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable&pool_max_conns=20)" \
	  --backends "$(or $(BACKENDS),postgres)" \
	  --csv docs/loadtest/dispatch-tuning.csv \
	  --md docs/loadtest/dispatch-tuning.md

.PHONY: loadtest-k8s loadtest-k8s-clean loadtest-k8s-install
loadtest-k8s-install: ## One-time install of k6-operator + Prom + Grafana in loadtest namespace
	loadtest/k8s/install.sh

loadtest-k8s:      ## Run a cluster load test (SCENARIO=... PARALLELISM=... VUS=... DURATION=... LOADSEED_IMAGE=...)
	SCENARIO=$(or $(SCENARIO),send) \
	PARALLELISM=$(or $(PARALLELISM),2) \
	TARGET_RPS=$(or $(TARGET_RPS),500) \
	VUS=$(or $(VUS),1000) \
	DURATION=$(or $(DURATION),10m) \
	LOADSEED_IMAGE=$(or $(LOADSEED_IMAGE),ghcr.io/hermes-notifications/loadseed:latest) \
	loadtest/scripts/run-k8s.sh

loadtest-k8s-clean: ## Delete the last TestRun and the seed Job
	kubectl -n loadtest delete testrun --all || true
	kubectl -n loadtest delete job loadseed --ignore-not-found
