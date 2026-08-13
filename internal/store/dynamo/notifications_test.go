//go:build integration

// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package dynamo_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	id "github.com/hermesnotifications/hermes/internal/id/v2"
	"github.com/hermesnotifications/hermes/internal/models"
	"github.com/hermesnotifications/hermes/internal/store"
	"github.com/hermesnotifications/hermes/internal/store/dynamo"
	"github.com/jackc/pgx/v5"
)

func testNotifStore(t *testing.T) *dynamo.NotificationStore {
	t.Helper()
	client := testClient(t)
	pgSt, _ := testPGStore(t)
	evStore := dynamo.NewEventStore(client, pgSt)
	return dynamo.NewNotificationStore(client, evStore)
}

func newNotif(userID, organizationID string) *models.Notification {
	return &models.Notification{
		ID:             id.Notification.New(),
		OrganizationID: organizationID,
		UserID:         userID,
		Title:          "test notification",
		Body:           "test body",
		Channels:       []string{"inbox"},
		Status:         models.StatusPending,
	}
}

// ── CreateNotification / GetNotificationByID ──────────────────────────────────

func TestCreateAndGetNotification(t *testing.T) {
	st := testNotifStore(t)
	ctx := context.Background()

	userID := uuid.New().String()
	organizationID := uuid.New().String()
	n := newNotif(userID, organizationID)
	n.CategoryID = "cat-123"

	created, err := st.CreateNotification(ctx, n)
	if err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}
	if created.ID != n.ID {
		t.Errorf("ID mismatch: want %s got %s", n.ID, created.ID)
	}

	got, err := st.GetNotificationByID(ctx, n.ID)
	if err != nil {
		t.Fatalf("GetNotificationByID: %v", err)
	}
	if got.UserID != userID {
		t.Errorf("UserID: want %s got %s", userID, got.UserID)
	}
	if got.OrganizationID != organizationID {
		t.Errorf("OrganizationID: want %s got %s", organizationID, got.OrganizationID)
	}
	if got.CategoryID != "cat-123" {
		t.Errorf("CategoryID: want cat-123 got %s", got.CategoryID)
	}
	if got.Status != models.StatusPending {
		t.Errorf("Status: want pending got %s", got.Status)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
}

// DynamoDB is an equal store, not an afterthought, so metadata must round-trip identically to
// the Postgres path. It is stored as a JSON string rather than a native M attribute precisely so
// that both go through one encoding/json path: a native map would bring numbers back through
// DynamoDB's N type, which carries them as decimal strings, and the two stores would disagree
// about the Go type of the same value.
func TestCreateNotification_MetadataRoundTrip(t *testing.T) {
	st := testNotifStore(t)
	ctx := context.Background()

	n := newNotif(uuid.New().String(), uuid.New().String())
	n.Metadata = models.NotificationMetadata{
		"level":     "warning",
		"toast":     true,
		"invoiceId": "1041",
		"nested":    map[string]any{"tab": "billing"},
	}
	if _, err := st.CreateNotification(ctx, n); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	got, err := st.GetNotificationByID(ctx, n.ID)
	if err != nil {
		t.Fatalf("GetNotificationByID: %v", err)
	}
	if level, ok := got.Metadata.Level(); !ok || level != "warning" {
		t.Errorf("level = (%q, %v), want (\"warning\", true)", level, ok)
	}
	if !got.Metadata.Toast() {
		t.Error("toast did not survive the round trip")
	}
	if got.Metadata["invoiceId"] != "1041" {
		t.Errorf("opaque key = %#v", got.Metadata["invoiceId"])
	}
	nested, ok := got.Metadata["nested"].(map[string]any)
	if !ok || nested["tab"] != "billing" {
		t.Errorf("nested object = %#v", got.Metadata["nested"])
	}
}

func TestCreateNotification_NoMetadataStaysAbsent(t *testing.T) {
	st := testNotifStore(t)
	ctx := context.Background()

	n := newNotif(uuid.New().String(), uuid.New().String())
	if _, err := st.CreateNotification(ctx, n); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	got, err := st.GetNotificationByID(ctx, n.ID)
	if err != nil {
		t.Fatalf("GetNotificationByID: %v", err)
	}
	if got.Metadata != nil {
		t.Errorf("metadata = %#v, want nil", got.Metadata)
	}
}

