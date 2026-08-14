// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

//go:build integration

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hermesnotifications/hermes/internal/database"
)

func TestSeeder_EndToEnd(t *testing.T) {
	ctx := context.Background()
	pool, err := database.NewPool(ctx, envOr("HERMES_DATABASE_URL", "postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer pool.Close()

	out := filepath.Join(t.TempDir(), "manifest.json")
	cfg := Config{
		Organizations:                  2,
		UsersPerOrganization:           50,
		CategoriesPerOrganization:      2,
		SubscriptionsPerCategory: 2,
		TemplatesPerSubscription: 2,
		DatabaseURL:              envOr("HERMES_DATABASE_URL", "postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable"),
		HMACSecret:               envOr("HERMES_API_KEY_HMAC_SECRET", "hermes-dev-hmac-secret"),
		OutputPath:               out,
	}

	if err := runSeed(ctx, pool, cfg); err != nil {
		t.Fatalf("seed: %v", err)
	}
	defer func() { _ = runCleanup(ctx, pool, cfg) }()

	m, err := ReadManifest(out)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if len(m.Organizations) != 2 {
		t.Fatalf("want 2 organizations, got %d", len(m.Organizations))
	}
	if m.Organizations[0].UserCount != 50 {
		t.Fatalf("want 50 users, got %d", m.Organizations[0].UserCount)
	}
	// The regenerated ids must be the ones actually inserted, since nothing else ties the
	// manifest's counts back to the rows in the database.
	users := m.UsersOf(m.Organizations[0])
	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM users WHERE id = $1 AND external_id = $2`,
		users[0].ID, users[0].ExternalID).Scan(&count); err != nil {
		t.Fatalf("lookup regenerated user: %v", err)
	}
	if count != 1 {
		t.Fatalf("regenerated user %+v does not exist in the database", users[0])
	}
	if m.APIKey == "" {
		t.Fatalf("api key not set")
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("manifest not written: %v", err)
	}
}
