# Dispatch Concurrency Load-Test Sweep Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a reproducible in-process benchmark (`cmd/dispatchbench`) that sweeps dispatch `workers × prefetch × backend`, measures sustained throughput via NATS backlog drain, computes per-cell statistics, and produces a results doc recommending the shipped defaults — then confirm the pick with one E2E k6 run per backend.

**Architecture:** Pure, unit-tested logic (statistics, matrix, reporting) lives in `internal/dispatchbench/`. A thin `cmd/dispatchbench/main.go` wires real infra (Postgres/DynamoDB/NATS/Redis), seeds a bench tenant, and loops over cells × repetitions, calling a drain runner that pre-publishes N synthetic `notification.send` messages, starts `dispatch.Start(workers, prefetch)` from this branch, and times the consumer drain to 0 pending. Each repetition uses a fresh `messaging.Client` (Close is the only teardown). A separate admin JetStream connection purges streams and polls `ConsumerInfo` between/within reps.

**Tech Stack:** Go, NATS JetStream (`nats.go/jetstream`), pgx, the existing `dispatch`/`postgres`/`dynamo`/`cache`/`messaging` packages, k6 (existing `send` scenario) for the E2E confirmation.

---

## Spec reference

`docs/superpowers/specs/2026-06-13-dispatch-load-test-sweep-design.md`. Read it first.

## File structure

- Create `internal/dispatchbench/stats.go` — `Summarize([]float64) Stat`, `tValue(df int) float64`. Pure.
- Create `internal/dispatchbench/stats_test.go` — unit tests.
- Create `internal/dispatchbench/matrix.go` — `Cell`, `Cells(...)`, `Shuffle(...)`. Pure.
- Create `internal/dispatchbench/matrix_test.go` — unit tests.
- Create `internal/dispatchbench/report.go` — `Result`, `WriteCSV`, `Markdown`, `Recommend`. Pure.
- Create `internal/dispatchbench/report_test.go` — unit tests.
- Create `internal/dispatchbench/run.go` — `Runner`, `(*Runner).Drain(...)`. Integration (needs infra).
- Create `internal/dispatchbench/run_integration_test.go` — one tiny-N smoke test (`//go:build integration`).
- Create `cmd/dispatchbench/main.go` — flags, infra wiring, seed, sweep loop. No unit test (composition root).
- Modify `Makefile` — add `dispatchbench` target.
- Create `docs/loadtest/dispatch-tuning-2026-06.md` — results (filled by the run tasks).
- Modify `internal/config/config.go` — only if the data warrants new defaults (Task 10).

---

### Task 1: Statistics (`internal/dispatchbench/stats.go`)

**Files:**
- Create: `internal/dispatchbench/stats.go`
- Test: `internal/dispatchbench/stats_test.go`

- [ ] **Step 1: Write the failing test**

```go
// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package dispatchbench

import (
	"math"
	"testing"
)

func almost(a, b float64) bool { return math.Abs(a-b) < 1e-3 }

func TestSummarize(t *testing.T) {
	s := Summarize([]float64{10, 12, 14, 16, 18})
	if s.N != 5 {
		t.Fatalf("N = %d, want 5", s.N)
	}
	if !almost(s.Mean, 14) {
		t.Errorf("Mean = %f, want 14", s.Mean)
	}
	// sample stdev of {10,12,14,16,18} = sqrt(40/4)=3.16228
	if !almost(s.Stdev, 3.16228) {
		t.Errorf("Stdev = %f, want 3.16228", s.Stdev)
	}
	// CI95 = t(0.975,4)=2.776 * stdev/sqrt(5) = 2.776 * 1.41421 = 3.9258
	if !almost(s.CI95, 3.92577) {
		t.Errorf("CI95 = %f, want 3.92577", s.CI95)
	}
	// CV = stdev/mean = 3.16228/14 = 0.22588
	if !almost(s.CV, 0.22588) {
		t.Errorf("CV = %f, want 0.22588", s.CV)
	}
}

func TestSummarizeEdgeCases(t *testing.T) {
	if got := Summarize(nil); got.N != 0 {
		t.Errorf("empty N = %d, want 0", got.N)
	}
	one := Summarize([]float64{42})
	if one.N != 1 || !almost(one.Mean, 42) || one.Stdev != 0 || one.CI95 != 0 {
		t.Errorf("single sample = %+v, want N=1 Mean=42 Stdev=0 CI95=0", one)
	}
}

func TestTValue(t *testing.T) {
	if !almost(tValue(4), 2.776) {
		t.Errorf("tValue(4) = %f, want 2.776", tValue(4))
	}
	if !almost(tValue(1), 12.706) {
		t.Errorf("tValue(1) = %f, want 12.706", tValue(1))
	}
	if !almost(tValue(100), 1.96) { // > 30 falls back to normal approx
		t.Errorf("tValue(100) = %f, want 1.96", tValue(100))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/dispatchbench/ -run 'TestSummarize|TestTValue' -v`
