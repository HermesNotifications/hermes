// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package postgres

import (
	"context"
	"fmt"

	"github.com/hermes-notifications/hermes/internal/models"
)

func (s *Store) CreateNotification(ctx context.Context, n *models.Notification) (*models.Notification, error) {
	var categoryID *string
	if n.CategoryID != "" {
		categoryID = &n.CategoryID
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO notifications
			(id, organization_id, user_id, template_id, category_id, title, body, action_url, action_label, idempotency_key, channels, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		 RETURNING created_at`,
		n.ID, n.OrganizationID, n.UserID, n.TemplateID, categoryID,
		n.Title, n.Body, n.ActionURL, n.ActionLabel,
		n.IdempotencyKey, n.Channels, n.Status,
	).Scan(&n.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create notification: %w", err)
	}
	return n, nil
}

func scanNotification(scan func(dest ...any) error, n *models.Notification) error {
	var categoryID *string
	err := scan(
		&n.ID, &n.OrganizationID, &n.UserID, &n.TemplateID, &categoryID,
		&n.Title, &n.Body, &n.ActionURL, &n.ActionLabel,
		&n.IdempotencyKey, &n.Channels, &n.Status,
		&n.CreatedAt, &n.SentAt, &n.DeliveredAt, &n.ReadAt, &n.ArchivedAt, &n.DeletedAt,
	)
	if categoryID != nil {
		n.CategoryID = *categoryID
	}
	return err
}

func (s *Store) GetNotificationByID(ctx context.Context, notifID string) (*models.Notification, error) {
	n := &models.Notification{}
	row := s.pool.QueryRow(ctx,
		`SELECT id, organization_id, user_id, template_id, category_id, title, body,
		        action_url, action_label, idempotency_key, channels, status,
		        created_at, sent_at, delivered_at, read_at, archived_at, deleted_at
		 FROM notifications WHERE id = $1`, notifID,
	)
	if err := scanNotification(row.Scan, n); err != nil {
		return nil, fmt.Errorf("get notification by id: %w", err)
	}
	return n, nil
}

func (s *Store) GetNotificationByIdempotencyKey(ctx context.Context, organizationID, key string) (*models.Notification, error) {
	n := &models.Notification{}
	row := s.pool.QueryRow(ctx,
		`SELECT id, organization_id, user_id, template_id, category_id, title, body,
		        action_url, action_label, idempotency_key, channels, status,
		        created_at, sent_at, delivered_at, read_at, archived_at, deleted_at
		 FROM notifications
		 WHERE organization_id = $1 AND idempotency_key = $2
		   AND created_at > NOW() - INTERVAL '24 hours'`,
		organizationID, key,
	)
	if err := scanNotification(row.Scan, n); err != nil {
		return nil, fmt.Errorf("get notification by idempotency key: %w", err)
	}
	return n, nil
}

func (s *Store) ListRecentNotifications(ctx context.Context, limit int) ([]models.Notification, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, organization_id, user_id, template_id, category_id, title, body,
		        action_url, action_label, idempotency_key, channels, status,
		        created_at, sent_at, delivered_at, read_at, archived_at, deleted_at
		 FROM notifications ORDER BY created_at DESC LIMIT $1`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list recent notifications: %w", err)
	}
	defer rows.Close()

	var notifications []models.Notification
	for rows.Next() {
		var n models.Notification
		if err := scanNotification(rows.Scan, &n); err != nil {
			return nil, fmt.Errorf("list recent notifications scan: %w", err)
		}
		notifications = append(notifications, n)
	}
	return notifications, rows.Err()
}

func (s *Store) GetNotificationEvents(ctx context.Context, notificationID string) ([]models.NotificationEvent, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, notification_id, channel, event, severity, metadata, created_at
		 FROM notification_events
		 WHERE notification_id = $1
		 ORDER BY created_at`, notificationID,
	)
	if err != nil {
		return nil, fmt.Errorf("get notification events: %w", err)
	}
	defer rows.Close()

	var events []models.NotificationEvent
	for rows.Next() {
		var e models.NotificationEvent
		if err := rows.Scan(&e.ID, &e.NotificationID, &e.Channel, &e.Event, &e.Severity, &e.Metadata, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan notification event: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *Store) UpdateNotificationChannels(ctx context.Context, notificationID string, channels []string) error {
	_, err := s.pool.Exec(ctx, `UPDATE notifications SET channels = $2 WHERE id = $1`, notificationID, channels)
	return err
}

func (s *Store) UpdateNotificationRouting(ctx context.Context, n *models.Notification) error {
	var categoryID *string
	if n.CategoryID != "" {
		categoryID = &n.CategoryID
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE notifications
		 SET template_id = COALESCE($2, template_id),
		     category_id = COALESCE($3, category_id),
		     title = CASE WHEN $4 = '' THEN title ELSE $4 END,
		     body = CASE WHEN $5 = '' THEN body ELSE $5 END
		 WHERE id = $1`,
		n.ID, n.TemplateID, categoryID, n.Title, n.Body,
	)
	if err != nil {
		return fmt.Errorf("update notification routing: %w", err)
	}
	return nil
}

func (s *Store) FailNotification(ctx context.Context, notificationID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE notifications SET status = 'failed' WHERE id = $1 AND status = 'pending'`,
		notificationID,
	)
	if err != nil {
		return fmt.Errorf("fail notification: %w", err)
	}
	return nil
}
