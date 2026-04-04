package admin_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hermes-notifications/hermes/internal/models"
)

func TestHandleCreateCategory(t *testing.T) {
	srv := newTestServer(t)

	body := `{"slug":"billing","name":"Billing","default_channels":["email","inbox"],"default_state":"on","sort_order":0}`
	req := httptest.NewRequest(http.MethodPost, "/v1/subscriptions/categories", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var cat models.SubscriptionCategory
	json.NewDecoder(rec.Body).Decode(&cat)

	if cat.Slug != "billing" {
		t.Errorf("expected slug 'billing', got %q", cat.Slug)
	}
	if cat.Name != "Billing" {
		t.Errorf("expected name 'Billing', got %q", cat.Name)
	}
	if cat.DefaultState != "on" {
		t.Errorf("expected default_state 'on', got %q", cat.DefaultState)
	}
	if cat.ID == "" {
		t.Error("expected ID to be set")
	}
}

func TestHandleCreateCategory_MissingFields(t *testing.T) {
	srv := newTestServer(t)

	body := `{"slug":"billing"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/subscriptions/categories", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleCreateCategory_InvalidDefaultState(t *testing.T) {
	srv := newTestServer(t)

	body := `{"slug":"billing","name":"Billing","default_state":"maybe"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/subscriptions/categories", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleListCategories(t *testing.T) {
	store := &mockStore{
		categories: []models.SubscriptionCategory{
			{ID: "sct-1", Slug: "billing", Name: "Billing", DefaultState: "on"},
			{ID: "sct-2", Slug: "product", Name: "Product", DefaultState: "off"},
		},
	}
	srv := newTestServerWithStore(t, store)

	req := httptest.NewRequest(http.MethodGet, "/v1/subscriptions/categories", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var cats []models.SubscriptionCategory
	json.NewDecoder(rec.Body).Decode(&cats)
	if len(cats) != 2 {
		t.Fatalf("expected 2 categories, got %d", len(cats))
	}
}

func TestHandleListCategories_Empty(t *testing.T) {
	store := &mockStore{}
	srv := newTestServerWithStore(t, store)

	req := httptest.NewRequest(http.MethodGet, "/v1/subscriptions/categories", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleUpdateCategory(t *testing.T) {
	srv := newTestServer(t)

	// Create
	body := `{"slug":"billing","name":"Billing","default_state":"on"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/subscriptions/categories", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var cat models.SubscriptionCategory
	json.NewDecoder(rec.Body).Decode(&cat)

	// Update
	updateBody := `{"name":"Billing Updated","default_channels":["email"],"default_state":"off","sort_order":1}`
	req = httptest.NewRequest(http.MethodPut, "/v1/subscriptions/categories/"+cat.ID, bytes.NewBufferString(updateBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var updated models.SubscriptionCategory
	json.NewDecoder(rec.Body).Decode(&updated)
	if updated.Name != "Billing Updated" {
		t.Errorf("expected name 'Billing Updated', got %q", updated.Name)
	}
	if updated.DefaultState != "off" {
		t.Errorf("expected default_state 'off', got %q", updated.DefaultState)
	}
}

func TestHandleUpdateCategory_NotFound(t *testing.T) {
	srv := newTestServer(t)

	body := `{"name":"Nope","default_channels":[],"default_state":"on","sort_order":0}`
	req := httptest.NewRequest(http.MethodPut, "/v1/subscriptions/categories/sct-nonexistent", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleDeleteCategory(t *testing.T) {
	srv := newTestServer(t)

	// Create
	body := `{"slug":"billing","name":"Billing","default_state":"on"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/subscriptions/categories", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var cat models.SubscriptionCategory
	json.NewDecoder(rec.Body).Decode(&cat)

	// Delete
	req = httptest.NewRequest(http.MethodDelete, "/v1/subscriptions/categories/"+cat.ID, nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleDeleteCategory_NotFound(t *testing.T) {
	srv := newTestServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/v1/subscriptions/categories/sct-nonexistent", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleCreateCategory_StoreError(t *testing.T) {
	store := &mockStore{
		errors: map[string]error{"CreateCategory": fmt.Errorf("database connection refused")},
	}
	srv := newTestServerWithStore(t, store)

	body := `{"slug":"billing","name":"Billing","default_state":"on"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/subscriptions/categories", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleListCategories_StoreError(t *testing.T) {
	store := &mockStore{
		errors: map[string]error{"ListCategories": fmt.Errorf("database connection refused")},
	}
	srv := newTestServerWithStore(t, store)

	req := httptest.NewRequest(http.MethodGet, "/v1/subscriptions/categories", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}
}
