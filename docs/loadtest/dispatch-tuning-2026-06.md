# Dispatch concurrency tuning — June 2026

Harness: `cmd/dispatchbench` (see
[`docs/superpowers/specs/2026-06-13-dispatch-load-test-sweep-design.md`](../superpowers/specs/2026-06-13-dispatch-load-test-sweep-design.md)).

Method: in-process backlog drain of N synthetic `notification.send` messages;
throughput = N / drain-to-zero-pending. R=5 measured reps + 1 warmup per cell,
matrix workers {1,2,4,8,16} × prefetch {1,16,64,256}. Run with `make dispatchbench`.

The generated `docs/loadtest/dispatch-tuning.md` (per-cell mean ± 95% CI) and
`dispatch-tuning.csv` (per-rep) are pasted/summarised below.

## Postgres

<!-- paste the postgres section of docs/loadtest/dispatch-tuning.md after Task 7 -->
_Pending the run._

## DynamoDB (DynamoDB Local)

<!-- paste the dynamo section after Task 8, or note deferred -->
_Pending the run._

## E2E k6 confirmation

<!-- send scenario at the recommended config, per backend (Task 9) -->
_Pending the run._

## Decision

<!-- recommended HERMES_DISPATCH_CONCURRENCY / HERMES_DISPATCH_PREFETCH + rationale (Task 10) -->
_Pending the run._
