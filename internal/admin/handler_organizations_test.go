// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package admin_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hermes-notifications/hermes/internal/models"
)

func TestListOrganizations(t *testing.T) {
	store := &mockStore{
		organizations: []models.Organization{
			{ID: "organization-1", Name: "Acme Corp", DefaultLocale: "en", CreatedAt: time.Now()},
			{ID: "organization-2", Name: "Globex", DefaultLocale: "fr", CreatedAt: time.Now()},
		},
		users: []models.User{
			{ID: "usr-1", OrganizationID: "organization-1", ExternalID: "ext-1", CreatedAt: time.Now()},
			{ID: "usr-2", OrganizationID: "organization-1", ExternalID: "ext-2", CreatedAt: time.Now()},
			{ID: "usr-3", OrganizationID: "organization-2", ExternalID: "ext-3", CreatedAt: time.Now()},
		},
	}
	srv := newTestServerWithStore(t, store)

	req := httptest.NewRequest("GET", "/v1/organizations", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp []struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		DefaultLocale string `json:"default_locale"`
		UserCount     int    `json:"user_count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp) != 2 {
		t.Fatalf("expected 2 organizations, got %d", len(resp))
	}

	// Find organization-1 and check user count
	for _, organization := range resp {
		switch organization.ID {
		case "organization-1":
			if organization.UserCount != 2 {
				t.Errorf("expected organization-1 user_count=2, got %d", organization.UserCount)
			}
			if organization.Name != "Acme Corp" {
				t.Errorf("expected name Acme Corp, got %s", organization.Name)
			}
		case "organization-2":
			if organization.UserCount != 1 {
				t.Errorf("expected organization-2 user_count=1, got %d", organization.UserCount)
			}
		}
	}
}

func TestListOrganizations_Empty(t *testing.T) {
	srv := newTestServerWithStore(t, &mockStore{})

	req := httptest.NewRequest("GET", "/v1/organizations", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateOrganization(t *testing.T) {
	srv := newTestServerWithStore(t, &mockStore{})

	body := bytes.NewBufferString(`{"name":"New Organization"}`)
	req := httptest.NewRequest("POST", "/v1/organizations", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.ID == "" {
		t.Error("expected non-empty id in response")
	}
	if resp.Name != "New Organization" {
		t.Errorf("expected name %q, got %q", "New Organization", resp.Name)
	}
}

func TestCreateOrganization_MissingName(t *testing.T) {
	srv := newTestServerWithStore(t, &mockStore{})

	body := bytes.NewBufferString(`{}`)
	req := httptest.NewRequest("POST", "/v1/organizations", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code < 400 || rec.Code >= 500 {
		t.Fatalf("expected 4xx status for missing name, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify the response body indicates a validation error
	if !strings.Contains(rec.Body.String(), "name") {
		t.Errorf("expected error message to mention 'name', got: %s", rec.Body.String())
	}
}

func TestCreateOrganization_StoreError(t *testing.T) {
	store := &mockStore{
		errors: map[string]error{
			"CreateOrganization": fmt.Errorf("db connection failed"),
		},
	}
	srv := newTestServerWithStore(t, store)

	body := bytes.NewBufferString(`{"name":"New Organization"}`)
	req := httptest.NewRequest("POST", "/v1/organizations", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}
