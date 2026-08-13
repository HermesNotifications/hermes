# --- Variables ---
# natsprovision is a Job like migrate, not a long-running service: ADR 0005 phase 4 made it
# the only identity that may declare JetStream streams.
SERVICES := admin send dispatch worker-events worker-email worker-sms worker-inbox inbox user migrate natsprovision seed cleanup

# Where migrate/seed/cleanup point.
#
# HERMES_DATABASE_URL wins when it is set, which is what makes these targets follow a sandbox
# (`eval "$(make devworker-env)"`) instead of the shared localhost:5432. Before this, DB_URL was
# an unconditional `:=`, so `make migrate` silently ignored the sandbox you had just sourced and
# migrated whatever Docker Compose stack happened to own 5432 -- another worktree's, if one was
# up. That is not hypothetical: it is how migration 000020 first landed in the wrong database.
#
# Still overridable on the command line (`make migrate DB_URL=...`), because `?=` yields to an
# explicit assignment.
DB_URL  ?= $(if $(HERMES_DATABASE_URL),$(HERMES_DATABASE_URL),postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable)

# The manifest gates in scripts/ parse rendered YAML, so every one of them needs PyYAML.
# Nothing used to declare that. On a machine without it each gate printed SKIP and exited 0 --
# six controls off at once, inside a target whose whole job is catching controls that are
# present and do nothing. CI passed for an unrelated reason: GitHub's ubuntu-latest image
# happens to ship PyYAML for the system python3, so a runner image change would have turned
# every gate off with no failing step. The gates now exit non-zero when it is missing, and
# this venv is what stops that being a daily annoyance.
VENV   := .venv
PYTHON := $(VENV)/bin/python

# --- Which cluster local-dev targets talk to ---
#
# Pinned, not inherited. Every kubectl invocation below used to follow whatever
# `kubectl config current-context` happened to be, which is a footgun rather than a
# convenience: the context is global machine state that any other terminal, any other
# repository, or any other agent can change, and nothing in a `make devworker-up` prompt tells
# you where it is about to deploy.
#
# The failure mode is not theoretical. A full local stack -- fourteen Deployments, a NATS
# StatefulSet, two Ingresses -- was once applied to a shared remote cluster because the context
# was left on it, and the migration and seed steps ran against that cluster's database. Nothing
# in the output looked wrong, because nothing in the output mentioned a cluster at all.
#
# LOADTEST TARGETS ARE DELIBERATELY EXCLUDED. `make loadtest-*` is meant to run against a real
# remote cluster, so it keeps using the ambient context on purpose.
#
# Override for a deliberate exception:  make stack-up KUBE_CONTEXT=kind-helix
# Both are recursively expanded (`=`, not `:=`) because CLUSTER_NAME is defined further down, in
# the k3d section. With `:=` this would freeze here as `kubectl --context k3d-` and every target
# would fail with `context "k3d-" does not exist`.
KUBE_CONTEXT ?= k3d-$(CLUSTER_NAME)
KUBECTL       = kubectl --context $(KUBE_CONTEXT)

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
#
# `license-check` IS part of it. It is enforced by the ci.yml Lint job, so a tree
# that passes `verify` while failing the header policy is a green local gate that
# fails CI -- which is exactly what happened on PR #96, where a new file carried
# the pre-SPDX header and nothing local said so. The pre-commit hook covers the
# same ground, but hooks are opt-in (`make hooks`) and this target is what agent
# work and CONTRIBUTING both point at.
# Provisions the interpreter every manifest gate runs on. Depending on requirements.txt
# rather than order-only is deliberate: a pinned bump must rebuild the venv, and `touch`
# updates the directory mtime make compares against.
$(VENV): scripts/requirements.txt
	python3 -m venv $(VENV)
	$(VENV)/bin/pip install --quiet --disable-pip-version-check -r scripts/requirements.txt
	@touch $(VENV)

.PHONY: adr-new
adr-new: $(VENV)   ## Scaffold an ADR with a number free locally, on main, and in open PRs
	@test -n "$(TITLE)" || { echo 'usage: make adr-new TITLE="Short imperative title"'; exit 2; }
	$(PYTHON) scripts/adr_new.py "$(TITLE)"

