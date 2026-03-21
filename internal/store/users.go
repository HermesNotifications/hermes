package store

import (
	"context"
	"fmt"

	"github.com/hermes-notifications/hermes/internal/id"
	"github.com/hermes-notifications/hermes/internal/models"
)

func (s *Store) EnsureUser(ctx context.Context, tenantID, externalID string) (*models.User, error) {
	newID := id.New()
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

func (s *Store) GetUserPreferences(ctx context.Context, userID string) ([]models.UserPreference, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT user_id, group_id, channels
		 FROM user_preferences WHERE user_id = $1`, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("get user preferences: %w", err)
	}
	defer rows.Close()

	var prefs []models.UserPreference
	for rows.Next() {
		var p models.UserPreference
		if err := rows.Scan(&p.UserID, &p.GroupID, &p.Channels); err != nil {
			return nil, fmt.Errorf("scan user preference: %w", err)
		}
		prefs = append(prefs, p)
	}
	return prefs, rows.Err()
}
