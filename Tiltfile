load("ext://restart_process", "docker_build_with_restart")
load("ext://helm_remote", "helm_remote")

# --- Config ---
k3d_registry = "k3d-hermes-registry.localhost:5111"

# Detect host architecture for cross-compilation
host_arch = str(local("uname -m", quiet=True)).strip()
goarch = "arm64" if host_arch == "arm64" else "amd64"

services = {
    "admin":         {"port": 8080},
    "router":        {"port": 8081},
    "worker-events": {"port": 8082},
    "worker-email":  {"port": 8083},
    "worker-sms":    {"port": 8084},
    "worker-inbox":  {"port": 8085},
    "inbox":         {"port": 8086},
    "user":          {"port": 8087},
}

# --- Infrastructure from Kustomize local overlay ---
k8s_yaml(kustomize("deploy/k8s/overlays/local"))

k8s_resource("postgres", labels=["infra"], port_forwards=["5432:5432"])
k8s_resource("nats", labels=["infra"], port_forwards=["4222:4222", "8222:8222"])
k8s_resource("redis", labels=["infra"], port_forwards=["6379:6379"])
k8s_resource("centrifugo", labels=["infra"], port_forwards=["8000:8000"],
             resource_deps=["nats", "redis"])

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
    local_resource(
        "compile-" + svc_name,
        cmd="CGO_ENABLED=0 GOOS=linux GOARCH={goarch} go build -o ./bin/{svc}/service ./cmd/{svc}/".format(
            goarch=goarch, svc=svc_name,
        ),
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
        entrypoint=["/service"],
        live_update=[
            sync("./bin/" + svc_name + "/service", "/service"),
        ],
    )

    # Service deployments come from Kustomize; Tilt matches by image name
    k8s_resource(
        "hermes-" + svc_name,
        port_forwards=["{port}:{port}".format(port=port)],
        resource_deps=["compile-" + svc_name],
        labels=["services"],
    )
