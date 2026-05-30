// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

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

	tenantIDs, err := insertTenants(ctx, pool, cfg.Tenants, rid)
	if err != nil {
		return fmt.Errorf("tenants: %w", err)
	}

	m.Tenants = make([]Tenant, cfg.Tenants)
	for i, tid := range tenantIDs {
		userIDs, err := insertUsers(ctx, pool, tid, cfg.UsersPerTenant, rid, i)
		if err != nil {
			return fmt.Errorf("users[%d]: %w", i, err)
		}
		cats, err := insertSubscriptionTree(ctx, pool, rid, i,
			cfg.CategoriesPerTenant, cfg.SubscriptionsPerCategory, cfg.TemplatesPerSubscription)
		if err != nil {
			return fmt.Errorf("tree[%d]: %w", i, err)
		}
		m.Tenants[i] = Tenant{ID: tid, Users: userIDs, Categories: cats}
	}

	if err := m.Write(cfg.OutputPath); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	fmt.Printf("load-test seed complete: %d tenants, %d users, run_seed_id=%s\n  manifest=%s\n  api_key=%s\n",
		cfg.Tenants, cfg.Tenants*cfg.UsersPerTenant, rid, cfg.OutputPath, rawKey)
	return nil
}