func TestGetNotificationByID_Miss(t *testing.T) {
	st := testNotifStore(t)
	_, err := st.GetNotificationByID(context.Background(), uuid.New().String())
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("expected pgx.ErrNoRows, got %v", err)
	}
}

func TestCreateNotification_DuplicateFails(t *testing.T) {
	st := testNotifStore(t)
	ctx := context.Background()

	n := newNotif(uuid.New().String(), uuid.New().String())
	if _, err := st.CreateNotification(ctx, n); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := st.CreateNotification(ctx, n)
	if err == nil {
		t.Fatal("expected error on duplicate create, got nil")
	}
}

// The batch path exists for Postgres' sake — DynamoDB has no commit flush to amortise — but it
// must present the same contract on both stores, because dispatch calls it without knowing which
// one it has: every new row written, every row already there reported as not inserted rather than
// failing its neighbours.
func TestCreateNotifications_WritesNewRowsAndSkipsExisting(t *testing.T) {
	st := testNotifStore(t)
	ctx := context.Background()

	organizationID := uuid.New().String()
	already := newNotif(uuid.New().String(), organizationID)
	if _, err := st.CreateNotification(ctx, already); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	fresh := newNotif(uuid.New().String(), organizationID)
	other := newNotif(uuid.New().String(), organizationID)
	inserted, err := st.CreateNotifications(ctx, []*models.Notification{fresh, already, other})
	if err != nil {
		t.Fatalf("CreateNotifications: %v", err)
	}
	if len(inserted) != 2 {
		t.Fatalf("inserted = %v, want the two new IDs only", inserted)
	}
	for _, n := range []*models.Notification{fresh, other} {
		if _, err := st.GetNotificationByID(ctx, n.ID); err != nil {
			t.Errorf("row %s was not written: %v", n.ID, err)
		}
	}
}

// ── Idempotency ───────────────────────────────────────────────────────────────

func TestGetNotificationByIdempotencyKey(t *testing.T) {
	st := testNotifStore(t)
	ctx := context.Background()

	organizationID := uuid.New().String()
	idemKey := "idem-" + uuid.New().String()
	n := newNotif(uuid.New().String(), organizationID)
	n.IdempotencyKey = &idemKey

	if _, err := st.CreateNotification(ctx, n); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	got, err := st.GetNotificationByIdempotencyKey(ctx, organizationID, idemKey)
	if err != nil {
		t.Fatalf("GetNotificationByIdempotencyKey: %v", err)
	}
	if got.ID != n.ID {
		t.Errorf("ID: want %s got %s", n.ID, got.ID)
	}
}

func TestGetNotificationByIdempotencyKey_Miss(t *testing.T) {
	st := testNotifStore(t)
	_, err := st.GetNotificationByIdempotencyKey(context.Background(), uuid.New().String(), "no-such-key")
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("expected pgx.ErrNoRows, got %v", err)
	}
}

// ── UpdateNotificationChannels / UpdateNotificationRouting / FailNotification ─

func TestUpdateNotificationChannels(t *testing.T) {
	st := testNotifStore(t)
	ctx := context.Background()

	n := newNotif(uuid.New().String(), uuid.New().String())
	if _, err := st.CreateNotification(ctx, n); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	if err := st.UpdateNotificationChannels(ctx, n.ID, []string{"email", "inbox"}); err != nil {
		t.Fatalf("UpdateNotificationChannels: %v", err)
	}

	got, _ := st.GetNotificationByID(ctx, n.ID)
	if len(got.Channels) != 2 {
		t.Errorf("channels: want 2, got %d", len(got.Channels))
	}
}

func TestUpdateNotificationRouting(t *testing.T) {
	st := testNotifStore(t)
	ctx := context.Background()

	n := newNotif(uuid.New().String(), uuid.New().String())
	if _, err := st.CreateNotification(ctx, n); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	templateID := "tmpl-abc"
	if err := st.UpdateNotificationRouting(ctx, &models.Notification{
		ID:         n.ID,
		TemplateID: &templateID,
		CategoryID: "cat-xyz",
		Title:      "resolved title",
		Body:       "resolved body",
	}); err != nil {
		t.Fatalf("UpdateNotificationRouting: %v", err)
	}

	got, _ := st.GetNotificationByID(ctx, n.ID)
	if got.TemplateID == nil || *got.TemplateID != templateID {
		t.Errorf("TemplateID: want %s got %v", templateID, got.TemplateID)
	}
	if got.CategoryID != "cat-xyz" {
		t.Errorf("CategoryID: want cat-xyz got %s", got.CategoryID)
	}
	if got.Title != "resolved title" {
		t.Errorf("Title: want 'resolved title' got %s", got.Title)
	}
}