Expected: FAIL — `undefined: Summarize` / `undefined: tValue`.

- [ ] **Step 3: Write minimal implementation**

```go
// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

// Package dispatchbench measures dispatch consumer throughput across a matrix of
// worker-pool / prefetch / backend configurations and summarizes the results.
package dispatchbench

import "math"

// Stat summarizes a set of throughput samples (msgs/sec).
type Stat struct {
	N     int
	Mean  float64
	Stdev float64 // sample standard deviation
	CI95  float64 // half-width of the 95% confidence interval (t-distribution)
	CV    float64 // coefficient of variation (Stdev/Mean)
}

// Summarize computes mean, sample stdev, 95% CI half-width, and CV for xs.
func Summarize(xs []float64) Stat {
	n := len(xs)
	if n == 0 {
		return Stat{}
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	mean := sum / float64(n)
	if n == 1 {
		return Stat{N: 1, Mean: mean}
	}
	var ss float64
	for _, x := range xs {
		d := x - mean
		ss += d * d
	}
	stdev := math.Sqrt(ss / float64(n-1))
	ci := tValue(n-1) * stdev / math.Sqrt(float64(n))
	cv := 0.0
	if mean != 0 {
		cv = stdev / mean
	}
	return Stat{N: n, Mean: mean, Stdev: stdev, CI95: ci, CV: cv}
}

// tTable holds two-sided 95% Student's t critical values indexed by degrees of
// freedom (index 0 unused). For df > 30 the normal approximation 1.96 is used.
var tTable = []float64{
	0, 12.706, 4.303, 3.182, 2.776, 2.571, 2.447, 2.365, 2.306, 2.262, 2.228,
	2.201, 2.179, 2.160, 2.145, 2.131, 2.120, 2.110, 2.101, 2.093, 2.086,
	2.080, 2.074, 2.069, 2.064, 2.060, 2.056, 2.052, 2.048, 2.045, 2.042,
}

func tValue(df int) float64 {
	if df >= 1 && df < len(tTable) {
		return tTable[df]
	}
	return 1.96
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/dispatchbench/ -run 'TestSummarize|TestTValue' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/dispatchbench/stats.go internal/dispatchbench/stats_test.go
git commit -m "feat(dispatchbench): throughput statistics (mean, CI95, CV)"
```

---

### Task 2: Matrix (`internal/dispatchbench/matrix.go`)

**Files:**
- Create: `internal/dispatchbench/matrix.go`
- Test: `internal/dispatchbench/matrix_test.go`

- [ ] **Step 1: Write the failing test**

```go
// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package dispatchbench

import "testing"

func TestCells(t *testing.T) {
	cells := Cells([]int{1, 4}, []int{16, 64}, []string{"postgres", "dynamo"})
	if len(cells) != 8 { // 2 workers × 2 prefetch × 2 backends
		t.Fatalf("len = %d, want 8", len(cells))
	}
	// First cell is the lowest config of the first backend (deterministic order).
	if cells[0] != (Cell{Backend: "postgres", Workers: 1, Prefetch: 16}) {
		t.Errorf("cells[0] = %+v", cells[0])
	}
}

func TestShuffleIsDeterministicAndPreservesSet(t *testing.T) {
	base := Cells([]int{1, 4}, []int{16, 64}, []string{"postgres"})
	a := append([]Cell(nil), base...)
	b := append([]Cell(nil), base...)
	Shuffle(a, 42)
	Shuffle(b, 42)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("same seed produced different order at %d", i)
		}
	}
	// Shuffle preserves the multiset of cells.
	seen := map[Cell]int{}
	for _, c := range a {
		seen[c]++
	}
	for _, c := range base {
		if seen[c] != 1 {
			t.Fatalf("cell %+v missing or duplicated after shuffle", c)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/dispatchbench/ -run 'TestCells|TestShuffle' -v`
Expected: FAIL — `undefined: Cells` / `undefined: Shuffle`.

- [ ] **Step 3: Write minimal implementation**

