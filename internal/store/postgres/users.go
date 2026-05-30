// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package postgres

import (
	"context"
	"fmt"

	id "github.com/hermes-notifications/hermes/internal/id/v2"
	"github.com/hermes-notifications/hermes/internal/models"
)

func (s *Store) EnsureUser(ctx context.Context, tenantID, externalID string) (*models.User, error) {
	newID := id.User.New()
	u := &models.User{}
	err := s.pool.QueryRow(ctx,
		`WITH ins AS (
			INSERT INTO users (id, tenant_id, external_id)
			VALUES ($1, $2, $3)
			ON CONFLICT (tenant_id, external_id) DO NOTHING
			RETURNING id, tenant_id, external_id, email, phone, locale, created_at
		)
		SELECT * FROM ins
		UNION ALL
		SELECT id, tenant_id, external_id, email, phone, locale, created_at
		FROM users
		WHERE tenant_id = $2 AND external_id = $3
		LIMIT 1`,
		newID, tenantID, externalID,
	).Scan(&u.ID, &u.TenantID, &u.ExternalID, &u.Email, &u.Phone, &u.Locale, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("ensure user: %w", err)
	}
	return u, nil
}

func (s *Store) GetUserByID(ctx context.Context, userID string) (*models.User, error) {
	u := &models.User{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, external_id, email, phone, locale, created_at
		 FROM users WHERE id = $1`, userID,
	).Scan(&u.ID, &u.TenantID, &u.ExternalID, &u.Email, &u.Phone, &u.Locale, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get user by id: %w", err)
	}
	return u, nil
}

func (s *Store) ListUsers(ctx context.Context, tenantID string) ([]models.User, error) {
	var query string
	var args []any
	if tenantID != "" {
		query = `SELECT id, tenant_id, external_id, email, phone, locale, created_at
			FROM users WHERE tenant_id = $1 ORDER BY created_at DESC`
		args = []any{tenantID}
	} else {
		query = `SELECT id, tenant_id, external_id, email, phone, locale, created_at
			FROM users ORDER BY created_at DESC`
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.TenantID, &u.ExternalID, &u.Email, &u.Phone, &u.Locale, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("list users scan: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *Store) UpdateUserContacts(ctx context.Context, userID string, email, phone *string) (*models.User, error) {
	u := &models.User{}
	err := s.pool.QueryRow(ctx,
		`UPDATE users
		 SET email = COALESCE($2, email), phone = COALESCE($3, phone)
		 WHERE id = $1
		 RETURNING id, tenant_id, external_id, email, phone, locale, created_at`,
		userID, email, phone,
	).Scan(&u.ID, &u.TenantID, &u.ExternalID, &u.Email, &u.Phone, &u.Locale, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("update user contacts: %w", err)
	}
	return u, nil
}