.PHONY: adr-next
adr-next: $(VENV)  ## Print the next free ADR number without creating anything
	@$(PYTHON) scripts/adr_new.py --number-only

.PHONY: verify verify-manifests
verify:            ## Full local verification gate (no infra needed)
	go build ./...
	go test ./... -count=1
	golangci-lint run
	$(MAKE) license-check
	$(MAKE) verify-manifests
verify-manifests: $(VENV)  ## Static validation of k8s overlays, Crossplane and CI YAML
	kubectl kustomize deploy/k8s/overlays/local > /dev/null
	kubectl kustomize deploy/k8s/overlays/staging > /dev/null
	kubectl kustomize deploy/k8s/overlays/production > /dev/null
	$(PYTHON) -c "import yaml, glob; [list(yaml.safe_load_all(open(p))) for p in glob.glob('infra/crossplane/**/*.yaml', recursive=True) + glob.glob('deploy/kargo/**/*.yaml', recursive=True) + glob.glob('.github/workflows/*.yml')]"
	@# Finding 47: a NetworkPolicy whose podSelector matches nothing is silently inert.
	@# kustomize build and kubectl apply both accept it, so only this catches it.
	$(PYTHON) -m unittest discover -s scripts -p 'test_*.py' -t scripts
	@# An ADR number is allocated at authoring time by writers who cannot see each other, so
	@# two long-lived branches each add "the next one" and both are right until they meet.
	@# PR #73 carried an 0010 against main's different 0010, and a branch stacked on it added
	@# an 0011 and 0012 against main's own -- three collisions, found by reading.
	$(PYTHON) scripts/check_adr_numbering.py
	@# The rules files have said "CI check enforces this pairing" in capitals since they were
	@# written, and nothing did. A runbook_url is followed by whoever is paged, at 3am, and a
	@# 404 there costs exactly the minutes the annotation exists to save.
	$(PYTHON) scripts/check_runbook_links.py
	kubectl kustomize deploy/k8s/overlays/staging | $(PYTHON) scripts/check_networkpolicy_selectors.py -
	kubectl kustomize deploy/k8s/overlays/production | $(PYTHON) scripts/check_networkpolicy_selectors.py -
	@# The route gate ran over the Helm chart only, and the overlays are what deploy staging
	@# and production. They had drifted to 7 of base's 12 rules -- /v1/templates, /v1/apikeys,
	@# /v1/organizations and /v1/subscriptions unreachable in both environments, and /v1/users
	@# pointed at the user service instead of admin -- because a strategic-merge patch adding
	@# a `host` has to restate the whole atomic rules list. Rendering perfectly is exactly
	@# what made it survive.
	@#
	@# The deployed overlays get every check, including images: hermes-natsprovision and
	@# hermes-cleanup rendered as bare names for want of a Kargo entry, which resolves to
	@# docker.io/library/<name>:latest and cannot pull. Local is the exception and only gets
	@# the routing checks -- Tilt builds its images (so they are untagged by design) and runs
	@# stream provisioning as a local_resource instead of the in-cluster Job, both of which
	@# overlays/local/kustomization.yaml patches out deliberately.
	kubectl kustomize deploy/k8s/overlays/local | $(PYTHON) scripts/check_helm_render.py - --source-root=. --only=routes,rewrites
	kubectl kustomize deploy/k8s/overlays/staging | $(PYTHON) scripts/check_helm_render.py - --source-root=.
	kubectl kustomize deploy/k8s/overlays/production | $(PYTHON) scripts/check_helm_render.py - --source-root=.
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
	@# Centrifugo 403s any websocket whose Origin is not listed, but permits connections with
	@# no Origin at all -- so /health, curl and every server-side client succeed while no
	@# browser can connect. The local overlay shipped with no allowed_origins and the first
	@# live browser run spent 45 minutes failing 24 specs to say only "connecting".
	@# All three overlays: this is the one control whose absence is invisible everywhere else.
	kubectl kustomize deploy/k8s/overlays/local | $(PYTHON) scripts/check_centrifugo_origins.py -
	kubectl kustomize deploy/k8s/overlays/staging | $(PYTHON) scripts/check_centrifugo_origins.py -
	kubectl kustomize deploy/k8s/overlays/production | $(PYTHON) scripts/check_centrifugo_origins.py -
	@# The memory engine gives each Centrifugo node its own subscription registry, so above one
	@# replica a publication reaches only the clients on the node that received it -- silently,
	@# with nothing in any log or health check to say so. production.md has said this in prose
	@# since the chart shipped; prose does not fail a render.
	kubectl kustomize deploy/k8s/overlays/local | $(PYTHON) scripts/check_centrifugo_engine.py -
	kubectl kustomize deploy/k8s/overlays/staging | $(PYTHON) scripts/check_centrifugo_engine.py -
	kubectl kustomize deploy/k8s/overlays/production | $(PYTHON) scripts/check_centrifugo_engine.py -
	@# JetStream defaults to one replica when Replicas is unset, which is what every stream ran
	@# with on a three-node cluster: `nats stream ls` looks healthy, every publish succeeds, and
	@# the first evidence is a node going away and taking NOTIFICATIONS or DELIVERY with it.
	@# All three overlays -- staging and local are the single-node cases the gate must also pass.
	kubectl kustomize deploy/k8s/overlays/local | $(PYTHON) scripts/check_nats_stream_replicas.py -
	kubectl kustomize deploy/k8s/overlays/staging | $(PYTHON) scripts/check_nats_stream_replicas.py -
	kubectl kustomize deploy/k8s/overlays/production | $(PYTHON) scripts/check_nats_stream_replicas.py -
	@# Every Hermes image is FROM scratch, so preStop.exec cannot run `sleep` and the drain
	@# delay lives in-process instead. That splits one budget across a manifest and an env var,
	@# which is precisely the pairing that drifts: exceed terminationGracePeriodSeconds and the
	@# kubelet SIGKILLs mid-drain, which is strictly worse than not draining at all.
	kubectl kustomize deploy/k8s/overlays/staging | $(PYTHON) scripts/check_shutdown_budget.py -
	kubectl kustomize deploy/k8s/overlays/production | $(PYTHON) scripts/check_shutdown_budget.py -
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
	@# Rendered as release `hermes`, matching the documented install. Not arbitrary: the
	@# bundled Centrifugo reads two secrets from `<release>-hermes-secrets` by a name written
	@# out in values.yaml, because a parent chart cannot template a sub-chart's values. A
	@# different release name is refused at render time rather than silently losing realtime.
	@# Absent helm must fail loudly. A gate that skips when its tool is missing is the
	@# same class of defect as the one this target exists to close.
	@command -v helm >/dev/null 2>&1 || { \
	  echo "ERROR: helm not found on PATH."; \
	  echo "  The Helm chart gate cannot run without it, and skipping it is how the chart"; \
	  echo "  drifted in the first place. Install Helm v3:"; \
	  echo "    https://helm.sh/docs/intro/install/"; \
	  exit 1; }
	helm dependency build charts/hermes/
	helm template hermes charts/hermes/ \
	  --set hermes.jwt.secret=verify --set hermes.apiKey.hmacSecret=verify \
	  --set global.domain=verify.example.com \
	  | $(PYTHON) scripts/check_helm_render.py - --source-root=.
	@# The same two gates the kustomize overlays get. Running them only over kustomize is
	@# exactly how the chart drifted into shipping no PDBs, no grace periods and empty
	@# resource requests while the overlays had all three.
	helm template hermes charts/hermes/ \
	  --set hermes.jwt.secret=verify --set hermes.apiKey.hmacSecret=verify \
	  --set global.domain=verify.example.com \
	  | $(PYTHON) scripts/check_workload_resources.py - --skip=nats,centrifugo,postgresql,redis
	helm template hermes charts/hermes/ \
	  --set hermes.jwt.secret=verify --set hermes.apiKey.hmacSecret=verify \
	  --set global.domain=verify.example.com \
	  | $(PYTHON) scripts/check_shutdown_budget.py -
	@# Optional features render into workloads the default install never produces, so they
	@# need their own pass -- hermes-cleanup was missing from the cd.yml publish matrix and
	@# only shows up when the CronJob renders.
	helm template hermes charts/hermes/ \
	  --set hermes.jwt.secret=verify --set hermes.apiKey.hmacSecret=verify \
	  --set global.domain=verify.example.com \
	  --set hermes.cleanup.enabled=true --set networkPolicy.enabled=true \
	  --set observability.enabled=true \
	  | $(PYTHON) scripts/check_helm_render.py - --source-root=.
	@# Traefik renders a different realtime route entirely -- a stripPrefix Middleware and a
	@# plain prefix, because Traefik v3 removed regex from Ingress paths and the nginx form
	@# matches nothing there. Checking only the nginx render would leave half the
	@# self-hosting world (every k3s cluster) on a silently dead websocket endpoint.
	helm template hermes charts/hermes/ \
	  --set hermes.jwt.secret=verify --set hermes.apiKey.hmacSecret=verify \
	  --set global.domain=verify.example.com \
	  --set ingress.className=traefik \
	  | $(PYTHON) scripts/check_helm_render.py - --source-root=.
	@# Production on PLAINTEXT bundled datastores must still be refused: the URLs are built as
	@# ?sslmode=disable, redis:// and nats://, which every workload rejects at startup. This
	@# used to assert that production+bundled ALWAYS failed; it now asserts the narrower and
	@# still-true thing, because TLS made the combination legal.
	@if helm template hermes charts/hermes/ \
	     --set hermes.jwt.secret=verify --set hermes.apiKey.hmacSecret=verify \
	     --set global.domain=verify.example.com --set hermes.env=production >/dev/null 2>&1; then \
	  echo "ERROR: hermes.env=production rendered with plaintext bundled datastores."; \
	  echo "  It must fail: _validate.tpl should refuse a combination whose workloads all"; \
	  echo "  exit at startup on config.Validate()."; \
	  exit 1; \
	fi
	@echo "ok: production on plaintext bundled datastores is refused at render time"
	@# And the other direction, which is the point of the feature: production WITH TLS must
	@# render, and must survive the same gates as every other posture. A capability nothing
	@# exercises is a capability that rots.
	helm template hermes charts/hermes/ -f charts/hermes/values-production-bundled.yaml \
	  --set hermes.jwt.secret=verify --set hermes.apiKey.hmacSecret=verify \
	  --set hermes.centrifugo.apiKey=verify-centrifugo-key \
	  --set global.domain=verify.example.com \
	  --set tls.issuer.name=verify-ca \
	  --set externalCentrifugo.apiUrl=https://centrifugo.verify.example.com \
	  | $(PYTHON) scripts/check_helm_render.py - --source-root=.
	@# The bundled bus needs sub-chart values the parent cannot template, so a mismatched
	@# secretName must be refused rather than producing a NATS cluster serving plaintext while
	@# the ConfigMap advertises tls://.
	@if helm template hermes charts/hermes/ -f charts/hermes/values-production-bundled.yaml \
	     --set hermes.jwt.secret=verify --set hermes.apiKey.hmacSecret=verify \
	     --set hermes.centrifugo.apiKey=verify-centrifugo-key \
	     --set global.domain=verify.example.com --set tls.issuer.name=verify-ca \
	     --set externalCentrifugo.apiUrl=https://c.example.com \
	     --set nats.tlsCA.secretName=wrong >/dev/null 2>&1; then \
	  echo "ERROR: a NATS TLS secretName that names no certificate rendered cleanly."; \
	  exit 1; \
	fi
	@echo "ok: production on TLS bundled datastores renders, and a mismatched NATS secret is refused"
	@# No hermes-admin-portal image exists and nothing here can build one, so enabling it
	@# on chart defaults must be refused rather than deferred to ImagePullBackOff.
	@if helm template hermes charts/hermes/ \
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
.PHONY: sdk-ts-generate sdk-ts-build sdk-ts-typecheck sdk-ts-test sdk-generate
sdk-ts-generate:   ## Generate TypeScript types from OpenAPI specs
	pnpm --filter @hermes-notifications/server generate
	pnpm --filter @hermes-notifications/client generate
