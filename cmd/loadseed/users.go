// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// insertUsers bulk-inserts n users for the given tenant using pgx's CopyFrom.
// Deterministic IDs: lt-<runID>-t<tenantIdx>-u<i>; stable email/phone/external_id
// derived the same way so reruns against the same tenant produce the same users.
//
func insertUsers(ctx context.Context, pool *pgxpool.Pool, tenantID string, n int, runID string, tenantIdx int) ([]string, error) {
	rows := make([][]any, n)
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("lt-%s-t%d-u%d", runID, tenantIdx, i)
		email := fmt.Sprintf("%s@loadtest.local", id)
		phone := fmt.Sprintf("+1555%010d", (tenantIdx*1_000_000)+i)
		extID := fmt.Sprintf("ext-%d-%d", tenantIdx, i)
		ids[i] = id
		rows[i] = []any{id, tenantID, extID, email, phone, "en"}
	}
	if _, err := pool.CopyFrom(ctx,
		pgx.Identifier{"users"},
		[]string{"id", "tenant_id", "external_id", "email", "phone", "locale"},
		pgx.CopyFromRows(rows),
	); err != nil {
		return nil, fmt.Errorf("copy users: %w", err)
	}
	return ids, nil
}
