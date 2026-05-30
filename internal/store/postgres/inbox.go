// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package postgres

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hermes-notifications/hermes/internal/models"
)

func encodeCursor(createdAt time.Time, id string) string {
	return base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%d|%s", createdAt.UnixNano(), id)))
}

func decodeCursor(cursor string) (time.Time, string, error) {
	b, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", err
	}
	parts := strings.SplitN(string(b), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, "", fmt.Errorf("invalid cursor")
	}
	ns, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, "", err
	}
	return time.Unix(0, ns), parts[1], nil
}

// ListInbox returns cursor-paginated notifications for a user's inbox.
// Returns notifications, unread_count, next_cursor, error.
func (s *Store) ListInbox(ctx context.Context, userID string, archived bool, cursor string, limit int) ([]models.Notification, int, string, error) {
	if limit <= 0 {
		limit = 20
	}

	// Build the archive filter
	archiveFilter := "archived_at IS NULL"
	if archived {
		archiveFilter = "archived_at IS NOT NULL"
	}

	// Build the query
	query := fmt.Sprintf(`SELECT id, tenant_id, user_id, template_id, category_id, title, body,
	        action_url, action_label, idempotency_key, channels, status,
	        created_at, sent_at, delivered_at, read_at, archived_at, deleted_at
	 FROM notifications
	 WHERE user_id = $1 AND %s AND deleted_at IS NULL`, archiveFilter)

	args := []any{userID}
	argIdx := 2

	if cursor != "" {
		cursorTime, cursorID, err := decodeCursor(cursor)
		if err != nil {
			return nil, 0, "", fmt.Errorf("invalid cursor: %w", err)
		}
		query += fmt.Sprintf(` AND (created_at, id) < ($%d, $%d)`, argIdx, argIdx+1)
		args = append(args, cursorTime, cursorID)
		argIdx += 2
	}

	query += ` ORDER BY created_at DESC, id DESC`
	query += fmt.Sprintf(` LIMIT $%d`, argIdx)
	args = append(args, limit+1) // fetch one extra to determine if there's a next page

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, "", fmt.Errorf("list inbox: %w", err)
	}
	defer rows.Close()

	var notifications []models.Notification
	for rows.Next() {
		var n models.Notification
		if err := scanNotification(rows.Scan, &n); err != nil {
			return nil, 0, "", fmt.Errorf("scan inbox notification: %w", err)
		}
		notifications = append(notifications, n)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, "", fmt.Errorf("list inbox rows: %w", err)
	}

	// Determine next cursor
	var nextCursor string
	if len(notifications) > limit {
		notifications = notifications[:limit]
		last := notifications[limit-1]
		nextCursor = encodeCursor(last.CreatedAt, last.ID)
	}

	// Unread count (always for non-archived, non-deleted)
	var unreadCount int
	err = s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM notifications
		 WHERE user_id = $1 AND read_at IS NULL AND archived_at IS NULL AND deleted_at IS NULL`,
		userID,
	).Scan(&unreadCount)
	if err != nil {
		return nil, 0, "", fmt.Errorf("unread count: %w", err)
	}

	return notifications, unreadCount, nextCursor, nil
}

// UnreadCount returns the number of unread, non-archived, non-deleted notifications for a user.
func (s *Store) UnreadCount(ctx context.Context, userID string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM notifications
		 WHERE user_id = $1 AND read_at IS NULL AND archived_at IS NULL AND deleted_at IS NULL`,
		userID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("unread count: %w", err)
	}
	return count, nil
}

// MarkRead marks a notification as read and advances its status.
// Returns true if the notification was actually changed (was unread).
func (s *Store) MarkRead(ctx context.Context, userID, notificationID string) (bool, error) {
	result, err := s.pool.Exec(ctx, `
		UPDATE notifications
		SET read_at = NOW(),
		    status = CASE
		        WHEN (CASE status
		            WHEN 'pending' THEN 0
		            WHEN 'sent' THEN 1
		            WHEN 'delivered' THEN 2
		            WHEN 'read' THEN 3
		            WHEN 'archived' THEN 4
		            ELSE 0
		        END) < 3 THEN 'read'
		        ELSE status
		    END
		WHERE id = $1 AND user_id = $2 AND read_at IS NULL`,
		notificationID, userID,
	)
	if err != nil {
		return false, fmt.Errorf("mark read: %w", err)
	}
	return result.RowsAffected() > 0, nil
}

// MarkUnread marks a notification as unread and reverts status to delivered if it was read.
// Returns true if the notification was actually changed (was read).
func (s *Store) MarkUnread(ctx context.Context, userID, notificationID string) (bool, error) {
	result, err := s.pool.Exec(ctx, `
		UPDATE notifications
		SET read_at = NULL,
		    status = CASE
		        WHEN status = 'read' THEN 'delivered'
		        ELSE status
		    END
		WHERE id = $1 AND user_id = $2 AND read_at IS NOT NULL`,
		notificationID, userID,
	)
	if err != nil {
		return false, fmt.Errorf("mark unread: %w", err)
	}
	return result.RowsAffected() > 0, nil
}

// Archive archives a notification and advances its status to archived.
// Returns true if the archived notification was unread (affecting unread count).
func (s *Store) Archive(ctx context.Context, userID, notificationID string) (wasUnread bool, err error) {
	err = s.pool.QueryRow(ctx, `
		UPDATE notifications
		SET archived_at = NOW(),
		    status = 'archived'
		WHERE id = $1 AND user_id = $2 AND archived_at IS NULL
		RETURNING (read_at IS NULL)`,
		notificationID, userID,
	).Scan(&wasUnread)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("archive: %w", err)
	}
	return wasUnread, nil
}

// Unarchive unarchives a notification and reverts status based on read_at.
// Returns true if the unarchived notification is unread (affecting unread count).
func (s *Store) Unarchive(ctx context.Context, userID, notificationID string) (nowUnread bool, err error) {
	err = s.pool.QueryRow(ctx, `
		UPDATE notifications
		SET archived_at = NULL,
		    status = CASE
		        WHEN read_at IS NOT NULL THEN 'read'
		        ELSE 'delivered'
		    END
		WHERE id = $1 AND user_id = $2 AND archived_at IS NOT NULL
		RETURNING (read_at IS NULL)`,
		notificationID, userID,
	).Scan(&nowUnread)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("unarchive: %w", err)
	}
	return nowUnread, nil
}

// SoftDelete soft-deletes a notification.
// Returns true if the deleted notification was unread and non-archived (affecting unread count).
func (s *Store) SoftDelete(ctx context.Context, userID, notificationID string) (wasUnread bool, err error) {
	err = s.pool.QueryRow(ctx, `
		UPDATE notifications SET deleted_at = NOW()
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
		RETURNING (read_at IS NULL AND archived_at IS NULL)`,
		notificationID, userID,
	).Scan(&wasUnread)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("soft delete: %w", err)
	}
	return wasUnread, nil
}

// MarkAllRead marks all unread, non-archived, non-deleted notifications as read for a user.
func (s *Store) MarkAllRead(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE notifications
		SET read_at = NOW(),
		    status = CASE
		        WHEN (CASE status
		            WHEN 'pending' THEN 0
		            WHEN 'sent' THEN 1
		            WHEN 'delivered' THEN 2
		            WHEN 'read' THEN 3
		            WHEN 'archived' THEN 4
		            ELSE 0
		        END) < 3 THEN 'read'
		        ELSE status
		    END
		WHERE user_id = $1 AND read_at IS NULL AND archived_at IS NULL AND deleted_at IS NULL`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("mark all read: %w", err)
	}
	return nil
}
