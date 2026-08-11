// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package admin_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/internal/models"
)

func TestListUsers(t *testing.T) {
	store := &mockStore{
		organizations: []models.Organization{
			{ID: "organization-1", Name: "Acme Corp", CreatedAt: time.Now()},
			{ID: "organization-2", Name: "Globex", CreatedAt: time.Now()},
		},
		users: []models.User{
			{ID: "usr-1", OrganizationID: "organization-1", ExternalID: "ext-1", CreatedAt: time.Now()},
			{ID: "usr-2", OrganizationID: "organization-2", ExternalID: "ext-2", CreatedAt: time.Now()},
		},
	}
	srv := newTestServerWithStore(t, store)

	req := httptest.NewRequest("GET", "/v1/users", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp []struct {
		ID               string `json:"id"`
		OrganizationID   string `json:"organization_id"`
		OrganizationName string `json:"organization_name"`
		ExternalID       string `json:"external_id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp) != 2 {
		t.Fatalf("expected 2 users, got %d", len(resp))
	}

	// Check organization name resolution
	for _, u := range resp {
		switch u.ID {
		case "usr-1":
			if u.OrganizationName != "Acme Corp" {
				t.Errorf("expected organization_name Acme Corp, got %s", u.OrganizationName)
			}
		case "usr-2":
			if u.OrganizationName != "Globex" {
				t.Errorf("expected organization_name Globex, got %s", u.OrganizationName)
			}
		}
	}
}

func TestListUsers_FilterByOrganization(t *testing.T) {
	store := &mockStore{
		organizations: []models.Organization{
			{ID: "organization-1", Name: "Acme Corp", CreatedAt: time.Now()},
			{ID: "organization-2", Name: "Globex", CreatedAt: time.Now()},
		},
		users: []models.User{
			{ID: "usr-1", OrganizationID: "organization-1", ExternalID: "ext-1", CreatedAt: time.Now()},
			{ID: "usr-2", OrganizationID: "organization-1", ExternalID: "ext-2", CreatedAt: time.Now()},
			{ID: "usr-3", OrganizationID: "organization-2", ExternalID: "ext-3", CreatedAt: time.Now()},
		},
	}
	srv := newTestServerWithStore(t, store)

	req := httptest.NewRequest("GET", "/v1/users?organization_id=organization-1", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp []struct {
		ID             string `json:"id"`
		OrganizationID string `json:"organization_id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp) != 2 {
		t.Fatalf("expected 2 users for organization-1, got %d", len(resp))
	}
	for _, u := range resp {
		if u.OrganizationID != "organization-1" {
			t.Errorf("expected organization_id organization-1, got %s", u.OrganizationID)
		}
	}
}

func TestListUsers_Empty(t *testing.T) {
	srv := newTestServerWithStore(t, &mockStore{})

	req := httptest.NewRequest("GET", "/v1/users", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
