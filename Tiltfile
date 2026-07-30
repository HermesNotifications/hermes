load("ext://restart_process", "docker_build_with_restart")
load("ext://helm_remote", "helm_remote")

config.define_bool("datadog", usage="Enable Datadog Agent and orchestrion instrumentation (requires DD_API_KEY)")
config.define_bool("observability", usage="Enable the OSS observability stack (Prometheus/Loki/Tempo/Grafana/OTel Collector)")
config.define_bool("signoz", usage="Enable SigNoz OTel Collector export to an existing SigNoz endpoint")
cfg = config.parse()
datadog_enabled = cfg.get("datadog", False)
observability_enabled = cfg.get("observability", False)
signoz_enabled = cfg.get("signoz", False)

# --- Config ---
k3d_registry = "k3d-hermes-registry.localhost:5111"

# Detect host architecture for cross-compilation
host_arch = str(local("uname -m", quiet=True)).strip()
goarch = "arm64" if host_arch == "arm64" else "amd64"

services = {
    "admin":         {"port": 8080},
    "dispatch":      {"port": 8081},
    "worker-events": {"port": 8082},
    "worker-email":  {"port": 8083},
    "worker-sms":    {"port": 8084},
    "worker-inbox":  {"port": 8085},
    "inbox":         {"port": 8086},
    "user":          {"port": 8087},
    "send":          {"port": 8088},
}

# --- Infrastructure from Kustomize local overlay ---
k8s_yaml(kustomize("deploy/k8s/overlays/local"))

k8s_resource("postgres", labels=["infra"], port_forwards=["5432:5432"])
k8s_resource("nats", labels=["infra"], port_forwards=["4222:4222", "8222:8222"])
k8s_resource("redis", labels=["infra"], port_forwards=["6379:6379"])
k8s_resource("centrifugo", labels=["infra"], port_forwards=["8000:8000"],
             resource_deps=["nats", "redis"])
k8s_resource("mailpit", labels=["infra"], port_forwards=["8025:8025"])
k8s_resource("dynamodb-local", labels=["infra"], port_forwards=["8001:8000"])

# --- Ingress ---
helm_remote(
    "ingress-nginx",
    repo_name="ingress-nginx",
    repo_url="https://kubernetes.github.io/ingress-nginx",
    namespace="ingress-nginx",
    create_namespace=True,
    set=[
        "controller.hostPort.enabled=true",
        "controller.service.type=NodePort",
        "controller.admissionWebhooks.enabled=false",
    ],
)

# --- Migration ---
# Retry loop handles the race between postgres readiness and port-forward setup
local_resource(
    "migrate",
    cmd=" && ".join([
        "go build -o ./bin/migrate/service ./cmd/migrate/",
        "for i in 1 2 3 4 5 6 7 8 9 10; do " +
        "./bin/migrate/service -database-url 'postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable' " +
        "-migrations-path ./migrations && break || sleep 2; done",
    ]),
    deps=["migrations/", "cmd/migrate/"],
    resource_deps=["postgres"],
    labels=["infra"],
)

# --- Seed ---
local_resource(
    "seed",
    cmd=" && ".join([
        "go build -o ./bin/seed/service ./cmd/seed/",
        "./bin/seed/service -database-url 'postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable'",
    ]),
    deps=["cmd/seed/"],
    resource_deps=["migrate"],
    labels=["infra"],
)