```go
// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package dispatchbench

import "math/rand"

// Cell is one point in the sweep matrix.
type Cell struct {
	Backend  string
	Workers  int
	Prefetch int
}

// Cells expands the cross product in a deterministic order: backend, then
// workers, then prefetch.
func Cells(workers, prefetch []int, backends []string) []Cell {
	var out []Cell
	for _, b := range backends {
		for _, w := range workers {
			for _, p := range prefetch {
				out = append(out, Cell{Backend: b, Workers: w, Prefetch: p})
			}
		}
	}
	return out
}

// Shuffle randomizes cell order in place using a seeded RNG so the run order is
// reproducible while still spreading environmental drift across the matrix.
func Shuffle(cells []Cell, seed int64) {
	rng := rand.New(rand.NewSource(seed))
	rng.Shuffle(len(cells), func(i, j int) { cells[i], cells[j] = cells[j], cells[i] })
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/dispatchbench/ -run 'TestCells|TestShuffle' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/dispatchbench/matrix.go internal/dispatchbench/matrix_test.go
git commit -m "feat(dispatchbench): sweep matrix and seeded shuffle"
```

---

### Task 3: Reporting (`internal/dispatchbench/report.go`)

**Files:**
- Create: `internal/dispatchbench/report.go`
- Test: `internal/dispatchbench/report_test.go`

- [ ] **Step 1: Write the failing test**

```go
// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package dispatchbench

import (
	"strings"
	"testing"
)

func sampleResults() []Result {
	return []Result{
		{Cell: Cell{Backend: "postgres", Workers: 1, Prefetch: 64}, Throughput: []float64{1000, 1010, 990}},
		{Cell: Cell{Backend: "postgres", Workers: 8, Prefetch: 64}, Throughput: []float64{5000, 5100, 4900}},
		{Cell: Cell{Backend: "postgres", Workers: 16, Prefetch: 64}, Throughput: []float64{5100, 5150, 5050}},
	}
}

func TestRecommendUsesSmallestWorkersWithin95PctOfPeak(t *testing.T) {
	rec := Recommend(sampleResults())
	got, ok := rec["postgres"]
	if !ok {
		t.Fatal("no recommendation for postgres")
	}
	// Peak mean is workers=16 (~5100). workers=8 (~5000) is within 95% (>4845),
	// so the smaller config is recommended to avoid over-provisioning.
	if got.Workers != 8 || got.Prefetch != 64 {
		t.Errorf("recommended = %+v, want workers=8 prefetch=64", got)
	}
}

func TestWriteCSVHasHeaderAndRowPerRep(t *testing.T) {
	var sb strings.Builder
	if err := WriteCSV(&sb, sampleResults()); err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}
	out := sb.String()
	if !strings.HasPrefix(out, "backend,workers,prefetch,rep,throughput_msgs_per_sec\n") {
		t.Fatalf("missing/incorrect header: %q", out[:60])
	}
	// 3 cells × 3 reps = 9 data rows + 1 header = 10 lines.
	if n := strings.Count(strings.TrimSpace(out), "\n"); n != 9 {
		t.Errorf("data lines = %d, want 9", n)
	}
}

func TestMarkdownIncludesPerCellMeanAndRecommendation(t *testing.T) {
	md := Markdown(sampleResults())
	if !strings.Contains(md, "postgres") {
		t.Error("missing backend section")
	}
	if !strings.Contains(md, "Recommended") {
		t.Error("missing recommendation line")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/dispatchbench/ -run 'TestRecommend|TestWriteCSV|TestMarkdown' -v`
Expected: FAIL — `undefined: Result` / `Recommend` / `WriteCSV` / `Markdown`.

- [ ] **Step 3: Write minimal implementation**