func TestFailNotification(t *testing.T) {
	st := testNotifStore(t)
	ctx := context.Background()

	n := newNotif(uuid.New().String(), uuid.New().String())
	if _, err := st.CreateNotification(ctx, n); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	if err := st.FailNotification(ctx, n.ID); err != nil {
		t.Fatalf("FailNotification: %v", err)
	}
	got, _ := st.GetNotificationByID(ctx, n.ID)
	if got.Status != models.StatusFailed {
		t.Errorf("Status: want failed got %s", got.Status)
	}

	// Calling again on already-failed notification must not error.
	if err := st.FailNotification(ctx, n.ID); err != nil {
		t.Errorf("FailNotification (idempotent): %v", err)
	}
}

// ── Status rollup ─────────────────────────────────────────────────────────────

func TestUpdateNotificationStatus_Monotonic(t *testing.T) {
	st := testNotifStore(t)
	ctx := context.Background()

	n := newNotif(uuid.New().String(), uuid.New().String())
	if _, err := st.CreateNotification(ctx, n); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	now := time.Now().UTC()

	// pending → sent
	if err := st.UpdateNotificationStatus(ctx, n.ID, models.StatusSent, now); err != nil {
		t.Fatalf("UpdateNotificationStatus(sent): %v", err)
	}
	got, _ := st.GetNotificationByID(ctx, n.ID)
	if got.Status != models.StatusSent {
		t.Errorf("want sent, got %s", got.Status)
	}
	if got.SentAt == nil {
		t.Error("sent_at not set")
	}

	// sent → delivered
	if err := st.UpdateNotificationStatus(ctx, n.ID, models.StatusDelivered, now.Add(time.Second)); err != nil {
		t.Fatalf("UpdateNotificationStatus(delivered): %v", err)
	}
	got, _ = st.GetNotificationByID(ctx, n.ID)
	if got.Status != models.StatusDelivered {
		t.Errorf("want delivered, got %s", got.Status)
	}

	// stale: attempt regression delivered → sent (must be silently ignored)
	if err := st.UpdateNotificationStatus(ctx, n.ID, models.StatusSent, now); err != nil {
		t.Fatalf("regression UpdateNotificationStatus: %v", err)
	}
	got, _ = st.GetNotificationByID(ctx, n.ID)
	if got.Status != models.StatusDelivered {
		t.Errorf("status regression: want delivered, got %s", got.Status)
	}
}

func TestBatchUpdateNotificationStatuses(t *testing.T) {
	st := testNotifStore(t)
	ctx := context.Background()

	id1, id2 := id.Notification.New(), id.Notification.New()
	for _, nid := range []string{id1, id2} {
		if _, err := st.CreateNotification(ctx, newNotif(uuid.New().String(), uuid.New().String())); err != nil {
			_ = nid
		}
	}
	n1 := newNotif(uuid.New().String(), uuid.New().String())
	n1.ID = id1
	n2 := newNotif(uuid.New().String(), uuid.New().String())
	n2.ID = id2
	for _, n := range []*models.Notification{n1, n2} {
		if _, err := st.CreateNotification(ctx, n); err != nil {
			t.Fatalf("CreateNotification %s: %v", n.ID, err)
		}
	}

	now := time.Now().UTC()
	updates := []store.StatusUpdate{
		{NotificationID: id1, NewStatus: models.StatusDelivered, EventTime: now},
		{NotificationID: id1, NewStatus: models.StatusSent, EventTime: now}, // lower rank — ignored
		{NotificationID: id2, NewStatus: models.StatusSent, EventTime: now},
	}
	if err := st.BatchUpdateNotificationStatuses(ctx, updates); err != nil {
		t.Fatalf("BatchUpdateNotificationStatuses: %v", err)
	}

	got1, _ := st.GetNotificationByID(ctx, id1)
	if got1.Status != models.StatusDelivered {
		t.Errorf("id1: want delivered, got %s", got1.Status)
	}
	got2, _ := st.GetNotificationByID(ctx, id2)
	if got2.Status != models.StatusSent {
		t.Errorf("id2: want sent, got %s", got2.Status)
	}
}

// ── InboxRepository ───────────────────────────────────────────────────────────

