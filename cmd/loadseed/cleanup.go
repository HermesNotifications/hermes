// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// runCleanup reads the manifest and deletes every seeded entity.
// Order respects FK constraints: templates → subscriptions → categories → users → organizations → api_key.
// The users table does not have ON DELETE CASCADE on organization_id, so users must be
// deleted explicitly before organizations.
func runCleanup(ctx context.Context, pool *pgxpool.Pool, cfg Config) error {
	m, err := ReadManifest(cfg.OutputPath)
	if err != nil {
		return err
	}

	var allTmpl, allSub, allCat, allUsers, allOrganizations []string
	for _, o := range m.Organizations {
		allOrganizations = append(allOrganizations, o.ID)
		allUsers = append(allUsers, o.Users...)
		for _, c := range o.Categories {
			allCat = append(allCat, c.ID)
			for _, s := range c.Subscriptions {
				allSub = append(allSub, s.ID)
				for _, tpl := range s.Templates {
					allTmpl = append(allTmpl, tpl.ID)
				}
			}
		}
	}

	if _, err := pool.Exec(ctx, `DELETE FROM notification_templates WHERE id = ANY($1)`, allTmpl); err != nil {
		return fmt.Errorf("delete templates: %w", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM subscriptions WHERE id = ANY($1)`, allSub); err != nil {
		return fmt.Errorf("delete subscriptions: %w", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM subscription_categories WHERE id = ANY($1)`, allCat); err != nil {
		return fmt.Errorf("delete categories: %w", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = ANY($1)`, allUsers); err != nil {
		return fmt.Errorf("delete users: %w", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM organizations WHERE id = ANY($1)`, allOrganizations); err != nil {
		return fmt.Errorf("delete organizations: %w", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM api_keys WHERE name = $1`, "loadtest-"+m.RunSeedID); err != nil {
		return fmt.Errorf("delete api key: %w", err)
	}
	fmt.Printf("load-test cleanup complete (run_seed_id=%s)\n", m.RunSeedID)
	return nil
}
