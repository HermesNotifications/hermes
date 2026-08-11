// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package postgres

import (
	"context"
	"fmt"

	id "github.com/hermes-notifications/hermes/internal/id/v2"
	"github.com/hermes-notifications/hermes/internal/models"
)

func (s *Store) EnsureUser(ctx context.Context, organizationID, externalID string) (*models.User, error) {
	newID := id.User.New()
	u := &models.User{}
	err := s.pool.QueryRow(ctx,
		`WITH ins AS (
			INSERT INTO users (id, organization_id, external_id)
			VALUES ($1, $2, $3)
			ON CONFLICT (organization_id, external_id) DO NOTHING
			RETURNING id, organization_id, external_id, locale, created_at
		)
		SELECT * FROM ins
		UNION ALL
		SELECT id, organization_id, external_id, locale, created_at
		FROM users
		WHERE organization_id = $2 AND external_id = $3
		LIMIT 1`,
		newID, organizationID, externalID,
	).Scan(&u.ID, &u.OrganizationID, &u.ExternalID, &u.Locale, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("ensure user: %w", err)
	}
	contacts, err := s.GetUserContacts(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	u.Contacts = contacts
	return u, nil
}

func (s *Store) GetUserByID(ctx context.Context, userID string) (*models.User, error) {
	u := &models.User{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, organization_id, external_id, locale, created_at
		 FROM users WHERE id = $1`, userID,
	).Scan(&u.ID, &u.OrganizationID, &u.ExternalID, &u.Locale, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get user by id: %w", err)
	}
	contacts, err := s.GetUserContacts(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	u.Contacts = contacts
	return u, nil
}

func (s *Store) ListUsers(ctx context.Context, organizationID string) ([]models.User, error) {
	var query string
	var args []any
	if organizationID != "" {
		query = `SELECT id, organization_id, external_id, locale, created_at
			FROM users WHERE organization_id = $1 ORDER BY created_at DESC`
		args = []any{organizationID}
	} else {
		query = `SELECT id, organization_id, external_id, locale, created_at
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
		if err := rows.Scan(&u.ID, &u.OrganizationID, &u.ExternalID, &u.Locale, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("list users scan: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range users {
		contacts, err := s.GetUserContacts(ctx, users[i].ID)
		if err != nil {
			return nil, err
		}
		users[i].Contacts = contacts
	}
	return users, nil
}

func (s *Store) UpdateUserContacts(ctx context.Context, userID string, email, phone *string) (*models.User, error) {
	if email != nil {
		if err := s.SetUserContact(ctx, userID, "email", *email); err != nil {
			return nil, err
		}
	}
	if phone != nil {
		if err := s.SetUserContact(ctx, userID, "phone", *phone); err != nil {
			return nil, err
		}
	}
	return s.GetUserByID(ctx, userID)
}