func TestListInbox_BasicAndPagination(t *testing.T) {
	st := testNotifStore(t)
	ctx := context.Background()

	userID := uuid.New().String()
	organizationID := uuid.New().String()
	base := time.Now().UTC()

	const total = 5
	ids := make([]string, total)
	for i := range ids {
		n := newNotif(userID, organizationID)
		n.CreatedAt = base.Add(time.Duration(i) * time.Millisecond)
		if _, err := st.CreateNotification(ctx, n); err != nil {
			t.Fatalf("CreateNotification[%d]: %v", i, err)
		}
		ids[i] = n.ID
	}

	// Advance all to delivered so they show in inbox
	for _, nid := range ids {
		_ = st.UpdateNotificationStatus(ctx, nid, models.StatusDelivered, base)
	}

	// Page 1: limit=3
	page1, cursor1, err := st.ListInbox(ctx, userID, false, "", 3)
	if err != nil {
		t.Fatalf("ListInbox page1: %v", err)
	}
	if len(page1) != 3 {
		t.Fatalf("page1 len: want 3, got %d", len(page1))
	}
	if cursor1 == "" {
		t.Error("expected non-empty cursor after page 1")
	}

	// Page 2: should return remaining 2
	page2, cursor2, err := st.ListInbox(ctx, userID, false, cursor1, 3)
	if err != nil {
		t.Fatalf("ListInbox page2: %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("page2 len: want 2, got %d", len(page2))
	}
	if cursor2 != "" {
		t.Errorf("expected empty cursor on last page, got %q", cursor2)
	}

	// No overlap between pages
	seen := map[string]bool{}
	for _, n := range append(page1, page2...) {
		if seen[n.ID] {
			t.Errorf("duplicate notification ID %s across pages", n.ID)
		}
		seen[n.ID] = true
	}
	if len(seen) != total {
		t.Errorf("want %d unique notifications, got %d", total, len(seen))
	}
}

func TestListInbox_ArchivedSeparate(t *testing.T) {
	st := testNotifStore(t)
	ctx := context.Background()

	userID := uuid.New().String()
	organizationID := uuid.New().String()

	active := newNotif(userID, organizationID)
	archived := newNotif(userID, organizationID)
	for _, n := range []*models.Notification{active, archived} {
		if _, err := st.CreateNotification(ctx, n); err != nil {
			t.Fatalf("CreateNotification: %v", err)
		}
	}

	if _, err := st.Archive(ctx, userID, archived.ID); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	// Active inbox should only contain active
	activeList, _, err := st.ListInbox(ctx, userID, false, "", 20)
	if err != nil {
		t.Fatalf("ListInbox active: %v", err)
	}
	for _, n := range activeList {
		if n.ID == archived.ID {
			t.Errorf("archived notification appears in active inbox")
		}
	}

	// Archived inbox should only contain archived
	archivedList, _, err := st.ListInbox(ctx, userID, true, "", 20)
	if err != nil {
		t.Fatalf("ListInbox archived: %v", err)
	}
	found := false
	for _, n := range archivedList {
		if n.ID == archived.ID {
			found = true
		}
		if n.ID == active.ID {
			t.Errorf("active notification appears in archived inbox")
		}
	}
	if !found {
		t.Error("archived notification not found in archived inbox")
	}
}

func TestUnreadCount(t *testing.T) {
	st := testNotifStore(t)
	ctx := context.Background()

	userID := uuid.New().String()
	organizationID := uuid.New().String()

	// 3 delivered (unread), 1 read
	for i := 0; i < 3; i++ {
		n := newNotif(userID, organizationID)
		if _, err := st.CreateNotification(ctx, n); err != nil {
			t.Fatalf("CreateNotification: %v", err)
		}
		_ = st.UpdateNotificationStatus(ctx, n.ID, models.StatusDelivered, time.Now())
	}
	readN := newNotif(userID, organizationID)
	if _, err := st.CreateNotification(ctx, readN); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}
	_ = st.UpdateNotificationStatus(ctx, readN.ID, models.StatusDelivered, time.Now())
	if _, err := st.MarkRead(ctx, userID, readN.ID); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	count, _, err := st.UnreadCount(ctx, userID)
	if err != nil {
		t.Fatalf("UnreadCount: %v", err)
	}
	if count != 3 {
		t.Errorf("UnreadCount: want 3, got %d", count)
	}
}