# All four packages, not just server+client. The narrower version is why a `group_id` ->
# `category_id` rename once left hermes-react and hermes-web unable to build at all while
# `make` reported success — only ci-web.yml noticed.
sdk-ts-build:      ## Build TypeScript SDKs
	pnpm --filter "./sdks/typescript/packages/*" build
sdk-ts-typecheck:  ## Typecheck the TypeScript SDKs, tests included
	pnpm --filter "./sdks/typescript/packages/*" --parallel run --if-present typecheck
sdk-ts-test:       ## Run the TypeScript SDK unit tests
	pnpm --filter "./sdks/typescript/packages/*" --parallel run --if-present test
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

# Fail before touching anything if the pinned context is missing.
#
# Without this the first call reports `error: no context exists with the name "k3d-hermes-dev"`,
# which is accurate but reads as a kubectl problem rather than "your local cluster is not
# running". Say the actual next step instead.
.PHONY: require-local-context
require-local-context:
	@kubectl config get-contexts -o name 2>/dev/null | grep -qx '$(KUBE_CONTEXT)' || { \
		echo "No kubectl context named '$(KUBE_CONTEXT)'."; \
		echo; \
		echo "Local-dev targets are pinned to the k3d cluster and will not follow your current"; \
		echo "context, so they cannot deploy to a remote cluster by accident."; \
		echo; \
		echo "  make dev-up                       create/start the local cluster"; \
		echo "  make <target> KUBE_CONTEXT=name   aim somewhere else on purpose"; \
		exit 1; \
	}