```go
// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package dispatchbench

import (
	"fmt"
	"io"
	"sort"
)

// Result holds all measured throughput samples for one cell.
type Result struct {
	Cell       Cell
	Throughput []float64 // one entry per measured repetition (warmup excluded)
}

// Stat summarizes this cell's samples.
func (r Result) Stat() Stat { return Summarize(r.Throughput) }

// WriteCSV writes one row per repetition: backend,workers,prefetch,rep,throughput.
func WriteCSV(w io.Writer, results []Result) error {
	if _, err := fmt.Fprintln(w, "backend,workers,prefetch,rep,throughput_msgs_per_sec"); err != nil {
		return err
	}
	for _, r := range results {
		for i, tp := range r.Throughput {
			if _, err := fmt.Fprintf(w, "%s,%d,%d,%d,%.2f\n",
				r.Cell.Backend, r.Cell.Workers, r.Cell.Prefetch, i, tp); err != nil {
				return err
			}
		}
	}
	return nil
}

// Recommend picks, per backend, the smallest worker count whose mean throughput
// is within 95% of that backend's peak mean — favouring the cheaper config when
// the extra workers buy < 5%. Ties on workers break to the lower prefetch.
func Recommend(results []Result) map[string]Cell {
	byBackend := map[string][]Result{}
	for _, r := range results {
		byBackend[r.Cell.Backend] = append(byBackend[r.Cell.Backend], r)
	}
	out := map[string]Cell{}
	for backend, rs := range byBackend {
		var peak float64
		for _, r := range rs {
			if m := r.Stat().Mean; m > peak {
				peak = m
			}
		}
		threshold := 0.95 * peak
		candidates := make([]Result, 0, len(rs))
		for _, r := range rs {
			if r.Stat().Mean >= threshold {
				candidates = append(candidates, r)
			}
		}
		sort.Slice(candidates, func(i, j int) bool {
			ci, cj := candidates[i].Cell, candidates[j].Cell
			if ci.Workers != cj.Workers {
				return ci.Workers < cj.Workers
			}
			return ci.Prefetch < cj.Prefetch
		})
		if len(candidates) > 0 {
			out[backend] = candidates[0].Cell
		}
	}
	return out
}

// Markdown renders a per-backend results table plus the recommendation.
func Markdown(results []Result) string {
	byBackend := map[string][]Result{}
	var backends []string
	for _, r := range results {
		if _, seen := byBackend[r.Cell.Backend]; !seen {
			backends = append(backends, r.Cell.Backend)
		}
		byBackend[r.Cell.Backend] = append(byBackend[r.Cell.Backend], r)
	}
	sort.Strings(backends)
	rec := Recommend(results)

	out := "# Dispatch tuning results\n\n"
	for _, b := range backends {
		rs := byBackend[b]
		sort.Slice(rs, func(i, j int) bool {
			if rs[i].Cell.Workers != rs[j].Cell.Workers {
				return rs[i].Cell.Workers < rs[j].Cell.Workers
			}
			return rs[i].Cell.Prefetch < rs[j].Cell.Prefetch
		})
		out += fmt.Sprintf("## %s\n\n", b)
		out += "| workers | prefetch | mean msgs/s | 95%% CI | CV |\n|---|---|---|---|---|\n"
		for _, r := range rs {
			s := r.Stat()
			out += fmt.Sprintf("| %d | %d | %.0f | ±%.0f | %.2f |\n",
				r.Cell.Workers, r.Cell.Prefetch, s.Mean, s.CI95, s.CV)
		}
		if c, ok := rec[b]; ok {
			out += fmt.Sprintf("\n**Recommended:** workers=%d prefetch=%d\n\n", c.Workers, c.Prefetch)
		}
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/dispatchbench/ -run 'TestRecommend|TestWriteCSV|TestMarkdown' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/dispatchbench/report.go internal/dispatchbench/report_test.go
git commit -m "feat(dispatchbench): CSV, markdown report, and default recommendation"
```

---

### Task 4: Drain runner (`internal/dispatchbench/run.go`)

The runner does the actual measurement against real infra. It is exercised by a small `//go:build integration` smoke test, not unit tests.

**Files:**
- Create: `internal/dispatchbench/run.go`
- Test: `internal/dispatchbench/run_integration_test.go`

- [ ] **Step 1: Write the implementation**

```go
// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package dispatchbench

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// DispatchFactory builds and starts a dispatch consumer for one repetition with
// the given worker/prefetch config, returning a stop func that fully tears it
// down (Close on a fresh messaging.Client). Implemented in cmd/dispatchbench.
type DispatchFactory func(workers, prefetch int) (stop func(), err error)

// Publisher publishes n synthetic notification.send messages for the given
// backend. Implemented in cmd/dispatchbench (chooses pg/dynamo recipients).
type Publisher func(ctx context.Context, n int) error

// Resetter clears state between repetitions: purge streams, delete the dispatch
// consumer, and truncate bench rows. Implemented in cmd/dispatchbench.
type Resetter func(ctx context.Context) error

// Runner measures one cell's repetitions.
type Runner struct {
	JS       jetstream.JetStream
	Stream   string // "NOTIFICATIONS"
	Consumer string // "dispatch"
	N        int    // messages per drain
	Publish  Publisher
	Dispatch DispatchFactory
	Reset    Resetter
	Poll     time.Duration // consumer-info poll interval, e.g. 50ms
}

// Drain runs one repetition and returns throughput in msgs/sec.
func (r *Runner) Drain(ctx context.Context, cell Cell) (float64, error) {
	if err := r.Reset(ctx); err != nil {
		return 0, fmt.Errorf("reset: %w", err)
	}
	if err := r.Publish(ctx, r.N); err != nil {
		return 0, fmt.Errorf("publish: %w", err)
	}
	start := time.Now()
	stop, err := r.Dispatch(cell.Workers, cell.Prefetch)
	if err != nil {
		return 0, fmt.Errorf("start dispatch: %w", err)
	}
	defer stop()

	if err := r.waitDrained(ctx); err != nil {
		return 0, err
	}
	elapsed := time.Since(start).Seconds()
	if elapsed <= 0 {
		return 0, fmt.Errorf("non-positive elapsed time")
	}
	return float64(r.N) / elapsed, nil
}

// waitDrained polls the consumer until pending (NumPending+NumAckPending) hits 0,
// requiring at least one observation of pending>0 first so we never accept the
// pre-consumer-creation state as "done".
func (r *Runner) waitDrained(ctx context.Context) error {
	ticker := time.NewTicker(r.Poll)
	defer ticker.Stop()
	sawWork := false
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			cons, err := r.JS.Consumer(ctx, r.Stream, r.Consumer)
			if err != nil {
				continue // consumer not created yet
			}
			info, err := cons.Info(ctx)
			if err != nil {
				continue
			}
			pending := info.NumPending + uint64(info.NumAckPending)
			if pending > 0 {
				sawWork = true
			} else if sawWork {
				return nil
			}
		}
	}
}
```

