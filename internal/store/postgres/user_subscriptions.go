// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package postgres

import (
	"context"
	"fmt"

	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/jackc/pgx/v5"
)

func (s *Store) GetUserSubscription(ctx context.Context, userID, subscriptionID string) (*models.UserSubscription, error) {
	us := &models.UserSubscription{}
	err := s.pool.QueryRow(ctx,
		`SELECT user_id, subscription_id, opted_in, created_at
		 FROM user_subscriptions
		 WHERE user_id = $1 AND subscription_id = $2`,
		userID, subscriptionID,
	).Scan(&us.UserID, &us.SubscriptionID, &us.OptedIn, &us.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get user subscription: %w", err)
	}
	return us, nil
}

func (s *Store) GetUserSubscriptions(ctx context.Context, userID string) ([]models.UserSubscription, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT user_id, subscription_id, opted_in, created_at
		 FROM user_subscriptions WHERE user_id = $1`, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("get user subscriptions: %w", err)
	}
	defer rows.Close()

	var subs []models.UserSubscription
	for rows.Next() {
		var us models.UserSubscription
		if err := rows.Scan(&us.UserID, &us.SubscriptionID, &us.OptedIn, &us.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan user subscription: %w", err)
		}
		subs = append(subs, us)
	}
	return subs, rows.Err()
}

func (s *Store) SetUserSubscription(ctx context.Context, userID, subscriptionID string, optedIn bool) (*models.UserSubscription, error) {
	us := &models.UserSubscription{}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO user_subscriptions (user_id, subscription_id, opted_in)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (user_id, subscription_id) DO UPDATE SET opted_in = EXCLUDED.opted_in
		 RETURNING user_id, subscription_id, opted_in, created_at`,
		userID, subscriptionID, optedIn,
	).Scan(&us.UserID, &us.SubscriptionID, &us.OptedIn, &us.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("set user subscription: %w", err)
	}
	return us, nil
}

func (s *Store) DeleteUserSubscription(ctx context.Context, userID, subscriptionID string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM user_subscriptions WHERE user_id = $1 AND subscription_id = $2`,
		userID, subscriptionID,
	)
	if err != nil {
		return fmt.Errorf("delete user subscription: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delete user subscription: %w", pgx.ErrNoRows)
	}
	return nil
}
