package main

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// insertTenants creates n tenants and returns their UUIDs.
// Name is "loadtest-<runID>-<idx>" so runs are identifiable and easy to clean up manually.
//
func insertTenants(ctx context.Context, pool *pgxpool.Pool, n int, runID string) ([]string, error) {
	ids := make([]string, n)
	names := make([]string, n)
	for i := 0; i < n; i++ {
		ids[i] = uuid.NewString()
		names[i] = fmt.Sprintf("loadtest-%s-%d", runID, i)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO tenants (id, name)
		 SELECT unnest($1::uuid[]), unnest($2::text[])`,
		ids, names,
	); err != nil {
		return nil, fmt.Errorf("insert tenants: %w", err)
	}
	return ids, nil
}