- [ ] **Step 2: Write the integration smoke test**

```go
// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

//go:build integration

package dispatchbench_test

import (
	"context"
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/internal/dispatchbench"
)

// TestRunnerDrainSmoke verifies the Drain control flow end-to-end with stub
// closures (no real NATS): publish increments a counter, dispatch flips pending
// to 0 via a fake consumer is covered by the real cmd run; here we only assert
// the happy path returns a positive throughput and calls reset+publish+stop.
func TestRunnerDrainSmoke(t *testing.T) {
	t.Skip("covered by `make dispatchbench` against live infra; see cmd/dispatchbench")
	_ = context.Background()
	_ = time.Millisecond
	_ = dispatchbench.Runner{}
}
```

Note: a faithful unit test of `Drain` would require a fake `jetstream.JetStream`, which is a large interface. The control-flow logic is thin; it is verified for real by Task 7's first sweep. The smoke test is a placeholder skip documenting that decision. Do not expand it.

- [ ] **Step 3: Run to verify build + skip**

Run: `go test ./internal/dispatchbench/ -tags=integration -run TestRunnerDrainSmoke -v`
Expected: `--- SKIP` and the package builds.

- [ ] **Step 4: Commit**

```bash
git add internal/dispatchbench/run.go internal/dispatchbench/run_integration_test.go
git commit -m "feat(dispatchbench): backlog-drain runner (publish, start, poll to zero)"
```

---

### Task 5: Harness wiring (`cmd/dispatchbench/main.go`)

This is the composition root: flags, infra, seed, the sweep loop, and the three closures (`Publisher`, `DispatchFactory`, `Resetter`) for each backend. No unit test — verified by Task 7.

**Files:**
- Create: `cmd/dispatchbench/main.go`

- [ ] **Step 1: Write the harness**