func TestMarkRead_AdvancesStatus(t *testing.T) {
	st := testNotifStore(t)
	ctx := context.Background()

	userID := uuid.New().String()
	n := newNotif(userID, uuid.New().String())
	if _, err := st.CreateNotification(ctx, n); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}
	_ = st.UpdateNotificationStatus(ctx, n.ID, models.StatusDelivered, time.Now())

	changed, err := st.MarkRead(ctx, userID, n.ID)
	if err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	if !changed {
		t.Error("expected changed=true")
	}

	got, _ := st.GetNotificationByID(ctx, n.ID)
	if got.Status != models.StatusRead {
		t.Errorf("status: want read, got %s", got.Status)
	}
	if got.ReadAt == nil {
		t.Error("read_at not set")
	}

	// Second call should return changed=false
	changed, _ = st.MarkRead(ctx, userID, n.ID)
	if changed {
		t.Error("expected changed=false on second mark-read")
	}
}

func TestMarkUnread(t *testing.T) {
	st := testNotifStore(t)
	ctx := context.Background()

	userID := uuid.New().String()
	n := newNotif(userID, uuid.New().String())
	if _, err := st.CreateNotification(ctx, n); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}
	_ = st.UpdateNotificationStatus(ctx, n.ID, models.StatusDelivered, time.Now())
	st.MarkRead(ctx, userID, n.ID)

	changed, err := st.MarkUnread(ctx, userID, n.ID)
	if err != nil {
		t.Fatalf("MarkUnread: %v", err)
	}
	if !changed {
		t.Error("expected changed=true")
	}

	got, _ := st.GetNotificationByID(ctx, n.ID)
	if got.ReadAt != nil {
		t.Error("read_at should be nil after mark-unread")
	}
	if got.Status != models.StatusDelivered {
		t.Errorf("status: want delivered, got %s", got.Status)
	}

	// Already unread — should return changed=false
	changed, _ = st.MarkUnread(ctx, userID, n.ID)
	if changed {
		t.Error("expected changed=false when already unread")
	}
}

func TestArchiveAndUnarchive(t *testing.T) {
	st := testNotifStore(t)
	ctx := context.Background()

	userID := uuid.New().String()
	n := newNotif(userID, uuid.New().String())
	if _, err := st.CreateNotification(ctx, n); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	wasUnread, err := st.Archive(ctx, userID, n.ID)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if !wasUnread {
		t.Error("expected wasUnread=true (notification was unread when archived)")
	}
	got, _ := st.GetNotificationByID(ctx, n.ID)
	if got.ArchivedAt == nil {
		t.Error("archived_at not set")
	}
	if got.Status != models.StatusArchived {
		t.Errorf("status: want archived, got %s", got.Status)
	}

	// Archive again should be idempotent
	wasUnread2, _ := st.Archive(ctx, userID, n.ID)
	if wasUnread2 {
		t.Error("second Archive: expected wasUnread=false")
	}

	// Unarchive
	nowUnread, err := st.Unarchive(ctx, userID, n.ID)
	if err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	if !nowUnread {
		t.Error("expected nowUnread=true after unarchive")
	}
	got, _ = st.GetNotificationByID(ctx, n.ID)
	if got.ArchivedAt != nil {
		t.Error("archived_at should be nil after unarchive")
	}
}

func TestSoftDelete(t *testing.T) {
	st := testNotifStore(t)
	ctx := context.Background()

	userID := uuid.New().String()
	n := newNotif(userID, uuid.New().String())
	if _, err := st.CreateNotification(ctx, n); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}
	_ = st.UpdateNotificationStatus(ctx, n.ID, models.StatusDelivered, time.Now())

	wasUnread, err := st.SoftDelete(ctx, userID, n.ID)
	if err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	if !wasUnread {
		t.Error("expected wasUnread=true")
	}

	got, _ := st.GetNotificationByID(ctx, n.ID)
	if got.DeletedAt == nil {
		t.Error("deleted_at not set after soft delete")
	}

	// Deleted notification should not appear in inbox
	inbox, _, _ := st.ListInbox(ctx, userID, false, "", 20)
	for _, item := range inbox {
		if item.ID == n.ID {
			t.Error("soft-deleted notification appears in inbox")
		}
	}
}

