// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package dynamo

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"

	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/hermes-notifications/hermes/internal/store"
)

// notifStatusUpdater is the minimum interface EventStore needs to delegate status
// updates. Satisfied by both *postgres.Store (Phase 1) and *NotificationStore (Phase 2).
type notifStatusUpdater interface {
	UpdateNotificationStatus(ctx context.Context, notificationID string, newStatus models.NotificationStatus, eventTime time.Time) error
	BatchUpdateNotificationStatuses(ctx context.Context, updates []store.StatusUpdate) error
}

// EventStore implements store.EventRepository using DynamoDB for event inserts
// and TTL-based expiry.
//
// Status updates (UpdateNotificationStatus, BatchUpdateNotificationStatuses)
// are delegated to a Postgres-backed store.EventRepository during the
// transition period — they update the notifications table, which lives in
// Postgres until Phase 2 of the migration (see docs/adr/0001-*).
//
// Table: hermes-events
//   pk:  NOTIF#<notification_id>
//   sk:  EVT#<id>
type EventStore struct {
	client   *Client
	delegate notifStatusUpdater
}

// NewEventStore creates an EventStore. delegate handles status updates —
// pass *postgres.Store for Phase 1, *NotificationStore for Phase 2.
func NewEventStore(client *Client, delegate notifStatusUpdater) *EventStore {
	return &EventStore{client: client, delegate: delegate}
}

// InsertEvent inserts a single notification event into DynamoDB.
func (s *EventStore) InsertEvent(ctx context.Context, notificationID, channel, event, severity string, metadata []byte) error {
	id := uuid.New().String()
	now := time.Now().UTC()
	ttl := s.ttlSeconds(now)

	item := map[string]types.AttributeValue{
		"pk":              strVal("NOTIF#" + notificationID),
		"sk":              strVal("EVT#" + id),
		"notification_id": strVal(notificationID),
		"id":              strVal(id),
		"channel":         strVal(channel),
		"event":           strVal(event),
		"severity":        strVal(severity),
		"created_at":      strVal(now.Format(time.RFC3339Nano)),
		ttlAttr:           numVal(ttl),
	}
	if len(metadata) > 0 {
		item["metadata"] = strVal(string(metadata))
	}

	_, err := s.client.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.client.EventsTable),
		Item:      item,
	})
	if err != nil {
		return fmt.Errorf("dynamo insert event: %w", err)
	}
	return nil
}

// InsertEvents batch-inserts notification events using DynamoDB BatchWriteItem.
// DynamoDB limits batches to 25 items; this method fans out automatically.
func (s *EventStore) InsertEvents(ctx context.Context, events []models.NotificationEvent) error {
	if len(events) == 0 {
		return nil
	}

	const batchMax = 25
	now := time.Now().UTC()
	ttl := s.ttlSeconds(now)

	for i := 0; i < len(events); i += batchMax {
		end := i + batchMax
		if end > len(events) {
			end = len(events)
		}
		batch := events[i:end]

		reqs := make([]types.WriteRequest, 0, len(batch))
		for _, e := range batch {
			id := e.ID
			if id == "" {
				id = uuid.New().String()
			}
			ts := e.CreatedAt
			if ts.IsZero() {
				ts = now
			}
			item := map[string]types.AttributeValue{
				"pk":              strVal("NOTIF#" + e.NotificationID),
				"sk":              strVal("EVT#" + id),
				"notification_id": strVal(e.NotificationID),
				"id":              strVal(id),
				"channel":         strVal(e.Channel),
				"event":           strVal(e.Event),
				"severity":        strVal(e.Severity),
				"created_at":      strVal(ts.UTC().Format(time.RFC3339Nano)),
				ttlAttr:           numVal(ttl),
			}
			if len(e.Metadata) > 0 {
				item["metadata"] = strVal(string(e.Metadata))
			}
			reqs = append(reqs, types.WriteRequest{
				PutRequest: &types.PutRequest{Item: item},
			})
		}

		_, err := s.client.db.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{
			RequestItems: map[string][]types.WriteRequest{
				s.client.EventsTable: reqs,
			},
		})
		if err != nil {
			return fmt.Errorf("dynamo batch insert events: %w", err)
		}
	}
	return nil
}

