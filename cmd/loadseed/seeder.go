// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package main

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func runSeed(ctx context.Context, pool *pgxpool.Pool, cfg Config) error {
	rid := runID()
	m := &Manifest{
		SeededAt:  time.Now().UTC().Format(time.RFC3339),
		RunSeedID: rid,
	}

	rawKey, _, err := insertAPIKey(ctx, pool, cfg.HMACSecret, "loadtest-"+rid)
	if err != nil {
		return fmt.Errorf("api key: %w", err)
	}
	m.APIKey = rawKey

	organizationIDs, err := insertOrganizations(ctx, pool, cfg.Organizations, rid)
	if err != nil {
		return fmt.Errorf("organizations: %w", err)
	}

	m.Organizations = make([]Organization, cfg.Organizations)
	for i, tid := range organizationIDs {
		if _, err := insertUsers(ctx, pool, tid, cfg.UsersPerOrganization, rid, i); err != nil {
			return fmt.Errorf("users[%d]: %w", i, err)
		}
		cats, err := insertSubscriptionTree(ctx, pool, rid, i,
			cfg.CategoriesPerOrganization, cfg.SubscriptionsPerCategory, cfg.TemplatesPerSubscription)
		if err != nil {
			return fmt.Errorf("tree[%d]: %w", i, err)
		}
		m.Organizations[i] = Organization{
			ID:         tid,
			Index:      i,
			UserCount:  cfg.UsersPerOrganization,
			Categories: cats,
		}
	}

	if err := m.Write(cfg.OutputPath); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	fmt.Printf("load-test seed complete: %d organizations, %d users, run_seed_id=%s\n  manifest=%s\n  api_key=%s\n",
		cfg.Organizations, cfg.Organizations*cfg.UsersPerOrganization, rid, cfg.OutputPath, rawKey)
	return nil
}