func TestMarkAllRead(t *testing.T) {
	st := testNotifStore(t)
	ctx := context.Background()

	userID := uuid.New().String()
	organizationID := uuid.New().String()

	var notifIDs []string
	for i := 0; i < 4; i++ {
		n := newNotif(userID, organizationID)
		if _, err := st.CreateNotification(ctx, n); err != nil {
			t.Fatalf("CreateNotification: %v", err)
		}
		_ = st.UpdateNotificationStatus(ctx, n.ID, models.StatusDelivered, time.Now())
		notifIDs = append(notifIDs, n.ID)
	}

	// Archive one — should not be marked read by MarkAllRead
	if _, err := st.Archive(ctx, userID, notifIDs[3]); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	if err := st.MarkAllRead(ctx, userID); err != nil {
		t.Fatalf("MarkAllRead: %v", err)
	}

	// First 3 should now be read
	for _, nid := range notifIDs[:3] {
		got, _ := st.GetNotificationByID(ctx, nid)
		if got.ReadAt == nil {
			t.Errorf("notification %s: expected read_at set after MarkAllRead", nid)
		}
	}

	// Archived one should remain archived
	got, _ := st.GetNotificationByID(ctx, notifIDs[3])
	if got.ArchivedAt == nil {
		t.Error("archived notification should remain archived after MarkAllRead")
	}

	// Unread count should now be zero
	count, _, _ := st.UnreadCount(ctx, userID)
	if count != 0 {
		t.Errorf("UnreadCount after MarkAllRead: want 0, got %d", count)
	}
}

// TestUnarchive_RestoresReadStatus verifies that a notification which was read before
// being archived returns to status=read (not status=delivered) after Unarchive.
// This is the regression path that was broken by `if_not_exists(#s, :delivered)`.
func TestUnarchive_RestoresReadStatus(t *testing.T) {
	st := testNotifStore(t)
	ctx := context.Background()

	userID := uuid.New().String()
	n := newNotif(userID, uuid.New().String())
	if _, err := st.CreateNotification(ctx, n); err != nil {
		t.Fatalf("CreateNotification: %v", err)
	}

	// Advance to delivered, then mark read.
	if err := st.UpdateNotificationStatus(ctx, n.ID, models.StatusDelivered, time.Now()); err != nil {
		t.Fatalf("UpdateNotificationStatus: %v", err)
	}
	wasUnread, err := st.MarkRead(ctx, userID, n.ID)
	if err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	if !wasUnread {
		t.Fatal("expected wasUnread=true before MarkRead")
	}

	// Archive the already-read notification.
	wasUnread2, err := st.Archive(ctx, userID, n.ID)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if wasUnread2 {
		t.Error("expected wasUnread=false (notification was already read when archived)")
	}

	// Unarchive: should restore status=read, not status=delivered.
	nowUnread, err := st.Unarchive(ctx, userID, n.ID)
	if err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	if nowUnread {
		t.Error("expected nowUnread=false (notification was read before archiving)")
	}

	got, err := st.GetNotificationByID(ctx, n.ID)
	if err != nil {
		t.Fatalf("GetNotificationByID after Unarchive: %v", err)
	}
	if got.Status != models.StatusRead {
		t.Errorf("status after Unarchive: want read, got %s (regression: status was not restored)", got.Status)
	}
	if got.ArchivedAt != nil {
		t.Error("archived_at should be nil after Unarchive")
	}
	if got.ReadAt == nil {
		t.Error("read_at should still be set after Unarchive")
	}
}

// TestMarkAllRead_LargeInbox exercises the parallel worker-pool path (>10 items per page).
func TestMarkAllRead_LargeInbox(t *testing.T) {
	st := testNotifStore(t)
	ctx := context.Background()

	userID := uuid.New().String()
	organizationID := uuid.New().String()

	const total = 30
	for i := 0; i < total; i++ {
		n := newNotif(userID, organizationID)
		if _, err := st.CreateNotification(ctx, n); err != nil {
			t.Fatalf("CreateNotification %d: %v", i, err)
		}
		if err := st.UpdateNotificationStatus(ctx, n.ID, models.StatusDelivered, time.Now()); err != nil {
			t.Fatalf("UpdateNotificationStatus %d: %v", i, err)
		}
	}

	if err := st.MarkAllRead(ctx, userID); err != nil {
		t.Fatalf("MarkAllRead: %v", err)
	}

	count, _, err := st.UnreadCount(ctx, userID)
	if err != nil {
		t.Fatalf("UnreadCount: %v", err)
	}
	if count != 0 {
		t.Errorf("UnreadCount after MarkAllRead on %d items: want 0, got %d", total, count)
	}
}
