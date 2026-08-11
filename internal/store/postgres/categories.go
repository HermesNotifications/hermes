// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package postgres

import (
	"context"
	"fmt"

	id "github.com/hermes-notifications/hermes/internal/id/v2"
	"github.com/hermes-notifications/hermes/internal/models"
)

var categoryIDGen = id.NewGenerator(id.Config{Prefix: "sct", TimeBits: 48, RandBits: 80})

func (s *Store) CreateCategory(ctx context.Context, slug, name string, defaultChannels []string, defaultState string, sortOrder int) (*models.SubscriptionCategory, error) {
	c := &models.SubscriptionCategory{
		ID:              categoryIDGen.New(),
		Slug:            slug,
		Name:            name,
		DefaultChannels: defaultChannels,
		DefaultState:    defaultState,
		SortOrder:       sortOrder,
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO subscription_categories (id, slug, name, default_channels, default_state, sort_order)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING created_at`,
		c.ID, c.Slug, c.Name, c.DefaultChannels, c.DefaultState, c.SortOrder,
	).Scan(&c.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create category: %w", err)
	}
	return c, nil
}

func (s *Store) GetCategoryByID(ctx context.Context, id string) (*models.SubscriptionCategory, error) {
	c := &models.SubscriptionCategory{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, slug, name, default_channels, default_state, sort_order, created_at
		 FROM subscription_categories WHERE id = $1`, id,
	).Scan(&c.ID, &c.Slug, &c.Name, &c.DefaultChannels, &c.DefaultState, &c.SortOrder, &c.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get category by id: %w", err)
	}
	return c, nil
}

func (s *Store) GetCategoryBySlug(ctx context.Context, slug string) (*models.SubscriptionCategory, error) {
	c := &models.SubscriptionCategory{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, slug, name, default_channels, default_state, sort_order, created_at
		 FROM subscription_categories WHERE slug = $1`, slug,
	).Scan(&c.ID, &c.Slug, &c.Name, &c.DefaultChannels, &c.DefaultState, &c.SortOrder, &c.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get category by slug: %w", err)
	}
	return c, nil
}

func (s *Store) ListCategories(ctx context.Context) ([]models.SubscriptionCategory, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, slug, name, default_channels, default_state, sort_order, created_at
		 FROM subscription_categories ORDER BY sort_order, created_at`)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	defer rows.Close()

	var categories []models.SubscriptionCategory
	for rows.Next() {
		var c models.SubscriptionCategory
		if err := rows.Scan(&c.ID, &c.Slug, &c.Name, &c.DefaultChannels, &c.DefaultState, &c.SortOrder, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan category: %w", err)
		}
		categories = append(categories, c)
	}
	return categories, rows.Err()
}

func (s *Store) UpdateCategory(ctx context.Context, id, name string, defaultChannels []string, defaultState string, sortOrder int) (*models.SubscriptionCategory, error) {
	c := &models.SubscriptionCategory{}
	err := s.pool.QueryRow(ctx,
		`UPDATE subscription_categories SET name = $2, default_channels = $3, default_state = $4, sort_order = $5
		 WHERE id = $1
		 RETURNING id, slug, name, default_channels, default_state, sort_order, created_at`,
		id, name, defaultChannels, defaultState, sortOrder,
	).Scan(&c.ID, &c.Slug, &c.Name, &c.DefaultChannels, &c.DefaultState, &c.SortOrder, &c.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("update category: %w", err)
	}
	return c, nil
}

func (s *Store) DeleteCategory(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM subscription_categories WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete category: %w", err)
	}
	return nil
}
