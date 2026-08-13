// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package dynamo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/errgroup"

	"github.com/hermesnotifications/hermes/internal/models"
	"github.com/hermesnotifications/hermes/internal/store"
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
//
//	PK (pk):  NOTIF#<notification_id>
//	SK (sk):  NOTIF#<notification_id>
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
	created, err := s.putNotification(ctx, n)
	if err != nil {
		return nil, fmt.Errorf("create notification: %w", err)
	}
	if !created {
		return nil, fmt.Errorf("create notification: unique constraint violation")
	}
	return n, nil
}

// CreateNotifications persists a batch of notifications.
//
// One item at a time, deliberately. The batch exists to amortise a WAL flush, and DynamoDB has
// no such flush to amortise — TransactWriteItems would cap the batch at 100 items and double
// the write cost to buy an atomicity nothing here needs. So this satisfies the interface with
// the same observable contract (skip what is already there, report which IDs were new) and
// leaves the callers identical across backends.
//
// The consequence, spelled out in the interface: a failure part-way through leaves the earlier
// items written. That is safe only because each write is conditional on the notification ID not
// already existing, so the caller's per-row retry re-writes nothing.
func (s *NotificationStore) CreateNotifications(ctx context.Context, ns []*models.Notification) ([]string, error) {
	inserted := make([]string, 0, len(ns))
	for _, n := range ns {
		created, err := s.putNotification(ctx, n)
		if err != nil {
			return inserted, fmt.Errorf("create notification %s: %w", n.ID, err)
		}
		if created {
			inserted = append(inserted, n.ID)
		}
	}
	return inserted, nil
}

// putNotification writes the item, reporting whether it was newly created. A failed condition
// check means an item with this ID is already there — a distinction the batch path needs and
// the single-item path flattens back into an error.
func (s *NotificationStore) putNotification(ctx context.Context, n *models.Notification) (bool, error) {
	now := time.Now().UTC()
	if n.CreatedAt.IsZero() {
		n.CreatedAt = now
	}

	item := map[string]types.AttributeValue{
		"pk":              strVal("NOTIF#" + n.ID),
		"sk":              strVal("NOTIF#" + n.ID),
		"notif_id":        strVal(n.ID),
		"user_id":         strVal(n.UserID),
		"organization_id": strVal(n.OrganizationID),
		"title":           strVal(n.Title),
		"body":            strVal(n.Body),
		"status":          strVal(string(n.Status)),
		"status_rank":     numVal(fmt.Sprintf("%d", n.Status.Rank())),
		"created_at":      strVal(n.CreatedAt.UTC().Format(time.RFC3339Nano)),
		"inbox_state":     strVal(inboxStateActive),
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
	if len(n.Metadata) > 0 {
		// A JSON string, matching unmarshalNotif -- see the note there on why not a native M.
		encoded, err := json.Marshal(n.Metadata)
		if err != nil {
			return false, fmt.Errorf("marshal metadata: %w", err)
		}
		item["metadata"] = strVal(string(encoded))
	}

	_, err := s.client.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(s.client.NotificationsTable),
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(pk)"),
	})
	if err != nil {
		var ccfe *types.ConditionalCheckFailedException
		if errors.As(err, &ccfe) {
			return false, nil
		}
		return false, err
	}
	return true, nil
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
// Cursor is base64(notif_id of last returned item). Returns (items, nextCursor, error).
//
// It no longer returns the unread count. On this store that mattered more than on Postgres: the
// count is a paginated Query, so every page of a scroll paid for a fresh walk of the user's
// history. See UnreadCount.
func (s *NotificationStore) ListInbox(ctx context.Context, userID string, archived bool, cursor string, limit int) ([]models.Notification, string, error) {
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
			return nil, "", fmt.Errorf("invalid cursor: %w", err)
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
		return nil, "", fmt.Errorf("list inbox: %w", err)
	}

	var notifications []models.Notification
	for _, item := range out.Items {
		n, err := unmarshalNotif(item)
		if err != nil {
			return nil, "", fmt.Errorf("list inbox scan: %w", err)
		}
		notifications = append(notifications, *n)
	}

	var nextCursor string
	if len(notifications) > limit {
		notifications = notifications[:limit]
		nextCursor = encodeCursorNotif(notifications[limit-1].ID)
	}

	return notifications, nextCursor, nil
}

// maxUnreadCountQueries bounds the case models.UnreadCountCap alone cannot: a user whose unread
// rows are sparse within a very large history.
//
// Select: COUNT with a FilterExpression is billed for every item *scanned*, not every item
// counted, so this loop can burn a great deal of read capacity while count barely moves --
// and it is on the path of a badge. Twenty pages is roughly 20 MB scanned; past that we return
// what we have, because a bounded-but-low count beats a request that never returns.
//
// The real fix is a sparse GSI so that scanned == counted; see ADR 0011.
const maxUnreadCountQueries = 20