# --- Parallel development sandboxes (one namespace per worker) ---
#
# For running several agents or developers against one k3s cluster without them
# colliding. Each worker gets its own namespace holding Postgres, Redis, NATS with
# JetStream, and Mailpit. See docs/development.md.
#
# WORKER defaults to the current directory's name, which in a git worktree is the worktree's
# own name -- so every worktree gets its own sandbox with nothing to remember and no flag to
# pass. That default is the point: a name you have to supply is a name an agent will forget,
# and the failure mode of forgetting is silently sharing a database with someone else.
#
# In the main checkout this yields the repository directory name (`hermes`), which is a fine
# stable sandbox for whoever is working there. Pass WORKER explicitly to override.
#
# Lowercased and cleaned because a namespace must be a DNS-1123 label: lowercase alphanumerics
# and dashes only. Worktree names like `Inbox_Polish` would otherwise produce a namespace the
# API server rejects, several steps later, with an error that does not mention the worktree.
WORKER ?= $(shell basename "$(CURDIR)" | tr '[:upper:]_' '[:lower:]-' | tr -cd 'a-z0-9-' | cut -c1-40)
DEVWORKER_NS := hermes-dev-$(WORKER)

.PHONY: devworker-up devworker-down devworker-list devworker-env
devworker-up: require-local-context      ## Create an isolated dev sandbox namespace (WORKER=name)
	@$(KUBECTL) create namespace $(DEVWORKER_NS) --dry-run=client -o yaml | $(KUBECTL) apply -f - >/dev/null
	@# Labelled so devworker-list can find sandboxes without pattern-matching names, and
	@# so a stray sandbox is identifiable as disposable rather than someone's real work.
	@$(KUBECTL) label namespace $(DEVWORKER_NS) hermes.io/devworker=true --overwrite >/dev/null
	@# deploy/k8s/devworker deliberately sets no `namespace:` anywhere, so `-n` places
	@# everything. That is what makes this parallel-safe: no shared file is edited, and
	@# no throwaway overlay is needed (kustomize also rejects absolute `resources` paths).
	kubectl kustomize deploy/k8s/devworker | kubectl apply -n $(DEVWORKER_NS) -f -
	@echo "waiting for $(DEVWORKER_NS) to become ready..."
	@$(KUBECTL) -n $(DEVWORKER_NS) wait --for=condition=Ready pod --all --timeout=180s
	@$(MAKE) --no-print-directory devworker-env WORKER=$(WORKER)

