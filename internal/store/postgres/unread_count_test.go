// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	id "github.com/hermesnotifications/hermes/internal/id/v2"
	"github.com/hermesnotifications/hermes/internal/models"
	"github.com/hermesnotifications/hermes/internal/store/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// seedUnread creates n unread notifications for a fresh user and returns that user's ID.
func seedUnread(t *testing.T, s *postgres.Store, pool *pgxpool.Pool, n int) string {
	t.Helper()
	ctx := context.Background()

	organizationID := uuid.New().String()
	if _, err := s.CreateOrganization(ctx, organizationID, "Unread Count Test Org"); err != nil {
		t.Fatalf("CreateOrganization: %v", err)
	}
	user, err := s.EnsureUser(ctx, organizationID, "ext-unread-"+uuid.New().String())
	if err != nil {
		t.Fatalf("EnsureUser: %v", err)
	}
	cat, err := s.CreateCategory(ctx, "unread-cat-"+uuid.New().String(), "Unread Cat", []string{"inbox"}, "on", 0)
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}

	// COPY rather than n round trips: the cap test seeds over a thousand rows, and doing that
	// one INSERT at a time turns a two-second test into a thirty-second one.
	rows := make([][]any, 0, n)
	for i := 0; i < n; i++ {
		rows = append(rows, []any{
			id.Notification.New(), organizationID, user.ID, cat.ID,
			fmt.Sprintf("Notification %d", i), "Body", []string{"inbox"},
			string(models.StatusDelivered),
		})
	}
	_, err = pool.CopyFrom(ctx,
		pgx.Identifier{"notifications"},
		[]string{"id", "organization_id", "user_id", "category_id", "title", "body", "channels", "status"},
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		t.Fatalf("seed notifications: %v", err)
	}
	return user.ID
}

// The cap is what keeps a badge read from scaling with a user's lifetime history.
func TestUnreadCount_SaturatesAtCap(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notification_events", "notifications", "users", "notification_templates", "subscription_categories", "organizations")

	userID := seedUnread(t, s, pool, models.UnreadCountCap+25)

	got, _, err := s.UnreadCount(context.Background(), userID)
	if err != nil {
		t.Fatalf("UnreadCount: %v", err)
	}
	if got != models.UnreadCountCap {
		t.Fatalf("UnreadCount = %d, want it saturated at %d", got, models.UnreadCountCap)
	}
}

// Below the cap the number must still be exact -- the cap is a ceiling, not an approximation.
func TestUnreadCount_ExactBelowCap(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notification_events", "notifications", "users", "notification_templates", "subscription_categories", "organizations")

	userID := seedUnread(t, s, pool, 7)

	got, _, err := s.UnreadCount(context.Background(), userID)
	if err != nil {
		t.Fatalf("UnreadCount: %v", err)
	}
	if got != 7 {
		t.Fatalf("UnreadCount = %d, want exactly 7", got)
	}
}

// This is the only place idx_notifications_unread (migration 000018) is load-bearing, so it is
// the only place worth asserting the planner actually reaches for it. Without the index this
// query still returns the right answer -- it just scans the user's whole active history to do
// it, which is precisely the cost the index exists to remove and precisely the kind of
// regression a correctness test would sail past.
func TestUnreadCount_UsesPartialIndex(t *testing.T) {
	s, pool := testStore(t)
	cleanTable(t, pool, "notification_events", "notifications", "users", "notification_templates", "subscription_categories", "organizations")

	ctx := context.Background()

	// Production shape, which is the only shape where this assertion means anything: one user's
	// unread rows sitting inside a table full of everyone else's. On a table of fifty rows a
	// sequential scan is genuinely cheaper and the planner is right to choose it, so a test
	// seeded that small would either fail against correct behaviour or have to be rigged with
	// enable_seqscan=off -- which proves only that the index *can* be used, not that it wins.
	for i := 0; i < 20; i++ {
		seedUnread(t, s, pool, 500)
	}
	userID := seedUnread(t, s, pool, 40)

	// Most notifications end up read, and that is the entire reason idx_notifications_unread is
	// worth having: it is sparse, so it shrinks as users read while the table grows.
	//
	// Seeding every row unread -- which this test used to do -- erases that difference and makes
	// the assertion depend on a coin toss. Once migration 000021 added the non-partial
	// (user_id, id DESC) index for the watermark, both indexes offered the same selectivity on an
	// all-unread table, the planner picked the new one, and the test failed against behaviour that
	// was not actually wrong. Leaving 10% unread restores the asymmetry the index exists to
	// exploit, so the assertion now tests something true of production rather than of the fixture.
	// hashtext rather than random(): deterministic per row, so the fixture is identical on every
	// run. random() would leave the read/unread ratio -- and therefore the plan -- very slightly
	// different each time, which is how a planner assertion becomes a flaky test. It also cannot
	// be pinned with setseed here, because pgxpool may run the two statements on different
	// connections.
	if _, err := pool.Exec(ctx,
		`UPDATE notifications SET read_at = now() WHERE user_id <> $1 AND hashtext(id) % 10 <> 0`,
		userID); err != nil {
		t.Fatalf("mark read: %v", err)
	}

	// The planner has no statistics for a freshly seeded table and would guess from defaults.
	if _, err := pool.Exec(ctx, "ANALYZE notifications"); err != nil {
		t.Fatalf("ANALYZE: %v", err)
	}

	rows, err := pool.Query(ctx, `
		EXPLAIN SELECT count(*) FROM (
			SELECT 1 FROM notifications
			WHERE user_id = $1
			  AND read_at IS NULL AND archived_at IS NULL AND deleted_at IS NULL
			LIMIT $2
		) capped`, userID, models.UnreadCountCap)
	if err != nil {
		t.Fatalf("EXPLAIN: %v", err)
	}
	defer rows.Close()

	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan.WriteString(line)
		plan.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("plan rows: %v", err)
	}

	if !strings.Contains(plan.String(), "idx_notifications_unread") {
		t.Fatalf("the unread count is not using idx_notifications_unread; plan was:\n%s", plan.String())
	}
}
