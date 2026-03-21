package store

import (
	"context"
	"fmt"

	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/jackc/pgx/v5"
)

func (s *Store) GetUserPreference(ctx context.Context, userID, groupID string) (*models.UserPreference, error) {
	p := &models.UserPreference{}
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, group_id, channels
		 FROM user_preferences
		 WHERE user_id = $1 AND group_id = $2`,
		userID, groupID,
	).Scan(&p.UserID, &p.GroupID, &p.Channels)
	if err != nil {
		return nil, fmt.Errorf("get user preference: %w", err)
	}
	return p, nil
}

func (s *Store) SetUserPreference(ctx context.Context, userID, groupID string, channels []string) (*models.UserPreference, error) {
	p := &models.UserPreference{}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO user_preferences (user_id, group_id, channels)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (user_id, group_id) DO UPDATE SET channels = EXCLUDED.channels
		 RETURNING user_id, group_id, channels`,
		userID, groupID, channels,
	).Scan(&p.UserID, &p.GroupID, &p.Channels)
	if err != nil {
		return nil, fmt.Errorf("set user preference: %w", err)
	}
	return p, nil
}

func (s *Store) DeleteUserPreference(ctx context.Context, userID, groupID string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM user_preferences WHERE user_id = $1 AND group_id = $2`,
		userID, groupID,
	)
	if err != nil {
		return fmt.Errorf("delete user preference: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delete user preference: %w", pgx.ErrNoRows)
	}
	return nil
}
