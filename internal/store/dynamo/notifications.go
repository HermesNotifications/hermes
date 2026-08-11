// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package dynamo

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/errgroup"

	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/hermes-notifications/hermes/internal/store"
)

// inboxState values stored on notification items for GSI-based filtering.
const (
	inboxStateActive   = "active"
	inboxStateArchived = "archived"
	inboxStateDeleted  = "deleted"
)

// NotificationStore implements store.NotificationRepository and store.InboxRepository
// using the hermes-notifications DynamoDB table.
//
// Table: hermes-notifications
//   PK (pk):  NOTIF#<notification_id>
//   SK (sk):  NOTIF#<notification_id>
//
// GSI gsi-by-user  PK=user_id SK=notif_id  — inbox listing (time-sorted via sortable IDs)
// GSI gsi-by-idempotency  PK=idem_pk SK=created_at  — dispatch dedup (sparse; only set when key present)
//
// ListRecentNotifications is intentionally not implemented — it performs a
// cross-organization scan that belongs to the admin control plane and is delegated
// to Postgres. Wire adminStoreWithDynamoNotifs (cmd/admin) to route it there.
type NotificationStore struct {
	client *Client
	events *EventStore // for GetNotificationEvents delegation
}

// NewNotificationStore creates a NotificationStore. The events parameter is used
// for GetNotificationEvents; pass the EventStore wired to hermes-events.
func NewNotificationStore(client *Client, events *EventStore) *NotificationStore {
	return &NotificationStore{client: client, events: events}
}

// notifKey returns the DynamoDB primary key map for a notification ID.
func notifKey(id string) map[string]types.AttributeValue {
	k := "NOTIF#" + id
	return map[string]types.AttributeValue{
		"pk": strVal(k),
		"sk": strVal(k),
	}
}

// ── NotificationRepository ────────────────────────────────────────────────────

// CreateNotification writes a new notification item. On idempotency-key conflict
// it returns the error — callers (dispatch) catch duplicate strings to continue.
func (s *NotificationStore) CreateNotification(ctx context.Context, n *models.Notification) (*models.Notification, error) {
	now := time.Now().UTC()
	if n.CreatedAt.IsZero() {
		n.CreatedAt = now
	}

	item := map[string]types.AttributeValue{
		"pk":          strVal("NOTIF#" + n.ID),
		"sk":          strVal("NOTIF#" + n.ID),
		"notif_id":    strVal(n.ID),
		"user_id":     strVal(n.UserID),
		"organization_id":   strVal(n.OrganizationID),
		"title":       strVal(n.Title),
		"body":        strVal(n.Body),
		"status":      strVal(string(n.Status)),
		"status_rank": numVal(fmt.Sprintf("%d", n.Status.Rank())),
		"created_at":  strVal(n.CreatedAt.UTC().Format(time.RFC3339Nano)),
		"inbox_state": strVal(inboxStateActive),
	}

	if len(n.Channels) > 0 {
		ss := make([]string, len(n.Channels))
		copy(ss, n.Channels)
		item["channels"] = &types.AttributeValueMemberSS{Value: ss}
	}
	if n.TemplateID != nil {
		item["template_id"] = strVal(*n.TemplateID)
	}
	if n.CategoryID != "" {
		item["category_id"] = strVal(n.CategoryID)
	}
	if n.ActionURL != nil {
		item["action_url"] = strVal(*n.ActionURL)
	}
	if n.ActionLabel != nil {
		item["action_label"] = strVal(*n.ActionLabel)
	}
	if n.IdempotencyKey != nil && *n.IdempotencyKey != "" {
		item["idempotency_key"] = strVal(*n.IdempotencyKey)
		item["idem_pk"] = strVal("ORG#" + n.OrganizationID + "#IDEM#" + *n.IdempotencyKey)
	}

	_, err := s.client.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(s.client.NotificationsTable),
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(pk)"),
	})
	if err != nil {
		var ccfe *types.ConditionalCheckFailedException
		if errors.As(err, &ccfe) {
			return nil, fmt.Errorf("create notification: unique constraint violation")
		}
		return nil, fmt.Errorf("create notification: %w", err)
	}
	return n, nil
}

