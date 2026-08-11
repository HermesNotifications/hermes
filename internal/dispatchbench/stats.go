// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

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
