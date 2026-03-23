package postgres

import (
	"context"
	"fmt"

	"github.com/hermes-notifications/hermes/internal/id"
	"github.com/hermes-notifications/hermes/internal/models"
)

func (s *Store) CreateAPIKey(ctx context.Context, keyHash, name string) (*models.APIKey, error) {
	k := &models.APIKey{ID: id.New(), KeyHash: keyHash, Name: name}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO api_keys (id, key_hash, name) VALUES ($1, $2, $3) RETURNING created_at`,
		k.ID, k.KeyHash, k.Name,
	).Scan(&k.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create api key: %w", err)
	}
	return k, nil
}

func (s *Store) ListAPIKeys(ctx context.Context) ([]models.APIKey, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, key_hash, name, created_at FROM api_keys`)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	var keys []models.APIKey
	for rows.Next() {
		var k models.APIKey
		if err := rows.Scan(&k.ID, &k.KeyHash, &k.Name, &k.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// ListActiveJWTSigningKeys returns active keys WITH secrets (for internal JWT validation).
func (s *Store) ListActiveJWTSigningKeys(ctx context.Context) ([]models.JWTSigningKey, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, algorithm, secret, user_id_claim, tenant_id_claim, active, created_at
		 FROM jwt_signing_keys WHERE active = true`)
	if err != nil {
		return nil, fmt.Errorf("list active jwt signing keys: %w", err)
	}
	defer rows.Close()

	var keys []models.JWTSigningKey
	for rows.Next() {
		var k models.JWTSigningKey
		if err := rows.Scan(&k.ID, &k.Name, &k.Algorithm, &k.Secret, &k.UserIDClaim, &k.TenantIDClaim, &k.Active, &k.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan jwt signing key: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// EnsureHermesSigningKey inserts the Hermes internal signing key if it doesn't already exist.
func (s *Store) EnsureHermesSigningKey(ctx context.Context, secret string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO jwt_signing_keys (id, name, algorithm, secret, user_id_claim, tenant_id_claim)
		 VALUES ('hermes-internal', 'hermes-internal', 'HS256', $1, 'sub', 'tenant_id')
		 ON CONFLICT (id) DO UPDATE SET secret = $1`,
		secret,
	)
	if err != nil {
		return fmt.Errorf("ensure hermes signing key: %w", err)
	}
	return nil
}
