# Testing

Hermes has three test tiers, separated by Go build tags and what infrastructure they need.

| Tier | Build tag | Infra needed | Command |
|---|---|---|---|
| Unit | _(none)_ | none | `make test` |
| Integration | `integration` | Postgres, NATS, Redis | `make test-integration` |
| End-to-end | `integration` | Postgres, NATS, Redis | `make test-e2e` |

```bash
make test              # go test ./... -count=1
make test-integration  # go test ./... -tags=integration -race -timeout=120s -count=1
make test-e2e          # go test ./tests/e2e/... -tags=integration -v -timeout=30s
```

Integration and e2e tests need infrastructure running — start it with `make infra-up`
(see [development.md](development.md)).

## Unit tests

Plain `*_test.go` files with no build tag. They run without infrastructure by depending on each
service's **store interface** and substituting a mock. Handlers are exercised with
`net/http/httptest`.

The pattern: each service package declares the slice of persistence it needs as an interface
(e.g. `AdminStore`, `InboxStore`, `UserStore`), and the service's `testutil_test.go` provides a
mock implementation. To test a new handler, add the method to the interface, add a field/stub to
the mock, and drive the handler with an `httptest` request/recorder.

```bash
go test ./internal/auth -v                                   # one package
go test -tags=integration ./internal/store/... -run TestCreateCategory_And_GetBySlug -v   # one test
# The -tags=integration is required: store tests are behind //go:build integration, so
# without it the command matches nothing and exits 0, which looks like a pass.
```

## Integration tests

Guarded by `//go:build integration`. They run against **real** Postgres, NATS, and Redis — store
CRUD, cache behavior, and NATS publish/subscribe. Test helpers in
`internal/store/postgres/testutil_test.go` run migrations, open a pool, register cleanup, and
truncate tables for isolation:

```go
//go:build integration

func TestCreateSomething(t *testing.T) {
    s, pool := testStore(t)          // runs migrations, opens pool, t.Cleanup closes it
    cleanTable(t, pool, "users")     // TRUNCATE … CASCADE for isolation
    // ... exercise s against real Postgres
}
```

Connection targets come from the standard `HERMES_DATABASE_URL` / `HERMES_NATS_URL` /
`HERMES_REDIS_URL` variables ([configuration.md](configuration.md)), defaulting to the local
Docker Compose endpoints.

## End-to-end tests

In `tests/e2e/`, also under the `integration` tag. They wire several services together in-process
against real infrastructure and drive the full pipeline — send → dispatch → workers → events —
verifying the notification reaches its terminal state with auth enabled.

## In CI

CI runs all three tiers with `-race` (Postgres, Redis, and NATS provided as services), plus
`make lint`, a build of every service, and the spec checks (`make openapi-check`,
`make asyncapi-check`). The same gates run locally via `make hooks` — see
[CONTRIBUTING.md](../CONTRIBUTING.md).

## Tips

- `-count=1` (used by the make targets) disables Go's test cache — important when results depend
  on external state.
- Use `-race` when touching concurrent code (the integration target already does).
- Keep new code unit-testable by depending on the store interface, not `*store.Store` directly.