// GetNotificationByID returns a notification by its ID.
func (s *NotificationStore) GetNotificationByID(ctx context.Context, id string) (*models.Notification, error) {
	out, err := s.client.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.client.NotificationsTable),
		Key:       notifKey(id),
	})
	if err != nil {
		return nil, fmt.Errorf("get notification: %w", err)
	}
	if out.Item == nil {
		return nil, fmt.Errorf("get notification: %w", pgx.ErrNoRows)
	}
	return unmarshalNotif(out.Item)
}

// GetNotificationByIdempotencyKey returns the notification matching (organizationID, key)
// that was created within the last 24 hours.
func (s *NotificationStore) GetNotificationByIdempotencyKey(ctx context.Context, organizationID, key string) (*models.Notification, error) {
	cutoff := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339Nano)
	idemPK := "ORG#" + organizationID + "#IDEM#" + key

	out, err := s.client.db.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.client.NotificationsTable),
		IndexName:              aws.String(gsiByIdempotency),
		KeyConditionExpression: aws.String("idem_pk = :pk AND created_at > :cutoff"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk":     strVal(idemPK),
			":cutoff": strVal(cutoff),
		},
		Limit: aws.Int32(1),
	})
	if err != nil {
		return nil, fmt.Errorf("get notification by idempotency key: %w", err)
	}
	if len(out.Items) == 0 {
		return nil, fmt.Errorf("get notification by idempotency key: %w", pgx.ErrNoRows)
	}
	return unmarshalNotif(out.Items[0])
}

// GetNotificationEvents delegates to the EventStore which reads hermes-events.
func (s *NotificationStore) GetNotificationEvents(ctx context.Context, notificationID string) ([]models.NotificationEvent, error) {
	return s.events.GetNotificationEvents(ctx, notificationID)
}

// ListRecentNotifications is not implemented for the DynamoDB path — it is a
// cross-organization admin scan that stays on Postgres. Callers must use the composite
// store in cmd/admin that routes this method to the Postgres store.
func (s *NotificationStore) ListRecentNotifications(_ context.Context, _ int) ([]models.Notification, error) {
	return nil, fmt.Errorf("ListRecentNotifications: not supported on DynamoDB store — use Postgres path")
}

// UpdateNotificationChannels updates the channels field on a notification.
func (s *NotificationStore) UpdateNotificationChannels(ctx context.Context, notificationID string, channels []string) error {
	expr := "SET channels = :ch"
	vals := map[string]types.AttributeValue{}
	if len(channels) > 0 {
		vals[":ch"] = &types.AttributeValueMemberSS{Value: channels}
	} else {
		expr = "REMOVE channels"
	}
	_, err := s.client.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 aws.String(s.client.NotificationsTable),
		Key:                       notifKey(notificationID),
		UpdateExpression:          aws.String(expr),
		ExpressionAttributeValues: vals,
	})
	if err != nil {
		return fmt.Errorf("update notification channels: %w", err)
	}
	return nil
}

