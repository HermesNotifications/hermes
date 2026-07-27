// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package postgres

import (
	"context"
	"fmt"

	"github.com/hermes-notifications/hermes/internal/models"
)

func (s *Store) CreateOrganization(ctx context.Context, id, name string) (*models.Organization, error) {
	t := &models.Organization{ID: id, Name: name}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO organizations (id, name) VALUES ($1, $2) RETURNING default_locale, settings, created_at`,
		t.ID, t.Name,
	).Scan(&t.DefaultLocale, &t.Settings, &t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create organization: %w", err)
	}
	return t, nil
}

func (s *Store) EnsureOrganization(ctx context.Context, id string) (*models.Organization, error) {
	t := &models.Organization{}
	err := s.pool.QueryRow(ctx,
		`WITH ins AS (
			INSERT INTO organizations (id, name) VALUES ($1, $2)
			ON CONFLICT (id) DO NOTHING
			RETURNING id, name, default_locale, settings, created_at
		)
		SELECT * FROM ins
		UNION ALL
		SELECT id, name, default_locale, settings, created_at
		FROM organizations WHERE id = $1
		LIMIT 1`,
		id, id,
	).Scan(&t.ID, &t.Name, &t.DefaultLocale, &t.Settings, &t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("ensure organization: %w", err)
	}
	return t, nil
}

func (s *Store) ListOrganizations(ctx context.Context) ([]models.Organization, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, default_locale, settings, created_at FROM organizations ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list organizations: %w", err)
	}
	defer rows.Close()

	var organizations []models.Organization
	for rows.Next() {
		var t models.Organization
		if err := rows.Scan(&t.ID, &t.Name, &t.DefaultLocale, &t.Settings, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("list organizations scan: %w", err)
		}
		organizations = append(organizations, t)
	}
	return organizations, rows.Err()
}

func (s *Store) CountUsersByOrganization(ctx context.Context) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, `SELECT organization_id, COUNT(*)::int FROM users GROUP BY organization_id`)
	if err != nil {
		return nil, fmt.Errorf("count users by organization: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var organizationID string
		var count int
		if err := rows.Scan(&organizationID, &count); err != nil {
			return nil, fmt.Errorf("count users by organization scan: %w", err)
		}
		counts[organizationID] = count
	}
	return counts, rows.Err()
}

func (s *Store) GetOrganizationByID(ctx context.Context, id string) (*models.Organization, error) {
	t := &models.Organization{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, default_locale, settings, created_at FROM organizations WHERE id = $1`, id,
	).Scan(&t.ID, &t.Name, &t.DefaultLocale, &t.Settings, &t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get organization: %w", err)
	}
	return t, nil
}
