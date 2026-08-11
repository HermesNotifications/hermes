// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// insertUsers bulk-inserts n users for the given organization using pgx's CopyFrom.
// Deterministic IDs: lt-<runID>-t<organizationIdx>-u<i>; stable email/phone/external_id
// derived the same way so reruns against the same organization produce the same users.
//
func insertUsers(ctx context.Context, pool *pgxpool.Pool, organizationID string, n int, runID string, organizationIdx int) ([]string, error) {
	rows := make([][]any, n)
	contactRows := make([][]any, 0, n*2)
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("lt-%s-t%d-u%d", runID, organizationIdx, i)
		email := fmt.Sprintf("%s@loadtest.local", id)
		phone := fmt.Sprintf("+1555%010d", (organizationIdx*1_000_000)+i)
		extID := fmt.Sprintf("ext-%d-%d", organizationIdx, i)
		ids[i] = id
		rows[i] = []any{id, organizationID, extID, "en"}
		contactRows = append(contactRows,
			[]any{id, "email", email, false},
			[]any{id, "phone", phone, false},
		)
	}
	if _, err := pool.CopyFrom(ctx,
		pgx.Identifier{"users"},
		[]string{"id", "organization_id", "external_id", "locale"},
		pgx.CopyFromRows(rows),
	); err != nil {
		return nil, fmt.Errorf("copy users: %w", err)
	}
	if _, err := pool.CopyFrom(ctx,
		pgx.Identifier{"user_contact_points"},
		[]string{"user_id", "address_key", "address", "verified"},
		pgx.CopyFromRows(contactRows),
	); err != nil {
		return nil, fmt.Errorf("copy user contacts: %w", err)
	}
	return ids, nil
}