// UpdateNotificationRouting backfills template/category/title/body after dispatch resolves them.
func (s *NotificationStore) UpdateNotificationRouting(ctx context.Context, n *models.Notification) error {
	sets := []string{}
	vals := map[string]types.AttributeValue{}
	names := map[string]string{}

	if n.TemplateID != nil {
		sets = append(sets, "template_id = :tid")
		vals[":tid"] = strVal(*n.TemplateID)
	}
	if n.CategoryID != "" {
		sets = append(sets, "category_id = :cid")
		vals[":cid"] = strVal(n.CategoryID)
	}
	if n.Title != "" {
		sets = append(sets, "#t = :title")
		vals[":title"] = strVal(n.Title)
		names["#t"] = "title"
	}
	if n.Body != "" {
		sets = append(sets, "#b = :body")
		vals[":body"] = strVal(n.Body)
		names["#b"] = "body"
	}
	if len(sets) == 0 {
		return nil
	}

	input := &dynamodb.UpdateItemInput{
		TableName:        aws.String(s.client.NotificationsTable),
		Key:              notifKey(n.ID),
		UpdateExpression: aws.String("SET " + strings.Join(sets, ", ")),
	}
	if len(vals) > 0 {
		input.ExpressionAttributeValues = vals
	}
	if len(names) > 0 {
		input.ExpressionAttributeNames = names
	}
	if _, err := s.client.db.UpdateItem(ctx, input); err != nil {
		return fmt.Errorf("update notification routing: %w", err)
	}
	return nil
}

// FailNotification sets status to "failed" when the notification is still pending.
func (s *NotificationStore) FailNotification(ctx context.Context, notificationID string) error {
	_, err := s.client.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:           aws.String(s.client.NotificationsTable),
		Key:                 notifKey(notificationID),
		ConditionExpression: aws.String("#s = :pending"),
		UpdateExpression:    aws.String("SET #s = :failed, status_rank = :zero"),
		ExpressionAttributeNames: map[string]string{
			"#s": "status",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pending": strVal(string(models.StatusPending)),
			":failed":  strVal(string(models.StatusFailed)),
			":zero":    numVal("0"),
		},
	})
	if err != nil {
		var ccfe *types.ConditionalCheckFailedException
		if errors.As(err, &ccfe) {
			return nil // already advanced past pending — not an error
		}
		return fmt.Errorf("fail notification: %w", err)
	}
	return nil
}

// ── Status updates (satisfies notifStatusUpdater) ─────────────────────────────

// UpdateNotificationStatus advances a notification's status using a conditional write
// that prevents regression — stale or out-of-order events are silently skipped.
func (s *NotificationStore) UpdateNotificationStatus(ctx context.Context, notificationID string, newStatus models.NotificationStatus, eventTime time.Time) error {
	rank := newStatus.Rank()
	vals := map[string]types.AttributeValue{
		":rank":   numVal(fmt.Sprintf("%d", rank)),
		":status": strVal(string(newStatus)),
	}
	expr := "SET #s = :status, status_rank = :rank"

	if rank >= 1 { // sent or higher — set sent_at (COALESCE: only if not already set)
		expr += ", sent_at = if_not_exists(sent_at, :ts)"
		vals[":ts"] = strVal(eventTime.UTC().Format(time.RFC3339Nano))
	}
	if rank >= 2 { // delivered or higher — set delivered_at
		expr += ", delivered_at = if_not_exists(delivered_at, :ts)"
	}

	_, err := s.client.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:           aws.String(s.client.NotificationsTable),
		Key:                 notifKey(notificationID),
		ConditionExpression: aws.String("status_rank < :rank OR attribute_not_exists(status_rank)"),
		UpdateExpression:    aws.String(expr),
		ExpressionAttributeNames: map[string]string{
			"#s": "status",
		},
		ExpressionAttributeValues: vals,
	})
	if err != nil {
		var ccfe *types.ConditionalCheckFailedException
		if errors.As(err, &ccfe) {
			return nil // stale event — silently skip
		}
		return fmt.Errorf("update notification status: %w", err)
	}
	return nil
}

