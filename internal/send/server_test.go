// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

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

// scopedServer builds a server on the real auth path holding one key bound to
// organizationID. An empty organizationID models a key predating migration 000018.
func scopedServer(t *testing.T, organizationID string) (http.Handler, string) {
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
		ID:             keyID,
		KeyHash:        auth.HMACHashAPIKey(secret, testHMACSecret),
		OrganizationID: organizationID,
		Permissions:    []string{auth.PermNotificationsSend},
	}}}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	srv := send.NewServer(store, &mockPublisher{}, nil, nil, testHMACSecret, logger)
	return srv.Handler(), raw
}

// postSendFor issues a send addressed at organizationID.
func postSendFor(t *testing.T, handler http.Handler, bearer, organizationID string) int {
	t.Helper()
	body := `{"to":{"organization_id":"` + organizationID + `","user_id":"usr_1"},` +
		`"content":{"title":"t","body":"b"},"channels":["inbox"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/send", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Code
}

// ADR 0011, and the reason it exists: the organization arrives in the request
// body, and the permission check gates whether you may send, never for whom. A
// key belonging to one customer could therefore deliver notifications into
// another customer's users' inboxes given only an organization ID.
func TestHandler_KeyCannotSendForAnotherOrganization(t *testing.T) {
	handler, key := scopedServer(t, "org_mine")

	if code := postSendFor(t, handler, key, "org_mine"); code == http.StatusForbidden {
		t.Fatalf("the key's own organization was refused, got %d", code)
	}
	if code := postSendFor(t, handler, key, "org_someone_else"); code != http.StatusForbidden {
		t.Errorf("expected 403 sending for another organization, got %d — "+
			"a key can still address any organization", code)
	}
}

// Keys predating migration 000018 carry no organization. There is nothing to infer
// one from, so they keep working rather than breaking silently; the metric is what
// tells an operator they are still out there.
func TestHandler_KeyWithoutAnOrganizationIsUnconstrained(t *testing.T) {
	handler, key := scopedServer(t, "")

	for _, org := range []string{"org_a", "org_b"} {
		if code := postSendFor(t, handler, key, org); code == http.StatusForbidden {
			t.Errorf("legacy unscoped key refused for %s; it should keep working", org)
		}
	}
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
