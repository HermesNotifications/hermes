# Testing

The Go services have three test tiers, separated by Go build tags and what infrastructure
they need. The TypeScript surface — the admin portal and the SDKs — is tested separately
with vitest; see [TypeScript tests](#typescript-tests) below.

## Go tests

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

## TypeScript tests

The pnpm workspace (`web/admin`, `sdks/typescript/packages/*`, `examples/*` and
`tests/browser`) uses **vitest**, except the browser suite, which uses Playwright — see
[Browser tests](#browser-tests-live) below. Each package that has tests exposes a `test`
script; `.github/workflows/ci-web.yml` runs them.

```bash
pnpm --filter "@hermes/admin" test                                    # admin portal
pnpm --filter "./sdks/typescript/packages/*" --parallel run --if-present test
make sdk-ts-test                                                      # same thing
make demo-check                                                       # the demo packages
```

Use the repo's pinned pnpm — the root `package.json` sets `packageManager`, so corepack
selects it automatically. Installing under a different pnpm major rewrites `pnpm-lock.yaml`.

| Package | What is tested |
|---|---|
| `web/admin` | Pure helpers in `lib/` — `relativeTime` bucket boundaries, `slugify` |
| `hermes-client` | The inbox reducer and store, typed errors, the realtime wire format, the API surfaces, JWT subject decoding |
| `hermes-react` | The hooks and the `<HermesInbox>` wrapper, plus a node-environment suite proving the package is importable during server rendering |
| `hermes-web` | The `<hermes-inbox>` element and its controller in jsdom |
| `@hermes/demo-server` | Session assembly, cookie signing, the two proxy rules, node/WHATWG conversion |
| `@hermes/react-demo` | Token-refresh scheduling, which cannot be tested by waiting |
| `hermes-server` | No suite yet — mostly generated types |

`hermes-client` carries the bulk of it deliberately: the inbox reducer is the single
implementation both the widget and the hooks drive, so it is where a test buys the most. The
reducer takes no clock — every action that stamps a timestamp carries `at` — which is what turns
`expect(read_at).toBeTypeOf("string")` into an exact-value assertion. A test that only checks the
type cannot tell a correct stamp from any other.

Conventions, which differ from the Go tiers in ways worth knowing:

- **Fakes, not module mocks.** Type the fake against the real interface (see `InboxSurface`
  in `hermes-react/src/hooks.test.ts`) so a signature change breaks compilation instead of
  leaving a mock that passes against a method nobody has anymore.
- **Tests are type-checked, but not by the build.** `build` uses `tsconfig.build.json`, which
  excludes tests so they never reach the published `dist/`. `typecheck` uses the full
  `tsconfig.json` and does include them. Vitest transpiles without type-checking, so without
  the `typecheck` script nothing would check a test file at all.
- **jsdom is opt-in per package**, via `--environment jsdom` in the `test` script rather than
  a config file. A single file can opt out again with a `// @vitest-environment node` docblock,
  which is how `hermes-react/src/ssr.test.tsx` runs where `HTMLElement` genuinely does not exist.
- **Exclude both test extensions from `tsconfig.build.json`.** A `src/**/*.test.ts`-only pattern
  silently misses `.tsx`, which publishes tests to `dist/` and makes vitest collect a second
  compiled copy. That is not hypothetical: it showed up as `hermes-react` reporting 78 tests under
  `pnpm --filter` and 55 when run directly.
- **One shared fake.** `@hermes-notifications/client/testing` exports `FakeHermesClient`, typed
  against the real `InboxAPI`, so the widget, the hooks and the store suites drive one
  implementation rather than three drifting copies.
- Type errors are caught by `build`/`typecheck`, not by vitest. Both run in CI for every SDK
  package — a compile error in any one of them fails the job.

## Browser tests (live)

`tests/browser/` drives a real Chromium against a real cluster with Playwright. This is the tier
that answers "does the embedded widget actually work", and nothing below it can.

```bash
make dev-up             # the full stack must be running
make demo-e2e-install   # one-time: fetch the browser
make demo-e2e
```

It is **not** a required check on every PR — it needs a k3d cluster and ten Go services, so it runs
nightly, on demand, and per-PR behind an `e2e-live` label
(`.github/workflows/e2e-live.yml`). `ci-web.yml` runs `playwright test --list` instead, which
compiles every spec in seconds and catches most suite rot without launching anything.

To opt a PR in, add the label — the workflow triggers on `labeled`, so it starts immediately:

```bash
gh pr edit <number> --add-label e2e-live
```

Worth knowing when a PR touches the widget, the SDKs or the demo: without the label the check
reports as `skipping`, which reads a lot like "passed" in the checks list but means nothing ran.

Conventions that differ from every other tier here:

- **Read `smoke.spec.ts` first when it goes red.** It asserts the contract everything else depends
  on: the app renders, the element is registered (not merely present as an unknown tag), the inbox
  is empty, the socket connects. Its failure makes every other failure downstream noise.
- **Centrifugo validates `Origin`, and only for browsers.** The demo runs on `localhost:5173` while
  the socket is behind the ingress on `localhost:8888`, so every widget connection is cross-origin
  and Centrifugo answers `403` at the handshake unless the origin is listed in `allowed_origins`
  (`deploy/k8s/overlays/local/centrifugo-config.json`). Connections *without* an `Origin` header —
  every curl, health probe and Node client — are permitted by design, so the service can look
  entirely healthy while refusing the only connections that matter. `global-setup.ts` performs a
  real handshake carrying an `Origin` for exactly this reason; serve the demo from a different port
  and you must add that origin too.
- **`global-setup.ts` probes the environment before any browser starts**, and each check names the
  command that fixes it. Without that, an absent cluster looks like a wall of widget bugs.
- **Per-test organization and user.** Every test mints its own, so unread assertions are absolute
  (0 → 1 → 2, never "one more than before"), `fullyParallel` is safe (the API's rate limit is keyed
  by user), and no test can affect another.
- **The organization id must be a UUID.** `organizations.id` is a uuid column and
  `EnsureOrganization` inserts the value directly, so a friendly id yields a Postgres type error
  surfacing as a bare 500.
- **Never sleep.** `POST /v1/send` returns 202 before dispatch has created the row, so there is no
  fixed delay that is both reliable and quick. Every wait is `expect(...).toHaveText` or a poll with
  a deadline.
- **Gate on realtime being ready.** The widget subscribes after its initial fetch, and a publication
  landing before the subscription completes is lost permanently. `waitForRealtimeReady` waits for the
  widget's `hermes-connected` event; skipping it makes the suite flaky by construction.
- **Playwright pierces open shadow roots**, so the widget's internals are reachable with ordinary
  `getByRole` queries. "You cannot test web components" is not true here.
- **Nothing is cleaned up.** There is no API to delete an organization, a user or a notification
  (inbox `DELETE` is a soft delete). Per-test ids make accumulation harmless; `make dev-restart` is
  the reset.
- **No assertion may depend on a 404.** Inbox actions answer 200 for an unknown or foreign id,
  because the store reports `changed=false` and the handler ignores it. `inbox-actions.spec.ts`
  encodes that deliberately, so anyone "fixing" it into a 404 gets a red test and a reason.