// BatchUpdateNotificationStatuses advances statuses for multiple notifications.
// Deduplicates per notification ID keeping the highest-rank update before executing.
func (s *NotificationStore) BatchUpdateNotificationStatuses(ctx context.Context, updates []store.StatusUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	// Dedup — keep highest rank per notification ID (mirrors Postgres implementation).
	best := make(map[string]store.StatusUpdate)
	for _, u := range updates {
		if existing, ok := best[u.NotificationID]; !ok || u.NewStatus.Rank() > existing.NewStatus.Rank() {
			best[u.NotificationID] = u
		}
	}
	for _, u := range best {
		if err := s.UpdateNotificationStatus(ctx, u.NotificationID, u.NewStatus, u.EventTime); err != nil {
			return err
		}
	}
	return nil
}

// ── InboxRepository ───────────────────────────────────────────────────────────

// ListInbox returns cursor-paginated notifications for a user's inbox or archive.
// Cursor is base64(notif_id of last returned item). Returns (items, unreadCount, nextCursor, error).
func (s *NotificationStore) ListInbox(ctx context.Context, userID string, archived bool, cursor string, limit int) ([]models.Notification, int, string, error) {
	if limit <= 0 {
		limit = 20
	}

	state := inboxStateActive
	if archived {
		state = inboxStateArchived
	}

	input := &dynamodb.QueryInput{
		TableName:              aws.String(s.client.NotificationsTable),
		IndexName:              aws.String(gsiByUser),
		KeyConditionExpression: aws.String("user_id = :uid"),
		FilterExpression:       aws.String("inbox_state = :state"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":uid":   strVal(userID),
			":state": strVal(state),
		},
		ScanIndexForward: aws.Bool(false), // newest first (notif_id DESC)
		Limit:            aws.Int32(int32(limit + 1)),
	}

	if cursor != "" {
		lastID, err := decodeCursorNotif(cursor)
		if err != nil {
			return nil, 0, "", fmt.Errorf("invalid cursor: %w", err)
		}
		input.ExclusiveStartKey = map[string]types.AttributeValue{
			"user_id":  strVal(userID),
			"notif_id": strVal(lastID),
			"pk":       strVal("NOTIF#" + lastID),
			"sk":       strVal("NOTIF#" + lastID),
		}
	}

	out, err := s.client.db.Query(ctx, input)
	if err != nil {
		return nil, 0, "", fmt.Errorf("list inbox: %w", err)
	}

	var notifications []models.Notification
	for _, item := range out.Items {
		n, err := unmarshalNotif(item)
		if err != nil {
			return nil, 0, "", fmt.Errorf("list inbox scan: %w", err)
		}
		notifications = append(notifications, *n)
	}

	var nextCursor string
	if len(notifications) > limit {
		notifications = notifications[:limit]
		nextCursor = encodeCursorNotif(notifications[limit-1].ID)
	}

	unreadCount, err := s.UnreadCount(ctx, userID)
	if err != nil {
		return nil, 0, "", err
	}
	return notifications, unreadCount, nextCursor, nil
}

// UnreadCount returns the number of unread, active, non-deleted notifications for a user.
func (s *NotificationStore) UnreadCount(ctx context.Context, userID string) (int, error) {
	var count int
	var lastKey map[string]types.AttributeValue

	for {
		input := &dynamodb.QueryInput{
			TableName:              aws.String(s.client.NotificationsTable),
			IndexName:              aws.String(gsiByUser),
			KeyConditionExpression: aws.String("user_id = :uid"),
			FilterExpression:       aws.String("inbox_state = :active AND attribute_not_exists(read_at)"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":uid":    strVal(userID),
				":active": strVal(inboxStateActive),
			},
			Select: types.SelectCount,
		}
		if lastKey != nil {
			input.ExclusiveStartKey = lastKey
		}
		out, err := s.client.db.Query(ctx, input)
		if err != nil {
			return 0, fmt.Errorf("unread count: %w", err)
		}
		count += int(out.Count)
		if out.LastEvaluatedKey == nil {
			break
		}
		lastKey = out.LastEvaluatedKey
	}
	return count, nil
}