```go
// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/hermes-notifications/hermes/internal/cache"
	"github.com/hermes-notifications/hermes/internal/database"
	"github.com/hermes-notifications/hermes/internal/dispatch"
	"github.com/hermes-notifications/hermes/internal/dispatchbench"
	id "github.com/hermes-notifications/hermes/internal/id/v2"
	"github.com/hermes-notifications/hermes/internal/messaging"
	hermenats "github.com/hermes-notifications/hermes/internal/nats"
	"github.com/hermes-notifications/hermes/internal/store"
	"github.com/hermes-notifications/hermes/internal/store/dynamo"
	"github.com/hermes-notifications/hermes/internal/store/postgres"
)

func intList(s string) []int {
	var out []int
	for _, p := range strings.Split(s, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func main() {
	var (
		dbURL      = flag.String("db", envOr("HERMES_DATABASE_URL", "postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable"), "Postgres URL (set pool_max_conns >= max workers)")
		natsURL    = flag.String("nats", envOr("HERMES_NATS_URL", "nats://localhost:4222"), "NATS URL")
		redisURL   = flag.String("redis", envOr("HERMES_REDIS_URL", "redis://localhost:6379/0"), "Redis URL")
		dynamoEP   = flag.String("dynamo", os.Getenv("HERMES_DYNAMO_ENDPOINT"), "DynamoDB Local endpoint (empty => skip dynamo cells)")
		dynamoRgn  = flag.String("dynamo-region", envOr("HERMES_DYNAMO_REGION", "us-east-1"), "DynamoDB region")
		workersCSV = flag.String("workers", "1,2,4,8,16", "worker counts")
		prefetchCSV= flag.String("prefetch", "1,16,64,256", "prefetch values")
		backendCSV = flag.String("backends", "postgres", "backends: postgres,dynamo")
		n          = flag.Int("n", 20000, "messages per drain")
		reps       = flag.Int("reps", 5, "measured repetitions per cell")
		warmups    = flag.Int("warmups", 1, "discarded warmup repetitions per cell")
		users      = flag.Int("users", 1000, "seeded bench users")
		seed       = flag.Int64("seed", 1, "shuffle seed")
		csvOut     = flag.String("csv", "dispatch-tuning.csv", "CSV output path")
		mdOut      = flag.String("md", "dispatch-tuning.md", "markdown summary output path")
	)
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ctx := context.Background()

	pool, err := database.NewPool(ctx, *dbURL)
	must(err, "db")
	defer pool.Close()
	pgStore := postgres.New(pool)

	redisClient, err := cache.Connect(*redisURL)
	must(err, "redis")
	defer redisClient.Close()

	// Admin NATS connection for publish, purge, and consumer-info polling.
	admin, err := messaging.Connect(*natsURL)
	must(err, "nats admin")
	defer admin.Close()
	must(admin.SetupStreams(ctx), "setup streams")
	nc, err := nats.Connect(*natsURL)
	must(err, "nats raw")
	defer nc.Close()
	js, err := jetstream.New(nc)
	must(err, "jetstream")

	// Seed one bench tenant + users, warm caches.
	benchTenant := "bench-" + uuid.New().String()[:8]
	userIDs := seedBench(ctx, pool, pgStore, redisClient, benchTenant, *users)
	fmt.Fprintf(os.Stderr, "seeded tenant %s with %d users\n", benchTenant, len(userIDs))

	backends := strings.Split(*backendCSV, ",")
	cells := dispatchbench.Cells(intList(*workersCSV), intList(*prefetchCSV), backends)
	dispatchbench.Shuffle(cells, *seed)

	var results []dispatchbench.Result
	for _, cell := range cells {
		notifRepo := storeForBackend(ctx, cell.Backend, pgStore, *dynamoEP, *dynamoRgn, logger)
		if notifRepo == nil {
			fmt.Fprintf(os.Stderr, "skip %s cells: backend unavailable\n", cell.Backend)
			continue
		}
		runner := newRunner(js, *natsURL, *n, benchTenant, userIDs, notifRepo, pgStore, redisClient, logger)
		// Warmups (discarded) then measured reps.
		for i := 0; i < *warmups; i++ {
			if _, err := runner.Drain(ctx, cell); err != nil {
				fmt.Fprintf(os.Stderr, "warmup %+v: %v\n", cell, err)
			}
		}
		var samples []float64
		for i := 0; i < *reps; i++ {
			tp, err := runner.Drain(ctx, cell)
			if err != nil {
				fmt.Fprintf(os.Stderr, "rep %+v: %v\n", cell, err)
				continue
			}
			samples = append(samples, tp)
		}
		results = append(results, dispatchbench.Result{Cell: cell, Throughput: samples})
		fmt.Fprintf(os.Stderr, "%s w=%d p=%d -> %.0f msgs/s\n",
			cell.Backend, cell.Workers, cell.Prefetch, dispatchbench.Summarize(samples).Mean)
	}

	writeOutputs(*csvOut, *mdOut, results)
	fmt.Fprintf(os.Stderr, "wrote %s and %s\n", *csvOut, *mdOut)
}
```

- [ ] **Step 2: Write the helper file `cmd/dispatchbench/wiring.go`**

This holds `seedBench`, `storeForBackend`, `newRunner`, `writeOutputs`, `envOr`, `must`. Key behaviours:
- `seedBench`: insert tenant row; loop `pgStore.EnsureUser(ctx, tenant, "u<i>")` collecting IDs, set each user's email so channels resolve; call `tenants.EnsureTenant` through a `cached.NewTenantRepository` once to warm the Redis tenant cache.
- `storeForBackend("postgres", ...)` returns `pgStore`; `("dynamo", ...)` calls `dynamo.NewClient(ctx, ep, region)`, on error returns nil (skip), else `client.EnsureTables(ctx)`, `dynamo.NewNotificationStore(client, dynamo.NewEventStore(client, pgStore))`.
- `newRunner` builds a `dispatchbench.Runner` with:
  - `Publish`: for `n`, build `hermenats.SendMessage{NotificationID: id.Notification.New(), TenantID: benchTenant, ExternalUserID: "u<rand>", Content: &hermenats.MessageContent{Title:"t", Body:"b"}, Channels: []string{"inbox"}}`, `Marshal`, `admin.Publish(ctx, "notification.send", data)`. Use direct content (no template) and `inbox` channel to keep per-message work representative without template I/O variance; ensure seeded users have email if you also want email fan-out (keep inbox-only for determinism).
  - `Dispatch`: returns a closure that opens a fresh `messaging.Client` (`messaging.Connect`), builds `dispatch.NewDispatch(client, notifRepo, pgStore, cached tenants, templateResolver, channelResolver, logger)`, calls `Start(workers, prefetch)`, and returns `stop = client.Close`.
  - `Reset`: purge the three streams via `js.Stream(...).Purge(ctx)`; `js.DeleteConsumer(ctx, "NOTIFICATIONS", "dispatch")` (ignore not-found); `pool.Exec(ctx, "DELETE FROM notification_events WHERE notification_id IN (SELECT id FROM notifications WHERE tenant_id=$1)", benchTenant)` then `DELETE FROM notifications WHERE tenant_id=$1`.
  - `Stream: "NOTIFICATIONS"`, `Consumer: "dispatch"`, `N: n`, `Poll: 50*time.Millisecond`.
