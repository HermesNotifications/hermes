load("ext://restart_process", "docker_build_with_restart")
load("ext://helm_remote", "helm_remote")

config.define_bool("datadog", usage="Enable Datadog Agent and orchestrion instrumentation (requires DD_API_KEY)")
config.define_bool("observability", usage="Enable the OSS observability stack (Prometheus/Loki/Tempo/Grafana/OTel Collector)")
cfg = config.parse()
datadog_enabled = cfg.get("datadog", False)
observability_enabled = cfg.get("observability", False)

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

# --- Admin Portal (Next.js) ---
local_resource(
    "admin-portal-install",
    cmd="pnpm install --frozen-lockfile",
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

# --- Datadog (opt-in) ---
if datadog_enabled:
    k8s_yaml(kustomize("deploy/k8s/overlays/local/datadog"))
    k8s_resource("datadog-agent", labels=["infra"])

# --- Observability stack (opt-in) ---
# Enable with: `tilt up -- --observability`
# Brings up Prometheus, Loki, Tempo, Grafana, OTel Collector, Alloy DaemonSet, and infra
# exporters in the `observability` namespace. kube-prometheus-stack CRDs must be installed
# before anything that references them (PrometheusRule, ServiceMonitor) — Tilt handles the
# ordering via resource_deps.
if observability_enabled:
    # `kustomize --enable-helm` is required; recent Tilt versions pass flags after `--`.
    k8s_yaml(kustomize("deploy/observability/overlays/local", flags=["--enable-helm"]))
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
