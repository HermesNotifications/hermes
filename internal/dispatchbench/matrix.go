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
