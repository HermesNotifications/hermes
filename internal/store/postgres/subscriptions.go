// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package postgres

import (
	"context"
	"fmt"

	id "github.com/hermes-notifications/hermes/internal/id/v2"
	"github.com/hermes-notifications/hermes/internal/models"
)

var subscriptionIDGen = id.NewGenerator(id.Config{Prefix: "sub", TimeBits: 48, RandBits: 80})

func (s *Store) CreateSubscription(ctx context.Context, categoryID, slug, name string, sortOrder int) (*models.Subscription, error) {
	sub := &models.Subscription{
		ID:         subscriptionIDGen.New(),
		CategoryID: categoryID,
		Slug:       slug,
		Name:       name,
		SortOrder:  sortOrder,
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO subscriptions (id, category_id, slug, name, sort_order)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING created_at`,
		sub.ID, sub.CategoryID, sub.Slug, sub.Name, sub.SortOrder,
	).Scan(&sub.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create subscription: %w", err)
	}
	return sub, nil
}

func (s *Store) GetSubscriptionByID(ctx context.Context, id string) (*models.Subscription, error) {
	sub := &models.Subscription{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, category_id, slug, name, sort_order, created_at
		 FROM subscriptions WHERE id = $1`, id,
	).Scan(&sub.ID, &sub.CategoryID, &sub.Slug, &sub.Name, &sub.SortOrder, &sub.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get subscription by id: %w", err)
	}
	return sub, nil
}

func (s *Store) ListSubscriptionsByCategory(ctx context.Context, categoryID string) ([]models.Subscription, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, category_id, slug, name, sort_order, created_at
		 FROM subscriptions WHERE category_id = $1 ORDER BY sort_order, created_at`, categoryID,
	)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}
	defer rows.Close()

	var subs []models.Subscription
	for rows.Next() {
		var sub models.Subscription
		if err := rows.Scan(&sub.ID, &sub.CategoryID, &sub.Slug, &sub.Name, &sub.SortOrder, &sub.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan subscription: %w", err)
		}
		subs = append(subs, sub)
	}
	return subs, rows.Err()
}

func (s *Store) UpdateSubscription(ctx context.Context, id, name string, sortOrder int) (*models.Subscription, error) {
	sub := &models.Subscription{}
	err := s.pool.QueryRow(ctx,
		`UPDATE subscriptions SET name = $2, sort_order = $3
		 WHERE id = $1
		 RETURNING id, category_id, slug, name, sort_order, created_at`,
		id, name, sortOrder,
	).Scan(&sub.ID, &sub.CategoryID, &sub.Slug, &sub.Name, &sub.SortOrder, &sub.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("update subscription: %w", err)
	}
	return sub, nil
}

func (s *Store) DeleteSubscription(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM subscriptions WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete subscription: %w", err)
	}
	return nil
}
