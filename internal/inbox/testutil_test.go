// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package inbox_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/internal/inbox"
	"github.com/hermes-notifications/hermes/internal/models"
)

// mockInboxStore implements inbox.InboxStore with in-memory storage.
type mockInboxStore struct {
	notifications []models.Notification
	categories    []models.SubscriptionCategory

	// unreadCountCalls records how many times the store was asked for an authoritative count.
	// Tests assert on it to prove the cache path is doing its job -- a count served from cache
	// must not reach the store at all.
	unreadCountCalls int
	// unreadCountErr, when set, makes UnreadCount fail, exercising the fallback paths.
	unreadCountErr error
}

func (m *mockInboxStore) ListInbox(ctx context.Context, userID string, archived bool, cursor string, limit int) ([]models.Notification, string, error) {
	var result []models.Notification
	for _, n := range m.notifications {
		if n.UserID != userID || n.DeletedAt != nil {
			continue
		}
		if archived && n.ArchivedAt == nil {
			continue
		}
		if !archived && n.ArchivedAt != nil {
			continue
		}
		result = append(result, n)
	}

	if limit <= 0 {
		limit = 20
	}

	nextCursor := ""
	if len(result) > limit {
		result = result[:limit]
		nextCursor = "next-cursor"
	}

	return result, nextCursor, nil
}

func (m *mockInboxStore) UnreadCount(ctx context.Context, userID string) (int, error) {
	m.unreadCountCalls++
	if m.unreadCountErr != nil {
		return 0, m.unreadCountErr
	}
	count := 0
	for _, n := range m.notifications {
		if n.UserID == userID && n.ReadAt == nil && n.ArchivedAt == nil && n.DeletedAt == nil {
			count++
		}
		if count >= models.UnreadCountCap {
			return models.UnreadCountCap, nil
		}
	}
	return count, nil
}

func (m *mockInboxStore) MarkRead(ctx context.Context, userID, notificationID string) (bool, error) {
	for i, n := range m.notifications {
		if n.ID == notificationID && n.UserID == userID && n.ReadAt == nil {
			now := time.Now()
			m.notifications[i].ReadAt = &now
			return true, nil
		}
	}
	return false, nil
}

func (m *mockInboxStore) MarkUnread(ctx context.Context, userID, notificationID string) (bool, error) {
	for i, n := range m.notifications {
		if n.ID == notificationID && n.UserID == userID && n.ReadAt != nil {
			m.notifications[i].ReadAt = nil
			return true, nil
		}
	}
	return false, nil
}

func (m *mockInboxStore) Archive(ctx context.Context, userID, notificationID string) (bool, error) {
	for i, n := range m.notifications {
		if n.ID == notificationID && n.UserID == userID && n.ArchivedAt == nil {
			now := time.Now()
			wasUnread := n.ReadAt == nil
			m.notifications[i].ArchivedAt = &now
			return wasUnread, nil
		}
	}
	return false, nil
}

func (m *mockInboxStore) Unarchive(ctx context.Context, userID, notificationID string) (bool, error) {
	for i, n := range m.notifications {
		if n.ID == notificationID && n.UserID == userID && n.ArchivedAt != nil {
			m.notifications[i].ArchivedAt = nil
			nowUnread := n.ReadAt == nil
			return nowUnread, nil
		}
	}
	return false, nil
}

func (m *mockInboxStore) SoftDelete(ctx context.Context, userID, notificationID string) (bool, error) {
	for i, n := range m.notifications {
		if n.ID == notificationID && n.UserID == userID && n.DeletedAt == nil {
			now := time.Now()
			wasUnread := n.ReadAt == nil && n.ArchivedAt == nil
			m.notifications[i].DeletedAt = &now
			return wasUnread, nil
		}
	}
	return false, nil
}

func (m *mockInboxStore) MarkAllRead(ctx context.Context, userID string) error {
	now := time.Now()
	for i, n := range m.notifications {
		if n.UserID == userID && n.ReadAt == nil && n.DeletedAt == nil && n.ArchivedAt == nil {
			m.notifications[i].ReadAt = &now
		}
	}
	return nil
}

func (m *mockInboxStore) GetCategoryByID(ctx context.Context, id string) (*models.SubscriptionCategory, error) {
	for _, c := range m.categories {
		if c.ID == id {
			return &c, nil
		}
	}
	return nil, fmt.Errorf("category not found: %s", id)
}

const testUserID = "test-user-id"

func newTestServer(t *testing.T) (*inbox.Server, *mockInboxStore) {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	store := &mockInboxStore{
		notifications: []models.Notification{
			{
				ID:             "notif-1",
				OrganizationID: "organization-1",
				UserID:         testUserID,
				CategoryID:     "sct-1",
				Title:          "Test Notification 1",
				Body:           "Body 1",
				Channels:       []string{"inbox"},
				Status:         models.StatusDelivered,
				CreatedAt:      time.Now(),
			},
			{
				ID:             "notif-2",
				OrganizationID: "organization-1",
				UserID:         testUserID,
				CategoryID:     "sct-1",
				Title:          "Test Notification 2",
				Body:           "Body 2",
				Channels:       []string{"inbox"},
				Status:         models.StatusDelivered,
				CreatedAt:      time.Now(),
			},
		},
		categories: []models.SubscriptionCategory{
			{ID: "sct-1", Slug: "alerts", Name: "Alerts", DefaultChannels: []string{"inbox"}, DefaultState: "on"},
		},
	}
	srv := inbox.NewServer(store, nil, nil, nil, logger)
	srv.SetSkipAuth(true)
	return srv, store
}
