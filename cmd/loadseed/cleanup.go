// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

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
		for _, u := range m.UsersOf(o) {
			allUsers = append(allUsers, u.ID)
		}
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

	// The notifications the run actually produced have to go first, and they were missing.
	//
	// notifications.template_id references notification_templates, so cleanup failed outright
	// against any run that had sent traffic -- which is every real run:
	//
	//   update or delete on table "notification_templates" violates foreign key constraint
	//   "notifications_template_id_fkey" on table "notifications"
	//
	// It only ever succeeded on a seed nobody had used. Events are deleted by notification id
	// rather than left to a cascade, because migration 000014 dropped that foreign key.
	if _, err := pool.Exec(ctx,
		`DELETE FROM notification_events WHERE notification_id IN (
		     SELECT id FROM notifications WHERE organization_id = ANY($1))`,
		allOrganizations); err != nil {
		return fmt.Errorf("delete notification events: %w", err)
	}
	if _, err := pool.Exec(ctx,
		`DELETE FROM notifications WHERE organization_id = ANY($1)`, allOrganizations); err != nil {
		return fmt.Errorf("delete notifications: %w", err)
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
	// KNOWN LIMITATION: this does not finish in reasonable time on a large seed.
	//
	// Deleting 60k users out of 100k ran for 19 minutes without committing and had to be
	// cancelled, against a bundled single-pod Postgres on replicated (Longhorn) storage. The
	// plan itself is fine -- a nested loop over a hash aggregate, and rewriting `= ANY($1)`
	// as the unnest below changed nothing measurable -- so the cost is in the referential
	// integrity triggers, which fire per deleted row against notifications,
	// user_contact_points (two rows per user) and user_subscriptions, each with its own
	// fsync-bound write.
	//
	// Batching the delete and vacuuming between batches is the fix, and it has not been
	// done. Until it is, cleaning up a large seed means dropping and recreating the database,
	// or leaving the rows in place -- they are inert.
	if _, err := pool.Exec(ctx,
		`DELETE FROM users WHERE id IN (SELECT unnest($1::text[]))`, allUsers); err != nil {
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
