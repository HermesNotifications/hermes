// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

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
