package postgres

import (
	"context"
	"fmt"

	"github.com/hermes-notifications/hermes/internal/id"
	"github.com/hermes-notifications/hermes/internal/models"
)

func (s *Store) CreateGroup(ctx context.Context, slug, name string, defaultChannels []string) (*models.NotificationGroup, error) {
	g := &models.NotificationGroup{
		ID:              id.New(),
		Slug:            slug,
		Name:            name,
		DefaultChannels: defaultChannels,
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO notification_groups (id, slug, name, default_channels)
		 VALUES ($1, $2, $3, $4)
		 RETURNING created_at`,
		g.ID, g.Slug, g.Name, g.DefaultChannels,
	).Scan(&g.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create group: %w", err)
	}
	return g, nil
}

func (s *Store) GetGroupBySlug(ctx context.Context, slug string) (*models.NotificationGroup, error) {
	g := &models.NotificationGroup{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, slug, name, default_channels, created_at
		 FROM notification_groups WHERE slug = $1`, slug,
	).Scan(&g.ID, &g.Slug, &g.Name, &g.DefaultChannels, &g.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get group by slug: %w", err)
	}
	return g, nil
}

func (s *Store) GetGroupByID(ctx context.Context, id string) (*models.NotificationGroup, error) {
	g := &models.NotificationGroup{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, slug, name, default_channels, created_at
		 FROM notification_groups WHERE id = $1`, id,
	).Scan(&g.ID, &g.Slug, &g.Name, &g.DefaultChannels, &g.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get group by id: %w", err)
	}
	return g, nil
}

func (s *Store) ListGroups(ctx context.Context) ([]models.NotificationGroup, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, slug, name, default_channels, created_at
		 FROM notification_groups ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	defer rows.Close()

	var groups []models.NotificationGroup
	for rows.Next() {
		var g models.NotificationGroup
		if err := rows.Scan(&g.ID, &g.Slug, &g.Name, &g.DefaultChannels, &g.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan group: %w", err)
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

func (s *Store) UpdateGroup(ctx context.Context, id, name string, defaultChannels []string) (*models.NotificationGroup, error) {
	g := &models.NotificationGroup{}
	err := s.pool.QueryRow(ctx,
		`UPDATE notification_groups SET name = $2, default_channels = $3
		 WHERE id = $1
		 RETURNING id, slug, name, default_channels, created_at`,
		id, name, defaultChannels,
	).Scan(&g.ID, &g.Slug, &g.Name, &g.DefaultChannels, &g.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("update group: %w", err)
	}
	return g, nil
}
