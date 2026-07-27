// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

//go:build integration

package database_test

import (
	"context"
	"os"
	"testing"

	"github.com/hermes-notifications/hermes/internal/database"
)

func testDatabaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("HERMES_DATABASE_URL")
	if url == "" {
		url = "postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable"
	}
	return url
}

func TestNewPool(t *testing.T) {
	ctx := context.Background()
	pool, err := database.NewPool(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	var result int
	err = pool.QueryRow(ctx, "SELECT 1").Scan(&result)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if result != 1 {
		t.Fatalf("expected 1, got %d", result)
	}
}

func TestRunMigrations(t *testing.T) {
	err := database.RunMigrations(testDatabaseURL(t), "../../migrations")
	if err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	ctx := context.Background()
	pool, err := database.NewPool(ctx, testDatabaseURL(t))
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	tables := []string{"organizations", "api_keys", "users", "subscription_categories",
		"subscriptions", "notification_templates", "notifications", "notification_events", "user_subscriptions"}

	for _, table := range tables {
		var exists bool
		err := pool.QueryRow(ctx,
			"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)", table).Scan(&exists)
		if err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("table %s does not exist", table)
		}
	}
}
