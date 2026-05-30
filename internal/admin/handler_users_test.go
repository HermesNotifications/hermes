// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

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
		tenants: []models.Tenant{
			{ID: "tenant-1", Name: "Acme Corp", CreatedAt: time.Now()},
			{ID: "tenant-2", Name: "Globex", CreatedAt: time.Now()},
		},
		users: []models.User{
			{ID: "usr-1", TenantID: "tenant-1", ExternalID: "ext-1", CreatedAt: time.Now()},
			{ID: "usr-2", TenantID: "tenant-2", ExternalID: "ext-2", CreatedAt: time.Now()},
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
		ID         string `json:"id"`
		TenantID   string `json:"tenant_id"`
		TenantName string `json:"tenant_name"`
		ExternalID string `json:"external_id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp) != 2 {
		t.Fatalf("expected 2 users, got %d", len(resp))
	}

	// Check tenant name resolution
	for _, u := range resp {
		switch u.ID {
		case "usr-1":
			if u.TenantName != "Acme Corp" {
				t.Errorf("expected tenant_name Acme Corp, got %s", u.TenantName)
			}
		case "usr-2":
			if u.TenantName != "Globex" {
				t.Errorf("expected tenant_name Globex, got %s", u.TenantName)
			}
		}
	}
}

func TestListUsers_FilterByTenant(t *testing.T) {
	store := &mockStore{
		tenants: []models.Tenant{
			{ID: "tenant-1", Name: "Acme Corp", CreatedAt: time.Now()},
			{ID: "tenant-2", Name: "Globex", CreatedAt: time.Now()},
		},
		users: []models.User{
			{ID: "usr-1", TenantID: "tenant-1", ExternalID: "ext-1", CreatedAt: time.Now()},
			{ID: "usr-2", TenantID: "tenant-1", ExternalID: "ext-2", CreatedAt: time.Now()},
			{ID: "usr-3", TenantID: "tenant-2", ExternalID: "ext-3", CreatedAt: time.Now()},
		},
	}
	srv := newTestServerWithStore(t, store)

	req := httptest.NewRequest("GET", "/v1/users?tenant_id=tenant-1", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp []struct {
		ID       string `json:"id"`
		TenantID string `json:"tenant_id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp) != 2 {
		t.Fatalf("expected 2 users for tenant-1, got %d", len(resp))
	}
	for _, u := range resp {
		if u.TenantID != "tenant-1" {
			t.Errorf("expected tenant_id tenant-1, got %s", u.TenantID)
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
