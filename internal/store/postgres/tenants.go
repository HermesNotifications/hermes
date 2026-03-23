package postgres

import (
	"context"
	"fmt"

	"github.com/hermes-notifications/hermes/internal/models"
)

func (s *Store) CreateTenant(ctx context.Context, id, name string) (*models.Tenant, error) {
	t := &models.Tenant{ID: id, Name: name}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO tenants (id, name) VALUES ($1, $2) RETURNING default_locale, settings, created_at`,
		t.ID, t.Name,
	).Scan(&t.DefaultLocale, &t.Settings, &t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create tenant: %w", err)
	}
	return t, nil
}

func (s *Store) GetTenantByID(ctx context.Context, id string) (*models.Tenant, error) {
	t := &models.Tenant{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, default_locale, settings, created_at FROM tenants WHERE id = $1`, id,
	).Scan(&t.ID, &t.Name, &t.DefaultLocale, &t.Settings, &t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get tenant: %w", err)
	}
	return t, nil
}