// MarkRead marks a notification as read and advances its status if below 'read'.
// Returns true if the notification was actually changed (was unread).
func (s *NotificationStore) MarkRead(ctx context.Context, userID, notificationID string) (bool, error) {
	now := strVal(time.Now().UTC().Format(time.RFC3339Nano))

	// Common case: unread and status rank < read (3). Advance status to read.
	_, err := s.client.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:           aws.String(s.client.NotificationsTable),
		Key:                 notifKey(notificationID),
		ConditionExpression: aws.String("user_id = :uid AND attribute_not_exists(read_at) AND (status_rank < :r3 OR attribute_not_exists(status_rank))"),
		UpdateExpression:    aws.String("SET read_at = :now, #s = :read, status_rank = :r3"),
		ExpressionAttributeNames: map[string]string{"#s": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":uid":  strVal(userID),
			":now":  now,
			":read": strVal(string(models.StatusRead)),
			":r3":   numVal("3"),
		},
	})
	if err == nil {
		return true, nil
	}
	var ccfe *types.ConditionalCheckFailedException
	if !errors.As(err, &ccfe) {
		return false, fmt.Errorf("mark read: %w", err)
	}

	// Edge case: unread but archived (rank=4) — set read_at, keep status=archived.
	_, err = s.client.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:           aws.String(s.client.NotificationsTable),
		Key:                 notifKey(notificationID),
		ConditionExpression: aws.String("user_id = :uid AND attribute_not_exists(read_at) AND status_rank >= :r3"),
		UpdateExpression:    aws.String("SET read_at = :now"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":uid": strVal(userID),
			":now": now,
			":r3":  numVal("3"),
		},
	})
	if err == nil {
		return true, nil
	}
	if errors.As(err, &ccfe) {
		return false, nil // already read
	}
	return false, fmt.Errorf("mark read (archived path): %w", err)
}

// MarkUnread marks a notification as unread and reverts status to delivered if it was read.
// Returns true if the notification was actually changed (was read).
func (s *NotificationStore) MarkUnread(ctx context.Context, userID, notificationID string) (bool, error) {
	_, err := s.client.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:           aws.String(s.client.NotificationsTable),
		Key:                 notifKey(notificationID),
		ConditionExpression: aws.String("user_id = :uid AND attribute_exists(read_at)"),
		UpdateExpression:    aws.String("REMOVE read_at SET #s = :delivered, status_rank = :r2"),
		ExpressionAttributeNames: map[string]string{"#s": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":uid":       strVal(userID),
			":delivered": strVal(string(models.StatusDelivered)),
			":r2":        numVal("2"),
		},
	})
	if err == nil {
		return true, nil
	}
	var ccfe *types.ConditionalCheckFailedException
	if errors.As(err, &ccfe) {
		return false, nil // already unread
	}
	return false, fmt.Errorf("mark unread: %w", err)
}

// Archive archives a notification. Returns true if the archived notification was unread
// (so the caller can decrement the unread count cache).
func (s *NotificationStore) Archive(ctx context.Context, userID, notificationID string) (bool, error) {
	out, err := s.client.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:           aws.String(s.client.NotificationsTable),
		Key:                 notifKey(notificationID),
		ConditionExpression: aws.String("user_id = :uid AND inbox_state = :active"),
		UpdateExpression:    aws.String("SET archived_at = :now, inbox_state = :archived, #s = :archived_status, status_rank = :r4"),
		ExpressionAttributeNames: map[string]string{"#s": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":uid":             strVal(userID),
			":active":          strVal(inboxStateActive),
			":now":             strVal(time.Now().UTC().Format(time.RFC3339Nano)),
			":archived":        strVal(inboxStateArchived),
			":archived_status": strVal(string(models.StatusArchived)),
			":r4":              numVal("4"),
		},
		ReturnValues: types.ReturnValueAllNew,
	})
	if err != nil {
		var ccfe *types.ConditionalCheckFailedException
		if errors.As(err, &ccfe) {
			return false, nil // already archived
		}
		return false, fmt.Errorf("archive: %w", err)
	}
	_, wasUnread := out.Attributes["read_at"]
	return !wasUnread, nil
}

