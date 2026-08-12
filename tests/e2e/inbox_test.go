// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

//go:build integration

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/hermesnotifications/hermes/internal/auth"
	"github.com/hermesnotifications/hermes/internal/database"
	"github.com/hermesnotifications/hermes/internal/inbox"
	"github.com/hermesnotifications/hermes/internal/models"
	"github.com/hermesnotifications/hermes/internal/store/postgres"
)

const testJWTSecret = "test-inbox-jwt-secret-for-integration"

func makeJWT(t *testing.T, userID, organizationID string) string {
	t.Helper()
	claims := &auth.HermesClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
		OrganizationID: organizationID,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	return signed
}

func TestInbox_ReadPath(t *testing.T) {
	ctx := context.Background()
	dbURL := envOr("HERMES_DATABASE_URL", "postgres://hermes:hermes@localhost:5432/hermes?sslmode=disable")
	runID := uuid.New().String()[:8]
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// ── Infrastructure ──────────────────────────────────────────────────
	if err := database.RunMigrations(dbURL, "../../migrations"); err != nil {
		t.Fatalf("migrations: %v", err)
	}

	pool, err := database.NewPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("database: %v", err)
	}
	defer pool.Close()

	st := postgres.New(pool)

	// ── Seed Data ───────────────────────────────────────────────────────
	organizationID := uuid.New().String()
	_, err = pool.Exec(ctx, "INSERT INTO organizations (id, name) VALUES ($1, $2)", organizationID, "Inbox Test Organization "+runID)
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}

	userID := "inbox-user-" + runID
	_, err = pool.Exec(ctx,
		"INSERT INTO users (id, organization_id, external_id) VALUES ($1, $2, $3)",
		userID, organizationID, userID,
	)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Create a subscription category
	categoryID := uuid.New().String()
	_, err = pool.Exec(ctx,
		"INSERT INTO subscription_categories (id, slug, name, default_channels) VALUES ($1, $2, $3, $4)",
		categoryID, "inbox-cat-"+runID, "Inbox Category", []string{"inbox"},
	)
	if err != nil {
		t.Fatalf("create category: %v", err)
	}

	// Create 3 notifications with status "delivered"
	notifIDs := make([]string, 3)
	for i := 0; i < 3; i++ {
		n := &models.Notification{
			ID:             uuid.New().String(),
			OrganizationID: organizationID,
			UserID:         userID,
			CategoryID:     categoryID,
			Title:          fmt.Sprintf("Notification %d (%s)", i+1, runID),
			Body:           fmt.Sprintf("Body %d", i+1),
			Channels:       []string{"inbox"},
			Status:         models.StatusDelivered,
		}
		if _, err := st.CreateNotification(ctx, n); err != nil {
			t.Fatalf("create notification %d: %v", i, err)
		}
		notifIDs[i] = n.ID
		// Small sleep to ensure distinct created_at ordering
		time.Sleep(10 * time.Millisecond)
	}

	// ── Inbox Service ───────────────────────────────────────────────────
	keyProvider := auth.JWTKeyProvider(func() []auth.JWTSigningConfig {
		return []auth.JWTSigningConfig{
			{Name: "test", Secret: []byte(testJWTSecret), Algorithm: "HS256", UserIDClaim: "sub", OrganizationIDClaim: "organization_id"},
		}
	})
	srv := inbox.NewServer(st, nil, nil, keyProvider, logger)
	handler := srv.Handler()

	jwtToken := makeJWT(t, userID, organizationID)

	doRequest := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer "+jwtToken)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	type listResponse struct {
		Data        []map[string]any `json:"data"`
		UnreadCount int              `json:"unread_count"`
		Cursor      string           `json:"cursor,omitempty"`
	}

	parseList := func(rec *httptest.ResponseRecorder) listResponse {
		t.Helper()
		var resp listResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode list response: %v", err)
		}
		return resp
	}

	// ── (a) List inbox — 3 notifications, unread_count=3 ───────────────
	t.Run("list_initial", func(t *testing.T) {
		rec := doRequest("GET", "/v1/inbox")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		resp := parseList(rec)
		if len(resp.Data) != 3 {
			t.Fatalf("expected 3 notifications, got %d", len(resp.Data))
		}
		if resp.UnreadCount != 3 {
			t.Fatalf("expected unread_count=3, got %d", resp.UnreadCount)
		}
	})

	// ── (b) Mark read — verify success ─────────────────────────────────
	t.Run("mark_read", func(t *testing.T) {
		rec := doRequest("PUT", "/v1/inbox/"+notifIDs[0]+"/read")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	// ── (c) List again — unread_count=2 ────────────────────────────────
	t.Run("list_after_read", func(t *testing.T) {
		rec := doRequest("GET", "/v1/inbox")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		resp := parseList(rec)
		if len(resp.Data) != 3 {
			t.Fatalf("expected 3 notifications, got %d", len(resp.Data))
		}
		if resp.UnreadCount != 2 {
			t.Fatalf("expected unread_count=2, got %d", resp.UnreadCount)
		}
	})

	// ── (d) Archive — verify success ───────────────────────────────────
	t.Run("archive", func(t *testing.T) {
		rec := doRequest("PUT", "/v1/inbox/"+notifIDs[1]+"/archive")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	// ── (e) List default — 2 items (archived excluded) ─────────────────
	t.Run("list_after_archive", func(t *testing.T) {
		rec := doRequest("GET", "/v1/inbox")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		resp := parseList(rec)
		if len(resp.Data) != 2 {
			t.Fatalf("expected 2 notifications (archived excluded), got %d", len(resp.Data))
		}
	})

	// ── (f) List archived — 1 item ────────────────────────────────────
	t.Run("list_archived", func(t *testing.T) {
		rec := doRequest("GET", "/v1/inbox?archived=true")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		resp := parseList(rec)
		if len(resp.Data) != 1 {
			t.Fatalf("expected 1 archived notification, got %d", len(resp.Data))
		}
	})

	// ── (g) Unarchive — back to 3 in default list ─────────────────────
	t.Run("unarchive", func(t *testing.T) {
		rec := doRequest("DELETE", "/v1/inbox/"+notifIDs[1]+"/archive")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		rec = doRequest("GET", "/v1/inbox")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		resp := parseList(rec)
		if len(resp.Data) != 3 {
			t.Fatalf("expected 3 notifications after unarchive, got %d", len(resp.Data))
		}
	})

	// ── (h) Mark all read — unread_count=0 ─────────────────────────────
	t.Run("mark_all_read", func(t *testing.T) {
		rec := doRequest("PUT", "/v1/inbox/read-all")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		rec = doRequest("GET", "/v1/inbox")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		resp := parseList(rec)
		if resp.UnreadCount != 0 {
			t.Fatalf("expected unread_count=0, got %d", resp.UnreadCount)
		}
	})

	// ── (i) Pagination — create more notifications, test cursor ────────
	t.Run("pagination", func(t *testing.T) {
		// Create 5 more notifications (total 8 non-archived)
		for i := 0; i < 5; i++ {
			n := &models.Notification{
				ID:             uuid.New().String(),
				OrganizationID: organizationID,
				UserID:         userID,
				CategoryID:     categoryID,
				Title:          fmt.Sprintf("Paginated %d (%s)", i+1, runID),
				Body:           fmt.Sprintf("Page body %d", i+1),
				Channels:       []string{"inbox"},
				Status:         models.StatusDelivered,
			}
			if _, err := st.CreateNotification(ctx, n); err != nil {
				t.Fatalf("create paginated notification %d: %v", i, err)
			}
			time.Sleep(10 * time.Millisecond)
		}

		// Fetch first page with limit=3
		rec := doRequest("GET", "/v1/inbox?limit=3")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		resp := parseList(rec)
		if len(resp.Data) != 3 {
			t.Fatalf("expected 3 on first page, got %d", len(resp.Data))
		}
		if resp.Cursor == "" {
			t.Fatal("expected cursor for next page, got empty")
		}

		// Fetch second page
		rec = doRequest("GET", "/v1/inbox?limit=3&cursor="+resp.Cursor)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		resp2 := parseList(rec)
		if len(resp2.Data) != 3 {
			t.Fatalf("expected 3 on second page, got %d", len(resp2.Data))
		}

		// Ensure no overlap between pages
		page1IDs := make(map[string]bool)
		for _, n := range resp.Data {
			page1IDs[n["id"].(string)] = true
		}
		for _, n := range resp2.Data {
			if page1IDs[n["id"].(string)] {
				t.Fatal("found duplicate notification across pages")
			}
		}

		// Fetch third page (should have 2 remaining)
		if resp2.Cursor == "" {
			t.Fatal("expected cursor for third page, got empty")
		}
		rec = doRequest("GET", "/v1/inbox?limit=3&cursor="+resp2.Cursor)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		resp3 := parseList(rec)
		if len(resp3.Data) != 2 {
			t.Fatalf("expected 2 on third page, got %d", len(resp3.Data))
		}
		// No more pages
		if resp3.Cursor != "" {
			t.Fatalf("expected empty cursor on last page, got %q", resp3.Cursor)
		}
	})

	t.Log("Inbox integration test passed")
}
