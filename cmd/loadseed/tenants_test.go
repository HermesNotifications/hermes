// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

//go:build integration

package main

import (
	"context"
	"testing"

	"github.com/hermes-notifications/hermes/internal/database"
)

func TestInsertTenants(t *testing.T) {
	ctx := context.Background()
	pool, err := database.NewPool(ctx, envOr("HERMES_DATABASE_URL", "postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer pool.Close()

	rid := runID()
	ids, err := insertTenants(ctx, pool, 3, rid)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM tenants WHERE id = ANY($1)`, ids) }()

	if len(ids) != 3 {
		t.Fatalf("got %d ids, want 3", len(ids))
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM tenants WHERE id = ANY($1)`, ids).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 3 {
		t.Fatalf("want 3 rows, got %d", count)
	}
}
