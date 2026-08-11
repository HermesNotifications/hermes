// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package database

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolConfig bounds a service's Postgres connection usage.
//
// It exists because pgx's own default does not: MaxConns is max(4, runtime.NumCPU()), and
// NumCPU reads the *node*, not the pod. No Hermes service sets a CPU limit (a deliberate choice
// — see deploy/k8s/overlays/production/patches/resources.yaml), so the same image opened 4
// connections on a 4-core node and 16 on a 16-core one. With the inbox HPA at 10 replicas that
// is somewhere between 40 and 160 connections from one service, decided by the scheduler.
//
// A fixed number is what makes the cluster-wide arithmetic possible, and
// scripts/check_db_pool_budget.py does that arithmetic against Postgres' max_connections.
type PoolConfig struct {
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

// DefaultPoolConfig is applied when a caller passes the zero value.
var DefaultPoolConfig = PoolConfig{
	MaxConns:        10,
	MinConns:        2,
	MaxConnLifetime: 30 * time.Minute,
	MaxConnIdleTime: 5 * time.Minute,
}

// maxConnLifetimeJitter spreads connection recycling out.
//
// Without it every connection a pool opened during a burst reaches MaxConnLifetime at the same
// instant and is replaced together — a self-inflicted reconnect storm against Postgres,
// arriving exactly 30 minutes after the traffic spike that caused it.
const maxConnLifetimeJitter = 5 * time.Minute

// NewPool connects to Postgres with the default pool bounds.
func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	return NewPoolWithConfig(ctx, databaseURL, PoolConfig{})
}

// NewPoolWithConfig connects to Postgres with explicit pool bounds.
//
// Precedence, stated because it will surprise someone: pool_* parameters in the connection URL
// win over cfg. cmd/dispatchbench relies on `pool_max_conns` in the URL to sweep pool sizes, and
// silently overriding it would make every benchmark run at the same size while appearing to
// vary. cfg fills in whatever the URL did not set.
func NewPoolWithConfig(ctx context.Context, databaseURL string, cfg PoolConfig) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	urlParams := parsedPoolParams(databaseURL)

	if _, fromURL := urlParams["pool_max_conns"]; !fromURL {
		config.MaxConns = orDefault(cfg.MaxConns, DefaultPoolConfig.MaxConns)
	}
	if _, fromURL := urlParams["pool_min_conns"]; !fromURL {
		config.MinConns = orDefault(cfg.MinConns, DefaultPoolConfig.MinConns)
	}
	if _, fromURL := urlParams["pool_max_conn_lifetime"]; !fromURL {
		config.MaxConnLifetime = orDefaultDuration(cfg.MaxConnLifetime, DefaultPoolConfig.MaxConnLifetime)
	}
	if _, fromURL := urlParams["pool_max_conn_idle_time"]; !fromURL {
		config.MaxConnIdleTime = orDefaultDuration(cfg.MaxConnIdleTime, DefaultPoolConfig.MaxConnIdleTime)
	}
	if _, fromURL := urlParams["pool_max_conn_lifetime_jitter"]; !fromURL {
		config.MaxConnLifetimeJitter = maxConnLifetimeJitter
	}

	config.ConnConfig.Tracer = otelpgx.NewTracer()
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	instrumentPool(pool)
	return pool, nil
}

// parsedPoolParams reports which pool_* keys the URL set. Read from the raw string because
// ParseConfig has already folded them into the config, leaving no way to tell an explicit value
// from pgx's default.
func parsedPoolParams(databaseURL string) map[string]struct{} {
	found := map[string]struct{}{}
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return found
	}
	for key := range parsed.Query() {
		if strings.HasPrefix(key, "pool_") {
			found[key] = struct{}{}
		}
	}
	return found
}

func orDefault(v, fallback int32) int32 {
	if v <= 0 {
		return fallback
	}
	return v
}

func orDefaultDuration(v, fallback time.Duration) time.Duration {
	if v <= 0 {
		return fallback
	}
	return v
}

func RunMigrations(databaseURL, migrationsPath string) error {
	m, err := migrate.New("file://"+migrationsPath, databaseURL)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}
