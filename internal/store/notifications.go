package store

import (
	"context"
	"fmt"

	"github.com/hermes-notifications/hermes/internal/models"
)

func (s *Store) CreateNotification(ctx context.Context, n *models.Notification) (*models.Notification, error) {
	err := s.pool.QueryRow(ctx,
		`INSERT INTO notifications
			(id, tenant_id, user_id, type_id, group_id, title, body, action_url, action_label, idempotency_key, channels, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		 RETURNING created_at`,
		n.ID, n.TenantID, n.UserID, n.TypeID, n.GroupID,
		n.Title, n.Body, n.ActionURL, n.ActionLabel,
		n.IdempotencyKey, n.Channels, n.Status,
	).Scan(&n.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create notification: %w", err)
	}
	return n, nil
}

func (s *Store) GetNotificationByID(ctx context.Context, notifID string) (*models.Notification, error) {
	n := &models.Notification{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, user_id, type_id, group_id, title, body,
		        action_url, action_label, idempotency_key, channels, status,
		        created_at, sent_at, delivered_at, read_at, archived_at, deleted_at
		 FROM notifications WHERE id = $1`, notifID,
	).Scan(
		&n.ID, &n.TenantID, &n.UserID, &n.TypeID, &n.GroupID,
		&n.Title, &n.Body, &n.ActionURL, &n.ActionLabel,
		&n.IdempotencyKey, &n.Channels, &n.Status,
		&n.CreatedAt, &n.SentAt, &n.DeliveredAt, &n.ReadAt, &n.ArchivedAt, &n.DeletedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get notification by id: %w", err)
	}
	return n, nil
}

func (s *Store) GetNotificationByIdempotencyKey(ctx context.Context, tenantID, key string) (*models.Notification, error) {
	n := &models.Notification{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, user_id, type_id, group_id, title, body,
		        action_url, action_label, idempotency_key, channels, status,
		        created_at, sent_at, delivered_at, read_at, archived_at, deleted_at
		 FROM notifications
		 WHERE tenant_id = $1 AND idempotency_key = $2
		   AND created_at > NOW() - INTERVAL '24 hours'`,
		tenantID, key,
	).Scan(
		&n.ID, &n.TenantID, &n.UserID, &n.TypeID, &n.GroupID,
		&n.Title, &n.Body, &n.ActionURL, &n.ActionLabel,
		&n.IdempotencyKey, &n.Channels, &n.Status,
		&n.CreatedAt, &n.SentAt, &n.DeliveredAt, &n.ReadAt, &n.ArchivedAt, &n.DeletedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("get notification by idempotency key: %w", err)
	}
	return n, nil
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
