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
		out += "| workers | prefetch | mean msgs/s | 95% CI | CV |\n|---|---|---|---|---|\n"
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