# --- Services ---
for svc_name, svc_cfg in services.items():
    img = "{}/hermes-{}".format(k3d_registry, svc_name)
    port = svc_cfg["port"]

    # Compile Go binary on host (fast, uses module/build cache)
    # Output to bin/<svc>/service so it matches the Dockerfile COPY path
    if datadog_enabled:
        compile_cmd = " && ".join([
            "go build -o ./bin/_tools/orchestrion github.com/DataDog/orchestrion",
            "CGO_ENABLED=0 GOOS=linux GOARCH={goarch} ./bin/_tools/orchestrion go build -o ./bin/{svc}/service ./cmd/{svc}/".format(
                goarch=goarch, svc=svc_name,
            ),
        ])
    else:
        compile_cmd = "CGO_ENABLED=0 GOOS=linux GOARCH={goarch} go build -o ./bin/{svc}/service ./cmd/{svc}/".format(
            goarch=goarch, svc=svc_name,
        )

    local_resource(
        "compile-" + svc_name,
        cmd=compile_cmd,
        deps=[
            "cmd/" + svc_name + "/",
            "internal/",
            "go.mod",
            "go.sum",
        ],
        resource_deps=["migrate"],
        labels=["compile"],
    )

    # Build dev image with live_update: sync binary, restart process
    docker_build_with_restart(
        img,
        context=".",
        dockerfile="deploy/docker/Dockerfile.dev",
        build_args={"SERVICE": svc_name},
        only=["bin/" + svc_name + "/service", "migrations/"],
        entrypoint=["/app/service"],
        live_update=[
            sync("./bin/" + svc_name + "/service", "/app/service"),
        ],
    )

    # Service deployments come from Kustomize; Tilt matches by image name
    k8s_resource(
        "hermes-" + svc_name,
        port_forwards=["{port}:{port}".format(port=port)],
        resource_deps=["compile-" + svc_name],
        labels=["services"],
    )

# --- Cleanup CronJob ---
# Built like a service, but run-to-completion: no port-forward, no live_update,
# and a plain docker_build (the restart wrapper would keep the batch process
# alive and prevent the Job from completing). The CronJob only fires on schedule
# — this just makes a valid image available so scheduled or manually triggered
# runs (`kubectl create job --from=cronjob/hermes-cleanup ...`) don't
# ImagePullBackOff. The overlay sets the container command since Dockerfile.dev
# bakes no ENTRYPOINT.
local_resource(
    "compile-cleanup",
    cmd="CGO_ENABLED=0 GOOS=linux GOARCH={goarch} go build -o ./bin/cleanup/service ./cmd/cleanup/".format(
        goarch=goarch,
    ),
    deps=["cmd/cleanup/", "internal/", "go.mod", "go.sum"],
    resource_deps=["migrate"],
    labels=["compile"],
)
docker_build(
    "{}/hermes-cleanup".format(k3d_registry),
    context=".",
    dockerfile="deploy/docker/Dockerfile.dev",
    build_args={"SERVICE": "cleanup"},
    only=["bin/cleanup/service", "migrations/"],
)
k8s_resource(
    "hermes-cleanup",
    resource_deps=["compile-cleanup"],
    labels=["infra"],
)

# --- Admin Portal (Next.js) ---
local_resource(
    "admin-portal-install",
    # The SDK build is not incidental: the workspace packages are ESM compiled to dist/, and
    # admin resolves @hermes-notifications/server through it. ci-web.yml has carried an explicit
    # "Build workspace SDK dependency" step for this reason; without it here, a fresh clone's
    # first `tilt up` could fail to compile the portal.
    cmd="pnpm install --frozen-lockfile && pnpm --filter './sdks/typescript/packages/*' build",
    deps=["web/admin/package.json", "pnpm-lock.yaml"],
    resource_deps=["seed"],
    labels=["frontend"],
)

local_resource(
    "admin-portal",
    serve_cmd="cd web/admin && pnpm dev --port 3000",
    deps=["web/admin/package.json"],
    resource_deps=["admin-portal-install", "hermes-admin"],
    links=["http://localhost:3000"],
    labels=["frontend"],
)

# --- Inbox demo (examples/) ---
# The demo shares admin-portal-install, which already builds the workspace SDKs.
local_resource(
    "demo-server",
    serve_cmd="scripts/demo-env pnpm --filter @hermes/demo-server dev",
    deps=["examples/demo-server/package.json"],
    # Depends on hermes-send as well as hermes-admin, unlike the admin portal: the demo mints
    # tokens through admin *and* sends test notifications through send.
    resource_deps=["admin-portal-install", "hermes-admin", "hermes-send"],
    links=["http://localhost:8899"],
    labels=["frontend"],
)

local_resource(
    "demo-web",
    serve_cmd="pnpm --filter @hermes/react-demo dev",
    deps=["examples/react-demo/package.json"],
    resource_deps=["demo-server"],
    links=["http://localhost:5173"],
    labels=["frontend"],
)

