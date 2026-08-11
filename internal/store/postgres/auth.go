// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/jackc/pgx/v5"
)

// nullableOrg maps the empty string to SQL NULL. An empty organization_id must not
// be stored as '' — the column is a UUID with a foreign key, so '' fails the cast,
// and NULL is what "this key predates scoping" means in the schema.
func nullableOrg(id string) any {
	if id == "" {
		return nil
	}
	return id
}

func (s *Store) CreateAPIKey(ctx context.Context, id, keyHash, name, organizationID string, permissions []string) (*models.APIKey, error) {
	k := &models.APIKey{ID: id, KeyHash: keyHash, Name: name, OrganizationID: organizationID, Permissions: permissions}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO api_keys (id, key_hash, name, organization_id, permissions) VALUES ($1, $2, $3, $4, $5) RETURNING created_at`,
		k.ID, k.KeyHash, k.Name, nullableOrg(k.OrganizationID), k.Permissions,
	).Scan(&k.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create api key: %w", err)
	}
	return k, nil
}

func (s *Store) ListAPIKeys(ctx context.Context) ([]models.APIKey, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, key_hash, name, organization_id, permissions, created_at FROM api_keys`)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	var keys []models.APIKey
	for rows.Next() {
		var k models.APIKey
		var org *string
		if err := rows.Scan(&k.ID, &k.KeyHash, &k.Name, &org, &k.Permissions, &k.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		if org != nil {
			k.OrganizationID = *org
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (s *Store) GetAPIKeyByID(ctx context.Context, id string) (*models.APIKey, error) {
	var k models.APIKey
	var org *string
	err := s.pool.QueryRow(ctx,
		`SELECT id, key_hash, name, organization_id, permissions, created_at FROM api_keys WHERE id = $1`,
		id,
	).Scan(&k.ID, &k.KeyHash, &k.Name, &org, &k.Permissions, &k.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get api key: %w", err)
	}
	if org != nil {
		k.OrganizationID = *org
	}
	return &k, nil
}

func (s *Store) DeleteAPIKey(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM api_keys WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("api key not found: %s", id)
	}
	return nil
}

// ListActiveJWTSigningKeys returns active keys WITH secrets (for internal JWT validation).
func (s *Store) ListActiveJWTSigningKeys(ctx context.Context) ([]models.JWTSigningKey, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, algorithm, secret, user_id_claim, organization_id_claim, active, created_at
		 FROM jwt_signing_keys WHERE active = true`)
	if err != nil {
		return nil, fmt.Errorf("list active jwt signing keys: %w", err)
	}
	defer rows.Close()

	var keys []models.JWTSigningKey
	for rows.Next() {
		var k models.JWTSigningKey
		if err := rows.Scan(&k.ID, &k.Name, &k.Algorithm, &k.Secret, &k.UserIDClaim, &k.OrganizationIDClaim, &k.Active, &k.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan jwt signing key: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// EnsureHermesSigningKey inserts the Hermes internal signing key if it does not
// already exist. First write wins: an existing row is never overwritten.
//
// If the caller's secret disagrees with the stored one, the stored key is kept and
// a warning is logged. This runs at startup in admin, inbox and user, each passing
// cfg.JWTSecret — which has a default — so the previous ON CONFLICT DO UPDATE meant
// a single service booting without HERMES_JWT_SECRET set silently replaced a
// properly rotated signing key and invalidated every token issued under it.
//
// The consequence is that setting HERMES_JWT_SECRET against a database that
// already holds this row does NOT rotate it. Rotation is a deliberate operation on
// jwt_signing_keys, and must be coordinated with Centrifugo's single
// token_hmac_secret_key. See docs/architecture.md.
func (s *Store) EnsureHermesSigningKey(ctx context.Context, secret string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO jwt_signing_keys (id, name, algorithm, secret, user_id_claim, organization_id_claim)
		 VALUES ('hermes-internal', 'hermes-internal', 'HS256', $1, 'sub', 'organization_id')
		 ON CONFLICT (id) DO NOTHING`,
		secret,
	)
	if err != nil {
		return fmt.Errorf("ensure hermes signing key: %w", err)
	}

	var stored string
	if err := s.pool.QueryRow(ctx,
		`SELECT secret FROM jwt_signing_keys WHERE id = 'hermes-internal'`,
	).Scan(&stored); err != nil {
		return fmt.Errorf("read back hermes signing key: %w", err)
	}

	// Neither secret is logged, nor any prefix of one: the row's whole purpose is
	// that its contents are not disclosed. That the two differ is the signal.
	if stored != secret {
		slog.WarnContext(ctx,
			"configured JWT signing secret differs from the stored signing key; the configured value is ignored",
			"key_id", "hermes-internal",
			"remedy", "unset HERMES_JWT_SECRET to adopt the stored key, or rotate jwt_signing_keys deliberately",
		)
	}
	return nil
}
