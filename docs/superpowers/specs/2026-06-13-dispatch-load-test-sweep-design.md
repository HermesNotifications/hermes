# Dispatch Concurrency Load-Test Sweep — Design

**Date:** 2026-06-13
**Status:** Approved (design); pending implementation plan
**Context:** Phase B of the Hermes roadmap. PR #22 made the dispatch consumer a
single-fetcher worker pool with two tunables — `HERMES_DISPATCH_CONCURRENCY`
(worker count) and `HERMES_DISPATCH_PREFETCH` (fetch buffer) — defaulting to 4 / 64
as documented guesses. This sweep finds the throughput-optimal values empirically
and validates that the worker pool actually scales, on both store backends.

## Goal & success criteria

Produce data-backed answers to:

1. How does dispatch throughput (msgs/sec) scale with `workers` and `prefetch`?
2. Where is the **knee** (diminishing returns) on each backend — i.e. what should
   the shipped defaults be?
3. Does the worker pool scale at all (sanity: throughput at workers=8 ≫ workers=1)?

Success = a committed, reproducible harness; a results doc with per-cell
throughput (mean ± 95% CI) and a recommended default per backend; and an E2E k6
confirmation that the chosen config holds under realistic HTTP load.

## Non-goals

- Per-message tail-latency profiling inside dispatch (would require instrumenting
  dispatch internals). Latency is covered by the E2E k6 confirmation instead.
- Tuning delivery workers or the event-writer. Dispatch only.
- Production-target or multi-region runs.
- A managed orchestration/reporting layer (e.g. Taurus). It is the wrong layer for
  an internal NATS-consumer benchmark; revisit only for the future Phase D CI
  HTTP load-gate, wrapping the existing k6 scenarios.

## Approach: hybrid

1. **In-process Go drain harness** (rigorous) decides the defaults.
2. **One E2E k6 `send` run** per backend confirms the pick under the real HTTP path.

In-process is chosen over driving the full HTTP stack because the tunables govern
an internal NATS consumer: send→ack latency is upstream of dispatch and unaffected
by it, so HTTP load would measure the whole pipeline and confound the one variable
under test. Running dispatch in-process (as the e2e tests already do) isolates it
and builds from this branch automatically; the deployed k3d pods are stale images.

## Components & layout

- **`cmd/dispatchbench/`** — the harness binary. Imports the `dispatch` package and
  calls `Start(workers, prefetch)` directly, wired against real infra like the e2e
  tests. Emits per-run CSV and a markdown summary.
- **`make dispatchbench`** — convenience target (config via flags/env).
- **`docs/loadtest/dispatch-tuning-2026-06.md`** — committed results.
- The existing k6 `send` scenario (`loadtest/scenarios/send.js`) is reused unchanged
  for the E2E confirmation.

## Measurement methodology

Headline metric = **sustained dispatch throughput (msgs/sec) from a backlog drain**:

1. Pre-publish *N* synthetic `notification.send` messages to JetStream (WorkQueue
   retains them; no consumer running yet).
2. `t0` = start `dispatch.Start(workers, prefetch)`; it drains immediately.
3. Poll JetStream `ConsumerInfo` (`NumPending + NumAckPending`) every ~50ms.
   `t1` = first poll where pending reaches 0. **Throughput = N / (t1 − t0)**.

Only dispatch runs — no delivery workers, no event-writer. Dispatch acks a
`notification.send` message once it has persisted the record, resolved
template/channels, and fanned out to `delivery.*`; its drain rate is independent of
downstream consumers. The `delivery.*` / `notification.events` messages it publishes
accumulate in their streams for bounded *N* and are purged between cells.

`t0` is taken around `Start`; the small consumer-setup latency is constant across
cells. *N* is sized so a drain takes ≥ several seconds (e.g. 20k) to dwarf
setup/poll-granularity error.

## Isolation & repeatability

**Once, before the sweep:** seed one bench tenant + a fixed pool of users, and warm
the Redis caches, so every cell measures the steady-state hot path (cached
tenant/template, `EnsureUser` hitting existing rows) rather than cold-cache noise.

**Between cells:** purge the NOTIFICATIONS / DELIVERY / EVENTS streams, reset the
durable `dispatch` consumer (so `NumDelivered` starts clean), and truncate the bench
tenant's `notifications` (and events) rows so table/index growth doesn't skew later
cells. DynamoDB: clear or use fresh partition keys per cell.

The DB pool (`pool_max_conns`) is set ≥ the max worker count tested, so
`ClampWorkersToPool` does not distort the curve. The clamp/pool interaction is noted
in the results doc, not measured by the sweep.

## Matrix & statistics

- **workers** ∈ {1, 2, 4, 8, 16}
- **prefetch** ∈ {1, 16, 64, 256}
- **backends** ∈ {postgres, dynamo}
- → 40 cells, all overridable via flags.
- **R = 5** measured repetitions per cell + **1 discarded warmup**. Cell order
  randomized to spread any environmental drift.

Statistics computed in Go (no Python dependency): per cell **mean, stdev, 95% CI
(t-distribution), coefficient of variation**. Outputs:

- **CSV** — one row per run (`backend, workers, prefetch, rep, throughput, drain_ms`).
- **Markdown summary** — per cell mean ± 95% CI; the throughput-optimal cell and the
  knee of diminishing returns per backend; cells with high CV flagged as noisy.

Estimated cost: 40 cells × 6 runs ≈ 240 drains, ~1–1.5h.

## Backends

- **Postgres** — the running infra. This is the **primary** sweep and the gating
  deliverable; it must not be blocked by DynamoDB setup.
- **DynamoDB** — via DynamoDB Local (`HERMES_DYNAMO_ENDPOINT`). The harness stands up
  a known instance and creates the required tables rather than depending on another
  worktree's container. If this wiring proves fiddly, the Postgres sweep ships first
  and the Dynamo sweep is a fast follow-on — not a blocker.

## E2E confirmation (k6)

At the chosen optimal config per backend, run the existing k6 `send` scenario against
the full pipeline (this branch's dispatch) and confirm it holds under realistic HTTP
load: bounded e2e latency, no growing `notification.send` backlog. One scenario, one
run per backend — validation that the synthetic pick survives the real path, not a
second sweep.

## Deliverables

1. `cmd/dispatchbench/` + `make dispatchbench`.
2. `docs/loadtest/dispatch-tuning-2026-06.md` — results tables, recommended defaults
   per backend (data-cited), and notes on the pool/clamp interaction.
3. If the data warrants, an update to the `HERMES_DISPATCH_CONCURRENCY` /
   `HERMES_DISPATCH_PREFETCH` defaults in `internal/config/config.go` — a separate
   commit on the PR branch citing the numbers.

## Open dependency

DynamoDB Local standup + table creation for the bench is the one unproven piece;
resolved during implementation. Postgres path has no unknowns.