// Unarchive restores a notification to the active inbox.
// Returns true if the notification is now unread (so caller can increment unread count cache).
//
// Uses two conditional paths (mirroring MarkRead) to correctly restore the pre-archive status:
//   - If the notification had been read before archiving → restore status=read (rank 3), return false (still read).
//   - If the notification was unread when archived → restore status=delivered (rank 2), return true (now unread).
func (s *NotificationStore) Unarchive(ctx context.Context, userID, notificationID string) (bool, error) {
	// Path A: was read before archiving — restore to read state.
	_, err := s.client.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:           aws.String(s.client.NotificationsTable),
		Key:                 notifKey(notificationID),
		ConditionExpression: aws.String("user_id = :uid AND inbox_state = :archived AND attribute_exists(read_at)"),
		UpdateExpression:    aws.String("REMOVE archived_at SET inbox_state = :active, #s = :read, status_rank = :r3"),
		ExpressionAttributeNames: map[string]string{"#s": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":uid":      strVal(userID),
			":archived": strVal(inboxStateArchived),
			":active":   strVal(inboxStateActive),
			":read":     strVal(string(models.StatusRead)),
			":r3":       numVal("3"),
		},
	})
	if err == nil {
		return false, nil // restored to read — still read, not unread
	}
	var ccfe *types.ConditionalCheckFailedException
	if !errors.As(err, &ccfe) {
		return false, fmt.Errorf("unarchive: %w", err)
	}

	// Path B: was unread when archived — restore to delivered state.
	_, err = s.client.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:           aws.String(s.client.NotificationsTable),
		Key:                 notifKey(notificationID),
		ConditionExpression: aws.String("user_id = :uid AND inbox_state = :archived AND attribute_not_exists(read_at)"),
		UpdateExpression:    aws.String("REMOVE archived_at SET inbox_state = :active, #s = :delivered, status_rank = :r2"),
		ExpressionAttributeNames: map[string]string{"#s": "status"},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":uid":       strVal(userID),
			":archived":  strVal(inboxStateArchived),
			":active":    strVal(inboxStateActive),
			":delivered": strVal(string(models.StatusDelivered)),
			":r2":        numVal("2"),
		},
	})
	if err == nil {
		return true, nil // restored to delivered — now unread
	}
	if errors.As(err, &ccfe) {
		return false, nil // not archived (or wrong user)
	}
	return false, fmt.Errorf("unarchive: %w", err)
}

// SoftDelete soft-deletes a notification.
// Returns true if the deleted notification was unread and active (affecting unread count).
func (s *NotificationStore) SoftDelete(ctx context.Context, userID, notificationID string) (bool, error) {
	out, err := s.client.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:           aws.String(s.client.NotificationsTable),
		Key:                 notifKey(notificationID),
		ConditionExpression: aws.String("user_id = :uid AND inbox_state <> :deleted"),
		UpdateExpression:    aws.String("SET deleted_at = :now, inbox_state = :deleted"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":uid":     strVal(userID),
			":deleted": strVal(inboxStateDeleted),
			":now":     strVal(time.Now().UTC().Format(time.RFC3339Nano)),
		},
		ReturnValues: types.ReturnValueAllNew,
	})
	if err != nil {
		var ccfe *types.ConditionalCheckFailedException
		if errors.As(err, &ccfe) {
			return false, nil // already deleted
		}
		return false, fmt.Errorf("soft delete: %w", err)
	}
	// ALL_NEW gives post-update state: inbox_state is now "deleted".
	// Proxy for "was active before delete": archived_at absent → was active.
	_, hasReadAt := out.Attributes["read_at"]
	_, hadArchivedAt := out.Attributes["archived_at"]
	wasUnread := !hasReadAt && !hadArchivedAt
	return wasUnread, nil
}

