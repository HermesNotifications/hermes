// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

//go:build integration

package main

import (
	"context"
	"testing"

	"github.com/hermes-notifications/hermes/internal/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

// cleanupSubscriptionTree removes a tree of categories, subscriptions, and templates.
// Deletes in FK-safe order: templates → subscriptions → categories.
func cleanupSubscriptionTree(ctx context.Context, pool *pgxpool.Pool, cats []Category) {
	var tmplIDs, subIDs, catIDs []string
	for _, c := range cats {
		catIDs = append(catIDs, c.ID)
		for _, s := range c.Subscriptions {
			subIDs = append(subIDs, s.ID)
			for _, t := range s.Templates {
				tmplIDs = append(tmplIDs, t.ID)
			}
		}
	}
	_, _ = pool.Exec(ctx, `DELETE FROM notification_templates WHERE id = ANY($1)`, tmplIDs)
	_, _ = pool.Exec(ctx, `DELETE FROM subscriptions WHERE id = ANY($1)`, subIDs)
	_, _ = pool.Exec(ctx, `DELETE FROM subscription_categories WHERE id = ANY($1)`, catIDs)
}

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
