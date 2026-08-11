// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

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
	organizationIDs, err := insertOrganizations(ctx, pool, 1, rid)
	if err != nil {
		t.Fatalf("organizations: %v", err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM organizations WHERE id = ANY($1)`, organizationIDs) }()

	ids, err := insertUsers(ctx, pool, organizationIDs[0], 500, rid, 0)
	if err != nil {
		t.Fatalf("users: %v", err)
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = ANY($1)`, ids) }()

	if len(ids) != 500 {
		t.Fatalf("want 500 ids, got %d", len(ids))
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE organization_id = $1`, organizationIDs[0]).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 500 {
		t.Fatalf("want 500 rows, got %d", count)
	}

	var addr string
	if err := pool.QueryRow(ctx,
		`SELECT address FROM user_contact_points WHERE user_id = $1 AND address_key = 'email'`,
		ids[0]).Scan(&addr); err != nil {
		t.Fatalf("email contact: %v", err)
	}
	if addr == "" {
		t.Fatalf("empty email contact")
	}
}
