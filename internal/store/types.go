package store

import (
	"context"
	"fmt"

	"github.com/hermes-notifications/hermes/internal/id"
	"github.com/hermes-notifications/hermes/internal/models"
)

func (s *Store) CreateType(ctx context.Context, input *models.NotificationType) (*models.NotificationType, error) {
	input.ID = id.New()
	err := s.pool.QueryRow(ctx,
		`INSERT INTO notification_types (id, group_id, slug, name, email_subject, email_body, sms_body, inbox_title, inbox_body)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING created_at`,
		input.ID, input.GroupID, input.Slug, input.Name,
		input.EmailSubject, input.EmailBody, input.SMSBody,
		input.InboxTitle, input.InboxBody,
	).Scan(&input.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create type: %w", err)
	}
	return input, nil
}

func (s *Store) GetTypeBySlug(ctx context.Context, slug string) (*models.NotificationType, error) {
	t := &models.NotificationType{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, group_id, slug, name, email_subject, email_body, sms_body, inbox_title, inbox_body, created_at
		 FROM notification_types WHERE slug = $1`, slug,
	).Scan(&t.ID, &t.GroupID, &t.Slug, &t.Name,
		&t.EmailSubject, &t.EmailBody, &t.SMSBody,
		&t.InboxTitle, &t.InboxBody, &t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get type by slug: %w", err)
	}
	return t, nil
}

func (s *Store) GetTypeByID(ctx context.Context, id string) (*models.NotificationType, error) {
	t := &models.NotificationType{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, group_id, slug, name, email_subject, email_body, sms_body, inbox_title, inbox_body, created_at
		 FROM notification_types WHERE id = $1`, id,
	).Scan(&t.ID, &t.GroupID, &t.Slug, &t.Name,
		&t.EmailSubject, &t.EmailBody, &t.SMSBody,
		&t.InboxTitle, &t.InboxBody, &t.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get type by id: %w", err)
	}
	return t, nil
}

func (s *Store) ListTypes(ctx context.Context) ([]models.NotificationType, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, group_id, slug, name, email_subject, email_body, sms_body, inbox_title, inbox_body, created_at
		 FROM notification_types ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list types: %w", err)
	}
	defer rows.Close()

	var types []models.NotificationType
	for rows.Next() {
		var t models.NotificationType
		if err := rows.Scan(&t.ID, &t.GroupID, &t.Slug, &t.Name,
			&t.EmailSubject, &t.EmailBody, &t.SMSBody,
			&t.InboxTitle, &t.InboxBody, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan type: %w", err)
		}
		types = append(types, t)
	}
	return types, rows.Err()
}

func (s *Store) UpdateType(ctx context.Context, input *models.NotificationType) (*models.NotificationType, error) {
	err := s.pool.QueryRow(ctx,
		`UPDATE notification_types
		 SET name = $2, email_subject = $3, email_body = $4, sms_body = $5, inbox_title = $6, inbox_body = $7
		 WHERE id = $1
		 RETURNING id, group_id, slug, name, email_subject, email_body, sms_body, inbox_title, inbox_body, created_at`,
		input.ID, input.Name, input.EmailSubject, input.EmailBody,
		input.SMSBody, input.InboxTitle, input.InboxBody,
	).Scan(&input.ID, &input.GroupID, &input.Slug, &input.Name,
		&input.EmailSubject, &input.EmailBody, &input.SMSBody,
		&input.InboxTitle, &input.InboxBody, &input.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("update type: %w", err)
	}
	return input, nil
}

func (s *Store) DeleteType(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM notification_types WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete type: %w", err)
	}
	return nil
}
