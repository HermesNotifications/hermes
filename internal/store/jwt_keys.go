package store

import (
	"context"
	"fmt"

	"github.com/hermes-notifications/hermes/internal/models"
)

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
