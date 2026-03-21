//go:build integration

package store_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hermes-notifications/hermes/internal/id"
	"github.com/hermes-notifications/hermes/internal/models"
)

func TestInbox(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notification_events", "notifications", "users", "notification_types", "notification_groups", "tenants")

	ctx := context.Background()

	// Setup: tenant, user, group
	tenantID := uuid.New().String()
	_, err := s.CreateTenant(ctx, tenantID, "Inbox Test Tenant")
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	user, err := s.EnsureUser(ctx, tenantID, "ext-inbox-1")
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}

	group, err := s.CreateGroup(ctx, "inbox-test-group", "Inbox Test Group", []string{"inbox"})
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}

	// Create 5 notifications with slight time gaps to ensure ordering
	notifIDs := make([]string, 5)
	for i := 0; i < 5; i++ {
		notifIDs[i] = id.New()
		n := &models.Notification{
			ID:       notifIDs[i],
			TenantID: tenantID,
			UserID:   user.ID,
			GroupID:  group.ID,
			Title:    fmt.Sprintf("Notification %d", i+1),
			Body:     fmt.Sprintf("Body %d", i+1),
			Channels: []string{"inbox"},
			Status:   models.StatusDelivered,
		}
		_, err := s.CreateNotification(ctx, n)
		if err != nil {
			t.Fatalf("CreateNotification[%d]: %v", i, err)
		}
		// Small sleep to ensure distinct created_at timestamps
		time.Sleep(10 * time.Millisecond)
	}

	// 1. ListInbox with pagination
	t.Run("ListInbox_Pagination", func(t *testing.T) {
		// Page 1: limit=2
		notifs, unreadCount, cursor, err := s.ListInbox(ctx, user.ID, false, "", 2)
		if err != nil {
			t.Fatalf("ListInbox page 1: %v", err)
		}
		if len(notifs) != 2 {
			t.Fatalf("page 1: expected 2, got %d", len(notifs))
		}
		if unreadCount != 5 {
			t.Fatalf("expected unread_count=5, got %d", unreadCount)
		}
		if cursor == "" {
			t.Fatal("expected non-empty cursor for page 1")
		}

		// Page 2: limit=2
		notifs2, _, cursor2, err := s.ListInbox(ctx, user.ID, false, cursor, 2)
		if err != nil {
			t.Fatalf("ListInbox page 2: %v", err)
		}
		if len(notifs2) != 2 {
			t.Fatalf("page 2: expected 2, got %d", len(notifs2))
		}
		if cursor2 == "" {
			t.Fatal("expected non-empty cursor for page 2")
		}

		// Page 3: should return 1
		notifs3, _, cursor3, err := s.ListInbox(ctx, user.ID, false, cursor2, 2)
		if err != nil {
			t.Fatalf("ListInbox page 3: %v", err)
		}
		if len(notifs3) != 1 {
			t.Fatalf("page 3: expected 1, got %d", len(notifs3))
		}
		if cursor3 != "" {
			t.Fatalf("expected empty cursor for last page, got %q", cursor3)
		}

		// Verify no duplicates across pages
		seen := map[string]bool{}
		for _, n := range notifs {
			seen[n.ID] = true
		}
		for _, n := range notifs2 {
			if seen[n.ID] {
				t.Fatalf("duplicate notification %s across pages", n.ID)
			}
			seen[n.ID] = true
		}
		for _, n := range notifs3 {
			if seen[n.ID] {
				t.Fatalf("duplicate notification %s across pages", n.ID)
			}
		}
	})

	// 2. MarkRead
	t.Run("MarkRead", func(t *testing.T) {
		err := s.MarkRead(ctx, user.ID, notifIDs[0])
		if err != nil {
			t.Fatalf("MarkRead: %v", err)
		}

		n, err := s.GetNotificationByID(ctx, notifIDs[0])
		if err != nil {
			t.Fatalf("GetNotificationByID: %v", err)
		}
		if n.ReadAt == nil {
			t.Fatal("expected read_at to be set")
		}

		_, unreadCount, _, err := s.ListInbox(ctx, user.ID, false, "", 20)
		if err != nil {
			t.Fatalf("ListInbox: %v", err)
		}
		if unreadCount != 4 {
			t.Fatalf("expected unread_count=4, got %d", unreadCount)
		}
	})

	// 3. MarkUnread
	t.Run("MarkUnread", func(t *testing.T) {
		err := s.MarkUnread(ctx, user.ID, notifIDs[0])
		if err != nil {
			t.Fatalf("MarkUnread: %v", err)
		}

		n, err := s.GetNotificationByID(ctx, notifIDs[0])
		if err != nil {
			t.Fatalf("GetNotificationByID: %v", err)
		}
		if n.ReadAt != nil {
			t.Fatal("expected read_at to be nil")
		}

		_, unreadCount, _, err := s.ListInbox(ctx, user.ID, false, "", 20)
		if err != nil {
			t.Fatalf("ListInbox: %v", err)
		}
		if unreadCount != 5 {
			t.Fatalf("expected unread_count=5, got %d", unreadCount)
		}
	})

	// 4. Archive
	t.Run("Archive", func(t *testing.T) {
		err := s.Archive(ctx, user.ID, notifIDs[0])
		if err != nil {
			t.Fatalf("Archive: %v", err)
		}

		// Default inbox should return 4
		notifs, _, _, err := s.ListInbox(ctx, user.ID, false, "", 20)
		if err != nil {
			t.Fatalf("ListInbox default: %v", err)
		}
		if len(notifs) != 4 {
			t.Fatalf("expected 4 in default inbox, got %d", len(notifs))
		}

		// Archived inbox should return 1
		archived, _, _, err := s.ListInbox(ctx, user.ID, true, "", 20)
		if err != nil {
			t.Fatalf("ListInbox archived: %v", err)
		}
		if len(archived) != 1 {
			t.Fatalf("expected 1 in archived inbox, got %d", len(archived))
		}
	})

	// 5. Unarchive
	t.Run("Unarchive", func(t *testing.T) {
		err := s.Unarchive(ctx, user.ID, notifIDs[0])
		if err != nil {
			t.Fatalf("Unarchive: %v", err)
		}

		notifs, _, _, err := s.ListInbox(ctx, user.ID, false, "", 20)
		if err != nil {
			t.Fatalf("ListInbox: %v", err)
		}
		if len(notifs) != 5 {
			t.Fatalf("expected 5 after unarchive, got %d", len(notifs))
		}
	})

	// 6. SoftDelete
	t.Run("SoftDelete", func(t *testing.T) {
		err := s.SoftDelete(ctx, user.ID, notifIDs[0])
		if err != nil {
			t.Fatalf("SoftDelete: %v", err)
		}

		notifs, _, _, err := s.ListInbox(ctx, user.ID, false, "", 20)
		if err != nil {
			t.Fatalf("ListInbox: %v", err)
		}
		if len(notifs) != 4 {
			t.Fatalf("expected 4 after soft delete, got %d", len(notifs))
		}
	})

	// 7. MarkAllRead
	t.Run("MarkAllRead", func(t *testing.T) {
		err := s.MarkAllRead(ctx, user.ID)
		if err != nil {
			t.Fatalf("MarkAllRead: %v", err)
		}

		_, unreadCount, _, err := s.ListInbox(ctx, user.ID, false, "", 20)
		if err != nil {
			t.Fatalf("ListInbox: %v", err)
		}
		if unreadCount != 0 {
			t.Fatalf("expected unread_count=0 after MarkAllRead, got %d", unreadCount)
		}
	})
}
