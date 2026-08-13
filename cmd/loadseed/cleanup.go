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
	// VACUUM between deleting the notifications and deleting the users they referenced.
	//
	// Not tidiness -- the users delete does not complete without it. Every user deleted is
	// checked against notifications.user_id, and immediately after a mass delete that index
	// is still full of dead entries which each check has to walk. Measured on this database:
	// removing 220k users ran past ten minutes and was cancelled, then completed in 7m10s
	// once notifications had been vacuumed, with nothing else changed.
	//
	// It has to be its own statement because VACUUM cannot run inside a transaction block.
	//
	// PARALLEL 0 because a container's /dev/shm defaults to 64MB, and parallel vacuum workers
	// allocate shared memory segments out of it. Against a table this size the plain form
	// fails outright:
	//
	//   could not resize shared memory segment to 67145344 bytes: No space left on device
	//
	// Single-threaded is fast enough here and does not depend on how the server was packaged.
	// The bundled Postgres now also mounts a larger /dev/shm, but this has to work against
	// whatever is already deployed.
	if _, err := pool.Exec(ctx, `VACUUM (ANALYZE, PARALLEL 0) notifications`); err != nil {
		return fmt.Errorf("vacuum notifications: %w", err)
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
	// Expect this to be the slow step: roughly 2ms per user, so ~7 minutes for 220k, even
	// with the vacuum above. Deleting the notifications themselves is two orders of magnitude
	// faster -- 668k rows in 11 seconds -- because nothing references them and there is no
	// per-row work. Users are the opposite: each one is checked against notifications and
	// user_subscriptions and cascades into user_contact_points, and per-row trigger work on
	// replicated storage is what costs the time. Reducing it means fewer referencing tables,
	// not a better query.
	if _, err := pool.Exec(ctx,
		`DELETE FROM users WHERE id IN (SELECT unnest($1::text[]))`, allUsers); err != nil {
		return fmt.Errorf("delete users: %w", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM organizations WHERE id = ANY($1)`, allOrganizations); err != nil {
		return fmt.Errorf("delete organizations: %w", err)
	}
	// Sweep events orphaned during the run above.
	//
	// The pipeline is still draining when cleanup starts: the event writer keeps flushing
	// batches for notifications that were deleted seconds earlier, and since migration 000014
	// dropped the foreign key there is nothing to reject them. They land referencing rows
	// that no longer exist. Observed after a real run: 794 events arrived after their
	// notifications were gone, and no id-based delete would have caught them because the ids
	// they point at had already been removed from the manifest's reach.
	//
	// Deliberately not scoped to this run: an event whose notification does not exist cannot
	// be attributed to a run, and is unreadable garbage whoever created it.
	if _, err := pool.Exec(ctx,
		`DELETE FROM notification_events e
		  WHERE NOT EXISTS (SELECT 1 FROM notifications n WHERE n.id = e.notification_id)`); err != nil {
		return fmt.Errorf("sweep orphaned events: %w", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM api_keys WHERE name = $1`, "loadtest-"+m.RunSeedID); err != nil {
		return fmt.Errorf("delete api key: %w", err)
	}
	fmt.Printf("load-test cleanup complete (run_seed_id=%s)\n", m.RunSeedID)
	return nil
}