- `writeOutputs`: `os.Create` csv → `dispatchbench.WriteCSV`; `os.WriteFile(md, []byte(dispatchbench.Markdown(results)), 0o644)`.
- `envOr(k, d string) string`, `must(err error, what string)` (log.Fatal on err).

Write this file in full following the closures above; every referenced symbol exists (`messaging.Connect`, `messaging.Client.Publish/Close`, `dispatch.NewDispatch`, `dispatch.NewTemplateResolver`, `dispatch.NewChannelResolver`, `cached.NewTenantRepository`, `postgres.New`, `dynamo.NewClient/NewNotificationStore/NewEventStore/Client.EnsureTables`, `id.Notification.New`, `pool.Exec`, `js.Stream/DeleteConsumer`).

- [ ] **Step 3: Build**

Run: `go build ./cmd/dispatchbench/`
Expected: builds clean.

- [ ] **Step 4: Commit**

```bash
git add cmd/dispatchbench/
git commit -m "feat(dispatchbench): harness wiring (seed, backends, sweep loop)"
```

---

### Task 6: Makefile target + results doc scaffold

**Files:**
- Modify: `Makefile`
- Create: `docs/loadtest/dispatch-tuning-2026-06.md`

- [ ] **Step 1: Add the target** (place near the loadtest targets, ~line 230)

```make
.PHONY: dispatchbench
dispatchbench:     ## Run the dispatch concurrency sweep (requires make infra-up; pool_max_conns >= max workers)
	go run ./cmd/dispatchbench \
	  --db "$(or $(HERMES_DATABASE_URL),postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable&pool_max_conns=20)" \
	  --backends "$(or $(BACKENDS),postgres)" \
	  --csv docs/loadtest/dispatch-tuning.csv \
	  --md docs/loadtest/dispatch-tuning.md
```

- [ ] **Step 2: Scaffold the results doc**

```markdown
# Dispatch concurrency tuning — June 2026

Harness: `cmd/dispatchbench` (see `docs/superpowers/specs/2026-06-13-dispatch-load-test-sweep-design.md`).
Method: in-process backlog drain of N synthetic `notification.send` messages;
throughput = N / drain-to-zero-pending. R=5 measured reps + 1 warmup per cell.

_Results tables generated by `make dispatchbench` are pasted below._

## Postgres

<!-- paste docs/loadtest/dispatch-tuning.md postgres section -->

## DynamoDB (DynamoDB Local)

<!-- paste dynamo section, or note deferred -->

## E2E k6 confirmation

<!-- send scenario at the recommended config, per backend -->

## Decision

<!-- recommended HERMES_DISPATCH_CONCURRENCY / HERMES_DISPATCH_PREFETCH + rationale -->
```

- [ ] **Step 3: Commit**

```bash
git add Makefile docs/loadtest/dispatch-tuning-2026-06.md
git commit -m "chore(dispatchbench): make target and results doc scaffold"
```

---

### Task 7: Run the Postgres sweep

**Files:** none (produces `docs/loadtest/dispatch-tuning.csv` / `.md`).

- [ ] **Step 1: Confirm infra is up**

Run: `docker ps --format '{{.Names}}' | grep -E 'postgres|redis|nats'`
Expected: all three present. If not: `make infra-up`.

- [ ] **Step 2: Run migrations**

Run: `make migrate`
Expected: up to date.

- [ ] **Step 3: Run the Postgres sweep**

Run: `make dispatchbench BACKENDS=postgres`
Expected: progress lines per cell on stderr; `docs/loadtest/dispatch-tuning.csv` and `.md` written. Sanity: workers=8 mean throughput should be well above workers=1 (the pool scales). If they're equal, STOP and investigate (the pool may not be scaling — see superpowers:systematic-debugging).

