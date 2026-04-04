package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/hermes-notifications/hermes/internal/id"
	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/hermes-notifications/hermes/internal/store"
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

// BatchUpdateNotificationStatuses advances statuses for multiple notifications in a single query.
// It deduplicates per notification ID (keeping the highest rank) before executing, and uses
// the same rank-based WHERE clause as UpdateNotificationStatus to prevent status regression.
func (s *Store) BatchUpdateNotificationStatuses(ctx context.Context, updates []store.StatusUpdate) error {
	if len(updates) == 0 {
		return nil
	}

	// Deduplicate: keep only the highest-rank status per notification ID.
	best := make(map[string]store.StatusUpdate)
	for _, u := range updates {
		if existing, ok := best[u.NotificationID]; !ok || u.NewStatus.Rank() > existing.NewStatus.Rank() {
			best[u.NotificationID] = u
		}
	}

	ids := make([]string, 0, len(best))
	statuses := make([]string, 0, len(best))
	ranks := make([]int, 0, len(best))
	times := make([]time.Time, 0, len(best))
	for _, u := range best {
		ids = append(ids, u.NotificationID)
		statuses = append(statuses, string(u.NewStatus))
		ranks = append(ranks, u.NewStatus.Rank())
		times = append(times, u.EventTime)
	}

	_, err := s.pool.Exec(ctx, `
		UPDATE notifications n
		SET status = v.new_status,
		    sent_at = COALESCE(n.sent_at, CASE WHEN v.rank >= 1 THEN v.event_time ELSE NULL END),
		    delivered_at = COALESCE(n.delivered_at, CASE WHEN v.rank >= 2 THEN v.event_time ELSE NULL END)
		FROM (
			SELECT unnest($1::text[]) AS id,
			       unnest($2::text[]) AS new_status,
			       unnest($3::int[]) AS rank,
			       unnest($4::timestamptz[]) AS event_time
		) v
		WHERE n.id = v.id
		  AND (CASE n.status
		        WHEN 'pending' THEN 0
		        WHEN 'sent' THEN 1
		        WHEN 'delivered' THEN 2
		        WHEN 'read' THEN 3
		        WHEN 'archived' THEN 4
		        ELSE 0
		      END) < v.rank`,
		ids, statuses, ranks, times,
	)
	if err != nil {
		return fmt.Errorf("batch update notification statuses: %w", err)
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

// DeleteEventsOlderThan deletes up to batchSize events with created_at before the given time.
// Returns the number of rows deleted. Callers should loop until 0 is returned.
func (s *Store) DeleteEventsOlderThan(ctx context.Context, before time.Time, batchSize int) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM notification_events
		WHERE id IN (
			SELECT id FROM notification_events
			WHERE created_at < $1
			LIMIT $2
		)`, before, batchSize)
	if err != nil {
		return 0, fmt.Errorf("delete old events: %w", err)
	}
	return tag.RowsAffected(), nil
}
