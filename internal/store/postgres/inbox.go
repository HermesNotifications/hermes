// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

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
// Returns notifications, next_cursor, error.
//
// It deliberately does not return the unread count. The count is a property of the user, not of
// a page: computing it here meant paying for it again on page 7 of a scroll, and it put the same
// aggregate in two places that had to agree. Callers that need it call UnreadCount, which the
// inbox service fronts with a cache.
func (s *Store) ListInbox(ctx context.Context, userID string, archived bool, cursor string, limit int) ([]models.Notification, string, error) {
	if limit <= 0 {
		limit = 20
	}

	// Build the archive filter
	archiveFilter := "archived_at IS NULL"
	if archived {
		archiveFilter = "archived_at IS NOT NULL"
	}

	// Build the query
	query := fmt.Sprintf(`SELECT id, organization_id, user_id, template_id, category_id, title, body,
	        action_url, action_label, idempotency_key, channels, status, metadata,
	        created_at, sent_at, delivered_at, read_at, archived_at, deleted_at
	 FROM notifications
	 WHERE user_id = $1 AND %s AND deleted_at IS NULL`, archiveFilter)

	args := []any{userID}
	argIdx := 2

	if cursor != "" {
		cursorTime, cursorID, err := decodeCursor(cursor)
		if err != nil {
			return nil, "", fmt.Errorf("invalid cursor: %w", err)
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
		return nil, "", fmt.Errorf("list inbox: %w", err)
	}
	defer rows.Close()

	var notifications []models.Notification
	for rows.Next() {
		var n models.Notification
		if err := scanNotification(rows.Scan, &n); err != nil {
			return nil, "", fmt.Errorf("scan inbox notification: %w", err)
		}
		notifications = append(notifications, n)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("list inbox rows: %w", err)
	}

	// Determine next cursor
	var nextCursor string
	if len(notifications) > limit {
		notifications = notifications[:limit]
		last := notifications[limit-1]
		nextCursor = encodeCursor(last.CreatedAt, last.ID)
	}

	return notifications, nextCursor, nil
}

// unreadCountSQL counts through a bounded subquery rather than counting the whole set.
//
// COUNT(*) has no early exit, so a user with 400k unread rows scans 400k index entries every
// time a badge is drawn. LIMIT does have one: with idx_notifications_unread (migration 000018)
// this is an index-only scan that stops at the cap, so the pathological user costs the same as
// the ordinary one.
// The watermark is deliberately the newest notification for this user in **any** state, not the
// newest unread one. It answers "which arrivals does this count already account for", and a
// notification that arrived and was read immediately still arrived. Taking the unread maximum
// would leave a higher-id read notification outside the watermark, so a delivery still in flight
// for it would be counted again — the exact double-count this exists to prevent.
//
// Both values come from one statement so they describe the same snapshot. Read separately they
// could straddle an insert, and then either the count or the watermark is ahead of the other: one
// way loses an arrival, the other counts it twice.
const unreadCountSQL = `
	SELECT
		(SELECT count(*) FROM (
			SELECT 1 FROM notifications
			WHERE user_id = $1
			  AND read_at IS NULL AND archived_at IS NULL AND deleted_at IS NULL
			LIMIT $2
		) capped),
		coalesce((SELECT max(id) FROM notifications WHERE user_id = $1), '')`

// UnreadCount returns the number of unread, non-archived, non-deleted notifications for a user,
// saturating at models.UnreadCountCap, together with the watermark described above.
func (s *Store) UnreadCount(ctx context.Context, userID string) (int, string, error) {
	var count int
	var watermark string
	if err := s.pool.QueryRow(ctx, unreadCountSQL, userID, models.UnreadCountCap).Scan(&count, &watermark); err != nil {
		return 0, "", fmt.Errorf("unread count: %w", err)
	}
	return count, watermark, nil
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
