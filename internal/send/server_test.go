// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

package send_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/hermes-notifications/hermes/internal/auth"
	"github.com/hermes-notifications/hermes/internal/models"
	"github.com/hermes-notifications/hermes/internal/send"
)

const testHMACSecret = "test-hmac-secret"

// authenticatedServer builds a server on the REAL authentication path — no SetSkipAuth —
// holding one API key with exactly the permissions given. It returns the raw key to
// present as a bearer token.
//
// Going through the real path matters here: the defect in finding 3 was that permissions
// were never checked on any route, and a test that injects its own context would not
// exercise the middleware chain where that check has to happen.
func authenticatedServer(t *testing.T, permissions ...string) (http.Handler, string) {
	t.Helper()

	raw, keyID, err := auth.GenerateAPIKey("")
	if err != nil {
		t.Fatalf("generate api key: %v", err)
	}
	_, secret, err := auth.ParseAPIKey(raw)
	if err != nil {
		t.Fatalf("parse api key: %v", err)
	}

	store := &mockStore{apiKeys: []models.APIKey{{
		ID:          keyID,
		KeyHash:     auth.HMACHashAPIKey(secret, testHMACSecret),
		Permissions: permissions,
	}}}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	srv := send.NewServer(store, &mockPublisher{}, nil, nil, testHMACSecret, logger)
	return srv.Handler(), raw
}

func postSend(t *testing.T, handler http.Handler, bearer string) int {
	t.Helper()
	// A schema-valid body. Huma validates input before invoking the handler, so an
	// invalid one would 422 before any permission check ran and the test would pass or
	// fail for the wrong reason.
	body := `{"to":{"organization_id":"org_1","user_id":"usr_1"},` +
		`"content":{"title":"t","body":"b"},"channels":["inbox"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Code
}

// Finding 3, and the case that mattered most: /v1/send is the platform's primary write
// path, and before this change ANY valid API key could use it regardless of its
// permissions. A key issued narrowly for template management could forge notifications.
func TestHandler_RejectsSendWithoutTheSendPermission(t *testing.T) {
	handler, key := authenticatedServer(t, auth.PermTemplatesManage)

	if code := postSend(t, handler, key); code != http.StatusForbidden {
		t.Fatalf("expected 403 for a key without %s, got %d", auth.PermNotificationsSend, code)
	}
}

func TestHandler_AllowsSendWithTheSendPermission(t *testing.T) {
	handler, key := authenticatedServer(t, auth.PermNotificationsSend)

	// Asserting "not 403" rather than a specific success code: this test is about
	// authorization, and pinning the success status here would make it fail for reasons
	// that have nothing to do with permissions.
	if code := postSend(t, handler, key); code == http.StatusForbidden {
		t.Fatalf("a key holding %s was refused", auth.PermNotificationsSend)
	}
}

func TestHandler_RejectsAnUnauthenticatedSend(t *testing.T) {
	handler, _ := authenticatedServer(t, auth.PermNotificationsSend)

	if code := postSend(t, handler, ""); code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with no bearer token, got %d", code)
	}
}

// Skipping authentication must not skip authorization: with SetSkipAuth the handler runs
// on the same code path as production, holding a fully privileged synthetic key. If this
// regressed, every test in this package would start passing for the wrong reason.
func TestHandler_SkipAuthGrantsFullPermissionsRatherThanBypassingChecks(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	srv := send.NewServer(&mockStore{}, &mockPublisher{}, nil, nil, testHMACSecret, logger)
	srv.SetSkipAuth(true)

	if code := postSend(t, srv.Handler(), ""); code == http.StatusForbidden || code == http.StatusUnauthorized {
		t.Fatalf("skip-auth should behave as a fully privileged caller, got %d", code)
	}
}
