package store

import (
	"context"
	"fmt"

	"github.com/hermes-notifications/hermes/internal/id"
	"github.com/hermes-notifications/hermes/internal/models"
)

func (s *Store) CreateJWTSigningKey(ctx context.Context, name, algorithm, secret, userIDClaim, tenantIDClaim string) (*models.JWTSigningKey, error) {
	k := &models.JWTSigningKey{
		ID:            id.New(),
		Name:          name,
		Algorithm:     algorithm,
		Secret:        secret,
		UserIDClaim:   userIDClaim,
		TenantIDClaim: tenantIDClaim,
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO jwt_signing_keys (id, name, algorithm, secret, user_id_claim, tenant_id_claim)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING active, created_at`,
		k.ID, k.Name, k.Algorithm, k.Secret, k.UserIDClaim, k.TenantIDClaim,
	).Scan(&k.Active, &k.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create jwt signing key: %w", err)
	}
	return k, nil
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

// ListJWTSigningKeys returns all keys (secrets excluded by model json tag).
func (s *Store) ListJWTSigningKeys(ctx context.Context) ([]models.JWTSigningKey, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, algorithm, user_id_claim, tenant_id_claim, active, created_at
		 FROM jwt_signing_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list jwt signing keys: %w", err)
	}
	defer rows.Close()

	var keys []models.JWTSigningKey
	for rows.Next() {
		var k models.JWTSigningKey
		if err := rows.Scan(&k.ID, &k.Name, &k.Algorithm, &k.UserIDClaim, &k.TenantIDClaim, &k.Active, &k.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan jwt signing key: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (s *Store) DeleteJWTSigningKey(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM jwt_signing_keys WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete jwt signing key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("jwt signing key not found: %s", id)
	}
	return nil
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