devworker-down: require-local-context    ## Delete a dev sandbox namespace and its volumes (WORKER=name)
	$(KUBECTL) delete namespace $(DEVWORKER_NS) --wait=false

devworker-list: require-local-context    ## List all dev sandbox namespaces
	@$(KUBECTL) get ns -l hermes.io/devworker=true --no-headers 2>/dev/null || \
		$(KUBECTL) get ns --no-headers | grep '^hermes-dev-' || echo "no sandboxes"

devworker-env: require-local-context     ## Print eval-able env vars pointing at a sandbox (WORKER=name)
	@# Emits ClusterIPs, not .svc DNS names. Cluster DNS does not resolve from the host,
	@# but on a k3s node the host CAN route to both service and pod CIDRs — verified. So
	@# these URLs work directly from a shell, with no port-forward.
	@#
	@# Node-local by nature: these addresses are only reachable from this machine.
	@set -e; ns=$(DEVWORKER_NS); \
	ip() { $(KUBECTL) -n $$ns get svc $$1 -o jsonpath='{.spec.clusterIP}'; }; \
	nats_ip() { $(KUBECTL) -n $$ns get pod nats-0 -o jsonpath='{.status.podIP}'; }; \
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

# --- Full-stack sandboxes (services as well as infrastructure) ---
#
# devworker-* above gives you infrastructure only -- enough for `go test -tags=integration`,
# which is most work, and cheap. This gives you the whole thing: the nine services, Centrifugo
# and an ingress, in your own namespace, so several agents can run the demo and the browser
# suite at once.
#
# The two differ in cost, so they are separate targets rather than one with a flag. Do not run
# both for the same WORKER: they place services of the same name in the same namespace.
#
# Deliberately NOT Tilt. `make dev-up` port-forwards 5432, 4222, 6379, 8000, 8025 and 8001 to
# the host, so a second Tilt cannot start -- the port-forwards are themselves a contention
# point. A sandbox publishes nothing to the host: HTTP arrives through the one shared ingress on
# :8888, routed by hostname, and anything else is reached on a ClusterIP. The cost is no live
# reload; re-run `make images-push stack-restart` after a code change.
SANDBOX_HOST := $(WORKER).127.0.0.1.nip.io
SANDBOX_URL  := http://$(SANDBOX_HOST):8888
SANDBOX_DIR  := .hermes/sandbox/$(WORKER)

