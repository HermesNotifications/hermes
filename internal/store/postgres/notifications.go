// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/hermesnotifications/hermes/internal/models"
)

// insertNotificationSQL is shared by the single-row and the batch insert so the column list
// cannot drift between them; each appends its own conflict and RETURNING clauses.
const insertNotificationSQL = `INSERT INTO notifications
		(id, organization_id, user_id, template_id, category_id, title, body, action_url, action_label, idempotency_key, channels, status, metadata)
	 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`

// notificationInsertArgs returns the arguments for insertNotificationSQL, in placeholder order.
func notificationInsertArgs(n *models.Notification) []any {
	// An empty category is a real absence, not the empty string: the column is a nullable FK.
	var categoryID *string
	if n.CategoryID != "" {
		categoryID = &n.CategoryID
	}
	return []any{
		n.ID, n.OrganizationID, n.UserID, n.TemplateID, categoryID,
		n.Title, n.Body, n.ActionURL, n.ActionLabel,
		n.IdempotencyKey, n.Channels, n.Status, n.Metadata,
	}
}

func (s *Store) CreateNotification(ctx context.Context, n *models.Notification) (*models.Notification, error) {
	err := s.pool.QueryRow(ctx,
		insertNotificationSQL+` RETURNING created_at`,
		notificationInsertArgs(n)...,
	).Scan(&n.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create notification: %w", err)
	}
	return n, nil
}

// CreateNotifications inserts a batch of notifications inside one transaction, so the batch
// costs one WAL flush instead of one per notification.
//
// That flush is the whole point. With synchronous_commit on, a COMMIT cannot return until its
// WAL record is durable, so on a volume where the flush is slow a write path that commits once
// per notification is bounded by fdatasync latency and by nothing else: one environment measured
// 1,933 fdatasync/s at 517us and 2,006 notifications/s, agreeing within 4%, with Postgres using
// 2.5 of 24 cores. Whether that bound is the one binding is hardware-dependent and the caller
// decides — see internal/dispatch/insertbatch.go, which is where the batching is switched on.
//
// Sent as a pipelined pgx batch rather than as one multi-row INSERT for two reasons: it reuses
// the identical statement the single-row path already prepares (no dynamically built SQL whose
// text, and therefore whose prepared-statement cache entry, changes with the batch size), and
// each row keeps its own result, which is what makes the conflict handling below per-row.
//
// ON CONFLICT DO NOTHING carries no conflict target on purpose. A row that is already present
// — colliding on the primary key because its message was redelivered, or on the partial unique
// index over (organization_id, idempotency_key) — is then skipped instead of aborting the
// transaction and failing every other row in the batch with it. Both collisions are outcomes
// the one-row-at-a-time path tolerated and continued from, and both stay tolerated here.
func (s *Store) CreateNotifications(ctx context.Context, ns []*models.Notification) ([]string, error) {
	if len(ns) == 0 {
		return nil, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	batch := &pgx.Batch{}
	for _, n := range ns {
		batch.Queue(insertNotificationSQL+` ON CONFLICT DO NOTHING RETURNING created_at`, notificationInsertArgs(n)...)
	}

	results := tx.SendBatch(ctx, batch)
	inserted := make([]string, 0, len(ns))
	var firstErr error
	for _, n := range ns {
		// Every queued statement is read even after one has failed. pgx hands back results
		// strictly in order, and closing a batch whose results were not all consumed leaves
		// the connection out of step with the server — on a pooled connection that is the
		// next borrower's problem, not this call's.
		err := results.QueryRow().Scan(&n.CreatedAt)
		switch {
		case err == nil:
			inserted = append(inserted, n.ID)
		case errors.Is(err, pgx.ErrNoRows):
			// Skipped by ON CONFLICT: the row was already there. Not an error — see above.
		case firstErr == nil:
			firstErr = fmt.Errorf("create notification %s: %w", n.ID, err)
		}
	}
	if err := results.Close(); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("close batch: %w", err)
	}
	if firstErr != nil {
		return nil, firstErr
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}
	return inserted, nil
}

// scanNotification reads a row in the column order every SELECT in this package uses.
//
// The column lists and this function are one unit: there are four SELECTs here and a fifth in
// inbox.go, and adding a column to some of them silently shifts every destination after it.
func scanNotification(scan func(dest ...any) error, n *models.Notification) error {
	var categoryID *string
	err := scan(
		&n.ID, &n.OrganizationID, &n.UserID, &n.TemplateID, &categoryID,
		&n.Title, &n.Body, &n.ActionURL, &n.ActionLabel,
		&n.IdempotencyKey, &n.Channels, &n.Status, &n.Metadata,
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
		        action_url, action_label, idempotency_key, channels, status, metadata,
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
		        action_url, action_label, idempotency_key, channels, status, metadata,
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
		        action_url, action_label, idempotency_key, channels, status, metadata,
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
