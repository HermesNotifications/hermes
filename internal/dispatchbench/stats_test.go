// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

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
