# Dispatch tuning results

## dynamo

| workers | prefetch | mean msgs/s | 95% CI | CV |
|---|---|---|---|---|
| 1 | 1 | 164 | ±7 | 0.03 |
| 1 | 16 | 108 | ±14 | 0.11 |
| 1 | 64 | 163 | ±14 | 0.07 |
| 1 | 256 | 116 | ±8 | 0.06 |
| 2 | 1 | 330 | ±8 | 0.02 |
| 2 | 16 | 210 | ±10 | 0.04 |
| 2 | 64 | 230 | ±8 | 0.03 |
| 2 | 256 | 270 | ±23 | 0.07 |
| 4 | 1 | 368 | ±38 | 0.08 |
| 4 | 16 | 396 | ±18 | 0.04 |
| 4 | 64 | 385 | ±40 | 0.08 |
| 4 | 256 | 511 | ±18 | 0.03 |
| 8 | 1 | 531 | ±11 | 0.02 |
| 8 | 16 | 685 | ±66 | 0.08 |
| 8 | 64 | 568 | ±13 | 0.02 |
| 8 | 256 | 725 | ±34 | 0.04 |
| 16 | 1 | 525 | ±38 | 0.06 |
| 16 | 16 | 744 | ±120 | 0.13 |
| 16 | 64 | 840 | ±41 | 0.04 |
| 16 | 256 | 860 | ±64 | 0.06 |

**Recommended:** workers=16 prefetch=64

> These are the values this benchmark found optimal against the DynamoDB path; they are **not**
> what ships. The defaults are `HERMES_DISPATCH_CONCURRENCY=8` and `HERMES_DISPATCH_PREFETCH=64`
> (`internal/config/config.go`), so adopting the recommendation means doubling concurrency
> explicitly. See also [dispatch-tuning-2026-06.md](dispatch-tuning-2026-06.md), which measured
> the Postgres path and reached a different conclusion.

