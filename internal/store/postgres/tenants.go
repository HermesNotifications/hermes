// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

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

func (s *Store) EnsureTenant(ctx context.Context, id string) (*models.Tenant, error) {
	t := &models.Tenant{}
	err := s.pool.QueryRow(ctx,
		`WITH ins AS (
			INSERT INTO tenants (id, name) VALUES ($1, $2)
			ON CONFLICT (id) DO NOTHING
			RETURNING id, name, default_locale, settings, created_at
		)
		SELECT * FROM ins
		UNION ALL
		SELECT id, name, default_locale, settings, created_at
		FROM tenants WHERE id = $1
		LIMIT 1`,
		id, id,
	).Scan(&t.ID, &t.Name, &t.DefaultLocale, &t.Settings, &t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("ensure tenant: %w", err)
	}
	return t, nil
}

func (s *Store) ListTenants(ctx context.Context) ([]models.Tenant, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, default_locale, settings, created_at FROM tenants ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()

	var tenants []models.Tenant
	for rows.Next() {
		var t models.Tenant
		if err := rows.Scan(&t.ID, &t.Name, &t.DefaultLocale, &t.Settings, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("list tenants scan: %w", err)
		}
		tenants = append(tenants, t)
	}
	return tenants, rows.Err()
}

func (s *Store) CountUsersByTenant(ctx context.Context) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, `SELECT tenant_id, COUNT(*)::int FROM users GROUP BY tenant_id`)
	if err != nil {
		return nil, fmt.Errorf("count users by tenant: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var tenantID string
		var count int
		if err := rows.Scan(&tenantID, &count); err != nil {
			return nil, fmt.Errorf("count users by tenant scan: %w", err)
		}
		counts[tenantID] = count
	}
	return counts, rows.Err()
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
