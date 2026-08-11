// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package dispatch

// ClampWorkersToPool caps the dispatch worker count at the database connection
// pool size. Dispatch is I/O-bound: each worker holds at most one Postgres
// connection at a time while processing a message, so running more workers than
// the pool has connections yields no extra throughput — the surplus workers just
// block acquiring a connection, adding contention and latency. This is a safety
// guardrail, not a tuning model: the throughput-optimal worker count is found by
// load testing and is always <= the pool size.
//
// dbMaxConns <= 0 means the pool size is unknown (e.g. a non-Postgres backend);
// in that case the requested count is left untouched. Returns the effective
// worker count and whether it was clamped, so the caller can warn.
func ClampWorkersToPool(requested, dbMaxConns int) (effective int, clamped bool) {
	if dbMaxConns > 0 && requested > dbMaxConns {
		return dbMaxConns, true
	}
	return requested, false
}
