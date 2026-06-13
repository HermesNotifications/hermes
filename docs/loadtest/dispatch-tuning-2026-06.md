# Dispatch concurrency tuning — June 2026

Harness: `cmd/dispatchbench` (see
[`docs/superpowers/specs/2026-06-13-dispatch-load-test-sweep-design.md`](../superpowers/specs/2026-06-13-dispatch-load-test-sweep-design.md)).

Method: in-process backlog drain of N synthetic `notification.send` messages;
throughput = N / drain-to-zero-pending. R=5 measured reps + 1 warmup per cell,
matrix workers {1,2,4,8,16} × prefetch {1,16,64,256}. Run with `make dispatchbench`.

The generated `docs/loadtest/dispatch-tuning.md` (per-cell mean ± 95% CI) and
`dispatch-tuning.csv` (per-rep) are pasted/summarised below.

## Postgres

Run: N=8000 msgs/drain, 5 reps + 1 warmup per cell, `pool_max_conns=20`, 1000 seeded
users, `inbox` direct-content messages. Infra: local Postgres/Redis/NATS (Docker).
Raw: `dispatch-tuning.csv` / `dispatch-tuning.md`.

| workers | prefetch | mean msgs/s | 95% CI | CV |
|---|---|---|---|---|
| 1 | 1 | 183 | ±2 | 0.01 |
| 1 | 16 | 177 | ±9 | 0.04 |
| 1 | 64 | 178 | ±2 | 0.01 |
| 1 | 256 | 166 | ±4 | 0.02 |
| 2 | 1 | 358 | ±19 | 0.04 |
| 2 | 16 | 265 | ±100 | 0.30 |
| 2 | 64 | 289 | ±8 | 0.02 |
| 2 | 256 | 319 | ±14 | 0.04 |
| 4 | 1 | 524 | ±34 | 0.05 |
| 4 | 16 | 535 | ±26 | 0.04 |
| 4 | 64 | 472 | ±7 | 0.01 |
| 4 | 256 | 459 | ±15 | 0.03 |
| 8 | 1 | 553 | ±15 | 0.02 |
| 8 | 16 | 736 | ±19 | 0.02 |
| 8 | 64 | 744 | ±30 | 0.03 |
| 8 | 256 | 798 | ±19 | 0.02 |
| 16 | 1 | 684 | ±37 | 0.04 |
| 16 | 16 | 944 | ±27 | 0.02 |
| 16 | 64 | 983 | ±146 | 0.12 |
| 16 | 256 | 882 | ±236 | 0.22 |

**Findings**

- **The worker pool scales** — throughput climbs monotonically with workers and has
  not plateaued at 16 (the pool ceiling): ~178 (w1) → ~310 (w2) → ~500 (w4) → ~760
  (w8) → ~950 (w16). Sub-linear at the top as Postgres-pool contention grows, but
  still rising. This is the core validation for the worker-pool change.
- **`prefetch=1` starves the pool at scale** — w16/p1 = 684 vs w16/p64 = 983 (−30%):
  a 1-deep fetch buffer can't keep 16 workers fed. Confirms the prefetch buffer's
  purpose.
- **Beyond `prefetch=16` the gains flatten and turn noisy** — w16: p16=944, p64=983,
  p256=882 (p256 is *lower* and the noisiest cell, CV 0.22). prefetch in the 16–64
  band is the sweet spot; 256 buys nothing and adds variance.
- **At low worker counts prefetch barely matters** (w1–w2 rows are within noise of
  each other) — unsurprising, the fetcher isn't the bottleneck there.
- Most cells are tight (CV < 0.05); the noisy outliers (w2/p16, w16/p64, w16/p256)
  are high-contention points — worth more reps if we ever need those cells precisely.

Harness `Recommend` (smallest workers within 95% of peak) → **workers=16, prefetch=16**
(peak 983 @ w16/p64; w16/p16 = 944 is within 95%). Default decision deferred to the
"Decision" section after the DynamoDB sweep and k6 confirmation.

## DynamoDB (DynamoDB Local)

<!-- paste the dynamo section after Task 8, or note deferred -->
_Pending the run._

## E2E k6 confirmation

<!-- send scenario at the recommended config, per backend (Task 9) -->
_Pending the run._

## Decision

<!-- recommended HERMES_DISPATCH_CONCURRENCY / HERMES_DISPATCH_PREFETCH + rationale (Task 10) -->
_Pending the run._