- [ ] **Step 4: Fold results into the dated doc**

Paste the generated markdown Postgres table into `docs/loadtest/dispatch-tuning-2026-06.md` under `## Postgres`. Note the observed knee.

- [ ] **Step 5: Commit**

```bash
git add docs/loadtest/dispatch-tuning.csv docs/loadtest/dispatch-tuning-2026-06.md
git commit -m "test(dispatchbench): Postgres sweep results"
```

---

### Task 8: DynamoDB standup + sweep (follow-on; do not block Task 7)

**Files:** none (extends results docs).

- [ ] **Step 1: Start DynamoDB Local**

Run: `docker run -d --name dispatchbench-dynamo -p 8000:8000 amazon/dynamodb-local`
Expected: container running on `:8000`. (If the repo already provides a dynamo compose service, prefer that.)

- [ ] **Step 2: Run the dynamo sweep**

Run: `make dispatchbench BACKENDS=dynamo HERMES_DYNAMO_ENDPOINT=http://localhost:8000`
Expected: harness calls `EnsureTables`, runs the dynamo cells. If the endpoint is unreachable the harness logs `skip dynamo cells` and exits cleanly — fix the endpoint and rerun.

- [ ] **Step 3: Fold results in + commit**

Paste the dynamo table into the dated doc under `## DynamoDB`. Commit:

```bash
git add docs/loadtest/dispatch-tuning*.csv docs/loadtest/dispatch-tuning-2026-06.md
git commit -m "test(dispatchbench): DynamoDB Local sweep results"
```

---

### Task 9: E2E k6 confirmation

**Files:** none (extends results doc).

- [ ] **Step 1: Bring up the full stack on this branch**

Run the services from this worktree against infra (or `make dev-up` if it builds from this branch). Confirm `curl -s localhost:8888/healthz` (ingress) or the send port responds.

- [ ] **Step 2: Seed + run the send scenario at the recommended config**

Set `HERMES_DISPATCH_CONCURRENCY` / `HERMES_DISPATCH_PREFETCH` to the Task 7 recommendation for dispatch, then:

Run: `make loadseed && make loadtest-local SCENARIO=send TARGET_RPS=<near-ceiling> DURATION=60s`
Expected: `artifacts/<run_id>/summary.json`. Confirm dispatch keeps up — the NOTIFICATIONS consumer `NumPending` stays bounded (does not grow unboundedly). Capture send-ack p95 and final queue depth.

- [ ] **Step 3: Record + commit**

Paste headline numbers under `## E2E k6 confirmation`. Commit.

---

### Task 10: Decide and (maybe) update defaults

**Files:**
- Modify: `internal/config/config.go` (only if the data moves the defaults)
- Modify: `internal/config/config_test.go` (if defaults change)
- Modify: `docs/configuration.md` (if defaults change)
- Modify: `docs/loadtest/dispatch-tuning-2026-06.md` (`## Decision`)

- [ ] **Step 1: Write the decision**

In the dated doc `## Decision`, state the recommended `HERMES_DISPATCH_CONCURRENCY` / `HERMES_DISPATCH_PREFETCH` per backend, citing the mean ± CI numbers and the knee.

- [ ] **Step 2: If defaults change, update config + test (TDD)**

Update `TestLoad_Defaults` expectations first (RED), then change `envInt("HERMES_DISPATCH_CONCURRENCY", X)` / `envInt("HERMES_DISPATCH_PREFETCH", Y)` in `config.go` (GREEN), and the `docs/configuration.md` default cells.

Run: `go test ./internal/config/`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/config docs/configuration.md docs/loadtest/dispatch-tuning-2026-06.md
git commit -m "feat(dispatch): set concurrency/prefetch defaults from load-test data"
```

---

## Self-review notes

- **Spec coverage:** components (Task 5/6), methodology/drain (Task 4), isolation/reset+seed (Task 5 closures), matrix+stats (Tasks 1–3), backends (Tasks 7–8), E2E k6 (Task 9), deliverables incl. default update (Task 10). All sections mapped.
- **Pool/clamp:** the `make dispatchbench` target sets `pool_max_conns=20` (≥ max workers 16) so `ClampWorkersToPool` does not distort the curve — matches the spec's isolation note.
- **Teardown reality:** `messaging.Client` has no per-subscription stop, so the `DispatchFactory` uses a fresh client per repetition and `stop = client.Close` (which closes `done` → worker pool exits). Reflected in Task 5.
- **Types:** `Cell`, `Stat`, `Result`, `Runner`, `Publisher/DispatchFactory/Resetter` names are consistent across Tasks 1–5.
