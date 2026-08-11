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

var templateIDGen = id.NewGenerator(id.Config{Prefix: "ntpl", TimeBits: 48, RandBits: 80})

func (s *Store) CreateTemplate(ctx context.Context, input *models.NotificationTemplate) (*models.NotificationTemplate, error) {
	input.ID = templateIDGen.New()
	err := s.pool.QueryRow(ctx,
		`INSERT INTO notification_templates (id, subscription_id, slug, name, default_channels)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING created_at`,
		input.ID, input.SubscriptionID, input.Slug, input.Name, input.DefaultChannels,
	).Scan(&input.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create template: %w", err)
	}
	if err := s.SetTemplateContent(ctx, input.ID, input.Content); err != nil {
		return nil, err
	}
	if input.Content, err = s.GetTemplateContent(ctx, input.ID); err != nil {
		return nil, err
	}
	return input, nil
}

func (s *Store) GetTemplateByID(ctx context.Context, id string) (*models.NotificationTemplate, error) {
	t := &models.NotificationTemplate{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, subscription_id, slug, name, default_channels, created_at
		 FROM notification_templates WHERE id = $1`, id,
	).Scan(&t.ID, &t.SubscriptionID, &t.Slug, &t.Name, &t.DefaultChannels, &t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get template by id: %w", err)
	}
	content, err := s.GetTemplateContent(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	t.Content = content
	return t, nil
}

func (s *Store) GetTemplateBySlug(ctx context.Context, slug string) (*models.NotificationTemplate, error) {
	t := &models.NotificationTemplate{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, subscription_id, slug, name, default_channels, created_at
		 FROM notification_templates WHERE slug = $1`, slug,
	).Scan(&t.ID, &t.SubscriptionID, &t.Slug, &t.Name, &t.DefaultChannels, &t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get template by slug: %w", err)
	}
	content, err := s.GetTemplateContent(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	t.Content = content
	return t, nil
}

func (s *Store) ListTemplates(ctx context.Context) ([]models.NotificationTemplate, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, subscription_id, slug, name, default_channels, created_at
		 FROM notification_templates ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list templates: %w", err)
	}
	defer rows.Close()

	var templates []models.NotificationTemplate
	for rows.Next() {
		var t models.NotificationTemplate
		if err := rows.Scan(&t.ID, &t.SubscriptionID, &t.Slug, &t.Name, &t.DefaultChannels, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan template: %w", err)
		}
		templates = append(templates, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range templates {
		content, err := s.GetTemplateContent(ctx, templates[i].ID)
		if err != nil {
			return nil, err
		}
		templates[i].Content = content
	}
	return templates, nil
}

func (s *Store) UpdateTemplate(ctx context.Context, input *models.NotificationTemplate) (*models.NotificationTemplate, error) {
	err := s.pool.QueryRow(ctx,
		`UPDATE notification_templates
		 SET name = $2, subscription_id = $3, default_channels = $4
		 WHERE id = $1
		 RETURNING id, subscription_id, slug, name, default_channels, created_at`,
		input.ID, input.Name, input.SubscriptionID, input.DefaultChannels,
	).Scan(&input.ID, &input.SubscriptionID, &input.Slug, &input.Name, &input.DefaultChannels, &input.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("update template: %w", err)
	}
	if err := s.SetTemplateContent(ctx, input.ID, input.Content); err != nil {
		return nil, err
	}
	if input.Content, err = s.GetTemplateContent(ctx, input.ID); err != nil {
		return nil, err
	}
	return input, nil
}

func (s *Store) DeleteTemplate(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM notification_templates WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete template: %w", err)
	}
	return nil
}