.PHONY: stack-up stack-down stack-list stack-env stack-url stack-restart stack-render
stack-up: require-local-context          ## Bring up a full per-worker stack (WORKER=name); run make images-push first
	@command -v kubectl >/dev/null || { echo "kubectl not found"; exit 1; }
	@$(KUBECTL) create namespace $(DEVWORKER_NS) --dry-run=client -o yaml | $(KUBECTL) apply -f - >/dev/null
	@$(KUBECTL) label namespace $(DEVWORKER_NS) hermes.io/devworker=true --overwrite >/dev/null
	@# Regenerated every time, so a rename or a change to the local overlay cannot leave a
	@# stale overlay behind quietly producing last week's topology.
	@scripts/sandbox-overlay $(WORKER) $(DEVWORKER_NS) $(SANDBOX_HOST) $(SANDBOX_TAG) >/dev/null
	$(KUBECTL) apply -k $(SANDBOX_DIR)
	@echo "waiting for infrastructure..."
	@$(KUBECTL) -n $(DEVWORKER_NS) rollout status deploy/postgres --timeout=180s
	@$(KUBECTL) -n $(DEVWORKER_NS) rollout status deploy/redis --timeout=180s
	@$(KUBECTL) -n $(DEVWORKER_NS) rollout status sts/nats --timeout=180s
	@# migrate and natsprovision run from the host against ClusterIPs, because the local
	@# overlay deletes their in-cluster Jobs -- Tilt runs them as local_resources and a sandbox
	@# has to do the same job somewhere. Without natsprovision the streams do not exist and
	@# every worker sits idle with no error worth reading.
	@$(MAKE) --no-print-directory stack-bootstrap WORKER=$(WORKER)
	@echo "waiting for services..."
	@$(KUBECTL) -n $(DEVWORKER_NS) wait --for=condition=Available deploy --all --timeout=300s
	@$(MAKE) --no-print-directory stack-url WORKER=$(WORKER)

.PHONY: stack-bootstrap
stack-bootstrap: require-local-context   ## Run migrations, NATS stream provisioning and the dev API key seed
	@scripts/sandbox-bootstrap $(DEVWORKER_NS) $(KUBE_CONTEXT)