// UpdateNotificationStatus delegates to the Postgres store during the
// transition period (notifications table still lives in Postgres).
func (s *EventStore) UpdateNotificationStatus(ctx context.Context, notificationID string, newStatus models.NotificationStatus, eventTime time.Time) error {
	return s.delegate.UpdateNotificationStatus(ctx, notificationID, newStatus, eventTime)
}

// BatchUpdateNotificationStatuses delegates to the Postgres store during the
// transition period.
func (s *EventStore) BatchUpdateNotificationStatuses(ctx context.Context, updates []store.StatusUpdate) error {
	return s.delegate.BatchUpdateNotificationStatuses(ctx, updates)
}

// GetNotificationEvents returns all events for a notification, sorted by created_at ascending.
// This satisfies the NotificationRepository.GetNotificationEvents contract so the admin
// panel can read events that were written by the DynamoDB path.
func (s *EventStore) GetNotificationEvents(ctx context.Context, notificationID string) ([]models.NotificationEvent, error) {
	out, err := s.client.db.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.client.EventsTable),
		KeyConditionExpression: aws.String("pk = :pk"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk": strVal("NOTIF#" + notificationID),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("dynamo get notification events: %w", err)
	}

	events := make([]models.NotificationEvent, 0, len(out.Items))
	for _, item := range out.Items {
		e, err := unmarshalNotificationEvent(item)
		if err != nil {
			return nil, err
		}
		events = append(events, *e)
	}

	// DynamoDB returns items sorted by SK (random UUID), not by time.
	// Sort by created_at to match the Postgres ORDER BY created_at behaviour.
	slices.SortFunc(events, func(a, b models.NotificationEvent) int {
		return a.CreatedAt.Compare(b.CreatedAt)
	})

	return events, nil
}

// DeleteEventsOlderThan is a no-op for the DynamoDB path: events expire
// automatically via native DynamoDB TTL. Returns 0, nil.
func (s *EventStore) DeleteEventsOlderThan(_ context.Context, _ time.Time, _ int) (int64, error) {
	return 0, nil
}

// ttlSeconds returns the Unix epoch second at which events should expire.
// Retention period comes from Client.RetentionDays (set from cfg.EventRetentionDays);
// falls back to 90 days when the value is unset (≤0).
func (s *EventStore) ttlSeconds(createdAt time.Time) string {
	days := s.client.RetentionDays
	if days <= 0 {
		days = 90
	}
	expiry := createdAt.Add(time.Duration(days) * 24 * time.Hour)
	return strconv.FormatInt(expiry.Unix(), 10)
}

// strVal is a convenience constructor for a DynamoDB String attribute.
func strVal(v string) types.AttributeValue {
	return &types.AttributeValueMemberS{Value: v}
}

// numVal is a convenience constructor for a DynamoDB Number attribute.
func numVal(v string) types.AttributeValue {
	return &types.AttributeValueMemberN{Value: v}
}

// boolVal is a convenience constructor for a DynamoDB Boolean attribute.
func boolVal(v bool) types.AttributeValue {
	return &types.AttributeValueMemberBOOL{Value: v}
}

// unmarshalNotificationEvent converts a raw DynamoDB item into a NotificationEvent.
func unmarshalNotificationEvent(item map[string]types.AttributeValue) (*models.NotificationEvent, error) {
	e := &models.NotificationEvent{}
	if v, ok := item["id"].(*types.AttributeValueMemberS); ok {
		e.ID = v.Value
	}
	if v, ok := item["notification_id"].(*types.AttributeValueMemberS); ok {
		e.NotificationID = v.Value
	}
	if v, ok := item["channel"].(*types.AttributeValueMemberS); ok {
		e.Channel = v.Value
	}
	if v, ok := item["event"].(*types.AttributeValueMemberS); ok {
		e.Event = v.Value
	}
	if v, ok := item["severity"].(*types.AttributeValueMemberS); ok {
		e.Severity = v.Value
	}
	if v, ok := item["metadata"].(*types.AttributeValueMemberS); ok && v.Value != "" {
		e.Metadata = json.RawMessage(v.Value)
	}
	if v, ok := item["created_at"].(*types.AttributeValueMemberS); ok {
		t, err := time.Parse(time.RFC3339Nano, v.Value)
		if err != nil {
			return nil, fmt.Errorf("parse event created_at: %w", err)
		}
		e.CreatedAt = t
	}
	return e, nil
}
