// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package apikeybootstrap

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresKeys implements KeyStore against the api_keys table.
//
// Deliberately not routed through internal/store: that package's AdminStore carries the whole
// admin surface, and this needs two statements. A Job that can only do these two is easier to
// reason about than one holding a handle on everything.
type PostgresKeys struct{ pool *pgxpool.Pool }

// NewPostgresKeys wraps a pool.
func NewPostgresKeys(pool *pgxpool.Pool) *PostgresKeys { return &PostgresKeys{pool: pool} }

// APIKeyExists implements KeyStore.
func (p *PostgresKeys) APIKeyExists(ctx context.Context, keyID string) (bool, error) {
	var exists bool
	err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM api_keys WHERE id = $1)`, keyID).Scan(&exists)
	return exists, err
}

// InsertAPIKey implements KeyStore.
//
// ON CONFLICT (id) DO NOTHING rather than an error, because two Jobs racing -- or one Job
// retried after a partial failure -- must converge on one key rather than fail the install.
// Callers treat a no-op insert as success; Run() has already established that this id is the
// one it wants present.
func (p *PostgresKeys) InsertAPIKey(ctx context.Context, keyID, keyHash, name string, permissions []string) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO api_keys (id, key_hash, name, permissions)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (id) DO NOTHING`,
		keyID, keyHash, name, permissions,
	)
	return err
}