// MarkAllRead marks all unread, active, non-deleted notifications as read for a user.
// Per-item MarkRead calls are dispatched concurrently (up to markAllReadConcurrency) to
// reduce wall-clock time for users with large inboxes.
const markAllReadConcurrency = 10

func (s *NotificationStore) MarkAllRead(ctx context.Context, userID string) error {
	var lastKey map[string]types.AttributeValue

	for {
		out, err := s.client.db.Query(ctx, &dynamodb.QueryInput{
			TableName:              aws.String(s.client.NotificationsTable),
			IndexName:              aws.String(gsiByUser),
			KeyConditionExpression: aws.String("user_id = :uid"),
			FilterExpression:       aws.String("inbox_state = :active AND attribute_not_exists(read_at)"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":uid":    strVal(userID),
				":active": strVal(inboxStateActive),
			},
			ExclusiveStartKey: lastKey,
		})
		if err != nil {
			return fmt.Errorf("mark all read (query): %w", err)
		}

		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(markAllReadConcurrency)
		for _, item := range out.Items {
			id, _ := strAttr(item, "notif_id")
			if id == "" {
				continue
			}
			g.Go(func() error {
				if _, err := s.MarkRead(gctx, userID, id); err != nil {
					return fmt.Errorf("mark all read (update %s): %w", id, err)
				}
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			return err
		}

		if out.LastEvaluatedKey == nil {
			break
		}
		lastKey = out.LastEvaluatedKey
	}
	return nil
}

// ── Cursor helpers ────────────────────────────────────────────────────────────

func encodeCursorNotif(notifID string) string {
	return base64.StdEncoding.EncodeToString([]byte(notifID))
}

func decodeCursorNotif(cursor string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ── Unmarshal helpers ─────────────────────────────────────────────────────────

// strAttr safely extracts a string attribute value.
func strAttr(item map[string]types.AttributeValue, key string) (string, bool) {
	v, ok := item[key].(*types.AttributeValueMemberS)
	if !ok {
		return "", false
	}
	return v.Value, true
}

func optTime(item map[string]types.AttributeValue, key string) *time.Time {
	s, ok := strAttr(item, key)
	if !ok || s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return nil
	}
	return &t
}

func unmarshalNotif(item map[string]types.AttributeValue) (*models.Notification, error) {
	n := &models.Notification{}

	n.ID, _ = strAttr(item, "notif_id")
	n.UserID, _ = strAttr(item, "user_id")
	n.OrganizationID, _ = strAttr(item, "organization_id")
	n.Title, _ = strAttr(item, "title")
	n.Body, _ = strAttr(item, "body")

	if s, ok := strAttr(item, "status"); ok {
		n.Status = models.NotificationStatus(s)
	}
	if s, ok := strAttr(item, "created_at"); ok {
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return nil, fmt.Errorf("parse created_at: %w", err)
		}
		n.CreatedAt = t
	}

	if tid, ok := strAttr(item, "template_id"); ok {
		n.TemplateID = &tid
	}
	n.CategoryID, _ = strAttr(item, "category_id")

	if v, ok := strAttr(item, "action_url"); ok {
		n.ActionURL = &v
	}
	if v, ok := strAttr(item, "action_label"); ok {
		n.ActionLabel = &v
	}
	if v, ok := strAttr(item, "idempotency_key"); ok {
		n.IdempotencyKey = &v
	}

	if ss, ok := item["channels"].(*types.AttributeValueMemberSS); ok {
		n.Channels = ss.Value
	}

	n.SentAt = optTime(item, "sent_at")
	n.DeliveredAt = optTime(item, "delivered_at")
	n.ReadAt = optTime(item, "read_at")
	n.ArchivedAt = optTime(item, "archived_at")
	n.DeletedAt = optTime(item, "deleted_at")

	return n, nil
}

