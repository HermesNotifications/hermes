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
	groups        []models.NotificationGroup
}

func (m *mockInboxStore) ListInbox(ctx context.Context, userID string, archived bool, cursor string, limit int) ([]models.Notification, int, string, error) {
	var result []models.Notification
	unread := 0
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
		if n.ReadAt == nil {
			unread++
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

	return result, unread, nextCursor, nil
}

func (m *mockInboxStore) MarkRead(ctx context.Context, userID, notificationID string) error {
	for i, n := range m.notifications {
		if n.ID == notificationID && n.UserID == userID {
			now := time.Now()
			m.notifications[i].ReadAt = &now
			return nil
		}
	}
	return fmt.Errorf("notification not found: %s", notificationID)
}

func (m *mockInboxStore) MarkUnread(ctx context.Context, userID, notificationID string) error {
	for i, n := range m.notifications {
		if n.ID == notificationID && n.UserID == userID {
			m.notifications[i].ReadAt = nil
			return nil
		}
	}
	return fmt.Errorf("notification not found: %s", notificationID)
}

func (m *mockInboxStore) Archive(ctx context.Context, userID, notificationID string) error {
	for i, n := range m.notifications {
		if n.ID == notificationID && n.UserID == userID {
			now := time.Now()
			m.notifications[i].ArchivedAt = &now
			return nil
		}
	}
	return fmt.Errorf("notification not found: %s", notificationID)
}

func (m *mockInboxStore) Unarchive(ctx context.Context, userID, notificationID string) error {
	for i, n := range m.notifications {
		if n.ID == notificationID && n.UserID == userID {
			m.notifications[i].ArchivedAt = nil
			return nil
		}
	}
	return fmt.Errorf("notification not found: %s", notificationID)
}

func (m *mockInboxStore) SoftDelete(ctx context.Context, userID, notificationID string) error {
	for i, n := range m.notifications {
		if n.ID == notificationID && n.UserID == userID {
			now := time.Now()
			m.notifications[i].DeletedAt = &now
			return nil
		}
	}
	return fmt.Errorf("notification not found: %s", notificationID)
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

func (m *mockInboxStore) GetGroupByID(ctx context.Context, id string) (*models.NotificationGroup, error) {
	for _, g := range m.groups {
		if g.ID == id {
			return &g, nil
		}
	}
	return nil, fmt.Errorf("group not found: %s", id)
}

const testUserID = "test-user-id"

func newTestServer(t *testing.T) (*inbox.Server, *mockInboxStore) {
	t.Helper()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	store := &mockInboxStore{
		notifications: []models.Notification{
			{
				ID:        "notif-1",
				TenantID:  "tenant-1",
				UserID:    testUserID,
				GroupID:   "group-1",
				Title:     "Test Notification 1",
				Body:      "Body 1",
				Channels:  []string{"inbox"},
				Status:    models.StatusDelivered,
				CreatedAt: time.Now(),
			},
			{
				ID:        "notif-2",
				TenantID:  "tenant-1",
				UserID:    testUserID,
				GroupID:   "group-1",
				Title:     "Test Notification 2",
				Body:      "Body 2",
				Channels:  []string{"inbox"},
				Status:    models.StatusDelivered,
				CreatedAt: time.Now(),
			},
		},
		groups: []models.NotificationGroup{
			{ID: "group-1", Slug: "alerts", Name: "Alerts", DefaultChannels: []string{"inbox"}},
		},
	}
	srv := inbox.NewServer(store, nil, nil, "test-centrifugo-secret", nil, nil, logger)
	srv.SetSkipAuth(true)
	return srv, store
}
