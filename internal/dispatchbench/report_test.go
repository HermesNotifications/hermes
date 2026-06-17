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
