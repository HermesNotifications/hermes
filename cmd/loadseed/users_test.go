// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

//go:build integration

package main

import (
	"context"
	"testing"

	"github.com/hermesnotifications/hermes/internal/database"
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

	users, err := insertUsers(ctx, pool, organizationIDs[0], 500, rid, 0)
	if err != nil {
		t.Fatalf("users: %v", err)
	}
	ids := make([]string, len(users))
	for i, u := range users {
		ids[i] = u.ID
	}
	defer func() { _, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = ANY($1)`, ids) }()

	if len(users) != 500 {
		t.Fatalf("want 500 users, got %d", len(users))
	}

	// The returned ExternalID must be the one actually stored, because the scenarios
	// send it as to.user_id and dispatch resolves it with EnsureUser(org, external_id).
	// If it drifts, dispatch creates a fresh user and the inbox push goes to a channel
	// nobody is subscribed to.
	var storedExt string
	if err := pool.QueryRow(ctx, `SELECT external_id FROM users WHERE id = $1`, users[0].ID).Scan(&storedExt); err != nil {
		t.Fatalf("external_id: %v", err)
	}
	if storedExt != users[0].ExternalID {
		t.Fatalf("external_id mismatch: manifest %q, database %q", users[0].ExternalID, storedExt)
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
