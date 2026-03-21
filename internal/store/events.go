package store

import (
	"context"
	"fmt"
	"time"

	"github.com/hermes-notifications/hermes/internal/id"
	"github.com/hermes-notifications/hermes/internal/models"
)

// InsertEvent inserts a single notification event.
func (s *Store) InsertEvent(ctx context.Context, notificationID, channel, event, severity string, metadata []byte) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO notification_events (id, notification_id, channel, event, severity, metadata)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		id.New(), notificationID, channel, event, severity, metadata,
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

// InsertEvents batch-inserts notification events in a transaction.
// Events with empty IDs are assigned new IDs via id.New().
func (s *Store) InsertEvents(ctx context.Context, events []models.NotificationEvent) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	for _, e := range events {
		eventID := e.ID
		if eventID == "" {
			eventID = id.New()
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO notification_events (id, notification_id, channel, event, severity, metadata)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			eventID, e.NotificationID, e.Channel, e.Event, e.Severity, e.Metadata,
		)
		if err != nil {
			return fmt.Errorf("insert event %s: %w", eventID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// UpdateNotificationStatus advances a notification's status using an out-of-order safe rollup.
// The WHERE clause prevents status regression — stale events are silently skipped.
func (s *Store) UpdateNotificationStatus(ctx context.Context, notificationID string, newStatus models.NotificationStatus, eventTime time.Time) error {
	rank := newStatus.Rank()
	_, err := s.pool.Exec(ctx, `
		UPDATE notifications
		SET status = $3,
		    sent_at = COALESCE(sent_at, CASE WHEN $2 >= 1 THEN $4 ELSE NULL::timestamptz END),
		    delivered_at = COALESCE(delivered_at, CASE WHEN $2 >= 2 THEN $4 ELSE NULL::timestamptz END)
		WHERE id = $1
		  AND (CASE status
		        WHEN 'pending' THEN 0
		        WHEN 'sent' THEN 1
		        WHEN 'delivered' THEN 2
		        WHEN 'read' THEN 3
		        WHEN 'archived' THEN 4
		        ELSE 0
		      END) < $2`,
		notificationID, rank, string(newStatus), eventTime,
	)
	if err != nil {
		return fmt.Errorf("update notification status: %w", err)
	}
	return nil
}