stack-restart: require-local-context     ## Roll every service after pushing new images (WORKER=name)
	$(KUBECTL) -n $(DEVWORKER_NS) rollout restart deploy -l app.kubernetes.io/part-of=hermes
	@$(KUBECTL) -n $(DEVWORKER_NS) wait --for=condition=Available deploy --all --timeout=300s

stack-down: require-local-context        ## Delete a full stack and its volumes (WORKER=name)
	$(KUBECTL) delete namespace $(DEVWORKER_NS) --wait=false
	@rm -rf $(SANDBOX_DIR)

stack-list: require-local-context        ## List sandboxes and the URL each answers on
	@$(KUBECTL) get ns -l hermes.io/devworker=true -o name 2>/dev/null | sed 's|namespace/hermes-dev-||' | \
		while read -r w; do echo "$$w  ->  http://$$w.127.0.0.1.nip.io:8888"; done || echo "no sandboxes"

stack-url:         ## Print the base URL for a sandbox (WORKER=name)
	@echo "$(SANDBOX_URL)"
	@echo "  api      $(SANDBOX_URL)/v1"
	@echo "  realtime $(SANDBOX_URL)/realtime"
	@echo
	@# Not `make dev-demo WORKER=...`: scripts/demo-env does not read WORKER, and defaulting it
	@# to a sandbox would be wrong for everyone pointing the demo at the shared stack. The eval
	@# is what redirects it, and it is also what the browser suite needs.
	@echo "  point the demo and the browser suite at it with:"
	@echo '    eval "$$(make stack-env WORKER=$(WORKER))"'
	@echo "    make dev-demo      # or: make demo-e2e"

stack-render:      ## Render a sandbox's manifests without applying (WORKER=name)
	@scripts/sandbox-overlay $(WORKER) $(DEVWORKER_NS) $(SANDBOX_HOST) $(SANDBOX_TAG) >/dev/null
	@kubectl kustomize $(SANDBOX_DIR)

stack-env: require-local-context         ## Print eval-able env vars for a full stack (WORKER=name)
	@$(MAKE) --no-print-directory devworker-env WORKER=$(WORKER)
	@echo "export HERMES_API_URL='$(SANDBOX_URL)'"
	@echo "export HERMES_SOCKET_URL='$(SANDBOX_URL)/realtime'"

# --- Infrastructure ---
.PHONY: infra-up infra-down migrate seed
infra-up:          ## Start local Postgres, NATS, Redis via Docker Compose (ports offset per worktree)
	@scripts/compose-env >/dev/null
	docker compose up -d
	@echo
	@echo "Host ports are offset for this worktree. Point your tools at them with:"
	@echo '  eval "$$(scripts/compose-env)"'
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

# --- Inbox demo (examples/) ---
.PHONY: demo-install dev-demo demo-check demo-e2e demo-e2e-install demo-e2e-ui demo-e2e-full
# Builds the SDKs as well as installing: the packages are ESM compiled to dist/, so the demo
# cannot resolve them from source. ci-web.yml carries the same step for the admin portal.
demo-install:      ## Install demo deps and build the workspace SDKs
	pnpm install
	pnpm --filter "./sdks/typescript/packages/*" build
dev-demo:          ## Start the inbox demo (app on :5173, token server on :8899)
	scripts/demo-env sh -c 'pnpm --filter @hermes/demo-server dev & pnpm --filter @hermes/react-demo dev; kill %1'
demo-check:        ## Typecheck, test and build the demo packages (no cluster needed)
	pnpm --filter @hermes/demo-server run typecheck
	pnpm --filter @hermes/demo-server test
	pnpm --filter @hermes/react-demo run typecheck
	pnpm --filter @hermes/react-demo test
	pnpm --filter @hermes/react-demo build
	pnpm --filter @hermes/browser-tests run typecheck
	pnpm --filter @hermes/browser-tests run test:list
demo-e2e-install:  ## Install the Playwright browser (one-time)
	pnpm --filter @hermes/browser-tests run install-browsers
demo-e2e:          ## Run the live browser E2E suite (requires make dev-up)
	scripts/demo-env pnpm --filter @hermes/browser-tests test
demo-e2e-ui:       ## Open the Playwright UI runner against the live stack
	scripts/demo-env pnpm --filter @hermes/browser-tests run test:ui
