// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

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

func TestListTenants(t *testing.T) {
	store := &mockStore{
		tenants: []models.Tenant{
			{ID: "tenant-1", Name: "Acme Corp", DefaultLocale: "en", CreatedAt: time.Now()},
			{ID: "tenant-2", Name: "Globex", DefaultLocale: "fr", CreatedAt: time.Now()},
		},
		users: []models.User{
			{ID: "usr-1", TenantID: "tenant-1", ExternalID: "ext-1", CreatedAt: time.Now()},
			{ID: "usr-2", TenantID: "tenant-1", ExternalID: "ext-2", CreatedAt: time.Now()},
			{ID: "usr-3", TenantID: "tenant-2", ExternalID: "ext-3", CreatedAt: time.Now()},
		},
	}
	srv := newTestServerWithStore(t, store)

	req := httptest.NewRequest("GET", "/v1/tenants", nil)
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
		t.Fatalf("expected 2 tenants, got %d", len(resp))
	}

	// Find tenant-1 and check user count
	for _, tenant := range resp {
		switch tenant.ID {
		case "tenant-1":
			if tenant.UserCount != 2 {
				t.Errorf("expected tenant-1 user_count=2, got %d", tenant.UserCount)
			}
			if tenant.Name != "Acme Corp" {
				t.Errorf("expected name Acme Corp, got %s", tenant.Name)
			}
		case "tenant-2":
			if tenant.UserCount != 1 {
				t.Errorf("expected tenant-2 user_count=1, got %d", tenant.UserCount)
			}
		}
	}
}

func TestListTenants_Empty(t *testing.T) {
	srv := newTestServerWithStore(t, &mockStore{})

	req := httptest.NewRequest("GET", "/v1/tenants", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateTenant(t *testing.T) {
	srv := newTestServerWithStore(t, &mockStore{})

	body := bytes.NewBufferString(`{"name":"New Tenant"}`)
	req := httptest.NewRequest("POST", "/v1/tenants", body)
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
	if resp.Name != "New Tenant" {
		t.Errorf("expected name %q, got %q", "New Tenant", resp.Name)
	}
}

func TestCreateTenant_MissingName(t *testing.T) {
	srv := newTestServerWithStore(t, &mockStore{})

	body := bytes.NewBufferString(`{}`)
	req := httptest.NewRequest("POST", "/v1/tenants", body)
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

func TestCreateTenant_StoreError(t *testing.T) {
	store := &mockStore{
		errors: map[string]error{
			"CreateTenant": fmt.Errorf("db connection failed"),
		},
	}
	srv := newTestServerWithStore(t, store)

	body := bytes.NewBufferString(`{"name":"New Tenant"}`)
	req := httptest.NewRequest("POST", "/v1/tenants", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}
