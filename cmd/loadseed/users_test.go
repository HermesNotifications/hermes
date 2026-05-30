// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

//go:build integration

package main

import (
	"context"
	"testing"

	"github.com/hermes-notifications/hermes/internal/database"
)

func TestInsertUsers_Copy(t *testing.T) {
	ctx := context.Background()
	pool, err := database.NewPool(ctx, envOr("HERMES_DATABASE_URL", "postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer pool.Close()

	rid := runID()
	tenantIDs, err := insertTenants(ctx, pool, 1, rid)
	if err != nil {
		t.Fatalf("tenants: %v", err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM tenants WHERE id = ANY($1)`, tenantIDs) }()

	ids, err := insertUsers(ctx, pool, tenantIDs[0], 500, rid, 0)
	if err != nil {
		t.Fatalf("users: %v", err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = ANY($1)`, ids) }()

	if len(ids) != 500 {
		t.Fatalf("want 500 ids, got %d", len(ids))
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE tenant_id = $1`, tenantIDs[0]).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 500 {
		t.Fatalf("want 500 rows, got %d", count)
	}

	var email string
	if err := pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, ids[0]).Scan(&email); err != nil {
		t.Fatalf("email: %v", err)
	}
	if email == "" {
		t.Fatalf("empty email")
	}
}