// UnreadCount returns the number of unread, active, non-deleted notifications for a user,
// saturating at models.UnreadCountCap.
// UnreadCount returns the unread count and the watermark in a single descending pass over the
// by-user index.
//
// One query, not two, and that is the whole point. The obvious implementation — a SelectCount for
// the count, then a Limit(1) query for the newest id — reads the same GSI twice, and a GSI is
// eventually consistent. The second read can therefore observe *less* than the first: the count
// includes a just-arrived notification while the watermark does not, so its delivery increments a
// value that already contains it. That is exactly the overcount the watermark exists to prevent,
// reintroduced by the way it was fetched. It reproduced about one run in eight.
//
// Reading descending means the newest id is simply the first item, so the watermark costs nothing
// beyond projecting one more attribute. Counting here rather than with a FilterExpression does not
// change what is scanned — a filter is applied after the scan, and the note above already says the
// scan is what gets billed.
func (s *NotificationStore) UnreadCount(ctx context.Context, userID string) (int, string, error) {
	var count int
	watermark := ""
	var lastKey map[string]types.AttributeValue

	for page := 0; page < maxUnreadCountQueries; page++ {
		input := &dynamodb.QueryInput{
			TableName:              aws.String(s.client.NotificationsTable),
			IndexName:              aws.String(gsiByUser),
			KeyConditionExpression: aws.String("user_id = :uid"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":uid": strVal(userID),
			},
			// read_at is a reserved word, hence the alias.
			ProjectionExpression:     aws.String("notif_id, inbox_state, #ra"),
			ExpressionAttributeNames: map[string]string{"#ra": "read_at"},
			ScanIndexForward:         aws.Bool(false), // newest first, so item 0 is the watermark
		}
		if lastKey != nil {
			input.ExclusiveStartKey = lastKey
		}
		out, err := s.client.db.Query(ctx, input)
		if err != nil {
			return 0, "", fmt.Errorf("unread count: %w", err)
		}

		for i, item := range out.Items {
			if page == 0 && i == 0 {
				if id, ok := item["notif_id"].(*types.AttributeValueMemberS); ok {
					watermark = id.Value
				}
			}
			state, _ := item["inbox_state"].(*types.AttributeValueMemberS)
			if state == nil || state.Value != inboxStateActive {
				continue
			}
			if _, read := item["read_at"]; read {
				continue
			}
			count++
			if count >= models.UnreadCountCap {
				return models.UnreadCountCap, watermark, nil
			}
		}

		if out.LastEvaluatedKey == nil {
			break
		}
		lastKey = out.LastEvaluatedKey
	}
	return count, watermark, nil
}

// MarkRead marks a notification as read and advances its status if below 'read'.
// Returns true if the notification was actually changed (was unread).
func (s *NotificationStore) MarkRead(ctx context.Context, userID, notificationID string) (bool, error) {
	now := strVal(time.Now().UTC().Format(time.RFC3339Nano))

	// Common case: unread and status rank < read (3). Advance status to read.
	_, err := s.client.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                aws.String(s.client.NotificationsTable),
		Key:                      notifKey(notificationID),
		ConditionExpression:      aws.String("user_id = :uid AND attribute_not_exists(read_at) AND (status_rank < :r3 OR attribute_not_exists(status_rank))"),
		UpdateExpression:         aws.String("SET read_at = :now, #s = :read, status_rank = :r3"),
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
		TableName:                aws.String(s.client.NotificationsTable),
		Key:                      notifKey(notificationID),
		ConditionExpression:      aws.String("user_id = :uid AND attribute_exists(read_at)"),
		UpdateExpression:         aws.String("REMOVE read_at SET #s = :delivered, status_rank = :r2"),
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
		TableName:                aws.String(s.client.NotificationsTable),
		Key:                      notifKey(notificationID),
		ConditionExpression:      aws.String("user_id = :uid AND inbox_state = :active"),
		UpdateExpression:         aws.String("SET archived_at = :now, inbox_state = :archived, #s = :archived_status, status_rank = :r4"),
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
		TableName:                aws.String(s.client.NotificationsTable),
		Key:                      notifKey(notificationID),
		ConditionExpression:      aws.String("user_id = :uid AND inbox_state = :archived AND attribute_exists(read_at)"),
		UpdateExpression:         aws.String("REMOVE archived_at SET inbox_state = :active, #s = :read, status_rank = :r3"),
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
		TableName:                aws.String(s.client.NotificationsTable),
		Key:                      notifKey(notificationID),
		ConditionExpression:      aws.String("user_id = :uid AND inbox_state = :archived AND attribute_not_exists(read_at)"),
		UpdateExpression:         aws.String("REMOVE archived_at SET inbox_state = :active, #s = :delivered, status_rank = :r2"),
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

	// Stored as a JSON string rather than a native M, so both stores round-trip metadata
	// through one encoding/json path and cannot disagree about numbers -- DynamoDB's N carries
	// numbers as decimal strings, so a native map would come back with a different Go type than
	// the Postgres path yields.
	//
	// A parse failure degrades to no metadata rather than failing the read: this column is
	// decoration, and refusing to return a notification because one opaque blob is malformed
	// would take the whole inbox down with it.
	if s, ok := strAttr(item, "metadata"); ok && s != "" {
		var metadata models.NotificationMetadata
		if err := json.Unmarshal([]byte(s), &metadata); err == nil {
			n.Metadata = metadata
		}
	}

	n.SentAt = optTime(item, "sent_at")
	n.DeliveredAt = optTime(item, "delivered_at")
	n.ReadAt = optTime(item, "read_at")
	n.ArchivedAt = optTime(item, "archived_at")
	n.DeletedAt = optTime(item, "deleted_at")

	return n, nil
}
