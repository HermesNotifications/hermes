// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package postgres

import (
	"github.com/hermes-notifications/hermes/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Compile-time check that *Store satisfies the full Repository interface.
var _ store.Repository = (*Store)(nil)

// Store is the PostgreSQL implementation of the store.Repository interface.
type Store struct {
	pool *pgxpool.Pool
}

// New creates a new PostgreSQL-backed store.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}
