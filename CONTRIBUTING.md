# Contributing to Hermes

Thanks for contributing! This page covers the mechanics. For depth, see the docs hub:
[**docs/README.md**](docs/README.md) — especially [Development](docs/development.md),
[Testing](docs/testing.md), and [Architecture](docs/architecture.md).

## Get set up

```bash
# Prerequisites: Go (see go.mod), Docker; for the k8s dev loop: k3d, tilt, kubectl; pnpm for the portal
#
# Only if you are regenerating the SDKs (`make sdk-generate`): a JVM is required.
# @openapitools/openapi-generator-cli (pinned in openapitools.json) is a Node wrapper
# around a Java JAR, so sdk-python, sdk-java and sdk-dotnet all fail without it — and the
# error does not mention Java. sdk-ts-generate needs only Node + pnpm.
#   JDK 21, Maven         — generate, then build/test the Java SDK
#   .NET 8 SDK            — build/test the C# SDK (Makefile pins targetFramework=net8.0)
# e.g. mise use -g java@temurin-21 maven@latest dotnet@8
make dev-up        # full local stack on k3d + Tilt (recommended)
# — or the lighter path used by the test suite —
make infra-up && make migrate && make seed && make build

make hooks         # install the lefthook git hooks (one-time)
```

See [docs/development.md](docs/development.md) for the full local-dev guide.

## Make a change

1. **Branch** off `main` (don't commit to `main` directly).
2. **Follow the codebase grain.** New handlers: add the method to the service's store interface,
   implement it in `internal/store/postgres`, add it to the mock in the package's
   `testutil_test.go`, and test with `httptest`. New services must call
   `internal/observability.Init` in `main.go` and follow the
   [semantic conventions](docs/observability/semantic-conventions.md).
3. **Write tests** at the right tier ([docs/testing.md](docs/testing.md)).
4. **Keep generated artifacts in sync.** If you change a public API, run `make openapi`. If you
   change the NATS contract, update `api/async/asyncapi.yaml` and run `make asyncapi-check`.
5. **Ship docs with the change.** A new alert rule must include a matching runbook in the same PR;
   user-facing or architectural changes should update the relevant doc under `docs/`.

## Before you push

The git hooks (installed via `make hooks`) enforce, automatically:

- **pre-commit** (on `*.go`): `go vet ./...`, `golangci-lint run`, `go build ./...`
- **pre-push**: `make test`; Kustomize renders for the local/staging/production overlays; and,
  when Terraform changed, `terraform fmt -check` + `terraform validate`

Run them on demand with `make hooks-check`. To reproduce what CI will run:

```bash
make lint
make test
make test-integration   # needs make infra-up
make openapi-check
make asyncapi-check
```

## Open a PR

- Keep PRs focused; write a clear description of what and why.
- Make sure CI is green: lint, unit + integration/e2e tests, all-service build, Docker build,
  Helm lint, Kustomize validation, and the spec checks.
- Code style is enforced by `golangci-lint` (`.golangci.yml`) — match the surrounding code.

## Project layout & conventions

See [docs/development.md](docs/development.md#project-layout) for the directory map and
[docs/architecture.md](docs/architecture.md) for the design patterns (per-service store
interfaces, status rollup, channel resolution, the two auth modes). Configuration is via
`HERMES_*` environment variables ([docs/configuration.md](docs/configuration.md)).
