// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package userservice_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleGetPreferenceCenter(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/users/me/preferences", nil)
	req = requestWithUser(req, testUserID)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Categories []struct {
			ID            string `json:"id"`
			Slug          string `json:"slug"`
			DefaultState  string `json:"default_state"`
			Subscriptions []struct {
				ID      string `json:"id"`
				Slug    string `json:"slug"`
				OptedIn bool   `json:"opted_in"`
			} `json:"subscriptions"`
		} `json:"categories"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Categories) != 2 {
		t.Fatalf("expected 2 categories, got %d", len(resp.Categories))
	}

	// General category: default_state=on, user has no explicit pref -> opted_in=true
	general := resp.Categories[0]
	if general.Slug != "general" {
		t.Fatalf("expected general category first, got %q", general.Slug)
	}
	if len(general.Subscriptions) != 1 {
		t.Fatalf("expected 1 subscription in general, got %d", len(general.Subscriptions))
	}
	if !general.Subscriptions[0].OptedIn {
		t.Fatal("expected general subscription to be opted in by default")
	}

	// Marketing category: default_state=off, user has explicit opted_in=true
	marketing := resp.Categories[1]
	if marketing.Slug != "marketing" {
		t.Fatalf("expected marketing category second, got %q", marketing.Slug)
	}
	if len(marketing.Subscriptions) != 1 {
		t.Fatalf("expected 1 subscription in marketing, got %d", len(marketing.Subscriptions))
	}
	if !marketing.Subscriptions[0].OptedIn {
		t.Fatal("expected marketing subscription to be opted in (explicit user pref)")
	}
}

func TestHandleGetPreferenceCenter_NoUser(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/users/me/preferences", nil)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSetPreference(t *testing.T) {
	srv, store := newTestServer(t)

	body := `{"opted_in":false}`
	req := httptest.NewRequest(http.MethodPut, "/v1/users/me/preferences/sub-2", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = requestWithUser(req, testUserID)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("expected status ok, got %q", resp.Status)
	}

	// Verify store was updated
	if store.userSubscriptions[0].OptedIn != false {
		t.Fatal("expected user subscription to be opted out")
	}
}

func TestHandleSetPreference_NewSubscription(t *testing.T) {
	srv, store := newTestServer(t)

	body := `{"opted_in":true}`
	req := httptest.NewRequest(http.MethodPut, "/v1/users/me/preferences/sub-1", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req = requestWithUser(req, testUserID)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if len(store.userSubscriptions) != 2 {
		t.Fatalf("expected 2 user subscriptions, got %d", len(store.userSubscriptions))
	}
}

func TestHandleDeletePreference(t *testing.T) {
	srv, store := newTestServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/v1/users/me/preferences/sub-2", nil)
	req = requestWithUser(req, testUserID)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if len(store.userSubscriptions) != 0 {
		t.Fatalf("expected 0 user subscriptions after delete, got %d", len(store.userSubscriptions))
	}
}

func TestHandleDeletePreference_NotFound(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/v1/users/me/preferences/nonexistent", nil)
	req = requestWithUser(req, testUserID)
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}
