// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

package admin_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hermesnotifications/hermes/internal/models"
)

func TestHandleCreateTemplate(t *testing.T) {
	srv := newTestServer(t)

	// Create template
	body := `{"slug":"invoice.paid","name":"Invoice Paid","content":{"email":{"subject":"Invoice paid"}}}`
	req := httptest.NewRequest("POST", "/v1/templates", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var tmpl map[string]any
	json.NewDecoder(rec.Body).Decode(&tmpl)
	if tmpl["slug"] != "invoice.paid" {
		t.Errorf("expected slug invoice.paid, got %v", tmpl["slug"])
	}
}

func TestHandleCreateTemplate_MissingFields(t *testing.T) {
	srv := newTestServer(t)
	body := `{"slug":"invoice.paid"}`
	req := httptest.NewRequest("POST", "/v1/templates", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", rec.Code)
	}
}

func TestHandleCreateTemplate_EmptySlug(t *testing.T) {
	srv := newTestServer(t)
	body := `{"slug":"","name":"Test"}`
	req := httptest.NewRequest("POST", "/v1/templates", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleListTemplates(t *testing.T) {
	store := &mockStore{
		templates: []models.NotificationTemplate{
			{ID: "ntpl-1", Slug: "welcome", Name: "Welcome"},
			{ID: "ntpl-2", Slug: "invoice", Name: "Invoice"},
		},
	}
	srv := newTestServerWithStore(t, store)

	req := httptest.NewRequest(http.MethodGet, "/v1/templates", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var templates []models.NotificationTemplate
	json.NewDecoder(rec.Body).Decode(&templates)
	if len(templates) != 2 {
		t.Fatalf("expected 2 templates, got %d", len(templates))
	}
}

func TestHandleListTemplates_Empty(t *testing.T) {
	store := &mockStore{}
	srv := newTestServerWithStore(t, store)

	req := httptest.NewRequest(http.MethodGet, "/v1/templates", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleUpdateTemplate(t *testing.T) {
	srv := newTestServer(t)

	// Create
	body := `{"slug":"welcome","name":"Welcome","content":{"email":{"subject":"Hello"}}}`
	req := httptest.NewRequest("POST", "/v1/templates", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var tmpl map[string]any
	json.NewDecoder(rec.Body).Decode(&tmpl)
	templateID := tmpl["id"].(string)

	// Update
	updateBody := `{"name":"Welcome Updated","content":{"email":{"subject":"Hello Updated"}}}`
	req = httptest.NewRequest("PUT", "/v1/templates/"+templateID, bytes.NewBufferString(updateBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var updated map[string]any
	json.NewDecoder(rec.Body).Decode(&updated)
	if updated["name"] != "Welcome Updated" {
		t.Errorf("expected name 'Welcome Updated', got %v", updated["name"])
	}
}

func TestHandleUpdateTemplate_NotFound(t *testing.T) {
	srv := newTestServer(t)

	body := `{"name":"Nope"}`
	req := httptest.NewRequest("PUT", "/v1/templates/ntpl-nonexistent", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleDeleteTemplate_NotFound(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest("DELETE", "/v1/templates/ntpl-nonexistent", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	// GetTemplateByID fails first (for cache invalidation), returning 500
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleDeleteTemplate(t *testing.T) {
	srv := newTestServer(t)

	// Create template
	body := `{"slug":"invoice.paid","name":"Invoice Paid"}`
	req := httptest.NewRequest("POST", "/v1/templates", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create template: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var tmpl map[string]any
	json.NewDecoder(rec.Body).Decode(&tmpl)
	templateID := tmpl["id"].(string)

	// Delete
	req = httptest.NewRequest("DELETE", "/v1/templates/"+templateID, nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleCreateTemplate_StoreError(t *testing.T) {
	store := &mockStore{
		errors: map[string]error{"CreateTemplate": fmt.Errorf("database connection refused")},
	}
	srv := newTestServerWithStore(t, store)

	body := `{"slug":"welcome","name":"Welcome"}`
	req := httptest.NewRequest("POST", "/v1/templates", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleListTemplates_StoreError(t *testing.T) {
	store := &mockStore{
		errors: map[string]error{"ListTemplates": fmt.Errorf("database connection refused")},
	}
	srv := newTestServerWithStore(t, store)

	req := httptest.NewRequest(http.MethodGet, "/v1/templates", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleDeleteTemplate_StoreError(t *testing.T) {
	store := &mockStore{
		templates: []models.NotificationTemplate{
			{ID: "ntpl-1", Slug: "welcome", Name: "Welcome"},
		},
		errors: map[string]error{"GetTemplateByID": fmt.Errorf("database connection refused")},
	}
	srv := newTestServerWithStore(t, store)

	req := httptest.NewRequest("DELETE", "/v1/templates/ntpl-1", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}
