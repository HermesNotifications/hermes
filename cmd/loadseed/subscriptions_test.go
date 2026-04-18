//go:build integration

package main

import (
	"context"
	"testing"

	"github.com/hermes-notifications/hermes/internal/database"
)

func TestInsertSubscriptionTree(t *testing.T) {
	ctx := context.Background()
	pool, err := database.NewPool(ctx, envOr("HERMES_DATABASE_URL", "postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable"))
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer pool.Close()

	rid := runID()
	cats, err := insertSubscriptionTree(ctx, pool, rid, 0, 2, 2, 2)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	defer cleanupSubscriptionTree(ctx, pool, cats)

	if len(cats) != 2 {
		t.Fatalf("want 2 categories, got %d", len(cats))
	}
	if len(cats[0].Subscriptions) != 2 {
		t.Fatalf("want 2 subscriptions, got %d", len(cats[0].Subscriptions))
	}
	if len(cats[0].Subscriptions[0].Templates) != 2 {
		t.Fatalf("want 2 templates, got %d", len(cats[0].Subscriptions[0].Templates))
	}

	tmplID := cats[0].Subscriptions[0].Templates[0].ID
	var channels []string
	if err := pool.QueryRow(ctx, `SELECT default_channels FROM notification_templates WHERE id = $1`, tmplID).Scan(&channels); err != nil {
		t.Fatalf("query template: %v", err)
	}
	if len(channels) != 2 {
		t.Fatalf("want 2 channels, got %v", channels)
	}
}