# --- Datadog (opt-in) ---
if datadog_enabled:
    k8s_yaml(kustomize("deploy/k8s/overlays/local/datadog"))
    k8s_resource("datadog-agent", labels=["infra"])

# --- Observability stack (opt-in) ---
# Enable with: `tilt up -- --observability`
# Brings up Prometheus, Loki, Tempo, Grafana, OTel Collector, Alloy DaemonSet, and infra
# exporters in the `observability` namespace.
#
# The kube-prometheus-stack CRDs are too large for client-side apply: the
# kubectl.kubernetes.io/last-applied-configuration annotation exceeds the 256KB
# object limit, so Tilt falls back to create-or-replace for them. That fallback
# registers the CRDs too late for the Prometheus/Alertmanager custom resources
# applied in the same pass, which then fail with `no matches for kind "Prometheus"`
# and abort the whole apply — leaving everything after that point (including the
# postgres-exporter DSN Secret) uncreated.
#
# Fix: split the render into three phases so the custom resources only apply once
# their CRDs are established.
#   1. observability-crds      — the 10 CustomResourceDefinitions
#   2. plain objects           — workloads, services, configmaps, secrets, webhooks
#                                (none reference the CRDs, so no ordering needed)
#   3. observability-monitors  — every monitoring.coreos.com custom resource,
#                                gated on observability-crds via resource_deps
if observability_enabled:
    observability_overlay = "deploy/observability/overlays/local-signoz" if signoz_enabled else "deploy/observability/overlays/local"
    # `kustomize --enable-helm` is required; recent Tilt versions pass flags after `--`.
    obs_objects = [
        o
        for o in decode_yaml_stream(
            kustomize(observability_overlay, flags=["--enable-helm"])
        )
        if o
    ]
    obs_crds = [o for o in obs_objects if o["kind"] == "CustomResourceDefinition"]
    obs_crd_kinds = [o["spec"]["names"]["kind"] for o in obs_crds]
    obs_crs = [o for o in obs_objects if o["kind"] in obs_crd_kinds]
    obs_plain = [
        o
        for o in obs_objects
        if o["kind"] != "CustomResourceDefinition" and o["kind"] not in obs_crd_kinds
    ]

    # Phase 1: CustomResourceDefinitions.
    k8s_yaml(encode_yaml_stream(obs_crds))
    k8s_resource(
        new_name="observability-crds",
        objects=[
            "{}:customresourcedefinition".format(o["metadata"]["name"]) for o in obs_crds
        ],
        labels=["observability"],
    )

    # Phase 2: everything that does not reference a CRD (includes the
    # postgres-exporter DSN Secret, which previously never applied because the
    # aborted apply stopped before reaching it).
    k8s_yaml(encode_yaml_stream(obs_plain))

    # Phase 3: the monitoring.coreos.com custom resources, gated on the CRDs.
    k8s_yaml(encode_yaml_stream(obs_crs))
    k8s_resource(
        new_name="observability-monitors",
        objects=[
            "{}:{}".format(o["metadata"]["name"], o["kind"].lower()) for o in obs_crs
        ],
        resource_deps=["observability-crds"],
        labels=["observability"],
    )

    # Port-forward Grafana (admin/admin). Prometheus and Alertmanager are
    # operator-managed StatefulSets — port-forward them manually if needed:
    #   kubectl -n observability port-forward svc/kps-prometheus 9090:9090
    #   kubectl -n observability port-forward svc/kps-alertmanager 9093:9093
    #
    # Grafana is a subchart of kube-prometheus-stack so it doesn't pick up the
    # fullnameOverride — the resource name stays the chart default.
    k8s_resource(
        "kube-prometheus-stack-grafana",
        port_forwards=["3001:3000"],
        labels=["observability"],
    )
    k8s_resource("otel-collector-opentelemetry-collector", labels=["observability"])
    k8s_resource("loki", labels=["observability"])
    k8s_resource("tempo", labels=["observability"])
    k8s_resource("alloy", labels=["observability"])
    k8s_resource("nats-exporter", labels=["observability"])
    k8s_resource("postgres-exporter", labels=["observability"])
    k8s_resource("redis-exporter", labels=["observability"])
