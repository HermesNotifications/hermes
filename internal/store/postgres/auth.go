package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/hermes-notifications/hermes/internal/models"
)

func (s *Store) CreateAPIKey(ctx context.Context, id, keyHash, name string, permissions []string) (*models.APIKey, error) {
	k := &models.APIKey{ID: id, KeyHash: keyHash, Name: name, Permissions: permissions}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO api_keys (id, key_hash, name, permissions) VALUES ($1, $2, $3, $4) RETURNING created_at`,
		k.ID, k.KeyHash, k.Name, k.Permissions,
	).Scan(&k.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create api key: %w", err)
	}
	return k, nil
}

func (s *Store) ListAPIKeys(ctx context.Context) ([]models.APIKey, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, key_hash, name, permissions, created_at FROM api_keys`)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	var keys []models.APIKey
	for rows.Next() {
		var k models.APIKey
		if err := rows.Scan(&k.ID, &k.KeyHash, &k.Name, &k.Permissions, &k.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (s *Store) GetAPIKeyByID(ctx context.Context, id string) (*models.APIKey, error) {
	var k models.APIKey
	err := s.pool.QueryRow(ctx,
		`SELECT id, key_hash, name, permissions, created_at FROM api_keys WHERE id = $1`,
		id,
	).Scan(&k.ID, &k.KeyHash, &k.Name, &k.Permissions, &k.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get api key: %w", err)
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

// EnsureHermesSigningKey inserts the Hermes internal signing key if it doesn't already exist.
func (s *Store) EnsureHermesSigningKey(ctx context.Context, secret string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO jwt_signing_keys (id, name, algorithm, secret, user_id_claim, organization_id_claim)
		 VALUES ('hermes-internal', 'hermes-internal', 'HS256', $1, 'sub', 'organization_id')
		 ON CONFLICT (id) DO UPDATE SET secret = $1`,
		secret,
	)
	if err != nil {
		return fmt.Errorf("ensure hermes signing key: %w", err)
	}
	return nil
}