demo-e2e-full:     ## Bring up the cluster, run the live E2E suite, tear it down
	$(MAKE) dev-up-ci
	$(MAKE) demo-e2e-install
	$(MAKE) demo-e2e; status=$$?; $(MAKE) dev-down; exit $$status

# --- Docker ---
.PHONY: docker-%
docker-%:          ## Build Docker image for a service (e.g. make docker-admin)
	docker build --build-arg SERVICE=$* -t hermes-$* -f deploy/docker/Dockerfile .

# --- Sandbox images ---
#
# Services that get an image in a sandbox: the long-running ones plus cleanup, which the
# overlay's CronJob references. migrate, natsprovision and seed are deliberately absent -- a
# sandbox runs those from the host through an ephemeral port-forward, exactly as Tilt runs them
# as local_resources, so there is no image to build and nothing to wait on in-cluster.
IMAGE_SERVICES := admin send dispatch worker-events worker-email worker-sms worker-inbox inbox user cleanup

# `:sandbox` rather than `:latest`, and that is load-bearing rather than cosmetic. Nothing in
# these manifests sets imagePullPolicy, so Kubernetes infers it from the tag: `latest` implies
# Always, which would send the node to a registry for an image that only exists in its own
# content store. Any other tag implies IfNotPresent, which is what makes an imported image work.
SANDBOX_TAG := sandbox

# The k3d node is Linux even though the host is not, so these binaries must be cross-compiled.
# `make build-%` targets the host and its output would die in the container with "exec format
# error" -- a failure that surfaces as CrashLoopBackOff with no useful log line. GOARCH follows
# the host so an Apple Silicon machine does not run its whole stack under emulation.
SANDBOX_GOARCH := $(if $(filter arm64,$(shell uname -m)),arm64,amd64)

.PHONY: images-sandbox images-sandbox-%
images-sandbox:    ## Build service images and import them into the k3d cluster
	@$(MAKE) --no-print-directory $(addprefix images-sandbox-,$(IMAGE_SERVICES))
	@# One import for all of them: k3d tars and loads into each node, and paying that startup
	@# cost ten times is most of the wall clock.
	@#
	@# Imported rather than pushed to the k3d registry, because there is no name that works on
	@# both sides here. The node's registries.yaml mirrors `k3d-hermes-registry:5111`, but from
	@# the host that name resolves through the LAN's DNS search domain to an unrelated machine --
	@# so a push would go somewhere else entirely and the pull would still fail. Tilt sidesteps
	@# this by rewriting every image reference itself; without Tilt, importing is the honest way.
	k3d image import $(addprefix hermes-,$(addsuffix :$(SANDBOX_TAG),$(IMAGE_SERVICES))) -c $(CLUSTER_NAME)

images-sandbox-%:  ## Build one sandbox image (e.g. make images-sandbox-admin)
	@# Dockerfile.dev, not Dockerfile: it copies a host-compiled binary instead of running a
	@# full orchestrion build per service inside Docker. Ten of the latter is minutes; this is
	@# seconds, and it reuses the Go build cache. Same trade Tilt makes.
	@#
	@# Plain `go build`, not orchestrion: a sandbox is for behaviour, and orchestrion roughly
	@# doubles compile time to add Datadog instrumentation nothing here reads.
	CGO_ENABLED=0 GOOS=linux GOARCH=$(SANDBOX_GOARCH) go build -o bin/$*/service ./cmd/$*/
	docker build -q --build-arg SERVICE=$* -t hermes-$*:$(SANDBOX_TAG) -f deploy/docker/Dockerfile.dev .

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
	@$(KUBECTL) get pods -n hermes -o wide 2>/dev/null || echo "No pods found"
	@echo ""
	@echo "=== Services ==="
	@$(KUBECTL) get svc -n hermes 2>/dev/null || echo "No services found"

## Tail logs for a service (usage: make dev-logs SERVICE=admin)
dev-logs:
	@test -n "$(SERVICE)" || { echo "Usage: make dev-logs SERVICE=admin"; exit 1; }
	$(KUBECTL) logs -n hermes -l app=hermes-$(SERVICE) -f --tail=100

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
