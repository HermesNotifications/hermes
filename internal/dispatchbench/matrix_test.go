// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

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
